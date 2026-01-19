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
	"fmt"
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

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
	neo4jclient "github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

const (
	// DefaultAdminSecretNamePlugin is the default name for admin credentials secret
	DefaultAdminSecretNamePlugin = "neo4j-admin-secret"
)

// getAdminSecretName safely extracts the admin secret name from cluster spec with fallback to default
func getClusterAdminSecretName(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
	if cluster.Spec.Auth != nil && cluster.Spec.Auth.AdminSecret != "" {
		return cluster.Spec.Auth.AdminSecret
	}
	return DefaultAdminSecretNamePlugin
}

// getStandaloneAdminSecretName safely extracts the admin secret name from standalone spec with fallback to default
func getStandaloneAdminSecretName(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) string {
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
}

// PluginFinalizer is the finalizer for Neo4j plugin resources
const PluginFinalizer = "neo4j.neo4j.com/plugin-finalizer"

//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jplugins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jplugins/finalizers,verbs=update

// Reconcile handles the reconciliation of Neo4jPlugin resources
func (r *Neo4jPluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jPlugin instance
	plugin := &neo4jv1alpha1.Neo4jPlugin{}
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

	// Get target deployment (cluster or standalone)
	deployment, err := r.getTargetDeployment(ctx, plugin)
	if err != nil {
		logger.Error(err, "Failed to get target deployment")
		r.updatePluginStatus(ctx, plugin, "Failed", fmt.Sprintf("Target deployment not found: %v", err))
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
		return ctrl.Result{}, nil // Don't return error - status is set correctly
	}

	// Update status to "Ready"
	r.updatePluginStatus(ctx, plugin, "Ready", "Plugin installed and configured successfully")

	logger.Info("Plugin reconciliation completed")
	return ctrl.Result{}, nil
}

func (r *Neo4jPluginReconciler) handleDeletion(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) (ctrl.Result, error) {
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

func (r *Neo4jPluginReconciler) getTargetDeployment(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) (*DeploymentInfo, error) {
	// Try Neo4jEnterpriseCluster first
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
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
	standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{}
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

// getTargetCluster is deprecated but kept for backward compatibility
func (r *Neo4jPluginReconciler) getTargetCluster(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) (*neo4jv1alpha1.Neo4jEnterpriseCluster, error) {
	deployment, err := r.getTargetDeployment(ctx, plugin)
	if err != nil {
		return nil, err
	}

	if deployment.Type != "cluster" {
		return nil, fmt.Errorf("target deployment %s is not a cluster", plugin.Spec.ClusterRef)
	}

	if !deployment.IsReady {
		return nil, fmt.Errorf("cluster %s is not ready", deployment.Name)
	}

	return deployment.Object.(*neo4jv1alpha1.Neo4jEnterpriseCluster), nil
}

func (r *Neo4jPluginReconciler) installDependency(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin, _ *DeploymentInfo, dep neo4jv1alpha1.PluginDependency) error {
	logger := log.FromContext(ctx)

	logger.Info("Installing plugin dependency", "dependency", dep.Name, "constraint", dep.VersionConstraint)

	// Create dependency plugin resource
	depPlugin := &neo4jv1alpha1.Neo4jPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-dep", plugin.Name, dep.Name),
			Namespace: plugin.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "neo4j-plugin",
				"app.kubernetes.io/instance":  plugin.Spec.ClusterRef,
				"app.kubernetes.io/component": "dependency",
				"neo4j.plugin/parent":         plugin.Name,
			},
		},
		Spec: neo4jv1alpha1.Neo4jPluginSpec{
			ClusterRef: plugin.Spec.ClusterRef,
			Name:       dep.Name,
			Version:    dep.VersionConstraint,
			Enabled:    true,
		},
	}

	// Create or update dependency plugin
	if err := r.Create(ctx, depPlugin); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create dependency plugin: %w", err)
	}

	// Wait for dependency to be ready
	return r.waitForPluginReady(ctx, depPlugin)
}

