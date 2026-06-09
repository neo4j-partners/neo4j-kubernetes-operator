/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	neo4jclient "github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	// DefaultAdminSecretNamePlugin is the default name for admin credentials secret
	DefaultAdminSecretNamePlugin = "neo4j-admin-secret"

	// PluginInstallModeManaged is the default — operator adds the plugin to
	// NEO4J_PLUGINS so the Neo4j Docker entrypoint resolves and installs the
	// JAR at pod startup.
	PluginInstallModeManaged = "Managed"

	// PluginInstallModePreBaked — operator does not touch NEO4J_PLUGINS. The
	// JAR must already be in /plugins (baked into a custom Neo4j image).
	// Operator still applies plugin configuration (security allowlists,
	// unrestricted procedures, ConfigMap entries).
	PluginInstallModePreBaked = "PreBaked"

	// PluginInstallModeVerifiedDownload — operator injects an init
	// container into the target StatefulSet that downloads spec.source.url,
	// verifies the SHA256/SHA512 against spec.source.checksum, and drops
	// the JAR into the shared /plugins emptyDir before Neo4j starts. As
	// with PreBaked, NEO4J_PLUGINS is NOT mutated — the entrypoint's own
	// download path is bypassed entirely so it can't race the verified
	// JAR. Configuration env vars / ConfigMap entries still flow.
	PluginInstallModeVerifiedDownload = "VerifiedDownload"

	// PluginInitContainersAnnotation tracks the names of init containers
	// the plugin controller owns on a given StatefulSet, so the controller
	// can remove only its own containers on plugin uninstall without
	// disturbing init containers added by other controllers / users.
	// Value format: comma-separated list of container names.
	PluginInitContainersAnnotation = "neo4j.com/plugin-init-containers"
)

// isPreBakedInstallMode reports whether the operator should skip the
// NEO4J_PLUGINS env mutation for this plugin and only apply configuration.
func isPreBakedInstallMode(plugin *neo4jv1beta1.Neo4jPlugin) bool {
	return plugin.Spec.InstallMode == PluginInstallModePreBaked
}

// isVerifiedDownloadInstallMode reports whether the operator should
// inject an init container for this plugin AND skip the NEO4J_PLUGINS
// env mutation (same env-skip semantics as PreBaked, but with a new
// JAR-delivery path).
func isVerifiedDownloadInstallMode(plugin *neo4jv1beta1.Neo4jPlugin) bool {
	return plugin.Spec.InstallMode == PluginInstallModeVerifiedDownload
}

// shouldSkipNeo4jPluginsEnv reports whether either of the
// JAR-already-in-/plugins install modes is active. Both PreBaked and
// VerifiedDownload share the "operator must NOT add this plugin to
// NEO4J_PLUGINS" semantics — wrapping both into one predicate keeps
// the callsite in installPluginViaEnvironment readable.
func shouldSkipNeo4jPluginsEnv(plugin *neo4jv1beta1.Neo4jPlugin) bool {
	return isPreBakedInstallMode(plugin) || isVerifiedDownloadInstallMode(plugin)
}

// getAdminSecretName safely extracts the admin secret name from cluster spec with fallback to default
func getClusterAdminSecretName(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) string {
	if cluster.Spec.Auth != nil && cluster.Spec.Auth.AdminSecret != "" {
		return cluster.Spec.Auth.AdminSecret
	}
	return DefaultAdminSecretNamePlugin
}

// getStandaloneAdminSecretName safely extracts the admin secret name from standalone spec with fallback to default
func getStandaloneAdminSecretName(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) string {
	if standalone.Spec.Auth != nil && standalone.Spec.Auth.AdminSecret != "" {
		return standalone.Spec.Auth.AdminSecret
	}
	return DefaultAdminSecretNamePlugin
}

// Neo4jPluginReconciler reconciles a Neo4jPlugin object
type Neo4jPluginReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Recorder     record.EventRecorder
	RequeueAfter time.Duration

	// PluginInitImage overrides the default init container image
	// (resources.DefaultPluginInitContainerImage) used for
	// VerifiedDownload mode. Wired from the operator's
	// PLUGIN_INIT_CONTAINER_IMAGE env var which the helm chart sets
	// from .Values.pluginInitContainer.image. Empty = use the default
	// hardcoded in the resource builder.
	PluginInitImage string
}

// PluginFinalizer is the finalizer for Neo4j plugin resources
const PluginFinalizer = "neo4j.neo4j.com/plugin-finalizer"

// checkForDuplicatePlugin lists every Neo4jPlugin in the same namespace
// and returns the metadata.name of the CR that should "own" the
// (clusterRef, plugin name) tuple — i.e. the one this controller will
// actually reconcile. The newer duplicate is expected to drop itself
// into Failed state and stop reconciling.
//
// Selection rules:
//   - Candidates with a different clusterRef or a different plugin name
//     are ignored.
//   - Candidates currently being deleted (DeletionTimestamp != nil)
//     are ignored — they're on their way out, no point handing them
//     ownership.
//   - Among the remaining candidates (including the plugin under
//     reconciliation), the oldest CreationTimestamp wins; UID is the
//     tiebreaker when timestamps collide (which is rare but possible
//     in fast test loops).
//
// Returns the empty string if no other live CR contests the tuple —
// the caller treats that the same as "this CR is the winner".
func (r *Neo4jPluginReconciler) checkForDuplicatePlugin(
	ctx context.Context,
	plugin *neo4jv1beta1.Neo4jPlugin,
) (string, error) {
	list := &neo4jv1beta1.Neo4jPluginList{}
	if err := r.List(ctx, list, client.InNamespace(plugin.Namespace)); err != nil {
		return "", fmt.Errorf("failed to list Neo4jPlugin in namespace %q: %w", plugin.Namespace, err)
	}

	winner := plugin
	contested := false
	for i := range list.Items {
		candidate := &list.Items[i]
		if candidate.UID == plugin.UID {
			continue
		}
		if candidate.Spec.ClusterRef != plugin.Spec.ClusterRef {
			continue
		}
		if candidate.Spec.Name != plugin.Spec.Name {
			continue
		}
		if candidate.DeletionTimestamp != nil {
			continue
		}
		contested = true
		// Older CreationTimestamp wins. If timestamps tie, lower UID
		// wins — gives a deterministic ordering across replicas of
		// the operator.
		if candidate.CreationTimestamp.Before(&winner.CreationTimestamp) {
			winner = candidate
		} else if candidate.CreationTimestamp.Equal(&winner.CreationTimestamp) &&
			string(candidate.UID) < string(winner.UID) {
			winner = candidate
		}
	}

	if !contested {
		return "", nil
	}
	return winner.Name, nil
}

//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jplugins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jplugins/finalizers,verbs=update

