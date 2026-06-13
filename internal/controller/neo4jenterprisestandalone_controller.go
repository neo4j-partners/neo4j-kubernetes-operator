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
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	neo4jclient "github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/validation"
)

// Neo4jEnterpriseStandaloneReconciler reconciles a Neo4jEnterpriseStandalone object
type Neo4jEnterpriseStandaloneReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Recorder         record.EventRecorder
	RequeueAfter     time.Duration
	Validator        *validation.StandaloneValidator
	ConfigMapManager *ConfigMapManager
}

func podSecurityContextForStandalone(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *corev1.PodSecurityContext {
	if standalone.Spec.SecurityContext != nil && standalone.Spec.SecurityContext.PodSecurityContext != nil {
		return standalone.Spec.SecurityContext.PodSecurityContext
	}
	return resources.DefaultNeo4jPodSecurityContext()
}

func containerSecurityContextForStandalone(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *corev1.SecurityContext {
	if standalone.Spec.SecurityContext != nil && standalone.Spec.SecurityContext.ContainerSecurityContext != nil {
		return standalone.Spec.SecurityContext.ContainerSecurityContext
	}
	return resources.DefaultNeo4jContainerSecurityContext()
}

// standaloneImagePullSecrets converts the standalone's image pull secret names to []corev1.LocalObjectReference.
func standaloneImagePullSecrets(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) []corev1.LocalObjectReference {
	if len(standalone.Spec.Image.PullSecrets) == 0 {
		return nil
	}
	refs := make([]corev1.LocalObjectReference, 0, len(standalone.Spec.Image.PullSecrets))
	for _, name := range standalone.Spec.Image.PullSecrets {
		if name == "" {
			continue
		}
		refs = append(refs, corev1.LocalObjectReference{Name: name})
	}
	return refs
}

const (
	// StandaloneFinalizer is the finalizer for Neo4j enterprise standalone deployments
	StandaloneFinalizer = "neo4j.neo4j.com/standalone-finalizer"
)

//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterprisestandalones,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterprisestandalones/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterprisestandalones/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cert-manager.io,resources=issuers,verbs=get;list;watch
//+kubebuilder:rbac:groups=cert-manager.io,resources=clusterissuers,verbs=get;list;watch
//+kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch

func (r *Neo4jEnterpriseStandaloneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jEnterpriseStandalone instance
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	if err := r.Get(ctx, req.NamespacedName, standalone); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Neo4jEnterpriseStandalone resource not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Neo4jEnterpriseStandalone")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if standalone.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, standalone)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(standalone, StandaloneFinalizer) {
		controllerutil.AddFinalizer(standalone, StandaloneFinalizer)
		if err := r.Update(ctx, standalone); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Validate the standalone configuration
	if validationErrs := r.Validator.ValidateCreate(standalone); len(validationErrs) > 0 {
		logger.Error(fmt.Errorf("validation failed: %v", validationErrs), "Validation failed")
		r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonValidationFailed,
			fmt.Sprintf("Validation failed: %v", validationErrs))

		// Update status to reflect validation failure
		r.setFailedStatus(ctx, standalone, fmt.Sprintf("Validation failed: %v", validationErrs))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Verify the requested StorageClass exists before creating the StatefulSet.
	// A misnamed class (e.g. "standard" on a cluster that doesn't ship one) would
	// otherwise leave the pod Pending indefinitely with no operator-level signal.
	// An empty className is allowed and inherits the cluster default.
	if exists, scErr := storageClassExists(ctx, r.Client, standalone.Spec.Storage.ClassName); scErr != nil {
		logger.Error(scErr, "Failed to look up StorageClass", "storageClass", standalone.Spec.Storage.ClassName)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, scErr
	} else if !exists {
		msg := fmt.Sprintf("StorageClass %q not found; create it or set spec.storage.className to an existing class (or leave it empty to use the cluster default)", standalone.Spec.Storage.ClassName)
		logger.Error(fmt.Errorf("storage class not found"), msg)
		r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonStorageClassNotFound, msg)
		r.setFailedStatus(ctx, standalone, msg)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Reconcile the standalone deployment
	result, err := r.reconcileStandalone(ctx, standalone)
	if err != nil {
		logger.Error(err, "Failed to reconcile standalone deployment")
		r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonReconcileFailed,
			fmt.Sprintf("Failed to reconcile: %v", err))

		// Update status to reflect failure
		r.setFailedStatus(ctx, standalone, fmt.Sprintf("Reconciliation failed: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	return result, nil
}

// handleDeletion handles the deletion of a standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) handleDeletion(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Cleanup resources
	if err := r.cleanupResources(ctx, standalone); err != nil {
		logger.Error(err, "Failed to cleanup resources")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(standalone, StandaloneFinalizer)
	if err := r.Update(ctx, standalone); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully deleted Neo4jEnterpriseStandalone")
	return ctrl.Result{}, nil
}

// reconcileStandalone reconciles the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileStandalone(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Reconcile TLS Certificate (if TLS is enabled)
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		if err := r.reconcileTLSCertificate(ctx, standalone); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to reconcile TLS Certificate: %w", err)
		}
	}

	// Reconcile ConfigMap (always needed for config). The standalone controller
	// owns neo4j.conf (incl. plugin-derived settings) and rolls the pod itself
	// when the rendered conf changes.
	if err := r.reconcileConfigMap(ctx, standalone); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile ConfigMap: %w", err)
	}

	// Reconcile Service
	if err := r.reconcileService(ctx, standalone); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile Service: %w", err)
	}

	// Reconcile MCP resources if enabled
	if err := r.reconcileMCP(ctx, standalone); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile MCP resources: %w", err)
	}

	// Pre-upgrade health check if image tag is changing
	if r.isStandaloneUpgradeRequired(standalone) {
		if blocked := r.preUpgradeHealthCheck(ctx, standalone); blocked {
			logger.Info("Upgrade blocked by pre-upgrade health check, requeueing")
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
		r.Recorder.Event(standalone, corev1.EventTypeNormal, EventReasonUpgradeStarted,
			fmt.Sprintf("Upgrading Neo4j from %s to %s", standalone.Status.Version, standalone.Spec.Image.Tag))
	}

	// Check if PVC storage expansion is needed before creating/updating the StatefulSet.
	if requeue, err := r.reconcileStandaloneStorageExpansion(ctx, standalone); err != nil {
		logger.Error(err, "Failed to reconcile storage expansion")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	} else if requeue {
		logger.Info("Storage expansion completed, requeueing to recreate StatefulSet")
		return ctrl.Result{Requeue: true}, nil
	}

	// Reconcile StatefulSet
	if err := r.reconcileStatefulSet(ctx, standalone); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile StatefulSet: %w", err)
	}

	// Reconcile Ingress (if configured)
	if standalone.Spec.Service != nil && standalone.Spec.Service.Ingress != nil && standalone.Spec.Service.Ingress.Enabled {
		if err := r.reconcileIngress(ctx, standalone); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to reconcile Ingress: %w", err)
		}
	}

	// Reconcile NetworkPolicy if enabled (issue #128 gap #2 —
	// restricts port 6362 ingress to operator-managed backup pods).
	// Build returns nil when spec.networkPolicy.enabled is unset/false.
	if err := r.reconcileNetworkPolicy(ctx, standalone); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile NetworkPolicy: %w", err)
	}

	// Reconcile OpenShift Route (if configured via spec.service.route)
	if standalone.Spec.Service != nil && standalone.Spec.Service.Route != nil && standalone.Spec.Service.Route.Enabled {
		route := resources.BuildRouteForStandalone(standalone)
		if route != nil {
			route.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(standalone, neo4jv1beta1.GroupVersion.WithKind("Neo4jEnterpriseStandalone"))})

			if err := r.createOrUpdateUnstructured(ctx, route); err != nil {
				if meta.IsNoMatchError(err) {
					logger.Info("Route API not available; skipping Route creation")
				} else {
					return ctrl.Result{}, fmt.Errorf("failed to reconcile Route: %w", err)
				}
			}
		}
	}

	// Reconcile ServiceMonitor for Prometheus scraping (non-fatal if Prometheus Operator not installed)
	if standalone.Spec.Monitoring != nil && standalone.Spec.Monitoring.Enabled {
		if err := r.reconcileServiceMonitor(ctx, standalone); err != nil {
			if meta.IsNoMatchError(err) {
				logger.Info("ServiceMonitor API not available; skipping ServiceMonitor creation")
			} else {
				logger.Info("ServiceMonitor creation failed", "error", err.Error())
			}
		}
	}

	// Update status once at the end
	if err := r.updateStatus(ctx, standalone); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	// Collect live diagnostics when standalone is Ready (non-fatal).
	// Diagnostics are collected by default; only skipped when monitoring is explicitly disabled.
	standaloneMonitoringDisabled := standalone.Spec.Monitoring != nil && !standalone.Spec.Monitoring.Enabled
	if !standaloneMonitoringDisabled && standalone.Status.Phase == "Ready" {
		if diagErr := r.collectStandaloneDiagnostics(ctx, standalone); diagErr != nil {
			logger.Error(diagErr, "Failed to collect standalone diagnostics (non-fatal)")
		}
	}

	// Reconcile Aura Fleet Management registration if enabled (non-fatal if it fails)
	if standalone.Spec.AuraFleetManagement != nil && standalone.Spec.AuraFleetManagement.Enabled {
		if err := r.reconcileAuraFleetManagement(ctx, standalone); err != nil {
			logger.Error(err, "Failed to reconcile Aura Fleet Management registration")
			if r.Recorder != nil {
				r.Recorder.Eventf(standalone, corev1.EventTypeWarning, EventReasonAuraFleetFailed,
					"Aura Fleet Management registration failed: %v", err)
			}
		}
	}

	logger.Info("Successfully reconciled Neo4jEnterpriseStandalone")
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

// ownedStandaloneConfKeysAnnotation records the neo4j.conf setting keys the
// standalone controller rendered on the last reconcile. On the next reconcile it
// lets the controller enforce REMOVALS (keys the user deleted from spec.config)
// while preserving FOREIGN keys merged into the same ConfigMap by other
// controllers — notably a Neo4jPlugin, whose settings the plugin controller
// upserts into this standalone's neo4j.conf. Mirrors the cluster controller's
// env-var ownership annotation (neo4j.com/cluster-controller-env-vars).
const ownedStandaloneConfKeysAnnotation = "neo4j.com/standalone-controller-conf-keys"

// reconcileConfigMap reconciles the standalone's ConfigMap. The standalone
// controller is the single owner of neo4j.conf, folding in the UNION of every
// Neo4jPlugin's derived settings for this standalone (#146) — so plugin keys are
// rendered as operator-owned (and pruned when a plugin is uninstalled) rather
// than patched in afterward by the plugin controller. When the rendered conf
// actually changes, it rolls the pod (Neo4j only re-reads neo4j.conf at startup,
// and a ConfigMap-only change doesn't otherwise restart it).
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileConfigMap(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Fully re-render the operator-owned config from spec each reconcile.
	desired := r.createConfigMap(standalone)
	desiredConf := desired.Data["neo4j.conf"]

	// Fold in plugin-derived conf — the UNION across every Neo4jPlugin CR
	// targeting this standalone — so the standalone controller is the single
	// owner of neo4j.conf (issue #146). These keys become operator-owned (tracked
	// in the annotation below), so an uninstalled plugin's keys are PRUNED on the
	// next reconcile, and there's no after-the-fact ConfigMap patching by the
	// plugin controller to fight with.
	//
	// A LISTING FAILURE MUST BE FATAL here: treating it as "no plugins" would
	// prune the plugin-owned security keys from neo4j.conf and roll the pod with
	// those settings stripped. Returning the error requeues without rewriting the
	// ConfigMap, so the existing (correct) conf is preserved.
	pluginConf, err := r.collectPluginConfSettings(ctx, standalone)
	if err != nil {
		return fmt.Errorf("failed to collect plugin-derived config: %w", err)
	}
	if len(pluginConf) > 0 {
		desiredConf = resources.DedupeNeo4jConf(resources.UpsertNeo4jConfSettings(desiredConf, pluginConf))
	}
	desiredKeys := resources.Neo4jConfSettings(desiredConf) // operator-owned keys this reconcile

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace},
	}

	// Create or update ConfigMap with retry logic to handle resource version conflicts.
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
			// controllerutil.CreateOrUpdate's Get overwrites configMap with the
			// EXISTING object, so the desired state MUST be (re)applied here in the
			// mutate fn — an empty mutate would silently write the stale data back
			// (the cause of stale spec.config keys never clearing on update).
			prevOwned := splitCSVSet(configMap.Annotations[ownedStandaloneConfKeysAnnotation])

			// Decide which EXISTING conf keys to carry forward:
			//   - operator-owned this reconcile (in desiredKeys): SKIP — desiredConf
			//     already holds the authoritative value (scalar, or the additive
			//     UNION computed from spec.config + all plugin CRs). Carrying the
			//     existing value back would re-introduce stale additive tokens from
			//     an uninstalled plugin, so the key would never prune.
			//   - previously operator-owned but no longer (prevOwned ∖ desiredKeys):
			//     SKIP — the user/plugin removed it; don't resurrect it.
			//   - everything else: a genuinely FOREIGN key (added by some other
			//     actor the operator doesn't manage) → preserve it.
			mergeBack := make(map[string]string)
			for k, v := range resources.Neo4jConfSettings(configMap.Data["neo4j.conf"]) {
				if _, owned := desiredKeys[k]; owned {
					continue
				}
				if _, wasOwned := prevOwned[k]; wasOwned {
					continue
				}
				mergeBack[k] = v
			}
			// Start from the freshly-rendered operator conf, then merge the
			// preserved keys back: additive keys union (so plugin tokens survive),
			// scalars add-if-absent (so the operator's re-rendered value wins).
			// Idempotent → no ConfigMap churn and no tug-of-war with the plugin
			// controller (it sees its settings already present and makes no change).
			merged := resources.DedupeNeo4jConf(resources.UpsertNeo4jConfSettings(desiredConf, mergeBack))

			if configMap.Data == nil {
				configMap.Data = make(map[string]string)
			}
			configMap.Data["neo4j.conf"] = merged
			configMap.Data["health.sh"] = desired.Data["health.sh"]

			if configMap.Annotations == nil {
				configMap.Annotations = make(map[string]string)
			}
			configMap.Annotations[ownedStandaloneConfKeysAnnotation] = joinSortedKeys(desiredKeys)

			return controllerutil.SetControllerReference(standalone, configMap, r.Scheme)
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to create or update ConfigMap: %w", err)
	}
	logger.Info("Successfully created or updated ConfigMap", "name", configMap.Name)

	// Rolling the pod when neo4j.conf changes is handled by reconcileStatefulSet,
	// which stamps a hash of the rendered conf onto the pod template so the roll
	// flows through the normal template-apply path: present from pod creation (no
	// deferred extra restart), retried on failure, idempotent when unchanged.
	return nil
}