func (r *Neo4jPluginReconciler) waitForPluginReady(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) error {
	logger := log.FromContext(ctx)

	// Wait for plugin to be in Ready state with timeout
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for plugin %s to be ready", plugin.Name)
		case <-ticker.C:
			current := &neo4jv1alpha1.Neo4jPlugin{}
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

func (r *Neo4jPluginReconciler) restartNeo4jInstances(ctx context.Context, deployment *DeploymentInfo) error {
	logger := log.FromContext(ctx)
	logger.Info("Restarting Neo4j instances to load plugin", "type", deployment.Type)

	// Get the StatefulSet for the deployment
	sts := &appsv1.StatefulSet{}
	stsKey := types.NamespacedName{
		Name:      r.getStatefulSetName(deployment),
		Namespace: deployment.Namespace,
	}

	if err := r.Get(ctx, stsKey, sts); err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Trigger rolling restart by updating an annotation
	if sts.Spec.Template.Annotations == nil {
		sts.Spec.Template.Annotations = make(map[string]string)
	}
	sts.Spec.Template.Annotations["neo4j.neo4j.com/plugin-restart"] = time.Now().Format(time.RFC3339)

	if err := r.Update(ctx, sts); err != nil {
		return fmt.Errorf("failed to trigger StatefulSet restart: %w", err)
	}

	logger.Info("Triggered rolling restart of Neo4j instances")
	return nil
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
		case <-timeout:
			return fmt.Errorf("timeout waiting for cluster to be ready")
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
				logger.Info("Cluster is ready")
				return nil
			}

			logger.Info("Waiting for pods to be ready")
		}
	}
}

func (r *Neo4jPluginReconciler) verifyPluginLoaded(ctx context.Context, neo4jClient *neo4jclient.Client, plugin *neo4jv1alpha1.Neo4jPlugin) error {
	// Verify plugin is loaded by calling CALL dbms.components() or similar
	components, err := neo4jClient.GetLoadedComponents(ctx)
	if err != nil {
		return fmt.Errorf("failed to get loaded components: %w", err)
	}

	for _, component := range components {
		if component.Name == plugin.Spec.Name {
			return nil
		}
	}

	return fmt.Errorf("plugin %s not found in loaded components", plugin.Spec.Name)
}