// Reconcile handles the reconciliation of Neo4jPlugin resources
func (r *Neo4jPluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jPlugin instance
	plugin := &neo4jv1beta1.Neo4jPlugin{}
	if err := r.Get(ctx, req.NamespacedName, plugin); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Neo4jPlugin resource not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Neo4jPlugin")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if plugin.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, plugin)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(plugin, PluginFinalizer) {
		controllerutil.AddFinalizer(plugin, PluginFinalizer)
		if err := r.Update(ctx, plugin); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Refuse to reconcile if another Neo4jPlugin CR already owns this
	// (clusterRef, plugin name) tuple. Two reconcilers racing on the
	// same /plugins directory + NEO4J_PLUGINS env var produces flapping
	// state (one CR adds, the other removes, restart cycle). Validator
	// can't catch this — it's structural-only, no cross-CR visibility.
	// Resolved here at reconcile time with a deterministic "oldest
	// wins" tiebreaker; newer duplicates surface the conflict via
	// status + Event and stop reconciling. When the winner is deleted,
	// the watch on Neo4jPlugin fires for the survivor — at which point
	// the previously-failed duplicate becomes the new winner on its
	// own next reconcile.
	winner, dupErr := r.checkForDuplicatePlugin(ctx, plugin)
	if dupErr != nil {
		logger.Error(dupErr, "Failed to check for duplicate Neo4jPlugin CRs")
		return ctrl.Result{}, dupErr
	}
	if winner != "" && winner != plugin.Name {
		msg := fmt.Sprintf(
			"duplicate Neo4jPlugin: %q (older) already targets cluster %q with plugin name %q; delete one CR before this one can reconcile",
			winner, plugin.Spec.ClusterRef, plugin.Spec.Name,
		)
		r.updatePluginStatus(ctx, plugin, "Failed", msg)
		r.Recorder.Event(plugin, corev1.EventTypeWarning, EventReasonPluginDuplicate, msg)
		return ctrl.Result{}, nil
	}

	// Get target deployment (cluster or standalone)
	deployment, err := r.getTargetDeployment(ctx, plugin)
	if err != nil {
		logger.Error(err, "Failed to get target deployment",
			"clusterRef", plugin.Spec.ClusterRef, "namespace", plugin.Namespace)
		r.updatePluginStatus(
			ctx,
			plugin,
			"Failed",
			fmt.Sprintf(
				"Target deployment not found (clusterRef=%q, namespace=%q): %v",
				plugin.Spec.ClusterRef,
				plugin.Namespace,
				err,
			),
		)
		return ctrl.Result{}, nil // Don't return error - status is set correctly
	}

	// Apply ConfigMap-based configurations first (before checking connectivity)
	// This is critical for security settings that need to be in place before Neo4j starts
	if deployment.Type == "standalone" {
		if err := r.updateStandaloneConfigMapForPlugin(ctx, plugin, deployment); err != nil {
			logger.Error(err, "Failed to update ConfigMap for standalone deployment", "plugin", plugin.Spec.Name)
			r.updatePluginStatus(ctx, plugin, "Failed", fmt.Sprintf("Failed to update ConfigMap: %v", err))
			return ctrl.Result{}, nil
		}
		logger.Info("Successfully updated ConfigMap for standalone deployment", "plugin", plugin.Spec.Name)
	}

	// Check if deployment is actually functional, not just status reporting
	if !r.isDeploymentFunctional(ctx, deployment) {
		logger.Info("Target deployment not functional, requeuing", "type", deployment.Type, "name", deployment.Name)
		r.updatePluginStatus(ctx, plugin, "Waiting", fmt.Sprintf("Waiting for %s %s to be functional", deployment.Type, deployment.Name))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Update status to "Installing"
	r.updatePluginStatus(ctx, plugin, "Installing", "Installing plugin")

	// Install plugin using NEO4J_PLUGINS environment variable (recommended by Neo4j docs)
	if err := r.installPluginViaEnvironment(ctx, plugin, deployment); err != nil {
		logger.Error(err, "Failed to install plugin")
		r.updatePluginStatus(ctx, plugin, "Failed", fmt.Sprintf("Plugin installation failed: %v", err))
		r.Recorder.Eventf(plugin, corev1.EventTypeWarning, EventReasonPluginInstallFailed,
			"Plugin %s installation failed: %v", plugin.Spec.Name, err)
		return ctrl.Result{}, nil // Don't return error - status is set correctly
	}

	// Check if deployment is ready after restart (non-blocking)
	if !r.arePodsReady(ctx, deployment) {
		logger.Info("Waiting for pods to be ready after plugin installation")
		r.updatePluginStatus(ctx, plugin, "Installing", "Waiting for pods to be ready after plugin installation")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Configure plugin
	if err := r.configurePlugin(ctx, plugin, deployment); err != nil {
		logger.Error(err, "Failed to configure plugin")
		r.updatePluginStatus(ctx, plugin, "Failed", fmt.Sprintf("Plugin configuration failed: %v", err))
		r.Recorder.Eventf(plugin, corev1.EventTypeWarning, EventReasonPluginInstallFailed,
			"Plugin %s configuration failed: %v", plugin.Spec.Name, err)
		return ctrl.Result{}, nil // Don't return error - status is set correctly
	}

	// Update status to "Ready"
	r.updatePluginStatus(ctx, plugin, "Ready", "Plugin installed and configured successfully")
	r.Recorder.Eventf(plugin, corev1.EventTypeNormal, EventReasonPluginInstalled,
		"Plugin %s version %s installed successfully", plugin.Spec.Name, plugin.Spec.Version)

	logger.Info("Plugin reconciliation completed")
	return ctrl.Result{}, nil
}

func (r *Neo4jPluginReconciler) handleDeletion(ctx context.Context, plugin *neo4jv1beta1.Neo4jPlugin) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Uninstall plugin
	if err := r.uninstallPlugin(ctx, plugin); err != nil {
		logger.Error(err, "Failed to uninstall plugin")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(plugin, PluginFinalizer)
	if err := r.Update(ctx, plugin); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	logger.Info("Plugin deleted successfully")
	return ctrl.Result{}, nil
}

// DeploymentInfo holds information about the target deployment
type DeploymentInfo struct {
	Object    client.Object
	Type      string // "cluster" or "standalone"
	Name      string
	Namespace string
	IsReady   bool
}

func (r *Neo4jPluginReconciler) getTargetDeployment(ctx context.Context, plugin *neo4jv1beta1.Neo4jPlugin) (*DeploymentInfo, error) {
	// Try Neo4jEnterpriseCluster first
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      plugin.Spec.ClusterRef,
		Namespace: plugin.Namespace,
	}, cluster); err == nil {
		isReady := cluster.Status.Phase == "Ready"
		return &DeploymentInfo{
			Object:    cluster,
			Type:      "cluster",
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			IsReady:   isReady,
		}, nil
	}

	// Try Neo4jEnterpriseStandalone
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      plugin.Spec.ClusterRef,
		Namespace: plugin.Namespace,
	}, standalone); err == nil {
		isReady := standalone.Status.Phase == "Ready"
		return &DeploymentInfo{
			Object:    standalone,
			Type:      "standalone",
			Name:      standalone.Name,
			Namespace: standalone.Namespace,
			IsReady:   isReady,
		}, nil
	}

	return nil, fmt.Errorf("target deployment %s not found (tried both Neo4jEnterpriseCluster and Neo4jEnterpriseStandalone)", plugin.Spec.ClusterRef)
}

func (r *Neo4jPluginReconciler) waitForPluginReady(ctx context.Context, plugin *neo4jv1beta1.Neo4jPlugin) error {
	logger := log.FromContext(ctx)

	// Wait for plugin to be in Ready state with timeout
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for plugin %s: %w", plugin.Name, ctx.Err())
		case <-timeout:
			return fmt.Errorf("timeout waiting for plugin %s to be ready", plugin.Name)
		case <-ticker.C:
			current := &neo4jv1beta1.Neo4jPlugin{}
			if err := r.Get(ctx, types.NamespacedName{Name: plugin.Name, Namespace: plugin.Namespace}, current); err != nil {
				logger.Error(err, "Failed to get plugin status")
				continue
			}

			if current.Status.Phase == "Ready" {
				logger.Info("Plugin is ready", "plugin", plugin.Name)
				return nil
			}

			if current.Status.Phase == "Failed" {
				return fmt.Errorf("plugin %s failed to install: %s", plugin.Name, current.Status.Message)
			}

			logger.Info("Waiting for plugin to be ready", "plugin", plugin.Name, "phase", current.Status.Phase)
		}
	}
}

// arePodsReady checks if all pods are ready without blocking
func (r *Neo4jPluginReconciler) arePodsReady(ctx context.Context, deployment *DeploymentInfo) bool {
	logger := log.FromContext(ctx)

	// Check if all pods are ready
	pods := &corev1.PodList{}
	podLabels := r.getPodLabels(deployment)
	if err := r.List(ctx, pods, client.InNamespace(deployment.Namespace), client.MatchingLabels(podLabels)); err != nil {
		logger.Error(err, "Failed to list pods")
		return false
	}

	expectedReplicas := r.getExpectedReplicas(deployment)
	if len(pods.Items) != expectedReplicas {
		logger.Info("Not all pods are created yet", "current", len(pods.Items), "expected", expectedReplicas)
		return false
	}

	for _, pod := range pods.Items {
		// Check if pod is ready
		podReady := pod.Status.Phase == corev1.PodRunning
		if podReady {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady {
					podReady = condition.Status == corev1.ConditionTrue
					break
				}
			}
		}
		if !podReady {
			logger.Info("Pod not ready yet", "pod", pod.Name, "phase", pod.Status.Phase)
			return false
		}
	}

	logger.Info("All pods are ready")
	return true
}

func (r *Neo4jPluginReconciler) waitForDeploymentReady(ctx context.Context, deployment *DeploymentInfo) error {
	logger := log.FromContext(ctx)
	logger.Info("Waiting for deployment to be ready after plugin installation", "type", deployment.Type)

	timeout := time.After(10 * time.Minute)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for %s deployment %s/%s: %w",
				deployment.Type, deployment.Namespace, deployment.Name, ctx.Err())
		case <-timeout:
			return fmt.Errorf("timeout waiting for %s deployment %s/%s to be ready",
				deployment.Type, deployment.Namespace, deployment.Name)
		case <-ticker.C:
			// Check if all pods are ready
			pods := &corev1.PodList{}
			podLabels := r.getPodLabels(deployment)
			if err := r.List(ctx, pods, client.InNamespace(deployment.Namespace), client.MatchingLabels(podLabels)); err != nil {
				logger.Error(err, "Failed to list pods")
				continue
			}

			expectedReplicas := r.getExpectedReplicas(deployment)
			if len(pods.Items) != expectedReplicas {
				logger.Info("Waiting for all pods to be created", "current", len(pods.Items), "expected", expectedReplicas)
				continue
			}

			allReady := true
			for _, pod := range pods.Items {
				// Check if pod is ready
				podReady := pod.Status.Phase == corev1.PodRunning
				if podReady {
					for _, condition := range pod.Status.Conditions {
						if condition.Type == corev1.PodReady {
							podReady = condition.Status == corev1.ConditionTrue
							break
						}
					}
				}
				if !podReady {
					allReady = false
					break
				}
			}

			if allReady {
				logger.Info("Deployment is ready",
					"type", deployment.Type, "name", deployment.Name)
				return nil
			}

			logger.Info("Waiting for pods to be ready")
		}
	}
}