// collectPluginConfSettings returns the union of neo4j.conf settings derived from
// every enabled Neo4jPlugin targeting this standalone (#146). A list error is
// returned (not swallowed) so the caller can requeue without pruning plugin-owned
// keys from neo4j.conf.
func (r *Neo4jEnterpriseStandaloneReconciler) collectPluginConfSettings(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) (map[string]string, error) {
	// Cluster-first target resolution, mirroring the plugin controller's
	// getTargetDeployment: a Neo4jPlugin's clusterRef resolves to a same-named
	// Neo4jEnterpriseCluster when one exists, otherwise to the standalone. So if a
	// cluster of this name coexists in the namespace, plugins referencing the name
	// target the CLUSTER — fold none of them into this standalone's neo4j.conf.
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	switch err := r.Get(ctx, types.NamespacedName{Name: standalone.Name, Namespace: standalone.Namespace}, cluster); {
	case err == nil:
		return nil, nil // a same-named cluster owns these plugins
	case !errors.IsNotFound(err):
		return nil, fmt.Errorf("checking for a same-named cluster: %w", err)
	}

	plugins := &neo4jv1beta1.Neo4jPluginList{}
	if err := r.List(ctx, plugins, client.InNamespace(standalone.Namespace)); err != nil {
		return nil, fmt.Errorf("listing Neo4jPlugins in %s: %w", standalone.Namespace, err)
	}
	return unionPluginConfSettings(plugins.Items, standalone.Name), nil
}

// standaloneConfigHashAnnotation carries a hash of the rendered neo4j.conf on the
// pod template. Changing it triggers a StatefulSet rolling update (so Neo4j
// re-reads conf at startup); leaving it unchanged is a no-op. Being a function
// of the conf content makes the roll idempotent AND retried — a failed roll
// leaves the hash mismatched, so the next reconcile retries it.
const standaloneConfigHashAnnotation = "neo4j.com/config-hash"

// standaloneConfHash returns a stable hex hash of the rendered neo4j.conf.
func standaloneConfHash(conf string) string {
	sum := sha256.Sum256([]byte(conf))
	return hex.EncodeToString(sum[:])
}

// renderedConfHash reads the standalone's ConfigMap and returns a stable hash of
// its neo4j.conf, for stamping onto the pod template (see reconcileStatefulSet).
// Returns "" when the ConfigMap or its neo4j.conf isn't present yet — the caller
// then skips the stamp rather than rolling on a transient absence.
func (r *Neo4jEnterpriseStandaloneReconciler) renderedConfHash(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) string {
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: standalone.Name + "-config", Namespace: standalone.Namespace}, cm); err != nil {
		return ""
	}
	conf := cm.Data["neo4j.conf"]
	if conf == "" {
		return ""
	}
	return standaloneConfHash(conf)
}

// splitCSVSet parses a comma-separated annotation value into a set.
func splitCSVSet(s string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, k := range strings.Split(s, ",") {
		if k = strings.TrimSpace(k); k != "" {
			set[k] = struct{}{}
		}
	}
	return set
}

// joinSortedKeys returns the map's keys sorted and comma-joined (deterministic
// so the annotation value doesn't churn the ConfigMap between reconciles).
func joinSortedKeys(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// reconcileService reconciles the client-facing Service and the headless
// Service for the standalone deployment.
//
// The headless Service ({name}-headless) is what gives the standalone pod
// a stable DNS name — {name}-0.{name}-headless.<ns>.svc.cluster.local —
// which the backup Job uses to reach port 6362. Without it the Job's
// neo4j-admin --from=<fqdn> argument resolves to nothing and the backup
// fails. The standalone STS's spec.serviceName references this service.
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileService(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Headless Service first — the StatefulSet's spec.serviceName depends on it.
	// Capture the desired spec/labels up front: controllerutil.CreateOrUpdate
	// Gets the live object into its argument before running the mutate fn, so a
	// no-op mutate would drop our desired state on the update path — an existing
	// headless Service would keep its old ports across an operator upgrade (the
	// #184 port-parity additions would never apply). Re-assert the desired
	// fields inside the closure instead. ClusterIP is immutable once set, so it
	// is only assigned on create (empty live value).
	desiredHeadless := r.createHeadlessService(standalone)
	headless := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name:      desiredHeadless.Name,
		Namespace: desiredHeadless.Namespace,
	}}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, headless, func() error {
			if err := controllerutil.SetControllerReference(standalone, headless, r.Scheme); err != nil {
				return err
			}
			headless.Labels = desiredHeadless.Labels
			if headless.Spec.ClusterIP == "" {
				headless.Spec.ClusterIP = desiredHeadless.Spec.ClusterIP
			}
			headless.Spec.Selector = desiredHeadless.Spec.Selector
			headless.Spec.PublishNotReadyAddresses = desiredHeadless.Spec.PublishNotReadyAddresses
			headless.Spec.Ports = desiredHeadless.Spec.Ports
			return nil
		})
		return err
	}); err != nil {
		return fmt.Errorf("failed to create or update headless Service: %w", err)
	}
	logger.Info("Successfully created or updated headless Service", "name", headless.Name)

	// Create the canonical client-facing Service ({name}-client, #215) with
	// an ASSERTIVE mutate — the historical no-op mutate meant spec.service
	// changes never applied to existing Services (#203's headless lesson).
	// ClusterIP is immutable → only set on create.
	desiredClient := r.createService(standalone)
	clientSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: desiredClient.Name, Namespace: desiredClient.Namespace}}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, clientSvc, func() error {
			if err := controllerutil.SetControllerReference(standalone, clientSvc, r.Scheme); err != nil {
				return err
			}
			clientSvc.Labels = desiredClient.Labels
			// MERGE annotations rather than replace: cloud LB / ingress
			// controllers stamp their own annotations on the Service, and a
			// wholesale assign would strip them every reconcile (breaking
			// provider-managed exposure). Trade-off: removing a key from
			// spec.service.annotations doesn't remove it from the live
			// Service — same semantics as the cluster path.
			if clientSvc.Annotations == nil {
				clientSvc.Annotations = map[string]string{}
			}
			for k, v := range desiredClient.Annotations {
				clientSvc.Annotations[k] = v
			}
			clientSvc.Spec.Type = desiredClient.Spec.Type
			clientSvc.Spec.Selector = desiredClient.Spec.Selector
			// Preserve API-assigned NodePorts across reconciles (#228
			// review) — same helper the cluster controller uses; a bare
			// assign would churn node ports on every loop for
			// type=NodePort/LoadBalancer.
			clientSvc.Spec.Ports = mergeServicePorts(clientSvc.Spec.Ports, desiredClient.Spec.Ports)
			clientSvc.Spec.LoadBalancerIP = desiredClient.Spec.LoadBalancerIP
			clientSvc.Spec.LoadBalancerSourceRanges = desiredClient.Spec.LoadBalancerSourceRanges
			clientSvc.Spec.ExternalTrafficPolicy = desiredClient.Spec.ExternalTrafficPolicy
			clientSvc.Spec.SessionAffinity = desiredClient.Spec.SessionAffinity
			return nil
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to create or update client Service: %w", err)
	}
	logger.Info("Successfully created or updated client Service", "name", clientSvc.Name)

	// Deprecated {name}-service alias (#215): plain ClusterIP shim so existing
	// in-cluster connection strings survive one release. The assertive mutate
	// also CONVERTS a pre-rename LoadBalancer/NodePort {name}-service to
	// ClusterIP (external exposure moves to {name}-client — release-noted).
	desiredAlias := r.createAliasService(standalone)
	aliasSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: desiredAlias.Name, Namespace: desiredAlias.Namespace}}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, aliasSvc, func() error {
			if err := controllerutil.SetControllerReference(standalone, aliasSvc, r.Scheme); err != nil {
				return err
			}
			aliasSvc.Labels = desiredAlias.Labels
			if aliasSvc.Annotations == nil {
				aliasSvc.Annotations = map[string]string{}
			}
			for k, v := range desiredAlias.Annotations {
				aliasSvc.Annotations[k] = v
			}
			aliasSvc.Spec.Type = desiredAlias.Spec.Type
			aliasSvc.Spec.Selector = desiredAlias.Spec.Selector
			aliasSvc.Spec.Ports = desiredAlias.Spec.Ports
			return nil
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to create or update deprecated alias Service: %w", err)
	}
	logger.Info("Successfully created or updated deprecated alias Service", "name", aliasSvc.Name)

	// Reconcile OpenShift Route if requested
	if err := r.reconcileRoute(ctx, standalone); err != nil {
		return err
	}

	return nil
}