func (r *Neo4jPluginReconciler) configurePlugin(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin, deployment *DeploymentInfo) error {
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
		cluster := deployment.Object.(*neo4jv1alpha1.Neo4jEnterpriseCluster)
		neo4jClient, err = neo4jclient.NewClientForEnterprise(cluster, r.Client, getClusterAdminSecretName(cluster))
	} else {
		standalone := deployment.Object.(*neo4jv1alpha1.Neo4jEnterpriseStandalone)
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

func (r *Neo4jPluginReconciler) applySecurityConfiguration(ctx context.Context, neo4jClient *neo4jclient.Client, plugin *neo4jv1alpha1.Neo4jPlugin) error {
	logger := log.FromContext(ctx)

	// In Neo4j 5.26+, most security settings (allowlist, denylist, unrestricted) are non-dynamic
	// and must be applied as environment variables at startup, not at runtime
	logger.Info("Skipping runtime security configuration - all security settings are applied as environment variables in Neo4j 5.26+")

	// Future: If there are any dynamic security settings that can be applied at runtime,
	// add them here. Currently, all common security settings are non-dynamic.

	return nil
}

func hardenedPluginPodSecurityContext() *corev1.PodSecurityContext {
	uid := int64(7474)
	return &corev1.PodSecurityContext{
		RunAsUser:    ptr.To(uid),
		RunAsGroup:   ptr.To(uid),
		FSGroup:      ptr.To(uid),
		RunAsNonRoot: ptr.To(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

func hardenedPluginContainerSecurityContext() *corev1.SecurityContext {
	uid := int64(7474)
	return &corev1.SecurityContext{
		RunAsUser:                ptr.To(uid),
		RunAsGroup:               ptr.To(uid),
		RunAsNonRoot:             ptr.To(true),
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

func (r *Neo4jPluginReconciler) uninstallPlugin(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) error {
	logger := log.FromContext(ctx)

	// Get target deployment
	deployment, err := r.getTargetDeployment(ctx, plugin)
	if err != nil {
		// If deployment is not found, consider plugin already uninstalled
		logger.Info("Target deployment not found, considering plugin uninstalled")
		return nil
	}

	// Remove plugin from deployment
	if err := r.removePluginFromDeployment(ctx, plugin, deployment); err != nil {
		return fmt.Errorf("failed to remove plugin from deployment: %w", err)
	}

	// Clean up plugin dependencies
	if err := r.cleanupDependencies(ctx, plugin); err != nil {
		logger.Error(err, "Failed to cleanup dependencies")
	}

	logger.Info("Plugin uninstalled successfully")
	return nil
}

func (r *Neo4jPluginReconciler) removePluginFromDeployment(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin, deployment *DeploymentInfo) error {
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

	// Wait for job completion
	return r.waitForJobCompletion(ctx, removeJob)
}

func (r *Neo4jPluginReconciler) cleanupDependencies(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin) error {
	// Clean up dependency plugins created for this plugin
	dependencyPlugins := &neo4jv1alpha1.Neo4jPluginList{}
	if err := r.List(ctx, dependencyPlugins, client.InNamespace(plugin.Namespace), client.MatchingLabels{
		"neo4j.plugin/parent": plugin.Name,
	}); err != nil {
		return fmt.Errorf("failed to list dependency plugins: %w", err)
	}

	for _, depPlugin := range dependencyPlugins.Items {
		if err := r.Delete(ctx, &depPlugin); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete dependency plugin %s: %w", depPlugin.Name, err)
		}
	}

	return nil
}

func (r *Neo4jPluginReconciler) updatePluginStatus(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin, phase, message string) {
	update := func() error {
		latest := &neo4jv1alpha1.Neo4jPlugin{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(plugin), latest); err != nil {
			return err
		}
		latest.Status.Phase = phase
		latest.Status.Message = message
		latest.Status.ObservedGeneration = latest.Generation
		condition := metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             phase,
			Message:            message,
		}
		if phase == "Failed" || phase == "Installing" {
			condition.Status = metav1.ConditionFalse
		}
		found := false
		for i, cond := range latest.Status.Conditions {
			if cond.Type == condition.Type {
				latest.Status.Conditions[i] = condition
				found = true
				break
			}
		}
		if !found {
			latest.Status.Conditions = append(latest.Status.Conditions, condition)
		}
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
func (r *Neo4jPluginReconciler) installPluginViaEnvironment(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin, deployment *DeploymentInfo) error {
	logger := log.FromContext(ctx)
	logger.Info("Installing plugin via NEO4J_PLUGINS environment variable", "plugin", plugin.Spec.Name)

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

	// Add plugin configuration as environment variables
	for key, value := range plugin.Spec.Config {
		envVarName := fmt.Sprintf("NEO4J_%s", strings.ToUpper(strings.ReplaceAll(key, ".", "_")))
		neo4jContainer.Env = append(neo4jContainer.Env, corev1.EnvVar{
			Name:  envVarName,
			Value: value,
		})
	}

	// Add required procedure security settings if the plugin needs them and they're not environment-variable only
	if r.getPluginType(plugin.Spec.Name) != PluginTypeEnvironmentOnly {
		requiredSettings := r.getRequiredProcedureSecuritySettings(plugin.Spec.Name)
		for key, value := range requiredSettings {
			envVarName := fmt.Sprintf("NEO4J_%s", strings.ToUpper(strings.ReplaceAll(key, ".", "_")))

			// Check if this environment variable already exists
			exists := false
			for i, env := range neo4jContainer.Env {
				if env.Name == envVarName {
					// Merge values for allowlist/unrestricted settings
					if strings.Contains(key, "allowlist") || strings.Contains(key, "unrestricted") {
						if !strings.Contains(env.Value, value) {
							neo4jContainer.Env[i].Value = env.Value + "," + value
						}
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
			// Update existing NEO4J_PLUGINS - parse and add all new plugins
			currentValue := pluginsEnvVar.Value
			for _, plugin := range pluginsToInstall {
				quotedPlugin := fmt.Sprintf("\"%s\"", plugin)
				if !strings.Contains(currentValue, quotedPlugin) {
					if currentValue == "[]" || currentValue == "" {
						pluginsEnvVar.Value = fmt.Sprintf("[\"%s\"]", plugin)
					} else {
						// Remove closing bracket and add new plugin
						if strings.HasSuffix(currentValue, "]") {
							currentValue = currentValue[:len(currentValue)-1]
						}
						pluginsEnvVar.Value = fmt.Sprintf("%s,\"%s\"]", currentValue, plugin)
					}
				}
			}
		}

		// Add plugin-specific configuration as environment variables
		for key, value := range plugin.Spec.Config {
			envVarName := fmt.Sprintf("NEO4J_%s", strings.ToUpper(strings.ReplaceAll(key, ".", "_")))
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

		// Start with automatic security settings for plugins that require them
		userSecuritySettings := r.getAutomaticSecuritySettings(plugin.Spec.Name)

		// Add user-provided security settings as environment variables (non-dynamic settings only)
		if plugin.Spec.Security != nil {

			// Convert allowed procedures to allowlist setting
			if len(plugin.Spec.Security.AllowedProcedures) > 0 {
				allowedList := strings.Join(plugin.Spec.Security.AllowedProcedures, ",")
				userSecuritySettings["dbms.security.procedures.allowlist"] = allowedList
			}

			// Convert denied procedures to denylist setting
			if len(plugin.Spec.Security.DeniedProcedures) > 0 {
				deniedList := strings.Join(plugin.Spec.Security.DeniedProcedures, ",")
				userSecuritySettings["dbms.security.procedures.denylist"] = deniedList
			}

			// Convert sandbox setting to unrestricted (inverted logic)
			if plugin.Spec.Security.Sandbox {
				// Sandbox mode means restricted procedures - use allowlist only, no unrestricted
			} else {
				// Non-sandbox mode may need unrestricted procedures (use allowed list as unrestricted)
				if len(plugin.Spec.Security.AllowedProcedures) > 0 {
					allowedList := strings.Join(plugin.Spec.Security.AllowedProcedures, ",")
					userSecuritySettings["dbms.security.procedures.unrestricted"] = allowedList
				}
			}

		}

		// Apply security settings as environment variables (both automatic and user-provided)
		for key, value := range userSecuritySettings {
			envVarName := fmt.Sprintf("NEO4J_%s", strings.ToUpper(strings.ReplaceAll(key, ".", "_")))
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

		return r.Update(ctx, currentSts)
	})
	if err != nil {
		return fmt.Errorf("failed to update StatefulSet with plugin configuration: %w", err)
	}

	logger.Info("Successfully updated StatefulSet with plugin configuration", "plugin", pluginName)

	// Note: ConfigMap updates for standalone deployments are now handled earlier in reconcile flow
	// before connectivity checks to ensure security settings are applied before Neo4j starts

	return nil
}

// updateStandaloneConfigMapForPlugin updates the ConfigMap for standalone deployments with plugin security settings
func (r *Neo4jPluginReconciler) updateStandaloneConfigMapForPlugin(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin, deployment *DeploymentInfo) error {
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
	standalone := deployment.Object.(*neo4jv1alpha1.Neo4jEnterpriseStandalone)

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

	// Add plugin settings to neo4j.conf if they don't already exist
	updatedConf := currentConf
	for key, value := range allSettings {
		settingLine := fmt.Sprintf("%s=%s", key, value)
		if !strings.Contains(updatedConf, settingLine) {
			// Add a comment and the setting
			comment := fmt.Sprintf("\n# %s plugin configuration", plugin.Spec.Name)
			updatedConf += comment + "\n" + settingLine + "\n"
			logger.Info("Adding plugin setting to ConfigMap", "setting", settingLine)
		} else {
			logger.Info("Plugin setting already present in ConfigMap", "setting", settingLine)
		}
	}

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
func (r *Neo4jPluginReconciler) restartStandalonePods(ctx context.Context, standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) error {
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
	}

	return settings
}

// isDeploymentFunctional checks if the deployment is actually functional by testing Neo4j connectivity
func (r *Neo4jPluginReconciler) isDeploymentFunctional(ctx context.Context, deployment *DeploymentInfo) bool {
	logger := log.FromContext(ctx)

	// For clusters, try to connect and verify cluster formation
	if deployment.Type == "cluster" {
		cluster := deployment.Object.(*neo4jv1alpha1.Neo4jEnterpriseCluster)
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
		standalone := deployment.Object.(*neo4jv1alpha1.Neo4jEnterpriseStandalone)
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

// addPluginToList parses the existing NEO4J_PLUGINS JSON array and adds a new plugin
func (r *Neo4jPluginReconciler) addPluginToList(existing string, newPlugin string) (string, error) {
	// Simple JSON array manipulation for plugin list
	// Expected format: ["plugin1", "plugin2"]

	// Remove existing brackets and quotes, split by comma
	cleaned := strings.Trim(existing, "[]")
	cleaned = strings.ReplaceAll(cleaned, "\"", "")

	var plugins []string
	if cleaned != "" {
		plugins = strings.Split(cleaned, ",")
		for i := range plugins {
			plugins[i] = strings.TrimSpace(plugins[i])
		}
	}

	// Check if plugin already exists
	for _, plugin := range plugins {
		if plugin == newPlugin {
			return existing, nil // Plugin already in list
		}
	}

	// Add new plugin
	plugins = append(plugins, newPlugin)

	// Rebuild JSON array
	var quotedPlugins []string
	for _, plugin := range plugins {
		quotedPlugins = append(quotedPlugins, fmt.Sprintf("\"%s\"", plugin))
	}

	return fmt.Sprintf("[%s]", strings.Join(quotedPlugins, ",")), nil
}

func (r *Neo4jPluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1alpha1.Neo4jPlugin{}).
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

// getPluginsPVCName returns the correct PVC name for plugins based on deployment type
// Since plugins currently use EmptyDir, plugin installation will copy to running pods directly
func (r *Neo4jPluginReconciler) getPluginsPVCName(deployment *DeploymentInfo) string {
	// Since plugins use EmptyDir volumes, we'll need to copy plugins directly to pods
	// This is a placeholder - actual implementation should copy to running pod containers
	// For now, return a shared volume name that can be used by init containers
	return "plugin-downloads"
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
		cluster := deployment.Object.(*neo4jv1alpha1.Neo4jEnterpriseCluster)
		return int(cluster.Spec.Topology.Servers)
	}
	// Standalone always has 1 replica
	return 1
}

func (r *Neo4jPluginReconciler) buildRegistryEnvVars(registry *neo4jv1alpha1.PluginRegistry) []corev1.EnvVar {
	var envVars []corev1.EnvVar

	if registry.AuthSecret != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name: "REPO_USERNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: registry.AuthSecret,
					},
					Key: "username",
				},
			},
		})

		envVars = append(envVars, corev1.EnvVar{
			Name: "REPO_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: registry.AuthSecret,
					},
					Key: "password",
				},
			},
		})
	}

	return envVars
}

func (r *Neo4jPluginReconciler) waitForJobCompletion(ctx context.Context, job *batchv1.Job) error {
	logger := log.FromContext(ctx)
	timeout := time.After(10 * time.Minute)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("job completion timeout")
		case <-ticker.C:
			if err := r.Get(ctx, client.ObjectKeyFromObject(job), job); err != nil {
				return fmt.Errorf("failed to get job status: %w", err)
			}

			if job.Status.Succeeded > 0 {
				logger.Info("Job completed successfully")
				return nil
			}

			if job.Status.Failed > 0 {
				return fmt.Errorf("job failed")
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
func (r *Neo4jPluginReconciler) hasRuntimeSecurityConfiguration(security *neo4jv1alpha1.PluginSecurity) bool {
	// Currently, all common security settings are non-dynamic in Neo4j 5.26+
	// They must be applied as environment variables at StatefulSet creation time
	// Future: if there are any dynamic security settings, check for them here
	return false
}

// applyAPOCSecurityConfiguration applies APOC security configuration without using Neo4j client configuration
// APOC security is handled via environment variables and procedure allowlists in Neo4j configuration
func (r *Neo4jPluginReconciler) applyAPOCSecurityConfiguration(ctx context.Context, plugin *neo4jv1alpha1.Neo4jPlugin, deployment *DeploymentInfo) error {
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
			cluster := deployment.Object.(*neo4jv1alpha1.Neo4jEnterpriseCluster)
			neo4jClient, err = neo4jclient.NewClientForEnterprise(cluster, r.Client, getClusterAdminSecretName(cluster))
		} else {
			standalone := deployment.Object.(*neo4jv1alpha1.Neo4jEnterpriseStandalone)
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