func (r *Neo4jPluginReconciler) configurePlugin(ctx context.Context, plugin *neo4jv1beta1.Neo4jPlugin, deployment *DeploymentInfo) error {
	logger := log.FromContext(ctx)

	// Check if plugin requires automatic security configuration even without user config
	needsAutoSecurity := r.requiresAutomaticSecurityConfiguration(plugin.Spec.Name)

	if len(plugin.Spec.Config) == 0 && plugin.Spec.Security == nil && !needsAutoSecurity {
		logger.Info("No plugin configuration provided")
		return nil
	}

	// For plugins that require environment variable configuration only
	// APOC settings are no longer supported in neo4j.conf in Neo4j 5.26+
	if r.isEnvironmentVariableOnlyPlugin(plugin.Spec.Name) {
		logger.Info("Plugin configuration handled via environment variables only", "plugin", plugin.Spec.Name)
		return r.applyAPOCSecurityConfiguration(ctx, plugin, deployment)
	}

	// For other plugins that require Neo4j client configuration
	neo4jClientConfig := r.filterNeo4jClientConfig(plugin.Spec.Config)
	if len(neo4jClientConfig) == 0 && plugin.Spec.Security == nil && !needsAutoSecurity {
		logger.Info("No Neo4j client configuration required")
		return nil
	}

	// Create Neo4j client based on deployment type
	var neo4jClient *neo4jclient.Client
	var err error

	if deployment.Type == "cluster" {
		cluster := deployment.Object.(*neo4jv1beta1.Neo4jEnterpriseCluster)
		neo4jClient, err = neo4jclient.NewClientForEnterprise(cluster, r.Client, getClusterAdminSecretName(cluster))
	} else {
		standalone := deployment.Object.(*neo4jv1beta1.Neo4jEnterpriseStandalone)
		neo4jClient, err = neo4jclient.NewClientForEnterpriseStandalone(standalone, r.Client, getStandaloneAdminSecretName(standalone))
	}

	if err != nil {
		return fmt.Errorf("failed to create Neo4j client: %w", err)
	}
	defer neo4jClient.Close()

	// Configure plugin settings that should go through Neo4j configuration system
	for key, value := range neo4jClientConfig {
		if err := neo4jClient.SetConfiguration(ctx, key, value); err != nil {
			return fmt.Errorf("failed to set configuration %s=%s: %w", key, value, err)
		}
	}

	// Apply security configuration if specified
	// Only apply runtime security configuration for settings that are dynamic
	if plugin.Spec.Security != nil && r.hasRuntimeSecurityConfiguration(plugin.Spec.Security) {
		if err := r.applySecurityConfiguration(ctx, neo4jClient, plugin); err != nil {
			return fmt.Errorf("failed to apply security configuration: %w", err)
		}
	}

	logger.Info("Plugin configuration applied successfully")
	return nil
}

func (r *Neo4jPluginReconciler) applySecurityConfiguration(ctx context.Context, neo4jClient *neo4jclient.Client, plugin *neo4jv1beta1.Neo4jPlugin) error {
	logger := log.FromContext(ctx)

	// In Neo4j 5.26+, most security settings (allowlist, denylist, unrestricted) are non-dynamic
	// and must be applied as environment variables at startup, not at runtime
	logger.Info("Skipping runtime security configuration - all security settings are applied as environment variables in Neo4j 5.26+")

	// Future: If there are any dynamic security settings that can be applied at runtime,
	// add them here. Currently, all common security settings are non-dynamic.

	return nil
}

// Pod hardening for plugin install/remove Jobs delegates to the single
// source of truth in internal/resources/security_context.go so it can't
// drift from the cluster/standalone/backup/restore pods.
func hardenedPluginPodSecurityContext() *corev1.PodSecurityContext {
	return resources.DefaultNeo4jPodSecurityContext()
}

func hardenedPluginContainerSecurityContext() *corev1.SecurityContext {
	return resources.DefaultNeo4jContainerSecurityContext()
}

func (r *Neo4jPluginReconciler) uninstallPlugin(ctx context.Context, plugin *neo4jv1beta1.Neo4jPlugin) error {
	logger := log.FromContext(ctx)

	// Get target deployment
	deployment, err := r.getTargetDeployment(ctx, plugin)
	if err != nil {
		// If deployment is not found, consider plugin already uninstalled
		logger.Info("Target deployment not found, considering plugin uninstalled")
		return nil
	}

	// Prune this plugin's additive security tokens (e.g. gds.*) from the
	// StatefulSet env vars. The install path unions these in for EVERY mode
	// (mergePluginSecurityEnv runs regardless of PreBaked/VerifiedDownload),
	// so removal must be mode-independent too — done here, before the
	// mode-specific early returns below, rather than only in the Managed
	// JAR-removal path. The cluster controller's env merge is subset/
	// ownership-tracked and will NOT drop tokens it doesn't own, so this
	// explicit prune is the only thing that removes them. Best-effort: a
	// failure here must not block the finalizer.
	if err := r.prunePluginSecurityEnv(ctx, plugin, deployment); err != nil {
		logger.Error(err, "Failed to prune plugin security env vars from StatefulSet; continuing", "plugin", plugin.Spec.Name)
	}

	// VerifiedDownload installs need the init container + auth/CA
	// volumes removed from the StatefulSet on uninstall — otherwise
	// the next reconcile would still try to download and the pod
	// would re-add the JAR to the emptyDir each restart. Done first
	// so the PreBaked-style "skip JAR removal" guard below doesn't
	// short-circuit the cleanup.
	if isVerifiedDownloadInstallMode(plugin) {
		if err := r.removeVerifiedDownloadInitContainer(ctx, plugin, deployment); err != nil {
			// Non-fatal — the finalizer release shouldn't block on a
			// reconcile race against the cluster controller. Next
			// reconcile of the cluster will eventually drop the stale
			// init container.
			logger.Error(err, "Failed to remove VerifiedDownload init container; releasing finalizer anyway", "plugin", plugin.Spec.Name)
		}
		logger.Info("Removed VerifiedDownload init container", "plugin", plugin.Spec.Name)
		return nil
	}

	// PreBaked installs are JARs the operator did not deliver — never run the
	// jar-removal Job against them. The user-supplied custom image owns
	// /plugins/*; touching it could orphan files the operator did not put
	// there. The operator-owned configuration (security env tokens) has
	// already been pruned above; ConfigMap entries are reconciled separately.
	if isPreBakedInstallMode(plugin) {
		logger.Info("Skipping JAR removal for PreBaked plugin", "plugin", plugin.Spec.Name)
		return nil
	}

	// Remove plugin from deployment
	if err := r.removePluginFromDeployment(ctx, plugin, deployment); err != nil {
		return fmt.Errorf("failed to remove plugin from deployment: %w", err)
	}

	// Dependencies are folded into the parent plugin's NEO4J_PLUGINS env var
	// by installPlugin; there are no separate Neo4jPlugin CRs to clean up.

	logger.Info("Plugin uninstalled successfully")
	return nil
}

func (r *Neo4jPluginReconciler) removePluginFromDeployment(ctx context.Context, plugin *neo4jv1beta1.Neo4jPlugin, deployment *DeploymentInfo) error {
	logger := log.FromContext(ctx)
	logger.Info("Removing plugin from deployment", "plugin", plugin.Spec.Name, "type", deployment.Type)

	// Create a Job to remove the plugin from the cluster
	removeJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-remove-plugin-%s", deployment.Name, plugin.Spec.Name),
			Namespace: deployment.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "neo4j-plugin",
				"app.kubernetes.io/instance":  deployment.Name,
				"app.kubernetes.io/component": "plugin-removal",
				"neo4j.plugin/name":           plugin.Spec.Name,
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: func() *int32 { v := int32(300); return &v }(), // Clean up completed jobs after 5 minutes
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					SecurityContext: hardenedPluginPodSecurityContext(),
					RestartPolicy:   corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "plugin-remover",
							Image:   "busybox:latest",
							Command: []string{"sh", "-c"},
							Args: []string{
								fmt.Sprintf(`
									echo "Removing plugin %s from Neo4j %s %s"
									# Remove plugin jar file
									rm -f /plugins/%s*.jar
									echo "Plugin removal completed"
								`, plugin.Spec.Name, deployment.Type, deployment.Name, plugin.Spec.Name),
							},
							SecurityContext: hardenedPluginContainerSecurityContext(),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "plugins",
									MountPath: "/plugins",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "plugins",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	if err := r.Create(ctx, removeJob); err != nil {
		return fmt.Errorf("failed to create plugin removal job: %w", err)
	}

	// Security-token env pruning is handled mode-independently in
	// uninstallPlugin (before the mode branches), so it is not repeated here.

	// Wait for job completion
	return r.waitForJobCompletion(ctx, removeJob)
}

// prunePluginSecurityEnv removes the uninstalled plugin's additive security
// tokens from the target StatefulSet's neo4j container env vars. No-op when the
// StatefulSet is gone or the plugin contributed no additive env vars (e.g. a
// standalone, which carries plugin security in neo4j.conf rather than env vars).
func (r *Neo4jPluginReconciler) prunePluginSecurityEnv(ctx context.Context, plugin *neo4jv1beta1.Neo4jPlugin, deployment *DeploymentInfo) error {
	stsKey := types.NamespacedName{Name: r.getStatefulSetName(deployment), Namespace: deployment.Namespace}
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		sts := &appsv1.StatefulSet{}
		if err := r.Get(ctx, stsKey, sts); err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
		for i := range sts.Spec.Template.Spec.Containers {
			c := &sts.Spec.Template.Spec.Containers[i]
			if c.Name != "neo4j" {
				continue
			}
			pruned := removePluginSecurityEnv(c.Env, r.pluginSecurityRemovalSettings(plugin))
			if len(pruned) == len(c.Env) {
				// Recompute equality cheaply: lengths match AND values unchanged?
				// removePluginSecurityEnv only ever shrinks or rewrites values, so
				// compare to avoid a no-op Update (which would needlessly restart).
				same := true
				for j := range pruned {
					if pruned[j] != c.Env[j] {
						same = false
						break
					}
				}
				if same {
					return nil
				}
			}
			c.Env = pruned
			return r.Update(ctx, sts)
		}
		return nil
	})
}