// createHeadlessService builds the headless Service ({name}-headless) that
// gives the standalone pod a stable DNS identity for backup, peer Bolt
// connections, and any other pod-direct addressing. ClusterIP=None makes
// it headless; the same `app: <name>` selector matches the pod created by
// the StatefulSet.
//
// Ports mirror the client-reachable ports the cluster headless service
// exposes (#184): bolt + http (+ https under cert-manager TLS) for direct
// pod addressing via {name}-0.{name}-headless, plus backup (6362). Combined
// with PublishNotReadyAddresses below, this lets clients reach the pod over
// the headless name even before the readiness probe goes green — the
// client-facing {name}-service ClusterIP does NOT publish not-ready
// addresses, so it can't during startup.
//
// The cluster-only clustering ports (discovery 6000, routing 7688, raft
// 7000, tx 7689) are intentionally omitted: a standalone is a single
// non-clustered node that doesn't run RAFT/discovery, so nothing listens
// on them — exposing dead ports would be misleading.
func (r *Neo4jEnterpriseStandaloneReconciler) createHeadlessService(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *corev1.Service {
	ports := []corev1.ServicePort{
		{
			Name:       "bolt",
			Port:       resources.BoltPort,
			TargetPort: intstr.FromInt(resources.BoltPort),
			Protocol:   corev1.ProtocolTCP,
		},
		{
			Name:       "http",
			Port:       resources.HTTPPort,
			TargetPort: intstr.FromInt(resources.HTTPPort),
			Protocol:   corev1.ProtocolTCP,
		},
		{
			Name:       "backup",
			Port:       resources.BackupPort,
			TargetPort: intstr.FromInt(resources.BackupPort),
			Protocol:   corev1.ProtocolTCP,
		},
	}

	// Mirror the client service's HTTPS exposure under cert-manager TLS.
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		ports = append(ports, corev1.ServicePort{
			Name:       "https",
			Port:       resources.HTTPSPort,
			TargetPort: intstr.FromInt(resources.HTTPSPort),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-headless", standalone.Name),
			Namespace: standalone.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "neo4j",
				"app.kubernetes.io/instance":   standalone.Name,
				"app.kubernetes.io/component":  "standalone-headless",
				"app.kubernetes.io/managed-by": "neo4j-operator",
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			// PublishNotReadyAddresses lets the backup Job resolve the FQDN
			// even during a brief readiness blip. The pod is the same
			// pod whether or not its readiness probe is green at this
			// instant — losing DNS for it during reconciliation noise
			// would otherwise make the backup transiently fail.
			PublishNotReadyAddresses: true,
			Selector: map[string]string{
				"app": standalone.Name,
			},
			Ports: ports,
		},
	}
}

// standaloneOwnedEnvVarsAnnotation lists the env-var names the standalone
// controller rendered into the StatefulSet's neo4j container on the last
// reconcile. It mirrors the cluster controller's neo4j.com/cluster-controller-
// env-vars annotation and lets a subsequent reconcile drop a var the operator
// stopped owning (e.g. a removed spec.config key) WITHOUT clobbering foreign
// vars patched in by the plugin / fleet controllers (NEO4J_PLUGINS, fleet
// token, APOC vars). See mergeEnvVars for the merge semantics.
const standaloneOwnedEnvVarsAnnotation = "neo4j.com/standalone-controller-env-vars"

// standaloneOwnedInitContainersAnnotation / standaloneOwnedVolumesAnnotation
// record the init-container and volume NAMES the standalone controller rendered
// on the last reconcile. They play the same role for init containers / volumes
// that standaloneOwnedEnvVarsAnnotation plays for env vars: on a template apply
// they let the controller DROP an item it used to own but no longer renders
// (e.g. the truststore init container + its CA volume once spec.trustedCASecrets
// is cleared) WITHOUT clobbering foreign items added by another controller (the
// plugin controller's VerifiedDownload init container + auth/CA volumes). See
// mergeOwnedByName.
const (
	standaloneOwnedInitContainersAnnotation = "neo4j.com/standalone-controller-init-containers"
	standaloneOwnedVolumesAnnotation        = "neo4j.com/standalone-controller-volumes"
)

// standaloneTemplateHashAnnotation stores a hash of the operator's DESIRED pod
// template from the previous reconcile. reconcileStatefulSet applies the
// desired template (image, resources, probes, env, volumes…) only when this
// hash changes — comparing the operator's own desired-vs-last-desired rather
// than desired-vs-server-state, so API-server field defaulting can't make every
// reconcile look like a change and roll the pod in a loop.
const standaloneTemplateHashAnnotation = "neo4j.com/standalone-template-hash"

// podTemplateSpecHash returns a stable hash of a pod template. JSON
// marshalling sorts map keys, so the hash is deterministic across reconciles
// for an unchanged template (no spurious rolls). Returns "" on the (practically
// impossible) marshal error, which the caller treats as "changed" — converge
// rather than silently skip. Shared by the standalone and cluster controllers.
func podTemplateSpecHash(t corev1.PodTemplateSpec) string {
	data, err := json.Marshal(t)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// mergeOwnedByName applies the operator's desired items while preserving foreign
// ones, mirroring mergeEnvVars (which does the same for env vars):
//   - desired: applied (wins by name).
//   - previousOwned ∖ desired: DROPPED — an item the operator used to render and
//     no longer does (e.g. the truststore init container + its CA volume once
//     spec.trustedCASecrets is cleared). Without this they'd be mistaken for
//     foreign and re-appended forever.
//   - current ∖ previousOwned ∖ desired: preserved (genuinely foreign — the
//     plugin controller's VerifiedDownload init container + auth/CA volumes).
//
// Output: desired first (operator-owned), then preserved foreign items in their
// original relative order.
func mergeOwnedByName[T any](current, desired []T, previousOwned map[string]struct{}, nameOf func(T) string) []T {
	desiredNames := make(map[string]struct{}, len(desired))
	for _, d := range desired {
		desiredNames[nameOf(d)] = struct{}{}
	}
	out := make([]T, 0, len(current)+len(desired))
	out = append(out, desired...)
	for _, c := range current {
		n := nameOf(c)
		if _, inDesired := desiredNames[n]; inDesired {
			continue // desired already holds it
		}
		if _, owned := previousOwned[n]; owned {
			continue // operator-owned but no longer desired → drop
		}
		out = append(out, c) // foreign → preserve
	}
	return out
}

// sortedNamesCSV returns the items' names (per nameOf) sorted and comma-joined,
// for the owned-init-containers / owned-volumes annotations (stable so they
// don't churn the StatefulSet between reconciles).
func sortedNamesCSV[T any](items []T, nameOf func(T) string) string {
	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, nameOf(it))
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// standaloneEnvVarNames returns the env-var names sorted and comma-joined, for
// the owned-env-vars annotation (stable so it doesn't churn the StatefulSet).
func standaloneEnvVarNames(env []corev1.EnvVar) string {
	names := make([]string, 0, len(env))
	for _, e := range env {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// reconcileStatefulSet reconciles the StatefulSet for the standalone deployment.
//
// CreateOrUpdate's Get overwrites the passed object with the EXISTING cluster
// state on update, so the desired template MUST be (re)applied inside the mutate
// fn. The previous empty mutate (`return nil`) meant template changes — image
// upgrades, resource edits, probe/securityContext/volume changes, updateStrategy
// switches — were silently written back as the stale stored spec and never rolled
// to a running standalone (the upgrade event fired but no upgrade happened).
//
// To avoid rolling the pod on every reconcile (API-server field defaulting makes
// a naive DeepEqual always differ), the desired template is applied only when its
// hash differs from the hash recorded on the previous reconcile. Foreign env vars
// (NEO4J_PLUGINS etc. patched by the plugin / fleet controllers) and foreign pod-
// template annotations (the conf-path config-restart stamp, service-mesh
// injection) are preserved across the apply.
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileStatefulSet(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Create StatefulSet using the standalone configuration
	statefulSet := r.createStatefulSet(ctx, standalone)

	// Stamp a hash of the rendered neo4j.conf onto the desired pod template so a
	// conf change rolls the pod through the NORMAL template-apply path below
	// (Neo4j only reads conf at startup). Doing it here — rather than as a
	// separate post-hoc StatefulSet update — means the hash is present from pod
	// creation (no deferred extra restart) and the roll inherits the apply path's
	// every-reconcile, hash-gated, error-returning retry. reconcileConfigMap has
	// already written the ConfigMap this reconcile (it runs first), so the conf
	// is available; an absent ConfigMap (shouldn't happen) just skips the stamp.
	if confHash := r.renderedConfHash(ctx, standalone); confHash != "" {
		if statefulSet.Spec.Template.Annotations == nil {
			statefulSet.Spec.Template.Annotations = map[string]string{}
		}
		statefulSet.Spec.Template.Annotations[standaloneConfigHashAnnotation] = confHash
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(standalone, statefulSet, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Capture the desired spec + its template hash BEFORE CreateOrUpdate's Get
	// clobbers `statefulSet` with the existing cluster object on update.
	desiredSpec := *statefulSet.Spec.DeepCopy()
	desiredHash := podTemplateSpecHash(desiredSpec.Template)
	desiredEnv := []corev1.EnvVar{}
	if len(desiredSpec.Template.Spec.Containers) > 0 {
		desiredEnv = desiredSpec.Template.Spec.Containers[0].Env
	}

	// Create or update StatefulSet with retry logic to handle resource version conflicts
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, statefulSet, func() error {
			// On update, CreateOrUpdate's Get has replaced `statefulSet` with the
			// stored object (carrying our previously-stamped tracking annotations);
			// on create it still holds the freshly-built desired object (no
			// annotations yet). We branch on the template-hash annotation rather
			// than UID: UID isn't populated by the fake client used in unit tests,
			// and ResourceVersion is set even for new objects (so neither is a
			// reliable create-vs-update signal here).

			// Spec-level fields are cheap to set unconditionally — they don't roll
			// pods on their own and CreateOrUpdate skips the Update when nothing
			// changed. UpdateStrategy must apply so a Rolling↔Recreate switch takes
			// effect; Replicas is normally 1 for a standalone — but leave it alone
			// while a restore is quiescing the instance (it scales the STS to 0).
			// Otherwise we race the restore controller and the scale-to-0 never
			// sticks — the standalone analog of the cluster gate (#117 / #196).
			if !replicasReconciliationPaused(standalone) {
				statefulSet.Spec.Replicas = desiredSpec.Replicas
			}
			statefulSet.Spec.UpdateStrategy = desiredSpec.UpdateStrategy

			// Apply the desired template only when our desired hash differs from the
			// hash recorded last reconcile. Matching hash → leave the stored template
			// alone, preserving foreign env vars and the conf-path restart annotation,
			// and CreateOrUpdate no-ops (no pod disruption). A missing annotation —
			// create, or first reconcile after operator upgrade — is treated as
			// changed so we converge (on create the template already equals desired,
			// so the apply is a harmless no-op that just stamps the tracking).
			if statefulSet.Annotations[standaloneTemplateHashAnnotation] == desiredHash {
				return nil
			}

			// Apply the desired template, preserving foreign additions that other
			// controllers patch directly onto the StatefulSet:
			//   - env vars (plugin / fleet) via mergeEnvVars, which also drops vars
			//     the operator previously owned but no longer does (a removed
			//     spec.config key);
			//   - pod-template annotations (conf-path config-restart stamp, mesh
			//     injection, the plugin controller's neo4j.com/plugin-init-containers
			//     ownership annotation) by overlaying desired onto existing;
			//   - init containers and volumes (the plugin controller's
			//     VerifiedDownload init container + its auth/CA volumes) by name —
			//     otherwise an image/resource upgrade would drop them and the pod
			//     would roll without the verified plugin JAR.
			previousOwned := splitCSVSet(statefulSet.Annotations[standaloneOwnedEnvVarsAnnotation])
			prevOwnedInit := splitCSVSet(statefulSet.Annotations[standaloneOwnedInitContainersAnnotation])
			prevOwnedVolumes := splitCSVSet(statefulSet.Annotations[standaloneOwnedVolumesAnnotation])
			currentEnv := []corev1.EnvVar{}
			if len(statefulSet.Spec.Template.Spec.Containers) > 0 {
				currentEnv = statefulSet.Spec.Template.Spec.Containers[0].Env
			}
			mergedEnv := mergeEnvVars(currentEnv, desiredEnv, previousOwned)
			currentInit := statefulSet.Spec.Template.Spec.InitContainers
			currentVolumes := statefulSet.Spec.Template.Spec.Volumes

			// Re-derive operator-managed pod annotations (Prometheus hints) from
			// the desired template while preserving foreign ones (conf-restart
			// stamp, plugin-init markers, service-mesh injection).
			mergedAnnotations := mergePodTemplateAnnotations(statefulSet.Spec.Template.Annotations, desiredSpec.Template.Annotations)

			desiredTemplate := desiredSpec.Template.DeepCopy()
			statefulSet.Spec.Template = *desiredTemplate
			statefulSet.Spec.Template.Annotations = mergedAnnotations
			if len(statefulSet.Spec.Template.Spec.Containers) > 0 {
				statefulSet.Spec.Template.Spec.Containers[0].Env = mergedEnv
			}
			statefulSet.Spec.Template.Spec.InitContainers = mergeOwnedByName(
				currentInit, desiredTemplate.Spec.InitContainers, prevOwnedInit,
				func(c corev1.Container) string { return c.Name })
			statefulSet.Spec.Template.Spec.Volumes = mergeOwnedByName(
				currentVolumes, desiredTemplate.Spec.Volumes, prevOwnedVolumes,
				func(v corev1.Volume) string { return v.Name })

			r.stampStandaloneStatefulSetTracking(statefulSet, desiredHash, desiredSpec.Template)
			return nil
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to create or update StatefulSet: %w", err)
	}
	logger.Info("Successfully created or updated StatefulSet", "name", statefulSet.Name)

	return nil
}

// stampStandaloneStatefulSetTracking records the desired template hash and the
// operator-owned env-var / init-container / volume names on the StatefulSet so
// the next reconcile can diff against them (template-change detection +
// foreign-item preservation + owned-item removal). `desired` must be the
// pristine operator-rendered template (desiredSpec.Template), not the merged
// one — the annotations track what the operator OWNS, not what's live.
func (r *Neo4jEnterpriseStandaloneReconciler) stampStandaloneStatefulSetTracking(sts *appsv1.StatefulSet, templateHash string, desired corev1.PodTemplateSpec) {
	if sts.Annotations == nil {
		sts.Annotations = map[string]string{}
	}
	var ownedEnv []corev1.EnvVar
	if len(desired.Spec.Containers) > 0 {
		ownedEnv = desired.Spec.Containers[0].Env
	}
	sts.Annotations[standaloneTemplateHashAnnotation] = templateHash
	sts.Annotations[standaloneOwnedEnvVarsAnnotation] = standaloneEnvVarNames(ownedEnv)
	sts.Annotations[standaloneOwnedInitContainersAnnotation] = sortedNamesCSV(desired.Spec.InitContainers, func(c corev1.Container) string { return c.Name })
	sts.Annotations[standaloneOwnedVolumesAnnotation] = sortedNamesCSV(desired.Spec.Volumes, func(v corev1.Volume) string { return v.Name })
}

// reconcileNetworkPolicy reconciles the NetworkPolicy for the standalone
// deployment. Builds nil and returns early when spec.networkPolicy is
// disabled (default) — issue #128 gap #2.
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileNetworkPolicy(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	desired := resources.BuildNetworkPolicyForStandalone(standalone)
	if desired == nil {
		return nil
	}
	if err := controllerutil.SetControllerReference(standalone, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on NetworkPolicy: %w", err)
	}

	existing := &networkingv1.NetworkPolicy{}
	if err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating NetworkPolicy", "name", desired.Name)
			return r.Create(ctx, desired)
		}
		return fmt.Errorf("failed to get NetworkPolicy: %w", err)
	}
	// Update only when the spec or owned metadata actually drifted — avoids
	// ResourceVersion churn, and applyOwnedMetadata preserves labels written by
	// other controllers rather than stomping them.
	changed := false
	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		changed = true
	}
	if applyOwnedMetadata(existing, desired.Annotations, desired.Labels) {
		changed = true
	}
	if changed {
		logger.Info("Updating NetworkPolicy", "name", desired.Name)
		return r.Update(ctx, existing)
	}
	return nil
}

// reconcileIngress reconciles the Ingress for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileIngress(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	ingress := r.createIngress(standalone)
	if ingress == nil {
		return nil
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(standalone, ingress, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Create or update Ingress
	existing := &networkingv1.Ingress{}
	if err := r.Get(ctx, types.NamespacedName{Name: ingress.Name, Namespace: ingress.Namespace}, existing); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating Ingress", "name", ingress.Name)
			if err := r.Create(ctx, ingress); err != nil {
				return fmt.Errorf("failed to create Ingress: %w", err)
			}
		} else {
			return fmt.Errorf("failed to get Ingress: %w", err)
		}
	} else {
		// Update only on real drift; applyOwnedMetadata merges desired
		// annotations/labels while preserving keys written by cert-manager or
		// the ingress controller (a wholesale replace would fight them).
		changed := false
		if !reflect.DeepEqual(existing.Spec, ingress.Spec) {
			existing.Spec = ingress.Spec
			changed = true
		}
		if applyOwnedMetadata(existing, ingress.Annotations, ingress.Labels) {
			changed = true
		}
		if changed {
			logger.Info("Updating Ingress", "name", ingress.Name)
			if err := r.Update(ctx, existing); err != nil {
				return fmt.Errorf("failed to update Ingress: %w", err)
			}
		}
	}

	return nil
}

// reconcileRoute ensures an OpenShift Route exists when requested
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileRoute(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	route := resources.BuildRouteForStandalone(standalone)
	if route == nil {
		return nil
	}

	if err := controllerutil.SetControllerReference(standalone, route, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on route: %w", err)
	}

	desired := route.DeepCopy()

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, route, func() error {
		route.SetLabels(desired.GetLabels())
		route.SetAnnotations(desired.GetAnnotations())
		route.Object["spec"] = desired.Object["spec"]
		return nil
	})
	if err != nil {
		if meta.IsNoMatchError(err) {
			logger.Info("Route API not available; skipping Route reconciliation")
			if r.Recorder != nil {
				r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonRouteAPINotFound, "route.openshift.io/v1 not available; skipping Route reconciliation")
			}
			return nil
		}
		return fmt.Errorf("failed to create or update Route: %w", err)
	}

	logger.Info("Successfully reconciled Route", "name", route.GetName())
	return nil
}