func (r *Neo4jPluginReconciler) updatePluginStatus(ctx context.Context, plugin *neo4jv1beta1.Neo4jPlugin, phase, message string) {
	update := func() error {
		latest := &neo4jv1beta1.Neo4jPlugin{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(plugin), latest); err != nil {
			return err
		}
		latest.Status.Phase = phase
		latest.Status.Message = message
		latest.Status.ObservedGeneration = latest.Generation
		condStatus, condReason := PhaseToConditionStatus(phase)
		SetReadyCondition(&latest.Status.Conditions, latest.Generation, condStatus, condReason, message)
		return r.Status().Update(ctx, latest)
	}
	err := retry.RetryOnConflict(retry.DefaultBackoff, update)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to update plugin status")
	}
}

// SetupWithManager configures the controller with the manager
// installPluginViaEnvironment installs the plugin using NEO4J_PLUGINS environment variable
// This is the recommended approach by Neo4j for Docker plugin installation
func (r *Neo4jPluginReconciler) installPluginViaEnvironment(ctx context.Context, plugin *neo4jv1beta1.Neo4jPlugin, deployment *DeploymentInfo) error {
	logger := log.FromContext(ctx)
	// preBaked retained as the local variable name for diff stability —
	// it gates the same code paths whether the user is bringing the JAR
	// via a custom image (PreBaked) or via the operator-injected init
	// container (VerifiedDownload).
	preBaked := shouldSkipNeo4jPluginsEnv(plugin)
	switch {
	case isVerifiedDownloadInstallMode(plugin):
		logger.Info("Configuring plugin in VerifiedDownload mode — injecting checksum-verifying init container, skipping NEO4J_PLUGINS mutation", "plugin", plugin.Spec.Name)
	case isPreBakedInstallMode(plugin):
		logger.Info("Configuring plugin in PreBaked mode — skipping NEO4J_PLUGINS mutation", "plugin", plugin.Spec.Name)
	default:
		logger.Info("Installing plugin via NEO4J_PLUGINS environment variable", "plugin", plugin.Spec.Name)
	}

	// Get the StatefulSet for the deployment
	sts := &appsv1.StatefulSet{}
	stsKey := types.NamespacedName{
		Name:      r.getStatefulSetName(deployment),
		Namespace: deployment.Namespace,
	}

	if err := r.Get(ctx, stsKey, sts); err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Find the Neo4j container
	var neo4jContainer *corev1.Container
	for i := range sts.Spec.Template.Spec.Containers {
		if sts.Spec.Template.Spec.Containers[i].Name == "neo4j" {
			neo4jContainer = &sts.Spec.Template.Spec.Containers[i]
			break
		}
	}

	if neo4jContainer == nil {
		return fmt.Errorf("neo4j container not found in StatefulSet")
	}

	// Prepare plugin name and dependencies for NEO4J_PLUGINS
	pluginName := r.mapPluginName(plugin.Spec.Name)

	// Collect all plugins to install (main plugin + dependencies)
	pluginsToInstall := []string{pluginName}
	for _, dep := range plugin.Spec.Dependencies {
		depName := r.mapPluginName(dep.Name)
		pluginsToInstall = append(pluginsToInstall, depName)
	}

	if !preBaked {
		// Find existing NEO4J_PLUGINS environment variable or create new one
		var pluginsEnvVar *corev1.EnvVar
		for i := range neo4jContainer.Env {
			if neo4jContainer.Env[i].Name == "NEO4J_PLUGINS" {
				pluginsEnvVar = &neo4jContainer.Env[i]
				break
			}
		}

		if pluginsEnvVar == nil {
			// Add new NEO4J_PLUGINS environment variable with all plugins
			var quotedPlugins []string
			for _, plugin := range pluginsToInstall {
				quotedPlugins = append(quotedPlugins, fmt.Sprintf("\"%s\"", plugin))
			}
			neo4jContainer.Env = append(neo4jContainer.Env, corev1.EnvVar{
				Name:  "NEO4J_PLUGINS",
				Value: fmt.Sprintf("[%s]", strings.Join(quotedPlugins, ",")),
			})
		} else {
			// Update existing NEO4J_PLUGINS - parse and add all new plugins
			currentValue := pluginsEnvVar.Value
			for _, plugin := range pluginsToInstall {
				updatedPlugins, err := r.addPluginToList(currentValue, plugin)
				if err != nil {
					return fmt.Errorf("failed to update plugin list: %w", err)
				}
				currentValue = updatedPlugins
			}
			pluginsEnvVar.Value = currentValue
		}
	}

	// Add plugin configuration as environment variables
	for key, value := range plugin.Spec.Config {
		envVarName := resources.Neo4jSettingEnvVarName(key)
		neo4jContainer.Env = append(neo4jContainer.Env, corev1.EnvVar{
			Name:  envVarName,
			Value: value,
		})
	}

	// Add required procedure security settings if the plugin needs them and they're not environment-variable only
	if r.getPluginType(plugin.Spec.Name) != PluginTypeEnvironmentOnly {
		requiredSettings := r.getRequiredProcedureSecuritySettings(plugin.Spec.Name)
		for key, value := range requiredSettings {
			envVarName := resources.Neo4jSettingEnvVarName(key)

			// Check if this environment variable already exists
			exists := false
			for i, env := range neo4jContainer.Env {
				if env.Name == envVarName {
					// Merge (union, dedup) for known comma-separated settings.
					// Previously we used strings.Contains(key, "allowlist"|"unrestricted")
					// which technically covered all our known keys — but a user-typed
					// custom setting like `my.custom.allowlist` could trigger merge
					// semantics that aren't appropriate. And the old "value+,+new"
					// concat produced duplicates whenever the existing value was
					// itself multi-valued (e.g. Bloom's http_auth_allowlist).
					if isMergeableCSVKey(key) {
						neo4jContainer.Env[i].Value = mergeCSV(env.Value, value)
					}
					exists = true
					break
				}
			}

			if !exists {
				neo4jContainer.Env = append(neo4jContainer.Env, corev1.EnvVar{
					Name:  envVarName,
					Value: value,
				})
			}
		}
	}

	// Update the StatefulSet with retry on conflict
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Fetch latest version of StatefulSet for each retry
		currentSts := &appsv1.StatefulSet{}
		if err := r.Get(ctx, stsKey, currentSts); err != nil {
			return err
		}

		// Find the Neo4j container in the current StatefulSet
		var currentNeo4jContainer *corev1.Container
		for i := range currentSts.Spec.Template.Spec.Containers {
			if currentSts.Spec.Template.Spec.Containers[i].Name == "neo4j" {
				currentNeo4jContainer = &currentSts.Spec.Template.Spec.Containers[i]
				break
			}
		}
		if currentNeo4jContainer == nil {
			return fmt.Errorf("neo4j container not found in current StatefulSet")
		}

		// Apply the same plugin changes to the current StatefulSet
		pluginName := r.mapPluginName(plugin.Spec.Name)
		pluginsToInstall := []string{pluginName}
		for _, dep := range plugin.Spec.Dependencies {
			depName := r.mapPluginName(dep.Name)
			pluginsToInstall = append(pluginsToInstall, depName)
		}

		if !preBaked {
			// Find existing NEO4J_PLUGINS environment variable or create new one
			var pluginsEnvVar *corev1.EnvVar
			for i := range currentNeo4jContainer.Env {
				if currentNeo4jContainer.Env[i].Name == "NEO4J_PLUGINS" {
					pluginsEnvVar = &currentNeo4jContainer.Env[i]
					break
				}
			}
			if pluginsEnvVar == nil {
				// Add new NEO4J_PLUGINS environment variable with all plugins
				var quotedPlugins []string
				for _, plugin := range pluginsToInstall {
					quotedPlugins = append(quotedPlugins, fmt.Sprintf("\"%s\"", plugin))
				}
				currentNeo4jContainer.Env = append(currentNeo4jContainer.Env, corev1.EnvVar{
					Name:  "NEO4J_PLUGINS",
					Value: fmt.Sprintf("[%s]", strings.Join(quotedPlugins, ",")),
				})
			} else {
				// Update existing NEO4J_PLUGINS — delegate to the same helper used
				// in the outer block (above) so the merge is JSON-aware,
				// idempotent, and bug-free. The previous inline implementation
				// here read `currentValue` once but then mutated only
				// `pluginsEnvVar.Value`, leaving `currentValue` stale across
				// loop iterations — only the first plugin in pluginsToInstall
				// would actually land in the env var.
				currentValue := pluginsEnvVar.Value
				for _, plugin := range pluginsToInstall {
					updated, err := r.addPluginToList(currentValue, plugin)
					if err != nil {
						return fmt.Errorf("failed to update plugin list: %w", err)
					}
					currentValue = updated
				}
				pluginsEnvVar.Value = currentValue
			}
		}

		// Add plugin-specific configuration as environment variables
		for key, value := range plugin.Spec.Config {
			envVarName := resources.Neo4jSettingEnvVarName(key)
			// Check if environment variable already exists
			exists := false
			for i := range currentNeo4jContainer.Env {
				if currentNeo4jContainer.Env[i].Name == envVarName {
					currentNeo4jContainer.Env[i].Value = value
					exists = true
					break
				}
			}
			if !exists {
				currentNeo4jContainer.Env = append(currentNeo4jContainer.Env, corev1.EnvVar{
					Name:  envVarName,
					Value: value,
				})
			}
		}

		// Apply security settings as environment variables, unioning additive
		// allowlists across plugins (see mergePluginSecurityEnv).
		currentNeo4jContainer.Env = mergePluginSecurityEnv(currentNeo4jContainer.Env, r.pluginSecuritySettings(plugin))

		return r.Update(ctx, currentSts)
	})
	if err != nil {
		return fmt.Errorf("failed to update StatefulSet with plugin configuration: %w", err)
	}

	logger.Info("Successfully updated StatefulSet with plugin configuration", "plugin", pluginName)

	// VerifiedDownload mode: after the regular config/env-var write,
	// inject the checksum-verifying init container so the JAR lands in
	// /plugins before Neo4j starts. This is the only code path that
	// emits init containers; PreBaked relies on the user's custom
	// image carrying the JAR, and Managed delegates to the entrypoint.
	if isVerifiedDownloadInstallMode(plugin) {
		if err := r.injectVerifiedDownloadInitContainer(ctx, plugin, deployment); err != nil {
			return fmt.Errorf("failed to inject verified-download init container: %w", err)
		}
	}

	// Note: ConfigMap updates for standalone deployments are now handled earlier in reconcile flow
	// before connectivity checks to ensure security settings are applied before Neo4j starts

	return nil
}

// mergePluginSecurityEnv applies a plugin's security settings to a container's
// env vars without clobbering across plugins. For additive list keys
// (resources.IsAdditiveConfKey — e.g. dbms.security.procedures.unrestricted /
// allowlist), the value is UNIONED with any existing env value, so GDS's `gds.*`
// and APOC's `apoc.*` both survive instead of the last-reconciled plugin
// overwriting the first. Scalar keys are set in place. Deterministic (keys
// processed sorted) and idempotent (re-applying the same settings is a no-op).
//
// NOTE: this accumulates tokens; tokens from an *uninstalled* plugin are not
// pruned here — that needs the authoritative recompute tracked in issue #146.
// pluginSecuritySettings returns the Neo4j security settings a plugin
// contributes (automatic per-plugin defaults plus any user-provided
// spec.security). Used both when applying settings at install and when removing
// them on uninstall, so the two paths can't diverge.
func (r *Neo4jPluginReconciler) pluginSecuritySettings(plugin *neo4jv1beta1.Neo4jPlugin) map[string]string {
	settings := r.getAutomaticSecuritySettings(plugin.Spec.Name)
	if plugin.Spec.Security != nil {
		if len(plugin.Spec.Security.AllowedProcedures) > 0 {
			allowedList := strings.Join(plugin.Spec.Security.AllowedProcedures, ",")
			settings["dbms.security.procedures.allowlist"] = allowedList
			// Non-sandbox mode also runs the allowed procedures unrestricted.
			if !plugin.Spec.Security.Sandbox {
				settings["dbms.security.procedures.unrestricted"] = allowedList
			}
		}
		if len(plugin.Spec.Security.DeniedProcedures) > 0 {
			settings["dbms.security.procedures.denylist"] = strings.Join(plugin.Spec.Security.DeniedProcedures, ",")
		}
	}
	return settings
}

// pluginSecurityRemovalSettings returns the additive-key token set to subtract on
// uninstall: the UNION of the plugin's automatic per-name defaults and its
// current spec.security override. The merge path (mergePluginSecurityEnv) unions
// every reconcile's pluginSecuritySettings into the env, but pluginSecuritySettings
// *replaces* unrestricted/allowlist with the custom list when allowedProcedures is
// set — so a plugin installed with defaults and later given an override leaves the
// automatic tokens (e.g. gds.*,apoc.load.*) accumulated in env that the current
// spec no longer mentions. Always subtracting the automatic defaults too means
// they can never be orphaned, regardless of spec toggling.
//
// This closes the common orphaned-defaults case. The fully general gap — a plugin
// whose custom allowedProcedures was changed to several *different* values over its
// lifetime — still needs per-plugin token ownership tracking, deferred to #146,
// because only the current custom value is knowable here.
func (r *Neo4jPluginReconciler) pluginSecurityRemovalSettings(plugin *neo4jv1beta1.Neo4jPlugin) map[string]string {
	auto := r.getAutomaticSecuritySettings(plugin.Spec.Name)
	out := make(map[string]string, len(auto))
	for k, v := range auto {
		out[k] = v
	}
	for k, v := range r.pluginSecuritySettings(plugin) {
		if existing, ok := out[k]; ok && resources.IsAdditiveConfKey(k) {
			out[k] = resources.MergeConfListValues(existing, v)
		} else {
			out[k] = v
		}
	}
	return out
}

// removePluginSecurityEnv reverses mergePluginSecurityEnv for a single plugin on
// uninstall: for additive list keys it subtracts that plugin's tokens from the
// env var (dropping the var entirely if nothing remains), so another plugin's
// allowlist is preserved while the uninstalled plugin's entries are pruned.
// Non-additive (scalar) keys are left untouched — they may be shared, and the
// accumulation problem this prunes is specific to the additive lists. Plugin
// token sets are disjoint (gds.*, apoc.*, …), so subtraction is exact.
func removePluginSecurityEnv(env []corev1.EnvVar, settings map[string]string) []corev1.EnvVar {
	remove := make(map[string]string) // env var name -> tokens to subtract
	for key, value := range settings {
		if resources.IsAdditiveConfKey(key) {
			remove[resources.Neo4jSettingEnvVarName(key)] = value
		}
	}
	if len(remove) == 0 {
		return env
	}

	out := env[:0:0]
	for _, e := range env {
		if toks, ok := remove[e.Name]; ok {
			e.Value = subtractCSV(e.Value, toks)
			if e.Value == "" {
				continue // nothing left for this key — drop it
			}
		}
		out = append(out, e)
	}
	return out
}

// subtractCSV returns value's comma-separated tokens with remove's tokens taken
// out, preserving order.
func subtractCSV(value, remove string) string {
	rm := make(map[string]bool)
	for _, t := range strings.Split(remove, ",") {
		if t = strings.TrimSpace(t); t != "" {
			rm[t] = true
		}
	}
	var keep []string
	for _, t := range strings.Split(value, ",") {
		if t = strings.TrimSpace(t); t != "" && !rm[t] {
			keep = append(keep, t)
		}
	}
	return strings.Join(keep, ",")
}

func mergePluginSecurityEnv(env []corev1.EnvVar, settings map[string]string) []corev1.EnvVar {
	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		envName := resources.Neo4jSettingEnvVarName(key)
		idx := -1
		for i := range env {
			if env[i].Name == envName {
				idx = i
				break
			}
		}
		switch {
		case idx >= 0 && resources.IsAdditiveConfKey(key):
			env[idx].Value = resources.MergeConfListValues(env[idx].Value, settings[key])
		case idx >= 0:
			env[idx].Value = settings[key]
		default:
			env = append(env, corev1.EnvVar{Name: envName, Value: settings[key]})
		}
	}
	return env
}

// injectVerifiedDownloadInitContainer adds (or updates) the plugin's
// init container on the target StatefulSet's pod template, plus the
// matching auth/CA volumes. Ownership-tracked via the
// PluginInitContainersAnnotation so the controller can remove its
// own containers on uninstall without disturbing foreign ones.
//
// Same retry-on-conflict mechanics as the env-var path — the
// StatefulSet's ResourceVersion races with the cluster controller's
// own reconciles.
func (r *Neo4jPluginReconciler) injectVerifiedDownloadInitContainer(
	ctx context.Context,
	plugin *neo4jv1beta1.Neo4jPlugin,
	deployment *DeploymentInfo,
) error {
	stsKey := types.NamespacedName{
		Name:      r.getStatefulSetName(deployment),
		Namespace: deployment.Namespace,
	}
	containerName := resources.PluginInitContainerName(plugin.Spec.Name)

	// CA bundle is sourced from the OWNING cluster/standalone's
	// trustedCASecrets field — the JAR fetch typically goes to an
	// internal Artifactory using the same corporate CA the rest of
	// the deployment already trusts.
	var caSecrets []neo4jv1beta1.TrustedCASecret
	switch deployment.Type {
	case "cluster":
		if c, ok := deployment.Object.(*neo4jv1beta1.Neo4jEnterpriseCluster); ok {
			caSecrets = c.Spec.TrustedCASecrets
		}
	case "standalone":
		if s, ok := deployment.Object.(*neo4jv1beta1.Neo4jEnterpriseStandalone); ok {
			caSecrets = s.Spec.TrustedCASecrets
		}
	}

	desired := resources.BuildPluginVerifiedDownloadInitContainer(plugin, r.PluginInitImage, caSecrets)
	authVol := resources.BuildPluginAuthVolume(plugin)
	caVol := resources.BuildPluginCAVolume(caSecrets)

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		sts := &appsv1.StatefulSet{}
		if err := r.Get(ctx, stsKey, sts); err != nil {
			return err
		}

		podSpec := &sts.Spec.Template.Spec

		// Replace existing init container of the same name; otherwise append.
		// Same name is the source of truth — Kubernetes itself enforces
		// uniqueness, and our annotation tracks ownership for cleanup.
		found := false
		for i, ic := range podSpec.InitContainers {
			if ic.Name == containerName {
				podSpec.InitContainers[i] = desired
				found = true
				break
			}
		}
		if !found {
			podSpec.InitContainers = append(podSpec.InitContainers, desired)
		}

		// Add volumes if not already present (multiple VerifiedDownload
		// plugins on the same cluster share the CA volume; each plugin
		// has its own auth volume).
		if authVol != nil && !podSpecHasVolume(podSpec, authVol.Name) {
			podSpec.Volumes = append(podSpec.Volumes, *authVol)
		}
		if caVol != nil && !podSpecHasVolume(podSpec, caVol.Name) {
			podSpec.Volumes = append(podSpec.Volumes, *caVol)
		}

		// Record ownership in the annotation so the uninstall path can
		// remove only our init containers. The annotation lives on the
		// PodTemplate annotation map so it travels with the spec
		// fingerprint and triggers a rolling restart on change.
		if sts.Spec.Template.Annotations == nil {
			sts.Spec.Template.Annotations = map[string]string{}
		}
		owned := parsePluginInitOwned(sts.Spec.Template.Annotations[PluginInitContainersAnnotation])
		if _, exists := owned[containerName]; !exists {
			owned[containerName] = struct{}{}
			sts.Spec.Template.Annotations[PluginInitContainersAnnotation] = formatPluginInitOwned(owned)
		}

		return r.Update(ctx, sts)
	})
}