func (r *Neo4jEnterpriseStandaloneReconciler) reconcileMCP(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	if standalone.Spec.MCP == nil || !standalone.Spec.MCP.Enabled {
		return nil
	}

	if service := resources.BuildMCPServiceForStandalone(standalone); service != nil {
		if err := r.createOrUpdateMCPResource(ctx, service, standalone); err != nil {
			return fmt.Errorf("failed to reconcile MCP service: %w", err)
		}
	}

	if deployment := resources.BuildMCPDeploymentForStandalone(standalone); deployment != nil {
		if err := r.createOrUpdateMCPResource(ctx, deployment, standalone); err != nil {
			return fmt.Errorf("failed to reconcile MCP deployment: %w", err)
		}
	}

	if ingress := resources.BuildMCPIngressForStandalone(standalone); ingress != nil {
		if err := r.createOrUpdateMCPResource(ctx, ingress, standalone); err != nil {
			return fmt.Errorf("failed to reconcile MCP ingress: %w", err)
		}
	}

	if err := r.reconcileMCPRoute(ctx, standalone); err != nil {
		return err
	}

	r.warnIfMCPMissingAPOC(ctx, standalone)
	return nil
}

func (r *Neo4jEnterpriseStandaloneReconciler) reconcileMCPRoute(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	route := resources.BuildMCPRouteForStandalone(standalone)
	if route == nil {
		return nil
	}

	if err := controllerutil.SetControllerReference(standalone, route, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on MCP route: %w", err)
	}

	desired := route.DeepCopy()

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, route, func() error {
			route.SetLabels(desired.GetLabels())
			route.SetAnnotations(desired.GetAnnotations())
			route.Object["spec"] = desired.Object["spec"]
			return nil
		})
		return err
	})
	if err != nil {
		if meta.IsNoMatchError(err) {
			logger.Info("Route API not available; skipping MCP Route reconciliation")
			if r.Recorder != nil {
				r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonRouteAPINotFound, "route.openshift.io/v1 not available; skipping MCP Route reconciliation")
			}
			return nil
		}
		return fmt.Errorf("failed to create or update MCP Route: %w", err)
	}

	logger.Info("Successfully reconciled MCP Route", "name", route.GetName())
	return nil
}

func (r *Neo4jEnterpriseStandaloneReconciler) createOrUpdateMCPResource(ctx context.Context, obj client.Object, owner client.Object) error {
	if err := controllerutil.SetControllerReference(owner, obj, r.Scheme); err != nil {
		return err
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
			return nil
		})
		return err
	})
}

func (r *Neo4jEnterpriseStandaloneReconciler) warnIfMCPMissingAPOC(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) {
	if standalone.Spec.MCP == nil || !standalone.Spec.MCP.Enabled || r.Recorder == nil {
		return
	}

	plugins := &neo4jv1beta1.Neo4jPluginList{}
	if err := r.List(ctx, plugins, client.InNamespace(standalone.Namespace)); err != nil {
		log.FromContext(ctx).V(1).Info("Unable to list plugins for MCP APOC check", "error", err)
		return
	}

	for _, plugin := range plugins.Items {
		if plugin.Spec.ClusterRef != standalone.Name || !plugin.Spec.Enabled {
			continue
		}
		if isAPOCPluginName(plugin.Spec.Name) {
			return
		}
	}

	r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonMCPApocMissing,
		"MCP is enabled but APOC is not configured via Neo4jPlugin; MCP may fail in stdio mode and some tools may be unavailable.")
}

// createConfigMap creates a ConfigMap for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) createConfigMap(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *corev1.ConfigMap {
	// Build neo4j.conf content
	var configLines []string

	// Add header comment
	configLines = append(configLines, "# Neo4j Standalone Configuration (5.26+ / 2025.x.x)")
	configLines = append(configLines, "")

	// Add basic server configuration
	configLines = append(configLines, "# Basic Server Configuration")
	configLines = append(configLines, "server.default_listen_address=0.0.0.0")
	configLines = append(configLines, "server.http.enabled=true")
	configLines = append(configLines, "server.http.listen_address=:7474")

	// Add TLS configuration if enabled
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		// Bolt with TLS — listen on all interfaces with TLS required
		configLines = append(configLines, "server.bolt.enabled=true")
		configLines = append(configLines, "server.bolt.listen_address=0.0.0.0:7687")
		configLines = append(configLines, "server.bolt.tls_level=REQUIRED")
		configLines = append(configLines, "")
		configLines = append(configLines, "# TLS Configuration")
		configLines = append(configLines, "server.https.enabled=true")
		configLines = append(configLines, "server.https.listen_address=0.0.0.0:7473")
		configLines = append(configLines, "")
		configLines = append(configLines, "# SSL Policy for HTTPS")
		configLines = append(configLines, "dbms.ssl.policy.https.enabled=true")
		configLines = append(configLines, "dbms.ssl.policy.https.base_directory=/ssl")
		configLines = append(configLines, "dbms.ssl.policy.https.private_key=tls.key")
		configLines = append(configLines, "dbms.ssl.policy.https.public_certificate=tls.crt")
		configLines = append(configLines, "dbms.ssl.policy.https.client_auth=NONE")
		configLines = append(configLines, "dbms.ssl.policy.https.tls_versions=TLSv1.3,TLSv1.2")
		configLines = append(configLines, "")
		configLines = append(configLines, "# SSL Policy for Bolt")
		configLines = append(configLines, "dbms.ssl.policy.bolt.enabled=true")
		configLines = append(configLines, "dbms.ssl.policy.bolt.base_directory=/ssl")
		configLines = append(configLines, "dbms.ssl.policy.bolt.private_key=tls.key")
		configLines = append(configLines, "dbms.ssl.policy.bolt.public_certificate=tls.crt")
		configLines = append(configLines, "dbms.ssl.policy.bolt.client_auth=NONE")
		configLines = append(configLines, "dbms.ssl.policy.bolt.tls_versions=TLSv1.3,TLSv1.2")
		configLines = append(configLines, "")
	} else {
		// Bolt without TLS
		configLines = append(configLines, "server.bolt.enabled=true")
		configLines = append(configLines, "server.bolt.listen_address=:7687")
		configLines = append(configLines, "")
	}

	// Backup listener — required for Neo4jBackup CRs targeting this
	// standalone to reach port 6362. Mirrors the cluster path's settings
	// in internal/resources/cluster.go (server.backup.{enabled,listen_address}).
	// Without these the standalone pod doesn't bind 6362 and the backup
	// Job's neo4j-admin --from=...:6362 connection is refused.
	configLines = append(configLines, "# Backup listener (port 6362)")
	configLines = append(configLines, "server.backup.enabled=true")
	configLines = append(configLines, "server.backup.listen_address=0.0.0.0:6362")
	configLines = append(configLines, "")

	// Metrics-subsystem hardening (unconditional, mirrors cluster path):
	// JMX off (unauthenticated MBeans surface) + CSV off (pod-ephemeral
	// files). See internal/resources/cluster.go for the full rationale.
	// Users who need either subsystem can re-enable via spec.config.
	configLines = append(configLines,
		"# Metrics subsystem hardening",
		"server.metrics.jmx.enabled=false",
		"server.metrics.csv.enabled=false",
		"",
		"# Seed-from-URI providers — register modern providers so every",
		"# documented URI scheme resolves. ServerSeedProvider is version-gated",
		"# (Neo4j 2026.04+); S3SeedProvider deliberately excluded — CloudSeedProvider",
		"# handles s3:// via SDK default credentials.",
		fmt.Sprintf("dbms.databases.seed_from_uri_providers=%s", resources.SeedFromURIProvidersConfigValue(standalone.Spec.Image.Tag)),
		"",
	)

	if standalone.Spec.Monitoring != nil && standalone.Spec.Monitoring.Enabled {
		configLines = append(configLines, strings.Split(resources.BuildMonitoringConfig(standalone.Spec.Monitoring), "\n")...)
		configLines = append(configLines, "")
	}

	// Audit logging — appended AFTER monitoring so audit-driven values
	// override monitoring defaults on shared keys. BuildAuditConfig
	// returns "" when spec.audit is nil, so the call is unconditional.
	if auditCfg := resources.BuildAuditConfig(standalone.Spec.Audit); auditCfg != "" {
		configLines = append(configLines, strings.Split(strings.TrimRight(auditCfg, "\n"), "\n")...)
		configLines = append(configLines, "")
	}

	// Aura Fleet Management configuration
	if standalone.Spec.AuraFleetManagement != nil && standalone.Spec.AuraFleetManagement.Enabled {
		configLines = append(configLines, "# Aura Fleet Management")
		configLines = append(configLines, "dbms.security.procedures.unrestricted=fleetManagement.*")
		configLines = append(configLines, "dbms.security.procedures.allowlist=fleetManagement.*")
		configLines = append(configLines, "")
	}

	// Authentication/Authorization configuration from typed auth fields
	var authGeneratedKeys map[string]bool
	if standalone.Spec.Auth != nil {
		authResult := resources.BuildAuthConfig(standalone.Spec.Auth)
		if authResult.Config != "" {
			configLines = append(configLines, "# Authentication/Authorization Configuration")
			configLines = append(configLines, strings.Split(strings.TrimRight(authResult.Config, "\n"), "\n")...)
			configLines = append(configLines, "")
			authGeneratedKeys = make(map[string]bool, len(authResult.GeneratedKeys))
			for _, key := range authResult.GeneratedKeys {
				authGeneratedKeys[key] = true
			}
		}
	}

	// Add user-provided configuration. SSL policy keys are excluded
	// belt-and-suspenders style — the config validator already rejects
	// dbms.ssl.policy.* / server.bolt.tls_level / server.directories.
	// certificates at apply time, but since
	// server.config.strict_validation.enabled=false elsewhere lets Neo4j
	// silently honour a duplicate-key override, we must not append user
	// values here even if the validator was bypassed somehow.
	// Sort keys for deterministic conf ordering (the cluster path does the same):
	// the ConfigMap is hash-compared, so an unsorted map iteration would reorder
	// lines between reconciles and trigger spurious rolling restarts.
	userConfigKeys := make([]string, 0, len(standalone.Spec.Config))
	for key := range standalone.Spec.Config {
		userConfigKeys = append(userConfigKeys, key)
	}
	sort.Strings(userConfigKeys)
	for _, key := range userConfigKeys {
		if authGeneratedKeys != nil && authGeneratedKeys[key] {
			continue
		}
		if strings.HasPrefix(key, "dbms.ssl.policy.") ||
			key == "server.bolt.tls_level" ||
			key == "server.directories.certificates" {
			continue
		}
		configLines = append(configLines, fmt.Sprintf("%s=%s", key, standalone.Spec.Config[key]))
	}

	// Trusted-CA truststore JVM args (legacy spec.auth.trustStore + new
	// spec.trustedCASecrets). Emitted as `server.jvm.additional=...` so the
	// init container's truststore JKS is picked up by every JVM (incl. OIDC
	// HTTP client, LDAPS, plugin downloads, replication that uses default
	// JVM trust). User-supplied server.jvm.additional values via spec.Config
	// are preserved — Neo4j accepts repeated `server.jvm.additional=...` keys
	// and concatenates them.
	if len(standalone.Spec.TrustedCASecrets) > 0 ||
		(standalone.Spec.Auth != nil && standalone.Spec.Auth.TrustStore != nil) {
		configLines = append(configLines,
			"server.jvm.additional=-Djavax.net.ssl.trustStore=/truststore/truststore.jks",
			"server.jvm.additional=-Djavax.net.ssl.trustStorePassword=changeit",
		)
	}

	// Join all lines, then de-duplicate setting keys (keep last occurrence).
	// Without this, a key emitted by both monitoring and user spec.config
	// (e.g. db.logs.query.threshold) appears twice and CalVer Neo4j refuses to
	// start ("declared multiple times"). See resources.DedupeNeo4jConf.
	neo4jConf := resources.DedupeNeo4jConf(strings.Join(configLines, "\n"))

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-config", standalone.Name),
			Namespace: standalone.Namespace,
		},
		Data: map[string]string{
			"neo4j.conf": neo4jConf,
			"health.sh":  buildStandaloneHealthScript(),
		},
	}
}