// removeVerifiedDownloadInitContainer is the inverse of
// injectVerifiedDownloadInitContainer: drops the plugin's init
// container from the StatefulSet PodSpec, removes the plugin-specific
// auth volume if present, and updates the ownership annotation. The
// shared CA volume is left alone — other VerifiedDownload plugins on
// the same StatefulSet may still be using it; deleting it
// unilaterally would break them.
//
// Idempotent: a second call (or a call after a delete-already-happened
// scenario) is a no-op via the "not found" early returns.
func (r *Neo4jPluginReconciler) removeVerifiedDownloadInitContainer(
	ctx context.Context,
	plugin *neo4jv1beta1.Neo4jPlugin,
	deployment *DeploymentInfo,
) error {
	stsKey := types.NamespacedName{
		Name:      r.getStatefulSetName(deployment),
		Namespace: deployment.Namespace,
	}
	containerName := resources.PluginInitContainerName(plugin.Spec.Name)
	authVolName := "plugin-auth-" + plugin.Spec.Name

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		sts := &appsv1.StatefulSet{}
		if err := r.Get(ctx, stsKey, sts); err != nil {
			if errors.IsNotFound(err) {
				return nil // StatefulSet already gone — nothing to clean up.
			}
			return err
		}

		podSpec := &sts.Spec.Template.Spec
		changed := false

		// Strip the init container.
		filtered := podSpec.InitContainers[:0]
		for _, ic := range podSpec.InitContainers {
			if ic.Name == containerName {
				changed = true
				continue
			}
			filtered = append(filtered, ic)
		}
		podSpec.InitContainers = filtered

		// Strip the per-plugin auth volume (CA volume is shared — leave it).
		filteredVols := podSpec.Volumes[:0]
		for _, v := range podSpec.Volumes {
			if v.Name == authVolName {
				changed = true
				continue
			}
			filteredVols = append(filteredVols, v)
		}
		podSpec.Volumes = filteredVols

		// Drop ourselves from the ownership annotation.
		if sts.Spec.Template.Annotations != nil {
			owned := parsePluginInitOwned(sts.Spec.Template.Annotations[PluginInitContainersAnnotation])
			if _, ok := owned[containerName]; ok {
				delete(owned, containerName)
				if len(owned) == 0 {
					delete(sts.Spec.Template.Annotations, PluginInitContainersAnnotation)
				} else {
					sts.Spec.Template.Annotations[PluginInitContainersAnnotation] = formatPluginInitOwned(owned)
				}
				changed = true
			}
		}

		if !changed {
			return nil // Nothing to update — return without bumping ResourceVersion.
		}
		return r.Update(ctx, sts)
	})
}

// podSpecHasVolume reports whether a volume of the given name is
// already declared on the PodSpec. Used to avoid duplicate Volume
// entries when multiple VerifiedDownload plugins land on the same
// StatefulSet over successive reconciles.
func podSpecHasVolume(podSpec *corev1.PodSpec, name string) bool {
	for _, v := range podSpec.Volumes {
		if v.Name == name {
			return true
		}
	}
	return false
}

// parsePluginInitOwned + formatPluginInitOwned shuttle the comma-separated
// annotation value through a set so add/remove operations are
// order-independent and idempotent. Empty/whitespace tokens are
// skipped.
func parsePluginInitOwned(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	return out
}

func formatPluginInitOwned(set map[string]struct{}) string {
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// updateStandaloneConfigMapForPlugin updates the ConfigMap for standalone deployments with plugin security settings
func (r *Neo4jPluginReconciler) updateStandaloneConfigMapForPlugin(ctx context.Context, plugin *neo4jv1beta1.Neo4jPlugin, deployment *DeploymentInfo) error {
	logger := log.FromContext(ctx)

	// Get automatic security settings for this plugin
	automaticSettings := r.getAutomaticSecuritySettings(plugin.Spec.Name)

	// Get user-provided non-dynamic settings that must be applied at startup
	nonDynamicUserSettings := make(map[string]string)
	for key, value := range plugin.Spec.Config {
		// Include settings that are non-dynamic and must be in neo4j.conf at startup
		if r.isNonDynamicSetting(key) || r.isSecuritySetting(key) {
			nonDynamicUserSettings[key] = value
		}
	}

	// Combine automatic and user-provided settings
	allSettings := make(map[string]string)
	for key, value := range automaticSettings {
		allSettings[key] = value
	}
	for key, value := range nonDynamicUserSettings {
		allSettings[key] = value // User settings can override automatic ones
	}

	if len(allSettings) == 0 {
		logger.Info("No ConfigMap settings required for plugin", "plugin", plugin.Spec.Name)
		return nil
	}

	// Get the standalone resource
	standalone := deployment.Object.(*neo4jv1beta1.Neo4jEnterpriseStandalone)

	// Get the ConfigMap name for the standalone
	configMapName := fmt.Sprintf("%s-config", standalone.Name)
	configMapKey := types.NamespacedName{
		Namespace: standalone.Namespace,
		Name:      configMapName,
	}

	// Retrieve the current ConfigMap
	configMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, configMapKey, configMap); err != nil {
		return fmt.Errorf("failed to get ConfigMap %s: %w", configMapName, err)
	}

	// Get current neo4j.conf content
	currentConf := configMap.Data["neo4j.conf"]
	if currentConf == "" {
		return fmt.Errorf("neo4j.conf not found in ConfigMap %s", configMapName)
	}

	// Merge plugin settings into neo4j.conf without creating duplicate keys.
	// The previous approach appended a line guarded only by an exact-substring
	// check, so a plugin allowlist (e.g. dbms.security.procedures.unrestricted=
	// apoc.*) was added as a SECOND declaration when the conf already had that
	// key (e.g. =gds.*) — CalVer Neo4j then refuses to start ("declared multiple
	// times"). UpsertNeo4jConfSettings unions additive keys and adds scalar keys
	// only when absent; it is idempotent, so a no-op won't churn the ConfigMap
	// or restart the pod.
	updatedConf := resources.UpsertNeo4jConfSettings(currentConf, allSettings)

	// Update the ConfigMap if changes were made
	if updatedConf != currentConf {
		configMap.Data["neo4j.conf"] = updatedConf
		if err := r.Update(ctx, configMap); err != nil {
			return fmt.Errorf("failed to update ConfigMap %s: %w", configMapName, err)
		}
		logger.Info("ConfigMap updated with plugin security settings", "plugin", plugin.Spec.Name)

		// Restart the standalone pod to pick up the configuration changes
		if err := r.restartStandalonePods(ctx, standalone); err != nil {
			logger.Error(err, "Failed to restart standalone pods after ConfigMap update")
			// Don't fail the entire operation if restart fails - ConfigMap is updated
		}
	}

	return nil
}

// restartStandalonePods restarts the pods of a standalone deployment to pick up ConfigMap changes
func (r *Neo4jPluginReconciler) restartStandalonePods(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Get the StatefulSet for the standalone
	stsKey := types.NamespacedName{
		Namespace: standalone.Namespace,
		Name:      standalone.Name,
	}

	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, stsKey, sts); err != nil {
		return fmt.Errorf("failed to get StatefulSet %s: %w", standalone.Name, err)
	}

	// Add a restart annotation to force pod restart
	if sts.Spec.Template.Annotations == nil {
		sts.Spec.Template.Annotations = make(map[string]string)
	}
	sts.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	if err := r.Update(ctx, sts); err != nil {
		return fmt.Errorf("failed to update StatefulSet to restart pods: %w", err)
	}

	logger.Info("StatefulSet updated with restart annotation", "name", standalone.Name)
	return nil
}

// mapPluginName maps our plugin names to Neo4j's expected plugin names for NEO4J_PLUGINS
func (r *Neo4jPluginReconciler) mapPluginName(pluginName string) string {
	switch pluginName {
	case "apoc":
		return "apoc"
	case "apoc-extended":
		return "apoc-extended"
	case "graph-data-science", "gds":
		return "graph-data-science"
	case "bloom":
		return "bloom"
	case "genai":
		return "genai"
	case "n10s", "neosemantics":
		return "n10s"
	case "graphql":
		return "graphql"
	case "fleet-management":
		return "fleet-management"
	default:
		return pluginName // Use as-is for custom plugins
	}
}

// getRequiredProcedureSecuritySettings returns the required procedure security settings for a plugin
func (r *Neo4jPluginReconciler) getRequiredProcedureSecuritySettings(pluginName string) map[string]string {
	settings := make(map[string]string)

	switch pluginName {
	case "graph-data-science", "gds":
		// GDS requires unrestricted access for performance
		settings["dbms.security.procedures.unrestricted"] = "gds.*"
		settings["dbms.security.procedures.allowlist"] = "gds.*"
	case "bloom":
		// Bloom requires unrestricted access
		settings["dbms.security.procedures.unrestricted"] = "bloom.*"
		// Bloom also needs HTTP auth allowlist for web interface
		settings["dbms.security.http_auth_allowlist"] = "/,/browser.*,/bloom.*"
		// Unmanaged extension for hosting Bloom client
		settings["server.unmanaged_extension_classes"] = "com.neo4j.bloom.server=/bloom"
	case "apoc", "apoc-extended":
		// APOC procedures should be configured via allowlist (principle of least privilege)
		// Note: This is handled separately since APOC config is environment-variable only
		// These settings would still go through neo4j.conf for procedure security
		break
	case "genai":
		// GenAI may need procedure allowlist depending on usage
		break
	case "n10s", "neosemantics":
		// Neo Semantics procedures
		settings["dbms.security.procedures.unrestricted"] = "n10s.*"
		settings["dbms.security.procedures.allowlist"] = "n10s.*"
	case "graphql":
		// GraphQL plugin procedures
		settings["dbms.security.procedures.unrestricted"] = "graphql.*"
		settings["dbms.security.procedures.allowlist"] = "graphql.*"
	case "fleet-management":
		// Fleet management plugin procedures
		settings["dbms.security.procedures.unrestricted"] = "fleetManagement.*"
		settings["dbms.security.procedures.allowlist"] = "fleetManagement.*"
	}

	return settings
}

// isDeploymentFunctional checks if the deployment is actually functional by testing Neo4j connectivity
func (r *Neo4jPluginReconciler) isDeploymentFunctional(ctx context.Context, deployment *DeploymentInfo) bool {
	logger := log.FromContext(ctx)

	// For clusters, try to connect and verify cluster formation
	if deployment.Type == "cluster" {
		cluster := deployment.Object.(*neo4jv1beta1.Neo4jEnterpriseCluster)
		neo4jClient, err := neo4jclient.NewClientForEnterprise(cluster, r.Client, getClusterAdminSecretName(cluster))
		if err != nil {
			logger.Info("Cannot create Neo4j client", "error", err)
			return false
		}
		defer neo4jClient.Close()

		// Test connectivity by getting server list
		servers, err := neo4jClient.GetServerList(ctx)
		if err != nil {
			logger.Info("Cannot get server list", "error", err)
			return false
		}

		// For clusters, verify we have the expected number of servers
		expectedServers := int(cluster.Spec.Topology.Servers)
		if len(servers) < expectedServers {
			logger.Info("Cluster not fully formed", "expected", expectedServers, "actual", len(servers))
			return false
		}

		logger.Info("Cluster is functional", "servers", len(servers))
		return true
	}

	// For standalone, just check basic connectivity
	if deployment.Type == "standalone" {
		standalone := deployment.Object.(*neo4jv1beta1.Neo4jEnterpriseStandalone)
		neo4jClient, err := neo4jclient.NewClientForEnterpriseStandalone(standalone, r.Client, getStandaloneAdminSecretName(standalone))
		if err != nil {
			logger.Info("Cannot create Neo4j standalone client", "error", err)
			return false
		}
		defer neo4jClient.Close()

		// Test connectivity by getting server list (should have 1 server)
		servers, err := neo4jClient.GetServerList(ctx)
		if err != nil {
			logger.Info("Cannot get server list for standalone", "error", err)
			return false
		}

		if len(servers) == 0 {
			logger.Info("Standalone instance not ready")
			return false
		}

		logger.Info("Standalone is functional")
		return true
	}

	logger.Info("Unknown deployment type", "type", deployment.Type)
	return false
}

// addPluginToList is a method shim that delegates to the package-level MergeNeo4jPluginList.
func (r *Neo4jPluginReconciler) addPluginToList(existing string, newPlugin string) (string, error) {
	return MergeNeo4jPluginList(existing, newPlugin)
}

// mergeableCSVKeys is the closed set of plugin-driven Neo4j config keys whose
// values are comma-separated lists where merging the union (rather than
// overwriting one with the other) is the correct behaviour. Keep this list
// closed — substring matches are dangerous: a user-supplied setting like
// `my.custom.allowlist` should not silently get merge semantics.
var mergeableCSVKeys = map[string]struct{}{
	"dbms.security.procedures.allowlist":    {},
	"dbms.security.procedures.unrestricted": {},
	"dbms.security.procedures.denylist":     {},
	"dbms.security.http_auth_allowlist":     {},
	"server.unmanaged_extension_classes":    {},
}

// isMergeableCSVKey reports whether a Neo4j config key carries a CSV value
// that should be unioned (rather than overwritten) when both an existing
// env var and a plugin-required setting specify a value.
func isMergeableCSVKey(key string) bool {
	_, ok := mergeableCSVKeys[key]
	return ok
}

// mergeCSV returns the deduplicated comma-separated union of a and b,
// preserving first-seen order. Whitespace around each element is trimmed;
// empty elements are dropped.
//
// Used for security/extension settings (see mergeableCSVKeys) where
// multiple sources contribute entries — typically the user via
// plugin.Spec.Config plus the operator's required defaults for the
// plugin. The previous code concatenated `existing + "," + new` blindly,
// which produced duplicate-laden values when `new` was itself
// multi-element (e.g. Bloom's `http_auth_allowlist`).
func mergeCSV(a, b string) string {
	seen := make(map[string]struct{})
	var out []string
	for _, src := range [...]string{a, b} {
		for _, p := range strings.Split(src, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, dup := seen[p]; dup {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}

// MergeNeo4jPluginList parses the existing NEO4J_PLUGINS JSON array value and adds
// newPlugin if it is not already present. Returns the (possibly unchanged) JSON array.
// Expected format: ["plugin1","plugin2"].
//
// This is intentionally exported so other controllers (e.g. the fleet
// management reconciler) can perform the same idempotent merge without
// duplicating the logic.
//
// Uses encoding/json rather than ad-hoc string trimming so values with
// whitespace, embedded escapes, or unusual but legal JSON formatting
// round-trip correctly. Empty/whitespace input is treated as the empty
// list (the env var is sometimes literally "" before the operator has
// touched it).
func MergeNeo4jPluginList(existing string, newPlugin string) (string, error) {
	var plugins []string
	if strings.TrimSpace(existing) != "" {
		if err := json.Unmarshal([]byte(existing), &plugins); err != nil {
			return "", fmt.Errorf("failed to parse existing NEO4J_PLUGINS as JSON array: %w", err)
		}
	}

	for _, plugin := range plugins {
		if plugin == newPlugin {
			return existing, nil // already present — no change
		}
	}

	plugins = append(plugins, newPlugin)
	merged, err := json.Marshal(plugins)
	if err != nil {
		return "", fmt.Errorf("failed to marshal merged NEO4J_PLUGINS: %w", err)
	}
	return string(merged), nil
}

func (r *Neo4jPluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1beta1.Neo4jPlugin{}).
		Complete(r)
}

// Helper functions

// getStatefulSetName returns the correct StatefulSet name for the deployment type
func (r *Neo4jPluginReconciler) getStatefulSetName(deployment *DeploymentInfo) string {
	if deployment.Type == "cluster" {
		return deployment.Name + "-server"
	}
	// For standalone, the StatefulSet name is the same as the deployment name
	return deployment.Name
}

// getPodLabels returns the appropriate pod labels for the deployment type
func (r *Neo4jPluginReconciler) getPodLabels(deployment *DeploymentInfo) map[string]string {
	if deployment.Type == "cluster" {
		return map[string]string{
			"app.kubernetes.io/name":     "neo4j",
			"app.kubernetes.io/instance": deployment.Name,
		}
	}
	// For standalone
	return map[string]string{
		"app": deployment.Name,
	}
}

// getExpectedReplicas returns the expected number of replicas for the deployment
func (r *Neo4jPluginReconciler) getExpectedReplicas(deployment *DeploymentInfo) int {
	if deployment.Type == "cluster" {
		cluster := deployment.Object.(*neo4jv1beta1.Neo4jEnterpriseCluster)
		return int(cluster.Spec.Topology.Servers)
	}
	// Standalone always has 1 replica
	return 1
}

func (r *Neo4jPluginReconciler) waitForJobCompletion(ctx context.Context, job *batchv1.Job) error {
	logger := log.FromContext(ctx)
	timeout := time.After(10 * time.Minute)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for job %s/%s to complete", job.Namespace, job.Name)
		case <-ticker.C:
			if err := r.Get(ctx, client.ObjectKeyFromObject(job), job); err != nil {
				return fmt.Errorf("failed to get status of job %s/%s: %w", job.Namespace, job.Name, err)
			}

			if job.Status.Succeeded > 0 {
				logger.Info("Job completed successfully", "job", job.Name, "namespace", job.Namespace)
				return nil
			}

			if job.Status.Failed > 0 {
				// Inspect the JobFailed condition for a richer error message.
				var failureReason, failureMessage string
				for _, condition := range job.Status.Conditions {
					if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
						failureReason = condition.Reason
						failureMessage = condition.Message
						break
					}
				}
				if failureReason != "" || failureMessage != "" {
					return fmt.Errorf(
						"job %s/%s failed (failed=%d): reason=%q message=%q",
						job.Namespace,
						job.Name,
						job.Status.Failed,
						failureReason,
						failureMessage,
					)
				}
				return fmt.Errorf("job %s/%s failed (failed=%d)",
					job.Namespace, job.Name, job.Status.Failed)
			}

			// Still running, continue waiting
		}
	}
}