// buildStandaloneHealthScript creates a health check script for standalone deployments
func buildStandaloneHealthScript() string {
	return `#!/bin/bash
# Health check script for Neo4j standalone

# Check if Neo4j process is running
if ! (pgrep -f "EnterpriseEntryPoint" > /dev/null || pgrep -f "Neo4jEnterprise" > /dev/null); then
    echo "Neo4j process not running"
    exit 1
fi

# Check if HTTP port is responding
if (echo > /dev/tcp/localhost/7474) >/dev/null 2>&1; then
    echo "Neo4j HTTP port responding - healthy"
    exit 0
fi

echo "Neo4j process running but HTTP port not responding"
exit 1
`
}

// buildStandaloneReadinessProbe creates a readiness probe for standalone deployments.
// The startup probe gates this — initialDelaySeconds is 0 because by the time
// this probe runs, the startup probe has already confirmed Neo4j is responding.
func buildStandaloneReadinessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"/bin/bash", "-c", "/conf/health.sh"},
			},
		},
		InitialDelaySeconds: 0,
		PeriodSeconds:       10,
		TimeoutSeconds:      5,
		FailureThreshold:    3,
	}
}

// buildStandaloneLivenessProbe creates a liveness probe for standalone deployments.
// Gated by the startup probe — only runs after Neo4j is confirmed healthy.
func buildStandaloneLivenessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"/bin/bash", "-c", "/conf/health.sh"},
			},
		},
		InitialDelaySeconds: 0,
		PeriodSeconds:       30,
		TimeoutSeconds:      5,
		FailureThreshold:    3,
	}
}

// buildStandaloneStartupProbe creates a startup probe for standalone deployments.
// This is the only probe that runs during initial Neo4j startup. Readiness and
// liveness probes are disabled until this succeeds.
func buildStandaloneStartupProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"/bin/bash", "-c", "/conf/health.sh"},
			},
		},
		InitialDelaySeconds: 30,
		PeriodSeconds:       10,
		TimeoutSeconds:      5,
		// ~10 minutes (30s initial + 60 * 10s). Matches the cluster startup
		// probe's budget (buildStartupProbe). The previous 5-minute budget was
		// too tight for a 5.26 Enterprise boot (JVM + APOC copy + store
		// recovery) under the CI CPU limit (100m) on a slow/contended runner:
		// the startup probe would exhaust before Neo4j bound :7474, the kubelet
		// would kill and restart the pod, and it CrashLooped instead of ever
		// reaching Ready. The cluster pods survived the same runner because
		// they had the larger budget.
		FailureThreshold: 60,
		SuccessThreshold: 1,
	}
}

// createService creates a Service for the standalone deployment
// tlsCertHasClientSAN reports whether the issued TLS Secret's certificate
// already carries the canonical "{name}-client" SAN (#215). Used to gate the
// one-time rolling restart after the naming migration: Neo4j loads its
// keystore at startup, so until the pod restarts it presents the pre-rename
// cert — and connections to the canonical name would fail hostname
// verification. Restarting BEFORE cert-manager has re-issued would just load
// the old cert again, so the roll is deferred until the SAN is present.
// Best-effort: any read/parse problem reports false (no restart yet).
func (r *Neo4jEnterpriseStandaloneReconciler) tlsCertHasClientSAN(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) bool {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-tls-secret", standalone.Name), Namespace: standalone.Namespace}, secret); err != nil {
		return false
	}
	block, _ := pem.Decode(secret.Data["tls.crt"])
	if block == nil {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	want := fmt.Sprintf("%s-client", standalone.Name)
	for _, dns := range cert.DNSNames {
		if dns == want || strings.HasPrefix(dns, want+".") {
			return true
		}
	}
	return false
}

// createAliasService builds the DEPRECATED {name}-service alias (#215): a
// plain ClusterIP carrying the client ports so existing in-cluster DNS
// connection strings keep working for one release after the rename to
// {name}-client. It deliberately does NOT carry spec.service (type/LB) — the
// canonical Service owns external exposure. Removed in the release after the
// rename; the annotation makes the migration discoverable via kubectl.
func (r *Neo4jEnterpriseStandaloneReconciler) createAliasService(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *corev1.Service {
	ports := []corev1.ServicePort{
		{Name: "http", Port: 7474, TargetPort: intstr.FromInt(7474), Protocol: corev1.ProtocolTCP},
		{Name: "bolt", Port: 7687, TargetPort: intstr.FromInt(7687), Protocol: corev1.ProtocolTCP},
	}
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		ports = append(ports, corev1.ServicePort{Name: "https", Port: 7473, TargetPort: intstr.FromInt(7473), Protocol: corev1.ProtocolTCP})
	}
	// Keep the metrics port on the alias too (#228 review): the monitoring
	// docs promise legacy {name}-service scrape targets keep working for the
	// deprecation release.
	if standalone.Spec.Monitoring != nil && standalone.Spec.Monitoring.Enabled {
		ports = append(ports, corev1.ServicePort{Name: "metrics", Port: resources.MetricsPort, TargetPort: intstr.FromInt(resources.MetricsPort), Protocol: corev1.ProtocolTCP})
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-service", standalone.Name),
			Namespace: standalone.Namespace,
			Annotations: map[string]string{
				"neo4j.com/deprecated-alias-of": fmt.Sprintf("%s-client", standalone.Name),
			},
			Labels: map[string]string{
				"app.kubernetes.io/name":       "neo4j",
				"app.kubernetes.io/instance":   standalone.Name,
				"app.kubernetes.io/component":  "standalone",
				"app.kubernetes.io/managed-by": "neo4j-operator",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"app": standalone.Name},
			Ports:    ports,
		},
	}
}

func (r *Neo4jEnterpriseStandaloneReconciler) createService(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *corev1.Service {
	// Determine service type from spec
	serviceType := corev1.ServiceTypeClusterIP
	if standalone.Spec.Service != nil && standalone.Spec.Service.Type != "" {
		serviceType = corev1.ServiceType(standalone.Spec.Service.Type)
	}

	// Get annotations from spec
	annotations := make(map[string]string)
	if standalone.Spec.Service != nil && standalone.Spec.Service.Annotations != nil {
		for k, v := range standalone.Spec.Service.Annotations {
			annotations[k] = v
		}
	}
	resources.ApplyExternalDNSAnnotation(annotations, standalone.Spec.Service)

	// Build service ports
	ports := []corev1.ServicePort{
		{
			Name:       "http",
			Port:       7474,
			TargetPort: intstr.FromInt(7474),
			Protocol:   corev1.ProtocolTCP,
		},
		{
			Name:       "bolt",
			Port:       7687,
			TargetPort: intstr.FromInt(7687),
			Protocol:   corev1.ProtocolTCP,
		},
	}

	if standalone.Spec.Monitoring != nil && standalone.Spec.Monitoring.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name:       "metrics",
			Port:       resources.MetricsPort,
			TargetPort: intstr.FromInt(resources.MetricsPort),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	// Add HTTPS port if TLS is enabled
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		ports = append(ports, corev1.ServicePort{
			Name:       "https",
			Port:       7473,
			TargetPort: intstr.FromInt(7473),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			// {name}-client is the CANONICAL client-facing Service for both
			// kinds since #215 (clusters always used it). It carries the full
			// spec.service configuration (type/LB/annotations). The legacy
			// {name}-service name survives one release as a ClusterIP alias —
			// see createAliasService.
			Name:        fmt.Sprintf("%s-client", standalone.Name),
			Namespace:   standalone.Namespace,
			Annotations: annotations,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "neo4j",
				"app.kubernetes.io/instance":   standalone.Name,
				"app.kubernetes.io/component":  "standalone",
				"app.kubernetes.io/managed-by": "neo4j-operator",
				// Selected by the ServiceMonitor — present ONLY on the
				// canonical Service so Prometheus doesn't discover the
				// deprecated alias too and double-scrape the pod (#228).
				"neo4j.com/metrics-endpoint": "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
			Selector: map[string]string{
				"app": standalone.Name,
			},
			Ports: ports,
		},
	}

	// Add enhanced features if specified
	if standalone.Spec.Service != nil {
		// LoadBalancer specific configurations
		if standalone.Spec.Service.LoadBalancerIP != "" {
			svc.Spec.LoadBalancerIP = standalone.Spec.Service.LoadBalancerIP
		}
		if len(standalone.Spec.Service.LoadBalancerSourceRanges) > 0 {
			svc.Spec.LoadBalancerSourceRanges = standalone.Spec.Service.LoadBalancerSourceRanges
		}

		// External traffic policy
		if standalone.Spec.Service.ExternalTrafficPolicy != "" {
			svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicy(standalone.Spec.Service.ExternalTrafficPolicy)
		}
	}

	return svc
}

// standalonePrometheusAnnotations returns the Prometheus scrape hints the
// operator adds to the pod template when monitoring is enabled (nil otherwise).
// Single source of truth for these operator-owned annotations.
func standalonePrometheusAnnotations(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) map[string]string {
	if standalone.Spec.Monitoring == nil || !standalone.Spec.Monitoring.Enabled {
		return nil
	}
	return map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/port":   fmt.Sprintf("%d", resources.MetricsPort),
		"prometheus.io/path":   "/metrics",
	}
}

// createStatefulSet creates a StatefulSet for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) createStatefulSet(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *appsv1.StatefulSet {
	replicas := int32(1)
	annotations := map[string]string{}
	for k, v := range standalonePrometheusAnnotations(standalone) {
		annotations[k] = v
	}

	// #215 naming migration: once cert-manager has re-issued the TLS cert
	// with the canonical {name}-client SAN, stamp the template so the pod
	// rolls ONCE and the running server starts presenting it. Gated on the
	// SAN actually being in the Secret — restarting earlier would reload the
	// old cert and leave canonical-name connections failing hostname checks.
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" && r.tlsCertHasClientSAN(ctx, standalone) {
		annotations["neo4j.com/tls-cert-sans"] = "client-v1"
	}

	ports := []corev1.ContainerPort{
		{
			Name:          "http",
			ContainerPort: 7474,
			Protocol:      corev1.ProtocolTCP,
		},
		{
			Name:          "https",
			ContainerPort: 7473,
			Protocol:      corev1.ProtocolTCP,
		},
		{
			Name:          "bolt",
			ContainerPort: 7687,
			Protocol:      corev1.ProtocolTCP,
		},
		{
			Name:          "backup",
			ContainerPort: 6362,
			Protocol:      corev1.ProtocolTCP,
		},
	}

	if standalone.Spec.Monitoring != nil && standalone.Spec.Monitoring.Enabled {
		ports = append(ports, corev1.ContainerPort{
			Name:          "metrics",
			ContainerPort: resources.MetricsPort,
			Protocol:      corev1.ProtocolTCP,
		})
	}

	// Determine StatefulSet update strategy from spec
	updateStrategy := appsv1.StatefulSetUpdateStrategy{
		Type: appsv1.RollingUpdateStatefulSetStrategyType,
	}
	if standalone.Spec.UpgradeStrategy != nil && standalone.Spec.UpgradeStrategy.Strategy == "Recreate" {
		updateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
	}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      standalone.Name,
			Namespace: standalone.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			// ServiceName must reference a headless Service so each pod
			// gets a stable DNS name (<name>-0.<service>.<ns>.svc.cluster.local).
			// The backup Job path uses this FQDN on port 6362 — without a
			// ServiceName + matching headless service, neo4j-admin database
			// backup --from=... can't reach the standalone.
			ServiceName:    fmt.Sprintf("%s-headless", standalone.Name),
			UpdateStrategy: updateStrategy,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": standalone.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					// `app: <name>` is the immutable StatefulSet selector key and
					// must stay. The app.kubernetes.io/* labels are ADDED to the
					// template only (never the selector) so standard tooling and the
					// documented `-l app.kubernetes.io/name=neo4j` selector match
					// standalone pods the way they already match cluster server pods
					// (getLabelsForEnterpriseServer). Adding template labels triggers a
					// one-time rolling restart on upgrade. (#268)
					Labels: map[string]string{
						"app":                          standalone.Name,
						"app.kubernetes.io/name":       "neo4j",
						"app.kubernetes.io/instance":   standalone.Name,
						"app.kubernetes.io/managed-by": "neo4j-operator",
					},
					Annotations: annotations,
				},
				Spec: func() corev1.PodSpec {
					image := fmt.Sprintf("%s:%s", standalone.Spec.Image.Repo, standalone.Spec.Image.Tag)
					var legacyTrustStore *neo4jv1beta1.SecretKeyRef
					if standalone.Spec.Auth != nil {
						legacyTrustStore = standalone.Spec.Auth.TrustStore
					}
					trustedCAs := resources.CollectTrustedCASecrets(legacyTrustStore, standalone.Spec.TrustedCASecrets)
					var initContainers []corev1.Container
					if len(trustedCAs) > 0 {
						initContainers = append(initContainers, resources.BuildTrustStoreInitContainer(image, trustedCAs))
					}
					return corev1.PodSpec{
						InitContainers: initContainers,
						Containers: []corev1.Container{
							{
								Name:            "neo4j",
								Image:           image,
								SecurityContext: containerSecurityContextForStandalone(standalone),
								Ports:           ports,
								Env:             r.buildEnvVars(standalone),
								// Project user-supplied env-from-Secret / env-from-ConfigMap
								// bundles onto the container. Mirrors Neo4jEnterpriseCluster
								// behaviour: cloud creds for seedURI restores live here.
								EnvFrom:      standalone.Spec.ExtraEnvFrom,
								VolumeMounts: r.buildVolumeMounts(standalone),
								Resources: func() corev1.ResourceRequirements {
									if standalone.Spec.Resources != nil {
										return *standalone.Spec.Resources
									}
									return corev1.ResourceRequirements{}
								}(),
								ReadinessProbe: buildStandaloneReadinessProbe(),
								LivenessProbe:  buildStandaloneLivenessProbe(),
								StartupProbe:   buildStandaloneStartupProbe(),
							},
						},
						SecurityContext:  podSecurityContextForStandalone(standalone),
						Volumes:          r.buildVolumes(standalone),
						NodeSelector:     standalone.Spec.NodeSelector,
						Tolerations:      standalone.Spec.Tolerations,
						Affinity:         standalone.Spec.Affinity,
						ImagePullSecrets: standaloneImagePullSecrets(standalone),
					}
				}(),
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "neo4j-data",
						Labels: resources.GetLabelsForPVC(standalone.Name, "data"),
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						StorageClassName: resources.StorageClassNamePtr(standalone.Spec.Storage.ClassName),
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse(standalone.Spec.Storage.Size),
							},
						},
					},
				},
			},
		},
	}
}

// setFailedStatus records a terminal "Failed" phase, refetching the latest
// object and retrying on conflict. The early-return failure paths previously
// wrote the stale reconcile-start object directly (no refetch/retry), dropped
// ObservedGeneration, and one used an event-reason constant as the phase.
//
// ObservedGeneration is stamped from the generation we actually reconciled
// (the reconcile-start object), NOT the refetched latest.Generation. If the
// spec changed mid-reconcile, latest.Generation is newer than what produced
// this failure; claiming we observed it would suppress the re-reconcile the
// new generation deserves. Stamping the older generation leaves
// observedGeneration < generation, the correct "status is stale, reconcile
// pending" signal — and the refetch is purely to win the resourceVersion race.
func (r *Neo4jEnterpriseStandaloneReconciler) setFailedStatus(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone, message string) {
	logger := log.FromContext(ctx)
	reconciledGen := standalone.Generation
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
		if getErr := r.Get(ctx, types.NamespacedName{Name: standalone.Name, Namespace: standalone.Namespace}, latest); getErr != nil {
			return getErr
		}
		latest.Status.Phase = "Failed"
		latest.Status.Message = message
		latest.Status.Ready = false
		latest.Status.ObservedGeneration = reconciledGen
		condStatus, condReason := PhaseToConditionStatus("Failed")
		SetReadyCondition(&latest.Status.Conditions, reconciledGen, condStatus, condReason, message)
		// Mirror onto the in-memory object for the rest of the reconcile.
		standalone.Status.Phase = "Failed"
		standalone.Status.Message = message
		standalone.Status.Ready = false
		return r.Status().Update(ctx, latest)
	})
	if err != nil {
		logger.Error(err, "Failed to update standalone status to Failed")
	}
}

// updateStatus updates the status of the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) updateStatus(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Get the latest version of the resource to avoid conflicts
	latestStandalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	if err := r.Get(ctx, types.NamespacedName{Name: standalone.Name, Namespace: standalone.Namespace}, latestStandalone); err != nil {
		return fmt.Errorf("failed to get latest standalone resource: %w", err)
	}

	// Get the StatefulSet to check its status
	statefulSet := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: standalone.Name, Namespace: standalone.Namespace}, statefulSet); err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Calculate the desired status
	var phase, message string
	var ready bool

	if statefulSet.Status.ReadyReplicas == 1 {
		phase = "Ready"
		message = "Standalone deployment is ready"
		ready = true
	} else {
		phase = "Pending"
		message = "Waiting for standalone deployment to be ready"
		ready = false
	}

	// Check if status actually needs to be updated. The endpoints check
	// compares the canonical Bolt URL, not just non-nil-ness (#228 review):
	// after the #215 rename, an already-Ready standalone would otherwise keep
	// its pre-rename {name}-service URLs in status forever, because nothing
	// else in this condition changes on an operator upgrade.
	expectedBoltHost := fmt.Sprintf("%s-client.%s.svc.cluster.local:7687", standalone.Name, standalone.Namespace)
	endpointsCurrent := latestStandalone.Status.Endpoints != nil &&
		strings.HasSuffix(latestStandalone.Status.Endpoints.Bolt, expectedBoltHost)
	if latestStandalone.Status.Phase == phase &&
		latestStandalone.Status.Message == message &&
		latestStandalone.Status.Ready == ready &&
		latestStandalone.Status.Version == standalone.Spec.Image.Tag &&
		latestStandalone.Status.ObservedGeneration == latestStandalone.Generation &&
		endpointsCurrent {
		logger.V(1).Info("Status unchanged, skipping update")
		return nil
	}

	// Update status on the latest version
	latestStandalone.Status.Phase = phase
	latestStandalone.Status.Message = message
	latestStandalone.Status.Ready = ready
	latestStandalone.Status.Version = standalone.Spec.Image.Tag
	latestStandalone.Status.ObservedGeneration = latestStandalone.Generation

	// Update Ready condition using standard helper
	condStatus, condReason := PhaseToConditionStatus(phase)
	SetReadyCondition(&latestStandalone.Status.Conditions, latestStandalone.Generation, condStatus, condReason, message)

	// Update endpoints — use bolt+s:// when TLS is enabled
	boltScheme := "bolt"
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		boltScheme = "bolt+s"
	}
	hasTLS := standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager"

	// Resolve service type + external IP for the connection-example helper.
	// Defaults to ClusterIP when spec.service is unset; LoadBalancer external
	// IP comes from the live Service status if the cloud assigner has filled
	// it in, otherwise the helper substitutes a `<external-ip>` placeholder.
	serviceType := corev1.ServiceTypeClusterIP
	if standalone.Spec.Service != nil && standalone.Spec.Service.Type != "" {
		serviceType = corev1.ServiceType(standalone.Spec.Service.Type)
	}
	externalIP := standaloneServiceExternalIP(ctx, r.Client, standalone)

	latestStandalone.Status.Endpoints = &neo4jv1beta1.EndpointStatus{
		Bolt:               fmt.Sprintf("%s://%s-client.%s.svc.cluster.local:7687", boltScheme, standalone.Name, standalone.Namespace),
		HTTP:               fmt.Sprintf("http://%s-client.%s.svc.cluster.local:7474", standalone.Name, standalone.Namespace),
		HTTPS:              fmt.Sprintf("https://%s-client.%s.svc.cluster.local:7473", standalone.Name, standalone.Namespace),
		ConnectionExamples: GenerateStandaloneConnectionExamples(standalone.Name, standalone.Namespace, serviceType, externalIP, hasTLS),
	}

	// Update the status
	if err := r.Status().Update(ctx, latestStandalone); err != nil {
		logger.Error(err, "Failed to update status")
		return err
	}

	logger.V(1).Info("Status updated successfully", "phase", phase, "ready", ready)
	return nil
}

// collectStandaloneDiagnostics runs SHOW DATABASES against the standalone instance
// and writes results into status.diagnostics. Non-fatal: errors are surfaced in status only.
func (r *Neo4jEnterpriseStandaloneReconciler) collectStandaloneDiagnostics(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Collecting standalone diagnostics", "standalone", standalone.Name)

	diagnostics := &neo4jv1beta1.StandaloneDiagnosticsStatus{}

	neo4jClient, err := neo4jclient.NewClientForEnterpriseStandalone(standalone, r.Client, getStandaloneAdminSecretName(standalone))
	if err != nil {
		diagnostics.CollectionError = fmt.Sprintf("cannot connect to Neo4j: %v", err)
		return r.updateStandaloneDiagnostics(ctx, standalone, diagnostics)
	}
	defer neo4jClient.Close()

	databases, dbErr := neo4jClient.GetDatabases(ctx)
	if dbErr != nil {
		logger.Error(dbErr, "Failed to collect SHOW DATABASES")
		diagnostics.CollectionError = fmt.Sprintf("SHOW DATABASES failed: %v", dbErr)
	} else {
		for _, d := range databases {
			diagnostics.Databases = append(diagnostics.Databases, neo4jv1beta1.DatabaseDiagnosticInfo{
				Name:            d.Name,
				Status:          d.Status,
				RequestedStatus: d.RequestedStatus,
				Role:            d.Role,
				Default:         d.Default,
			})
		}
	}

	collectUsersAndRoles(ctx, neo4jClient,
		&diagnostics.Users, &diagnostics.UserCount,
		&diagnostics.Roles, &diagnostics.RoleCount,
		&diagnostics.CollectionError, logger)

	now := metav1.Now()
	diagnostics.LastCollected = &now

	return r.updateStandaloneDiagnostics(ctx, standalone, diagnostics)
}