// PluginType represents different categories of Neo4j plugins
type PluginType int

const (
	PluginTypeEnvironmentOnly PluginType = iota // APOC, APOC Extended - environment variables only
	PluginTypeNeo4jConfig                       // GDS, Bloom, GenAI - neo4j.conf configuration
	PluginTypeHybrid                            // Plugins that use both methods
)

// getPluginType determines the configuration approach for a plugin
func (r *Neo4jPluginReconciler) getPluginType(pluginName string) PluginType {
	switch pluginName {
	case "apoc", "apoc-extended":
		// APOC and APOC Extended in Neo4j 5.26+ use environment variables only
		// APOC-specific settings are no longer supported in neo4j.conf
		return PluginTypeEnvironmentOnly
	case "graph-data-science", "gds":
		// GDS uses neo4j.conf for configuration (gds.*, dbms.security.procedures.*)
		return PluginTypeNeo4jConfig
	case "bloom":
		// Bloom uses neo4j.conf for configuration (dbms.bloom.*, dbms.security.procedures.*)
		return PluginTypeNeo4jConfig
	case "genai":
		// GenAI uses neo4j.conf for configuration and procedure security
		return PluginTypeNeo4jConfig
	case "n10s", "neosemantics":
		// Neo Semantics uses neo4j.conf for configuration
		return PluginTypeNeo4jConfig
	case "graphql":
		// GraphQL plugin uses neo4j.conf for configuration
		return PluginTypeNeo4jConfig
	case "fleet-management":
		// Fleet management plugin requires procedure security configuration.
		// NEO4J_PLUGINS=["fleet-management"] copies the pre-bundled jar from
		// /var/lib/neo4j/products/ — no internet access required.
		return PluginTypeNeo4jConfig
	default:
		// Default to neo4j.conf configuration for unknown plugins
		return PluginTypeNeo4jConfig
	}
}

// isEnvironmentVariableOnlyPlugin checks if the plugin requires environment variable configuration only
func (r *Neo4jPluginReconciler) isEnvironmentVariableOnlyPlugin(pluginName string) bool {
	return r.getPluginType(pluginName) == PluginTypeEnvironmentOnly
}

// requiresAutomaticSecurityConfiguration checks if the plugin needs automatic security settings even without user config
func (r *Neo4jPluginReconciler) requiresAutomaticSecurityConfiguration(pluginName string) bool {
	switch pluginName {
	case "bloom":
		// Bloom requires automatic security configuration:
		// - dbms.security.procedures.unrestricted=bloom.*
		// - dbms.security.http_auth_allowlist=/,/browser.*,/bloom.*
		// - server.unmanaged_extension_classes=com.neo4j.bloom.server=/bloom
		return true
	case "graph-data-science", "gds":
		// GDS requires automatic security configuration:
		// - dbms.security.procedures.unrestricted=gds.* (or allowlist based on sandbox setting)
		return true
	case "fleet-management":
		// Fleet management requires automatic security configuration:
		// - dbms.security.procedures.unrestricted=fleetManagement.*
		// - dbms.security.procedures.allowlist=fleetManagement.*
		return true
	default:
		return false
	}
}

// getAutomaticSecuritySettings returns the required security settings for plugins that need automatic configuration
func (r *Neo4jPluginReconciler) getAutomaticSecuritySettings(pluginName string) map[string]string {
	settings := make(map[string]string)

	switch pluginName {
	case "bloom":
		// Bloom requires these security settings to function properly
		settings["dbms.security.procedures.unrestricted"] = "bloom.*"
		settings["dbms.security.http_auth_allowlist"] = "/,/browser.*,/bloom.*"
		settings["server.unmanaged_extension_classes"] = "com.neo4j.bloom.server=/bloom"
	case "graph-data-science", "gds":
		// GDS default automatic security (can be overridden by user security settings)
		settings["dbms.security.procedures.unrestricted"] = "gds.*,apoc.load.*"
	case "fleet-management":
		settings["dbms.security.procedures.unrestricted"] = "fleetManagement.*"
		settings["dbms.security.procedures.allowlist"] = "fleetManagement.*"
	}

	return settings
}

// filterNeo4jClientConfig filters out plugin-specific configurations that should be handled via environment variables
func (r *Neo4jPluginReconciler) filterNeo4jClientConfig(config map[string]string) map[string]string {
	filtered := make(map[string]string)

	for key, value := range config {
		// Skip APOC-specific settings - these must be handled via environment variables only in Neo4j 5.26+
		if strings.HasPrefix(key, "apoc.") {
			continue
		}

		// Skip non-dynamic GDS settings - these can only be set at startup, not runtime
		if r.isNonDynamicSetting(key) {
			continue
		}

		// Include settings that can be applied dynamically through Neo4j configuration system:
		// - Dynamic GDS settings (gds.* except license_file)
		// - Bloom settings (dbms.bloom.*, server.unmanaged_extension_classes, dbms.security.http_auth_allowlist)
		// - GenAI settings (provider configurations, procedure security)
		// - General Neo4j settings (dbms.*, server.*)
		// - Plugin procedure security settings (dbms.security.procedures.*)
		filtered[key] = value
	}

	return filtered
}

// isSecuritySetting determines if a configuration setting is security-related and must be in neo4j.conf at startup
func (r *Neo4jPluginReconciler) isSecuritySetting(key string) bool {
	return strings.HasPrefix(key, "dbms.security.") ||
		strings.HasPrefix(key, "server.unmanaged_extension_classes") ||
		strings.HasPrefix(key, "dbms.bloom.") // Bloom-specific settings
}

// isNonDynamicSetting determines if a configuration setting can only be applied at startup
func (r *Neo4jPluginReconciler) isNonDynamicSetting(key string) bool {
	nonDynamicSettings := []string{
		// Plugin license files (must be available at startup)
		"gds.enterprise.license_file",
		"dbms.bloom.license_file",
		// Security settings (must be applied at startup via environment variables)
		"dbms.security.procedures.allowlist",
		"dbms.security.procedures.denylist",
		"dbms.security.procedures.unrestricted",
		// Add other non-dynamic settings as needed
	}

	for _, nonDynamic := range nonDynamicSettings {
		if key == nonDynamic {
			return true
		}
	}

	return false
}

// hasRuntimeSecurityConfiguration checks if security configuration contains settings that can be applied at runtime
// Most security settings (allowlist, denylist, unrestricted) are non-dynamic and must be applied as environment variables
func (r *Neo4jPluginReconciler) hasRuntimeSecurityConfiguration(security *neo4jv1beta1.PluginSecurity) bool {
	// Currently, all common security settings are non-dynamic in Neo4j 5.26+
	// They must be applied as environment variables at StatefulSet creation time
	// Future: if there are any dynamic security settings, check for them here
	return false
}

// applyAPOCSecurityConfiguration applies APOC security configuration without using Neo4j client configuration
// APOC security is handled via environment variables and procedure allowlists in Neo4j configuration
func (r *Neo4jPluginReconciler) applyAPOCSecurityConfiguration(ctx context.Context, plugin *neo4jv1beta1.Neo4jPlugin, deployment *DeploymentInfo) error {
	logger := log.FromContext(ctx)

	if plugin.Spec.Security == nil {
		return nil
	}

	// For APOC security configuration, we need to set procedure allowlists via Neo4j configuration
	// but not APOC-specific settings
	if len(plugin.Spec.Security.AllowedProcedures) > 0 || len(plugin.Spec.Security.DeniedProcedures) > 0 {
		var neo4jClient *neo4jclient.Client
		var err error

		if deployment.Type == "cluster" {
			cluster := deployment.Object.(*neo4jv1beta1.Neo4jEnterpriseCluster)
			neo4jClient, err = neo4jclient.NewClientForEnterprise(cluster, r.Client, getClusterAdminSecretName(cluster))
		} else {
			standalone := deployment.Object.(*neo4jv1beta1.Neo4jEnterpriseStandalone)
			neo4jClient, err = neo4jclient.NewClientForEnterpriseStandalone(standalone, r.Client, getStandaloneAdminSecretName(standalone))
		}

		if err != nil {
			return fmt.Errorf("failed to create Neo4j client for security configuration: %w", err)
		}
		defer neo4jClient.Close()

		// In Neo4j 5.26+, all security settings are applied as environment variables
		// Skip runtime security configuration for consistency
		logger.Info("Skipping APOC runtime security configuration - all security settings are applied as environment variables in Neo4j 5.26+")
	}

	logger.Info("APOC security configuration applied successfully")
	return nil
}