// updateStandaloneDiagnostics persists diagnostics into standalone status with retry.
func (r *Neo4jEnterpriseStandaloneReconciler) updateStandaloneDiagnostics(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone, diagnostics *neo4jv1beta1.StandaloneDiagnosticsStatus) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(standalone), latest); err != nil {
			return err
		}
		latest.Status.Diagnostics = diagnostics
		return r.Status().Update(ctx, latest)
	})
}

// isStandaloneUpgradeRequired returns true if the Neo4j image tag is changing.
func (r *Neo4jEnterpriseStandaloneReconciler) isStandaloneUpgradeRequired(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) bool {
	return standalone.Status.Version != "" && standalone.Status.Version != standalone.Spec.Image.Tag
}

// preUpgradeHealthCheck runs a health check before allowing an image upgrade.
// Returns true if the upgrade should be blocked (health check failed and autoPause enabled).
func (r *Neo4jEnterpriseStandaloneReconciler) preUpgradeHealthCheck(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) bool {
	logger := log.FromContext(ctx)

	// Skip if no upgrade strategy or health check disabled
	if standalone.Spec.UpgradeStrategy == nil || !standalone.Spec.UpgradeStrategy.PreUpgradeHealthCheck {
		return false
	}

	// Only check if standalone is currently Ready
	if standalone.Status.Phase != "Ready" {
		return false
	}

	neo4jClient, err := neo4jclient.NewClientForEnterpriseStandalone(standalone, r.Client, getStandaloneAdminSecretName(standalone))
	if err != nil {
		logger.Error(err, "Pre-upgrade health check: cannot connect to Neo4j")
		if standalone.Spec.UpgradeStrategy.AutoPauseOnFailure {
			r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonUpgradeFailed,
				fmt.Sprintf("Pre-upgrade health check failed (cannot connect): %v", err))
			return true
		}
		return false
	}
	defer neo4jClient.Close()

	// Simple connectivity check
	if err := neo4jClient.VerifyConnectivity(ctx); err != nil {
		logger.Error(err, "Pre-upgrade health check failed")
		if standalone.Spec.UpgradeStrategy.AutoPauseOnFailure {
			r.Recorder.Event(standalone, corev1.EventTypeWarning, EventReasonUpgradeFailed,
				fmt.Sprintf("Pre-upgrade health check failed: %v", err))
			return true
		}
	}

	logger.Info("Pre-upgrade health check passed")
	return false
}

// cleanupResources cleans up resources during deletion
func (r *Neo4jEnterpriseStandaloneReconciler) cleanupResources(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Cleanup based on retention policy (uses spec.storage.retentionPolicy, matching cluster pattern)
	if standalone.Spec.Storage.RetentionPolicy == "Delete" || standalone.Spec.Storage.RetentionPolicy == "" {
		// Delete PVCs. Selector must match labels emitted by resources.GetLabelsForPVC,
		// which is what the standalone StatefulSet's VolumeClaimTemplates are labeled with.
		// Previously used "app=<name>" which only appears on pods, not PVCs — making this a no-op.
		pvcList := &corev1.PersistentVolumeClaimList{}
		if err := r.List(ctx, pvcList, client.InNamespace(standalone.Namespace),
			client.MatchingLabels(resources.PVCSelectorByInstance(standalone.Name))); err != nil {
			logger.Error(err, "Failed to list PVCs")
			return err
		}

		for _, pvc := range pvcList.Items {
			if err := r.Delete(ctx, &pvc); err != nil {
				logger.Error(err, "Failed to delete PVC", "pvc", pvc.Name)
				return err
			}
		}
	}

	return nil
}

// buildEnvVars builds environment variables for the standalone Neo4j container
func (r *Neo4jEnterpriseStandaloneReconciler) buildEnvVars(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) []corev1.EnvVar {
	envVars := []corev1.EnvVar{}

	// Add essential Neo4j environment variables
	envVars = append(envVars, corev1.EnvVar{
		Name:  "NEO4J_EDITION",
		Value: "enterprise",
	})
	envVars = append(envVars, corev1.EnvVar{
		Name:  "NEO4J_ACCEPT_LICENSE_AGREEMENT",
		Value: "yes",
	})
	envVars = append(envVars, corev1.EnvVar{
		Name:  "NEO4J_UDC_PACKAGING",
		Value: resources.OperatorUDCPackagingValue(),
	})

	// Determine auth secret name (use default if not specified)
	authSecretName := "neo4j-admin-secret" // Default secret name
	if standalone.Spec.Auth != nil && standalone.Spec.Auth.AdminSecret != "" {
		authSecretName = standalone.Spec.Auth.AdminSecret
	}

	// Always add auth credentials (either from specified secret or default)
	// This ensures Neo4j doesn't generate a random password
	envVars = append(envVars,
		corev1.EnvVar{
			Name: "DB_USERNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: authSecretName,
					},
					Key: "username",
				},
			},
		},
		corev1.EnvVar{
			Name: "DB_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: authSecretName,
					},
					Key: "password",
				},
			},
		},
		// Set NEO4J_AUTH in the format Neo4j expects (username/password)
		// This combines the username and password from the secret into the standard Neo4j format
		corev1.EnvVar{
			Name:  "NEO4J_AUTH",
			Value: "$(DB_USERNAME)/$(DB_PASSWORD)",
		},
	)

	// Add LDAP system account credentials from Secret (never in ConfigMap)
	if authEnvVars := resources.BuildAuthEnvVars(standalone.Spec.Auth); len(authEnvVars) > 0 {
		envVars = append(envVars, authEnvVars...)
	}

	// Add user-provided environment variables
	envVars = append(envVars, standalone.Spec.Env...)

	// NOTE: NEO4J_PLUGINS for fleet-management is applied via a live StatefulSet patch
	// in reconcileAuraFleetManagement, not baked here, so it merges with other plugins.

	// Set the config directory (always present now)
	envVars = append(envVars, corev1.EnvVar{
		Name:  "NEO4J_CONF",
		Value: "/conf",
	})

	return envVars
}

// buildVolumeMounts builds volume mounts for the standalone Neo4j container
func (r *Neo4jEnterpriseStandaloneReconciler) buildVolumeMounts(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) []corev1.VolumeMount {
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "neo4j-data",
			MountPath: "/data",
		},
	}

	// Add ConfigMap mount (always present now)
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "neo4j-config",
		MountPath: "/conf",
		ReadOnly:  true,
	})

	// Add TLS certificate mount if TLS is enabled
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "neo4j-certs",
			MountPath: "/ssl",
			ReadOnly:  true,
		})
	}

	// Add truststore volume mount when any trusted CA is configured (legacy
	// spec.auth.trustStore or new spec.trustedCASecrets list).
	if len(standalone.Spec.TrustedCASecrets) > 0 ||
		(standalone.Spec.Auth != nil && standalone.Spec.Auth.TrustStore != nil) {
		volumeMounts = append(volumeMounts, resources.TrustStoreVolumeMount)
	}

	// User-supplied extra volume mounts.
	if len(standalone.Spec.ExtraVolumeMounts) > 0 {
		volumeMounts = append(volumeMounts, standalone.Spec.ExtraVolumeMounts...)
	}

	return volumeMounts
}

// buildVolumes builds volumes for the standalone Neo4j pod
func (r *Neo4jEnterpriseStandaloneReconciler) buildVolumes(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) []corev1.Volume {
	volumes := []corev1.Volume{}

	// Add ConfigMap volume (always present now, 0755 for executable health.sh)
	volumes = append(volumes, corev1.Volume{
		Name: "neo4j-config",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: fmt.Sprintf("%s-config", standalone.Name),
				},
				DefaultMode: func() *int32 { mode := int32(0o755); return &mode }(),
			},
		},
	})

	// Add TLS certificate volume if TLS is enabled.
	//
	// DefaultMode 0440 makes the private key at /ssl/tls.key
	// owner+group readable but not world-readable. Neo4j runs as
	// UID/GID 7474 with FSGroup 7474 so both owner and group are
	// the Neo4j process; world-readable key files fail Pod Security
	// "restricted" and CIS Kubernetes baseline checks. Mirrors the
	// cluster path's hardening (internal/resources/cluster.go).
	if standalone.Spec.TLS != nil && standalone.Spec.TLS.Mode == "cert-manager" {
		volumes = append(volumes, corev1.Volume{
			Name: "neo4j-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  fmt.Sprintf("%s-tls-secret", standalone.Name),
					DefaultMode: func() *int32 { mode := int32(0o440); return &mode }(),
				},
			},
		})
	}

	// Add truststore volumes (legacy spec.auth.trustStore + new spec.trustedCASecrets).
	var legacyTrustStore *neo4jv1beta1.SecretKeyRef
	if standalone.Spec.Auth != nil {
		legacyTrustStore = standalone.Spec.Auth.TrustStore
	}
	trustedCAs := resources.CollectTrustedCASecrets(legacyTrustStore, standalone.Spec.TrustedCASecrets)
	if len(trustedCAs) > 0 {
		volumes = append(volumes, resources.BuildTrustStoreVolumes(trustedCAs)...)
	}

	// User-supplied extra volumes.
	if len(standalone.Spec.ExtraVolumes) > 0 {
		volumes = append(volumes, standalone.Spec.ExtraVolumes...)
	}

	return volumes
}

// reconcileTLSCertificate reconciles the TLS certificate for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileTLSCertificate(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)

	// Create Certificate using cert-manager
	certificate := r.createTLSCertificate(standalone)

	// Set owner reference
	if err := controllerutil.SetControllerReference(standalone, certificate, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Create or update Certificate
	existing := &certmanagerv1.Certificate{}
	if err := r.Get(ctx, types.NamespacedName{Name: certificate.Name, Namespace: certificate.Namespace}, existing); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating TLS Certificate", "name", certificate.Name)
			if err := r.Create(ctx, certificate); err != nil {
				return fmt.Errorf("failed to create TLS Certificate: %w", err)
			}
		} else {
			return fmt.Errorf("failed to get TLS Certificate: %w", err)
		}
	} else {
		// Update existing Certificate
		existing.Spec = certificate.Spec
		logger.Info("Updating TLS Certificate", "name", certificate.Name)
		if err := r.Update(ctx, existing); err != nil {
			return fmt.Errorf("failed to update TLS Certificate: %w", err)
		}
	}

	return nil
}

// createTLSCertificate creates a TLS certificate for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) createTLSCertificate(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *certmanagerv1.Certificate {
	// Dual SANs during the #215 naming migration: {name}-client is canonical;
	// the legacy {name}-service names stay until the alias Service is removed
	// so connections via either name pass hostname verification.
	dnsNames := []string{
		fmt.Sprintf("%s-client", standalone.Name),
		fmt.Sprintf("%s-client.%s", standalone.Name, standalone.Namespace),
		fmt.Sprintf("%s-client.%s.svc", standalone.Name, standalone.Namespace),
		fmt.Sprintf("%s-client.%s.svc.cluster.local", standalone.Name, standalone.Namespace),
		fmt.Sprintf("%s-service", standalone.Name),
		fmt.Sprintf("%s-service.%s", standalone.Name, standalone.Namespace),
		fmt.Sprintf("%s-service.%s.svc", standalone.Name, standalone.Namespace),
		fmt.Sprintf("%s-service.%s.svc.cluster.local", standalone.Name, standalone.Namespace),
	}
	// Include the public DNS name (spec.service.dnsName) so TLS connections
	// to the external hostname pass hostname verification — same rationale
	// as the cluster path in internal/resources/cluster.go.
	if standalone.Spec.Service != nil && standalone.Spec.Service.DNSName != "" {
		dnsNames = append(dnsNames, standalone.Spec.Service.DNSName)
	}

	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-tls-cert", standalone.Name),
			Namespace: standalone.Namespace,
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: fmt.Sprintf("%s-tls-secret", standalone.Name),
			IssuerRef: cmmeta.IssuerReference{
				Name:  standalone.Spec.TLS.IssuerRef.Name,
				Kind:  standalone.Spec.TLS.IssuerRef.Kind,
				Group: standalone.Spec.TLS.IssuerRef.Group,
			},
			// SecretTemplate propagates ownership labels onto the TLS
			// Secret cert-manager issues. Mirrors the cluster path's
			// labelling — see internal/resources/cluster.go.
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "neo4j-operator",
					"app.kubernetes.io/component":  "tls",
					"neo4j.com/owner-kind":         "Neo4jEnterpriseStandalone",
					"neo4j.com/owner-name":         standalone.Name,
				},
			},
			DNSNames: dnsNames,
		},
	}
}

// createIngress creates an Ingress for the standalone deployment
func (r *Neo4jEnterpriseStandaloneReconciler) createIngress(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *networkingv1.Ingress {
	if standalone.Spec.Service == nil || standalone.Spec.Service.Ingress == nil || !standalone.Spec.Service.Ingress.Enabled {
		return nil
	}

	ingressSpec := standalone.Spec.Service.Ingress

	// Build TLS configuration
	var tls []networkingv1.IngressTLS
	if ingressSpec.TLSSecretName != "" {
		tls = []networkingv1.IngressTLS{
			{
				Hosts:      []string{ingressSpec.Host},
				SecretName: ingressSpec.TLSSecretName,
			},
		}
	}

	// Build HTTP paths
	pathType := networkingv1.PathTypePrefix
	paths := []networkingv1.HTTPIngressPath{
		{
			Path:     "/",
			PathType: &pathType,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: fmt.Sprintf("%s-client", standalone.Name),
					Port: networkingv1.ServiceBackendPort{
						Number: 7474,
					},
				},
			},
		},
	}

	ingressAnnotations := map[string]string{}
	for k, v := range ingressSpec.Annotations {
		ingressAnnotations[k] = v
	}
	resources.ApplyExternalDNSAnnotation(ingressAnnotations, standalone.Spec.Service)

	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-ingress", standalone.Name),
			Namespace:   standalone.Namespace,
			Annotations: ingressAnnotations,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "neo4j",
				"app.kubernetes.io/instance":   standalone.Name,
				"app.kubernetes.io/component":  "ingress",
				"app.kubernetes.io/managed-by": "neo4j-operator",
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressSpec.ClassName,
			TLS:              tls,
			Rules: []networkingv1.IngressRule{
				{
					Host: ingressSpec.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: paths,
						},
					},
				},
			},
		},
	}
}

// reconcileServiceMonitor creates or updates a ServiceMonitor for standalone Prometheus scraping.
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileServiceMonitor(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	serviceMonitor := &unstructured.Unstructured{}
	serviceMonitor.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "ServiceMonitor",
	})
	serviceMonitor.SetName(standalone.Name + "-monitoring")
	serviceMonitor.SetNamespace(standalone.Namespace)
	serviceMonitor.SetLabels(map[string]string{
		"app":                        "neo4j",
		"app.kubernetes.io/name":     "neo4j",
		"app.kubernetes.io/instance": standalone.Name,
	})

	serviceMonitor.Object["spec"] = map[string]any{
		"selector": map[string]any{
			"matchLabels": map[string]any{
				"app.kubernetes.io/name":     "neo4j",
				"app.kubernetes.io/instance": standalone.Name,
				// Canonical client Service only — the deprecated
				// {name}-service alias carries the same name/instance
				// labels (and the metrics port for legacy static scrape
				// configs), so without this Prometheus would scrape the
				// pod twice (#228).
				"neo4j.com/metrics-endpoint": "true",
			},
		},
		"endpoints": []map[string]any{
			{
				"port":     "metrics",
				"interval": "30s",
				"path":     "/metrics",
			},
		},
	}

	if err := ctrl.SetControllerReference(standalone, serviceMonitor, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on ServiceMonitor: %w", err)
	}

	return r.createOrUpdateUnstructured(ctx, serviceMonitor)
}

func (r *Neo4jEnterpriseStandaloneReconciler) createOrUpdateUnstructured(ctx context.Context, obj *unstructured.Unstructured) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())

	err := r.Get(ctx, client.ObjectKeyFromObject(obj), existing)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, obj)
		}
		return err
	}

	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// SetupWithManager sets up the controller with the Manager.
func (r *Neo4jEnterpriseStandaloneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1beta1.Neo4jEnterpriseStandalone{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&networkingv1.Ingress{}).
		// A Neo4jPlugin's settings are folded into the standalone's neo4j.conf
		// (the standalone controller is the single owner — issue #146), so
		// re-reconcile the targeted standalone whenever a plugin is added,
		// changed, or removed (so its keys are rendered or pruned).
		Watches(&neo4jv1beta1.Neo4jPlugin{}, handler.EnqueueRequestsFromMapFunc(
			func(_ context.Context, obj client.Object) []ctrl.Request {
				plugin, ok := obj.(*neo4jv1beta1.Neo4jPlugin)
				if !ok || plugin.Spec.ClusterRef == "" {
					return nil
				}
				return []ctrl.Request{{NamespacedName: types.NamespacedName{
					Namespace: plugin.Namespace,
					Name:      plugin.Spec.ClusterRef,
				}}}
			}))

	// Only watch Certificate resources if cert-manager is actually installed
	// in the cluster. We check the REST mapper (real CRD presence) rather than
	// the scheme — the cert-manager types are always registered in the scheme
	// (cmd/main.go), so a scheme check (`Recognizes`) is always true and would
	// start a Certificate informer even when the CRD is absent, crashing the
	// manager at cache-sync. This makes cert-manager an OPTIONAL dependency,
	// required only for clusters that use cert-manager TLS. Mirrors the Route
	// guard below.
	certGVK := certmanagerv1.SchemeGroupVersion.WithKind("Certificate")
	if _, err := mgr.GetRESTMapper().RESTMapping(certGVK.GroupKind(), certGVK.Version); err == nil {
		builder = builder.Owns(&certmanagerv1.Certificate{})
	}

	routeGVK := schema.GroupVersionKind{Group: "route.openshift.io", Version: "v1", Kind: "Route"}
	if _, err := mgr.GetRESTMapper().RESTMapping(routeGVK.GroupKind(), routeGVK.Version); err == nil {
		routeObj := &unstructured.Unstructured{}
		routeObj.SetGroupVersionKind(routeGVK)
		routeHandler := handler.TypedEnqueueRequestForOwner[*unstructured.Unstructured](mgr.GetScheme(), mgr.GetRESTMapper(), &neo4jv1beta1.Neo4jEnterpriseStandalone{}, handler.OnlyControllerOwner())
		builder = builder.WatchesRawSource(source.Kind(mgr.GetCache(), routeObj, routeHandler))
	}

	return builder.Complete(r)
}

// reconcileAuraFleetManagement handles Aura Fleet Management for standalone deployments.
//
// Phase 1 — Plugin installation: merges "fleet-management" into NEO4J_PLUGINS on the
//
//	live StatefulSet (same pattern as the Neo4jPlugin controller), triggering a pod restart
//	so the Docker entrypoint copies the pre-bundled jar from /var/lib/neo4j/products/.
//
// Phase 2 — Token registration: once the standalone is Ready and the plugin is loaded,
//
//	reads the Aura token from the referenced Secret and calls registerToken.
func (r *Neo4jEnterpriseStandaloneReconciler) reconcileAuraFleetManagement(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) error {
	logger := log.FromContext(ctx)
	spec := standalone.Spec.AuraFleetManagement

	// --- Phase 1: ensure the plugin jar is loaded ---
	// The standalone StatefulSet name is the same as the standalone resource name.
	if err := r.mergeFleetManagementPlugin(ctx, standalone.Name, standalone.Namespace); err != nil {
		logger.Error(err, "Failed to patch StatefulSet NEO4J_PLUGINS for fleet-management")
		if r.Recorder != nil {
			r.Recorder.Eventf(standalone, corev1.EventTypeWarning, EventReasonAuraFleetPluginPatchFailed,
				"Failed to add fleet-management to NEO4J_PLUGINS: %v", err)
		}
		return nil
	}

	// --- Phase 2: token registration ---
	if spec.TokenSecretRef == nil {
		logger.Info("Aura Fleet Management enabled but no tokenSecretRef configured; plugin installed, registration deferred")
		return nil
	}

	if standalone.Status.Phase != "Ready" {
		logger.Info("Standalone not yet Ready; fleet-management plugin patch applied, registration deferred",
			"phase", standalone.Status.Phase)
		return nil
	}

	if standalone.Status.AuraFleetManagement != nil && standalone.Status.AuraFleetManagement.Registered {
		logger.V(1).Info("Aura Fleet Management already registered; nothing to do")
		return nil
	}

	secretName := spec.TokenSecretRef.Name
	secretKey := spec.TokenSecretRef.Key
	if secretKey == "" {
		secretKey = "token"
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: standalone.Namespace, Name: secretName}, secret); err != nil {
		return r.setFleetManagementStatus(ctx, standalone, false, fmt.Sprintf("cannot read token secret %s: %v", secretName, err))
	}

	tokenBytes, ok := secret.Data[secretKey]
	if !ok || len(tokenBytes) == 0 {
		return r.setFleetManagementStatus(ctx, standalone, false, fmt.Sprintf("key %q not found in secret %s", secretKey, secretName))
	}
	token := strings.TrimSpace(string(tokenBytes))

	neo4jClient, err := neo4jclient.NewClientForEnterpriseStandalone(standalone, r.Client, getStandaloneAdminSecretName(standalone))
	if err != nil {
		return r.setFleetManagementStatus(ctx, standalone, false, fmt.Sprintf("cannot connect to Neo4j: %v", err))
	}
	defer neo4jClient.Close()

	installed, err := neo4jClient.IsFleetManagementInstalled(ctx)
	if err != nil {
		return r.setFleetManagementStatus(ctx, standalone, false, fmt.Sprintf("cannot check fleet management plugin: %v", err))
	}
	if !installed {
		logger.Info("Fleet management plugin not yet loaded; will retry on next reconcile")
		return nil
	}

	if err := neo4jClient.RegisterFleetManagementToken(ctx, token); err != nil {
		return r.setFleetManagementStatus(ctx, standalone, false, fmt.Sprintf("token registration failed: %v", err))
	}

	logger.Info("Aura Fleet Management token registered successfully")
	if r.Recorder != nil {
		r.Recorder.Event(standalone, corev1.EventTypeNormal, EventReasonAuraFleetRegistered,
			"Successfully registered with Aura Fleet Management")
	}
	return r.setFleetManagementStatus(ctx, standalone, true, "Registered with Aura Fleet Management")
}

// mergeFleetManagementPlugin patches the named StatefulSet so "fleet-management" is present
// in NEO4J_PLUGINS. Idempotent: if already present, no patch is issued.
func (r *Neo4jEnterpriseStandaloneReconciler) mergeFleetManagementPlugin(ctx context.Context, stsName, namespace string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		sts := &appsv1.StatefulSet{}
		if err := r.Get(ctx, types.NamespacedName{Name: stsName, Namespace: namespace}, sts); err != nil {
			return fmt.Errorf("get StatefulSet %s: %w", stsName, err)
		}

		containerIdx := -1
		for i, c := range sts.Spec.Template.Spec.Containers {
			if c.Name == "neo4j" {
				containerIdx = i
				break
			}
		}
		if containerIdx < 0 {
			return fmt.Errorf("neo4j container not found in StatefulSet %s", stsName)
		}

		envs := sts.Spec.Template.Spec.Containers[containerIdx].Env
		pluginsIdx := -1
		for i, e := range envs {
			if e.Name == "NEO4J_PLUGINS" {
				pluginsIdx = i
				break
			}
		}

		var updated string
		var err error
		if pluginsIdx < 0 {
			updated = `["fleet-management"]`
		} else {
			current := envs[pluginsIdx].Value
			updated, err = MergeNeo4jPluginList(current, "fleet-management")
			if err != nil {
				return fmt.Errorf("merge plugin list: %w", err)
			}
			if updated == current {
				return nil // already present — nothing to patch
			}
		}

		stsCopy := sts.DeepCopy()
		if pluginsIdx < 0 {
			stsCopy.Spec.Template.Spec.Containers[containerIdx].Env = append(
				stsCopy.Spec.Template.Spec.Containers[containerIdx].Env,
				corev1.EnvVar{Name: "NEO4J_PLUGINS", Value: updated},
			)
		} else {
			stsCopy.Spec.Template.Spec.Containers[containerIdx].Env[pluginsIdx].Value = updated
		}
		return r.Update(ctx, stsCopy)
	})
}

func (r *Neo4jEnterpriseStandaloneReconciler) setFleetManagementStatus(ctx context.Context, standalone *neo4jv1beta1.Neo4jEnterpriseStandalone, registered bool, message string) error {
	now := metav1.Now()
	status := &neo4jv1beta1.AuraFleetManagementStatus{
		Registered: registered,
		Message:    message,
	}
	if registered {
		status.LastRegistrationTime = &now
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: standalone.Namespace, Name: standalone.Name}, latest); err != nil {
			return err
		}
		latest.Status.AuraFleetManagement = status
		standalone.Status.AuraFleetManagement = status // mirror for the in-memory caller
		return r.Status().Update(ctx, latest)
	})
}
