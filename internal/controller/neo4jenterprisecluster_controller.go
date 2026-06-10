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
	goerrors "errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/time/rate"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/metrics"
	neo4jclient "github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/validation"
)

// Neo4jEnterpriseClusterReconciler reconciles a Neo4jEnterpriseCluster object
type Neo4jEnterpriseClusterReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	Recorder           record.EventRecorder
	RequeueAfter       time.Duration
	TopologyScheduler  *TopologyScheduler
	Validator          *validation.ClusterValidator
	ConfigMapManager   *ConfigMapManager
	SplitBrainDetector *SplitBrainDetector
}

const (
	// ClusterFinalizer is the finalizer for Neo4j enterprise clusters
	ClusterFinalizer = "neo4j.neo4j.com/cluster-finalizer"
	// DefaultAdminSecretName is the default name for admin credentials
	DefaultAdminSecretName = "neo4j-admin-secret"
)

//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=endpoints,verbs=get;list;watch;create;update;patch;delete
// Endpoints permission is CRITICAL for Neo4j Kubernetes discovery to resolve pod IPs behind services
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cert-manager.io,resources=issuers,verbs=get;list;watch
//+kubebuilder:rbac:groups=cert-manager.io,resources=clusterissuers,verbs=get;list;watch
//+kubebuilder:rbac:groups=external-secrets.io,resources=externalsecrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=external-secrets.io,resources=secretstores,verbs=get;list;watch
//+kubebuilder:rbac:groups=external-secrets.io,resources=clustersecretstores,verbs=get;list;watch
//+kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors;prometheusrules,verbs=get;list;watch;create;update;patch;delete

// isStrictPeerValidationEnabled reports whether the cluster wants strict
// intra-cluster TLS peer validation. The CRD field defaults to true; an
// explicit false opts the cluster into the legacy debugging-only posture.
func isStrictPeerValidationEnabled(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) bool {
	if cluster.Spec.TLS == nil || cluster.Spec.TLS.Mode != "cert-manager" {
		return false
	}
	if cluster.Spec.TLS.StrictPeerValidation == nil {
		return true
	}
	return *cluster.Spec.TLS.StrictPeerValidation
}

// errTLSSecretPending is the sentinel returned by verifyTLSSecretHasCA when
// the cert-manager Secret has not yet been issued. The reconciler treats
// this as "wait for cert-manager, don't proceed to STS emission" — surfacing
// a clear Initializing status rather than the misleading Failed status that
// would result from treating it as a hard preflight error.
//
// Used with errors.Is so callers can distinguish "still bootstrapping" from
// "issuer is permanently mis-configured."
var errTLSSecretPending = goerrors.New("cert-manager Secret has not yet been issued")

// verifyTLSSecretHasCA fetches the cert-manager-issued Secret and returns:
//   - errTLSSecretPending if the Secret does not yet exist (cert-manager
//     is still working; the reconcile must NOT proceed to emit the strict
//     STS template, because that template references a Secret with a
//     required ca.crt key that doesn't exist yet — the Pod would get
//     stuck in CreateContainerConfigError).
//   - A descriptive error if the Secret exists but ca.crt is missing or
//     empty (the issuer doesn't populate it; permanent until the user
//     either fixes the issuer or opts out of strict peer validation).
//   - nil if the Secret exists and has a non-empty ca.crt — safe to emit
//     strict-mode resources.
func (r *Neo4jEnterpriseClusterReconciler) verifyTLSSecretHasCA(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	secretName := fmt.Sprintf("%s-tls-secret", cluster.Name)
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cluster.Namespace}, secret); err != nil {
		if errors.IsNotFound(err) {
			return errTLSSecretPending
		}
		return fmt.Errorf("strict peer validation preflight: failed to read Secret %s/%s: %w", cluster.Namespace, secretName, err)
	}
	if ca, ok := secret.Data["ca.crt"]; !ok || len(ca) == 0 {
		issuerName := ""
		if cluster.Spec.TLS != nil && cluster.Spec.TLS.IssuerRef != nil {
			issuerName = cluster.Spec.TLS.IssuerRef.Name
		}
		// Enumerate the keys that ARE present so debug output makes it
		// obvious whether the Secret is genuinely missing ca.crt (issuer
		// bug) or is in some other partial state we hadn't considered.
		// Previously the failure was opaque: "Failed != Ready" with no
		// hint as to which keys cert-manager populated.
		presentKeys := make([]string, 0, len(secret.Data))
		for k, v := range secret.Data {
			presentKeys = append(presentKeys, fmt.Sprintf("%s(%dB)", k, len(v))) //nolint:perfsprint // composite "key(NB)" debug string; Sprintf is clearer than strconv here
		}
		return fmt.Errorf("strict peer validation requires Secret %s/%s to expose a non-empty ca.crt key, but the cert-manager-issued Secret has keys=[%s]. The issuer %q likely does not populate ca.crt (some external issuers don't), or cert-manager has not finished issuing. Either fix the issuer to include the CA in its Secret output, or set spec.tls.strictPeerValidation=false on this cluster to opt into the legacy trust_all=true posture",
			cluster.Namespace, secretName, strings.Join(presentKeys, ","), issuerName)
	}
	return nil
}

func (r *Neo4jEnterpriseClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jEnterpriseCluster instance
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Neo4jEnterpriseCluster resource not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Neo4jEnterpriseCluster")
		return ctrl.Result{}, err
	}

	reconcileStart := time.Now()
	reconcileM := metrics.NewReconcileMetrics(cluster.Name, cluster.Namespace)
	defer func() {
		success := cluster.Status.Phase == "Ready"
		reconcileM.RecordReconcile(ctx, "cluster", time.Since(reconcileStart), success)
	}()

	// Handle deletion
	if cluster.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, cluster)
	}

	// Apply defaults and validate the cluster
	if r.Validator != nil {
		// Apply defaults to the cluster
		r.Validator.ApplyDefaults(ctx, cluster)

		// Check if this is an update by looking at the generation
		isUpdate := cluster.Generation > 1 || !cluster.CreationTimestamp.IsZero()

		if isUpdate {
			// For updates, we need to get the current state from the API server
			// to compare with the new desired state
			currentCluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
			if err := r.Get(ctx, req.NamespacedName, currentCluster); err != nil {
				if !errors.IsNotFound(err) {
					logger.Error(err, "Failed to get current cluster state for validation")
					return ctrl.Result{}, err
				}
				// If not found, treat as create
				isUpdate = false
			}

			if isUpdate {
				// Validate the cluster update with warnings
				result := r.Validator.ValidateUpdateWithWarnings(ctx, currentCluster, cluster)

				// Emit warnings as events
				for _, warning := range result.Warnings {
					r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonTopologyWarning, warning)
				}

				// Check for validation errors
				if len(result.Errors) > 0 {
					err := fmt.Errorf("validation failed: %s", result.Errors.ToAggregate().Error())
					logger.Error(err, "Cluster update validation failed")
					r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonValidationFailed, "Cluster update validation failed: %v", err)
					_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Update validation failed: %v", err))
					return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
				}
			}
		}

		if !isUpdate {
			// Validate the cluster configuration for create with warnings
			result := r.Validator.ValidateCreateWithWarnings(ctx, cluster)

			// Emit warnings as events
			for _, warning := range result.Warnings {
				r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonTopologyWarning, warning)
			}

			// Check for validation errors
			if len(result.Errors) > 0 {
				err := fmt.Errorf("validation failed: %s", result.Errors.ToAggregate().Error())
				logger.Error(err, "Cluster validation failed")
				r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonValidationFailed, "Cluster validation failed: %v", err)
				_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Validation failed: %v", err))
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
			}
		}

		// Validate server role hints for both create and update
		roleHintErrors := resources.ValidateServerRoleHints(cluster)
		if len(roleHintErrors) > 0 {
			for _, roleError := range roleHintErrors {
				logger.Error(fmt.Errorf("server role validation error"), roleError)
				r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonServerRoleFailed, "Server role hint validation failed: %s", roleError)
			}
			err := fmt.Errorf("server role validation failed: %v", roleHintErrors)
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Server role validation failed: %v", roleHintErrors))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Validate property sharding configuration if enabled
	if cluster.Spec.PropertySharding != nil && cluster.Spec.PropertySharding.Enabled {
		if err := r.validatePropertyShardingConfiguration(ctx, cluster); err != nil {
			logger.Error(err, "Property sharding validation failed")
			r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonPropertyShardingFailed, "Property sharding validation failed: %v", err)
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Property sharding validation failed: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Verify the requested StorageClass exists before creating the StatefulSet.
	// A misnamed class (e.g. "standard" on AKS, which ships "managed-csi") would
	// otherwise leave every server pod Pending indefinitely with no operator-level
	// signal. An empty className is allowed and inherits the cluster default.
	if exists, err := storageClassExists(ctx, r.Client, cluster.Spec.Storage.ClassName); err != nil {
		logger.Error(err, "Failed to look up StorageClass", "storageClass", cluster.Spec.Storage.ClassName)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	} else if !exists {
		msg := fmt.Sprintf("StorageClass %q not found; create it or set spec.storage.className to an existing class (or leave it empty to use the cluster default)", cluster.Spec.Storage.ClassName)
		logger.Error(fmt.Errorf("storage class not found"), msg)
		r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonStorageClassNotFound, msg)
		_ = r.updateClusterStatus(ctx, cluster, "Failed", msg)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(cluster, ClusterFinalizer) {
		controllerutil.AddFinalizer(cluster, ClusterFinalizer)
		if err := r.Update(ctx, cluster); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if this is an upgrade scenario
	if r.isUpgradeRequired(ctx, cluster) {
		logger.Info("Image upgrade detected, initiating rolling upgrade")
		return r.handleRollingUpgrade(ctx, cluster)
	}

	// Neo4jEnterpriseCluster is always multi-node from the start
	// (minimum 1 primary + 1 secondary OR 2+ primaries)
	// No need for single-node to multi-node transition logic

	// Set phase to "Initializing" only on the very first reconcile (no phase set yet).
	// Never regress an established phase (Forming, Ready, etc.) back to Initializing.
	if cluster.Status.Phase == "" {
		_ = r.updateClusterStatus(ctx, cluster, "Initializing", "Starting cluster reconciliation")
	}

	// Create Certificate if cert-manager is enabled
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == "cert-manager" {
		certificate := resources.BuildCertificateForEnterprise(cluster)
		if certificate != nil {
			if err := r.createOrUpdateResource(ctx, certificate, cluster); err != nil {
				logger.Error(err, "Failed to create Certificate")
				_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create Certificate: %v", err))
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
			}
		}

		// Strict peer validation requires the cert-manager Secret to
		// expose a ca.crt key (the issuer's CA bundle). Most issuers
		// populate it (CA, ACME, Vault); some external issuers do not.
		// We must NOT proceed to ConfigMap + STS emission until we've
		// verified the Secret is ready, because the strict-mode STS
		// template requires ca.crt in its volume projection (KeyToPath
		// has no per-item optional flag — the kubelet would refuse to
		// mount the volume if the key is missing).
		if isStrictPeerValidationEnabled(cluster) {
			if err := r.verifyTLSSecretHasCA(ctx, cluster); err != nil {
				if goerrors.Is(err, errTLSSecretPending) {
					// Bootstrap path: cert-manager hasn't issued the Secret
					// yet. Block downstream ConfigMap + STS emission so the
					// strict-mode template (which requires ca.crt in its
					// Secret items projection) never lands before the
					// Secret is verified.
					//
					// Phase handling honours the "never regress an
					// established phase to Initializing" policy documented
					// just above. Only surface Initializing on the
					// first-reconcile path (empty phase); for established
					// clusters in Forming/Ready/etc., requeue silently and
					// leave the phase intact. A subsequent reconcile will
					// proceed normally once cert-manager finishes.
					if cluster.Status.Phase == "" || cluster.Status.Phase == "Initializing" {
						_ = r.updateClusterStatus(ctx, cluster, "Initializing",
							fmt.Sprintf("Waiting for cert-manager to issue Secret %s-tls-secret (strict peer validation)", cluster.Name))
					} else {
						logger.Info("Strict peer validation preflight: cert-manager Secret not yet available; requeueing",
							"secret", fmt.Sprintf("%s-tls-secret", cluster.Name),
							"phase", cluster.Status.Phase)
					}
					return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
				}
				logger.Error(err, "Strict peer validation preflight failed")
				_ = r.updateClusterStatus(ctx, cluster, "Failed", err.Error())
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
			}
		}
	}

	// Create External Secrets if enabled
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.ExternalSecrets != nil && cluster.Spec.TLS.ExternalSecrets.Enabled {
		if err := r.createExternalSecretForTLS(ctx, cluster); err != nil {
			logger.Error(err, "Failed to create TLS ExternalSecret")
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create TLS ExternalSecret: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	if cluster.Spec.Auth != nil && cluster.Spec.Auth.ExternalSecrets != nil && cluster.Spec.Auth.ExternalSecrets.Enabled {
		if err := r.createExternalSecretForAuth(ctx, cluster); err != nil {
			logger.Error(err, "Failed to create Auth ExternalSecret")
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create Auth ExternalSecret: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Reconcile ConfigMap with immediate updates and pod restarts
	if err := r.ConfigMapManager.ReconcileConfigMap(ctx, cluster); err != nil {
		logger.Error(err, "Failed to reconcile ConfigMap")
		_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to reconcile ConfigMap: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Create RBAC resources for Kubernetes discovery
	serviceAccount := resources.BuildDiscoveryServiceAccountForEnterprise(cluster)
	if err := r.createOrUpdateResource(ctx, serviceAccount, cluster); err != nil {
		logger.Error(err, "Failed to create discovery ServiceAccount", "serviceAccount", serviceAccount.Name)
		_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create ServiceAccount %s: %v", serviceAccount.Name, err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	role := resources.BuildDiscoveryRoleForEnterprise(cluster)
	if err := r.createOrUpdateResource(ctx, role, cluster); err != nil {
		logger.Error(err, "Failed to create discovery Role", "role", role.Name)
		_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create Role %s: %v", role.Name, err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	roleBinding := resources.BuildDiscoveryRoleBindingForEnterprise(cluster)
	if err := r.createOrUpdateResource(ctx, roleBinding, cluster); err != nil {
		logger.Error(err, "Failed to create discovery RoleBinding", "roleBinding", roleBinding.Name)
		_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create RoleBinding %s: %v", roleBinding.Name, err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Create Services
	services := []*corev1.Service{
		resources.BuildHeadlessServiceForEnterprise(cluster),  // Headless service for StatefulSet
		resources.BuildDiscoveryServiceForEnterprise(cluster), // Discovery service for Neo4j K8s discovery
		resources.BuildInternalsServiceForEnterprise(cluster), // Internals service for client connections
		resources.BuildClientServiceForEnterprise(cluster),    // Client service for external access
		resources.BuildMetricsServiceForEnterprise(cluster),   // Metrics service for Prometheus scraping
	}

	// Filter out nil services (e.g., secondary service when secondaries = 0)
	var validServices []*corev1.Service
	for _, service := range services {
		if service != nil {
			validServices = append(validServices, service)
		}
	}
	services = validServices
	for _, service := range services {
		if err := r.createOrUpdateResource(ctx, service, cluster); err != nil {
			logger.Error(err, "Failed to create Service", "service", service.Name)
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create Service %s: %v", service.Name, err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Create Ingress if configured
	if cluster.Spec.Service != nil && cluster.Spec.Service.Ingress != nil && cluster.Spec.Service.Ingress.Enabled {
		ingress := resources.BuildIngressForEnterprise(cluster)
		if ingress != nil {
			if err := r.createOrUpdateResource(ctx, ingress, cluster); err != nil {
				logger.Error(err, "Failed to create Ingress")
				_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create Ingress: %v", err))
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
			}
		}
	}

	// Create NetworkPolicy if enabled (issue #128 gap #2 — restricts
	// port 6362 ingress to operator-managed backup pods). Effective only
	// on CNIs that enforce NetworkPolicy (Calico/Cilium/Antrea/Weave);
	// safe no-op on others. Build returns nil when disabled so this
	// branch trivially short-circuits without a feature-flag dance.
	if np := resources.BuildNetworkPolicyForEnterprise(cluster); np != nil {
		if err := r.createOrUpdateResource(ctx, np, cluster); err != nil {
			logger.Error(err, "Failed to create NetworkPolicy")
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create NetworkPolicy: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Create Route if configured (OpenShift)
	if err := r.reconcileRoute(ctx, cluster); err != nil {
		logger.Error(err, "Failed to reconcile Route")
		_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to reconcile Route: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Reconcile MCP resources if enabled
	if err := r.reconcileMCP(ctx, cluster); err != nil {
		logger.Error(err, "Failed to reconcile MCP resources")
		_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to reconcile MCP resources: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Reconcile Aura Fleet Management registration if enabled
	if cluster.Spec.AuraFleetManagement != nil && cluster.Spec.AuraFleetManagement.Enabled {
		if err := r.reconcileAuraFleetManagement(ctx, cluster); err != nil {
			// Fleet management registration failures are non-fatal: the cluster is operational,
			// only the Aura monitoring registration failed. Log and surface via status.
			logger.Error(err, "Failed to reconcile Aura Fleet Management registration")
			r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonAuraFleetFailed,
				"Aura Fleet Management registration failed: %v", err)
		}
	}

	// Calculate topology placement if topology scheduler is available
	var topologyPlacement *TopologyPlacement
	if r.TopologyScheduler != nil {
		placement, err := r.TopologyScheduler.CalculateTopologyPlacement(ctx, cluster)
		if err != nil {
			logger.Error(err, "Failed to calculate topology placement")
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to calculate topology placement: %v", err))
			r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonTopologyPlacementFailed, fmt.Sprintf("Failed to calculate topology placement: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
		topologyPlacement = placement
		logger.Info("Calculated topology placement",
			"useTopologySpread", placement.UseTopologySpread,
			"useAntiAffinity", placement.UseAntiAffinity,
			"zones", len(placement.AvailabilityZones),
			"enforceDistribution", placement.EnforceDistribution)

		if len(placement.AvailabilityZones) > 0 {
			r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonTopologyPlacementCalc,
				fmt.Sprintf("Calculated topology placement across %d zones", len(placement.AvailabilityZones)))
		}
	}

	// Check if PVC storage expansion is needed before creating/updating StatefulSets.
	// If expansion is needed, PVCs are patched and the StatefulSet is orphan-deleted,
	// then we requeue so the next reconcile recreates it with updated VolumeClaimTemplates.
	if requeue, err := r.reconcileStorageExpansion(ctx, cluster); err != nil {
		logger.Error(err, "Failed to reconcile storage expansion")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	} else if requeue {
		logger.Info("Storage expansion completed, requeueing to recreate StatefulSet")
		return ctrl.Result{Requeue: true}, nil
	}

	// Create single StatefulSet for all servers
	serverStatefulSet := resources.BuildServerStatefulSetForEnterprise(cluster)

	// Apply topology constraints to the server StatefulSet
	if r.TopologyScheduler != nil && topologyPlacement != nil {
		if err := r.TopologyScheduler.ApplyTopologyConstraints(ctx, serverStatefulSet, cluster, topologyPlacement); err != nil {
			logger.Error(err, "Failed to apply topology constraints to server StatefulSet")
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to apply topology constraints to server StatefulSet: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	if err := r.createOrUpdateResource(ctx, serverStatefulSet, cluster); err != nil {
		logger.Error(err, "Failed to create server StatefulSet")
		_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create server StatefulSet: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Handle Query Performance Monitoring
	if cluster.Spec.Monitoring != nil && cluster.Spec.Monitoring.Enabled {
		queryMonitor := NewQueryMonitor(r.Client, r.Scheme)
		if err := queryMonitor.ReconcileMonitoring(ctx, cluster); err != nil {
			logger.Error(err, "Failed to reconcile query monitoring")
			// Don't fail the entire reconciliation for monitoring issues
			logger.Info("Query monitoring setup failed, continuing with cluster reconciliation")
		}
	}

	// Collect live diagnostics when cluster is Ready.
	// Diagnostics are collected by default (monitoring nil or monitoring.enabled=true).
	// Only skipped when monitoring is explicitly disabled.
	monitoringDisabled := cluster.Spec.Monitoring != nil && !cluster.Spec.Monitoring.Enabled
	if !monitoringDisabled && cluster.Status.Phase == "Ready" {
		neo4jDiagClient, diagClientErr := r.createNeo4jClient(ctx, cluster)
		if diagClientErr != nil {
			logger.V(1).Info("Skipping diagnostics collection: could not create Neo4j client", "error", diagClientErr)
		} else {
			defer neo4jDiagClient.Close()
			diagMonitor := NewQueryMonitor(r.Client, r.Scheme)
			if diagErr := diagMonitor.CollectDiagnostics(ctx, cluster, neo4jDiagClient); diagErr != nil {
				logger.Error(diagErr, "Failed to collect cluster diagnostics (non-fatal)")
			}
		}
	}

	// Plugin management is now handled by the separate Neo4jPlugin CRD and controller

	// Verify Neo4j cluster formation before marking as Ready
	clusterFormed, formationMessage, err := r.verifyNeo4jClusterFormation(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to verify cluster formation")
		r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonClusterFormationFailed,
			"Cluster formation check failed: %v", err)
		_ = r.updateClusterStatus(ctx, cluster, "Forming", fmt.Sprintf("Verifying cluster formation: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	if !clusterFormed {
		if cluster.Status.Phase != "Forming" {
			r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonClusterFormationStarted,
				"Neo4j cluster formation started")
		}
		_ = r.updateClusterStatus(ctx, cluster, "Forming", formationMessage)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Update status to "Ready" only if cluster formation is verified
	// Note: Split-brain detection is already performed in verifyNeo4jClusterFormation
	statusChanged := r.updateClusterStatus(ctx, cluster, "Ready", "Neo4j cluster is fully formed and ready")

	// Update property sharding readiness status if enabled
	if cluster.Spec.PropertySharding != nil && cluster.Spec.PropertySharding.Enabled {
		if err := r.updatePropertyShardingStatus(ctx, cluster, true); err != nil {
			logger.Error(err, "Failed to update property sharding status")
			// Don't fail the reconciliation for status update issues
		}
	}

	// Only create event if status actually changed
	if statusChanged {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonClusterReady, "Neo4j Enterprise cluster is ready")
	}

	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jEnterpriseClusterReconciler) handleDeletion(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(cluster, ClusterFinalizer) {
		logger.Info("Finalizer not present, nothing to do", "finalizers", cluster.Finalizers, "deletionTimestamp", cluster.DeletionTimestamp)
		return ctrl.Result{}, nil
	}

	// Clean up PVCs if retention policy is Delete (default behavior)
	retentionPolicy := cluster.Spec.Storage.RetentionPolicy
	if retentionPolicy == "" || retentionPolicy == "Delete" {
		logger.Info("Cleaning up PVCs due to Delete retention policy", "retentionPolicy", retentionPolicy)
		if err := r.cleanupPVCs(ctx, cluster); err != nil {
			logger.Error(err, "Failed to cleanup PVCs")
			return ctrl.Result{RequeueAfter: time.Second * 10}, err
		}
		logger.Info("Successfully cleaned up PVCs")
	} else {
		logger.Info("Retaining PVCs due to Retain retention policy", "retentionPolicy", retentionPolicy)
	}

	// Explicitly delete StatefulSets to avoid slow GC with blockOwnerDeletion
	serverStsName := fmt.Sprintf("%s-server", cluster.Name)
	serverSts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: serverStsName, Namespace: cluster.Namespace}, serverSts); err == nil {
		logger.Info("Deleting server StatefulSet", "name", serverStsName)
		if err := r.Delete(ctx, serverSts); err != nil {
			logger.Error(err, "Failed to delete server StatefulSet")
		}
	}
	logger.Info("Removing finalizer from cluster", "finalizers", cluster.Finalizers, "deletionTimestamp", cluster.DeletionTimestamp)
	controllerutil.RemoveFinalizer(cluster, ClusterFinalizer)
	err := r.Update(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to update cluster after removing finalizer", "finalizers", cluster.Finalizers, "deletionTimestamp", cluster.DeletionTimestamp)
		return ctrl.Result{}, err
	}
	logger.Info("Successfully removed finalizer and updated cluster", "finalizers", cluster.Finalizers, "deletionTimestamp", cluster.DeletionTimestamp)
	return ctrl.Result{}, nil
}

func (r *Neo4jEnterpriseClusterReconciler) cleanupPVCs(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)

	// List PVCs that belong to this cluster
	pvcList := &corev1.PersistentVolumeClaimList{}
	labelSelector := client.MatchingLabels(resources.PVCSelectorByInstance(cluster.Name))

	if err := r.List(ctx, pvcList, client.InNamespace(cluster.Namespace), labelSelector); err != nil {
		return fmt.Errorf("failed to list PVCs for cluster %s: %w", cluster.Name, err)
	}

	logger.Info("Found PVCs to delete", "count", len(pvcList.Items), "cluster", cluster.Name)

	// Delete each PVC
	for _, pvc := range pvcList.Items {
		logger.Info("Deleting PVC", "pvc", pvc.Name, "cluster", cluster.Name)
		if err := r.Delete(ctx, &pvc); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed to delete PVC %s: %w", pvc.Name, err)
			}
			logger.Info("PVC already deleted", "pvc", pvc.Name)
		} else {
			logger.Info("Successfully deleted PVC", "pvc", pvc.Name)
		}
	}

	return nil
}

// CreateOrUpdateResource is exported for testing
func (r *Neo4jEnterpriseClusterReconciler) CreateOrUpdateResource(ctx context.Context, obj client.Object, owner client.Object) error {
	return r.createOrUpdateResource(ctx, obj, owner)
}

// replicasReconciliationPaused reports whether the cluster controller should
// leave sts.Spec.Replicas alone for this owner. Today the only pause signal
// is the restore-in-progress annotation set by Neo4jRestoreReconciler (issue
// #117), but extracting the check makes the gate testable in isolation and
// gives a single place to add new pause signals (e.g. operator-wide
// maintenance mode) without touching the resource builder.
func replicasReconciliationPaused(owner client.Object) bool {
	if owner == nil {
		return false
	}
	_, paused := owner.GetAnnotations()[RestoreInProgressAnnotation]
	return paused
}

func (r *Neo4jEnterpriseClusterReconciler) createOrUpdateResource(ctx context.Context, obj client.Object, owner client.Object) error {
	logger := log.FromContext(ctx)

	// Initialize metrics
	conflictMetrics := metrics.NewConflictMetrics()
	startTime := time.Now()

	// Use retry logic to handle resource version conflicts
	retryCount := 0
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if retryCount > 0 {
			logger.Info("Retrying resource update due to conflict",
				"resource", fmt.Sprintf("%T", obj),
				"name", obj.GetName(),
				"retryCount", retryCount)
			// Record conflict metric
			conflictMetrics.RecordConflict(fmt.Sprintf("%T", obj), obj.GetNamespace())
		}
		retryCount++
		return r.createOrUpdateResourceInternal(ctx, obj, owner)
	})

	// Record metrics if we had conflicts
	if retryCount > 1 {
		retryDuration := time.Since(startTime)
		conflictMetrics.RecordConflictRetry(fmt.Sprintf("%T", obj), obj.GetNamespace(), retryCount-1, retryDuration)

		if err != nil && errors.IsConflict(err) {
			logger.Error(err, "Failed to update resource after retries due to conflict",
				"resource", fmt.Sprintf("%T", obj),
				"name", obj.GetName(),
				"finalRetryCount", retryCount)
		} else {
			logger.Info("Successfully updated resource after conflict resolution",
				"resource", fmt.Sprintf("%T", obj),
				"name", obj.GetName(),
				"totalRetries", retryCount-1,
				"duration", retryDuration)
		}
	}

	return err
}

// createOrUpdateResourceInternal performs the actual create or update operation with resource conflict handling
//
// This function implements critical resource version conflict resolution that eliminates the Pod-2 restart
// pattern observed during Neo4j cluster formation. Key improvements:
//
// 1. Proper resource existence detection using UID instead of ResourceVersion (UID is empty for new resources)
// 2. Template comparison logic that prevents unnecessary pod restarts during cluster formation
// 3. Selective template updates only when changes are significant enough to warrant pod disruption
//
// The template comparison is essential for Neo4j cluster stability - without it, resource version conflicts
// during reconciliation loops would cause the highest-indexed pods (Pod-2) to restart repeatedly, disrupting
// cluster formation especially for Neo4j 2025.01.0 which is more sensitive to discovery timing.
func (r *Neo4jEnterpriseClusterReconciler) createOrUpdateResourceInternal(ctx context.Context, obj client.Object, owner client.Object) error {
	// Set owner reference
	if err := controllerutil.SetControllerReference(owner, obj, r.Scheme); err != nil {
		return err
	}

	// Capture the desired spec if this is a StatefulSet
	var desiredSpec appsv1.StatefulSetSpec
	if sts, ok := obj.(*appsv1.StatefulSet); ok {
		desiredSpec = *sts.Spec.DeepCopy()
	}

	logger := log.FromContext(ctx)
	logger.Info("Starting CreateOrUpdate operation",
		"resource", fmt.Sprintf("%T", obj),
		"name", obj.GetName(),
		"namespace", obj.GetNamespace())

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		if sts, ok := obj.(*appsv1.StatefulSet); ok {
			// Check if this is an update (object already exists in cluster)
			// CRITICAL FIX: Use UID to determine if this is an existing object, not ResourceVersion
			// ResourceVersion can be populated even for new resources during CreateOrUpdate operations,
			// but UID is only set for resources that actually exist in the cluster
			if sts.UID != "" {
				// This is an existing StatefulSet, only update allowed mutable fields
				// Note: sts already contains the current state from CreateOrUpdate
				originalMeta := sts.ObjectMeta.DeepCopy()
				originalStatus := sts.Status.DeepCopy()

				// Apply desired spec — but yield on replicas while a
				// Neo4jRestore is coordinating a scale-down → restore →
				// scale-up cycle on this cluster (issue #117). Without
				// this gate the cluster controller races the restore
				// controller every reconcile and the scale-to-0 never
				// sticks. Only the replicas line is skipped; everything
				// else (services, ConfigMap, certs, template updates)
				// continues to reconcile normally.
				if !replicasReconciliationPaused(owner) {
					sts.Spec.Replicas = desiredSpec.Replicas
				}
				sts.Spec.UpdateStrategy = desiredSpec.UpdateStrategy
				sts.Spec.PersistentVolumeClaimRetentionPolicy = desiredSpec.PersistentVolumeClaimRetentionPolicy
				sts.Spec.MinReadySeconds = desiredSpec.MinReadySeconds
				sts.Spec.Ordinals = desiredSpec.Ordinals

				// Preserve original metadata and status
				sts.ObjectMeta = *originalMeta
				sts.Status = *originalStatus

				// For template updates, check if changes are significant to avoid unnecessary pod restarts
				// This is the core mechanism that prevents Pod-2 restart patterns during cluster formation
				if sts.Spec.Selector != nil {
					// Copy the desired template but ensure labels match the existing selector
					updatedTemplate := desiredSpec.Template.DeepCopy()
					if updatedTemplate.Labels == nil {
						updatedTemplate.Labels = make(map[string]string)
					}
					// Ensure all selector labels are present in the template
					for key, value := range sts.Spec.Selector.MatchLabels {
						updatedTemplate.Labels[key] = value
					}

					// Check if template update is significant enough to warrant pod restarts
					// This prevents resource version conflicts from causing unnecessary pod disruption
					if r.isTemplateChangeSignificant(ctx, sts.Spec.Template, *updatedTemplate, sts) {
						logger := log.FromContext(ctx)
						logger.Info("Applying significant StatefulSet template changes",
							"statefulSet", sts.Name,
							"namespace", sts.Namespace)

						// Smart env-var merge: preserve foreign vars (added by
						// plugin / fleet controllers), drop previously-owned-but-
						// no-longer-desired (e.g. user removed a spec.config key),
						// apply desired. The wholesale `Template = updatedTemplate`
						// without this would clobber plugin / fleet env vars and
						// trigger cross-controller oscillation. See envVarsEqual
						// docs and mergeEnvVars for the design.
						previousOwned := readOwnedEnvVarNames(sts)
						currentEnv := []corev1.EnvVar{}
						if len(sts.Spec.Template.Spec.Containers) > 0 {
							currentEnv = sts.Spec.Template.Spec.Containers[0].Env
						}
						desiredEnv := []corev1.EnvVar{}
						if len(updatedTemplate.Spec.Containers) > 0 {
							desiredEnv = updatedTemplate.Spec.Containers[0].Env
						}
						merged := mergeEnvVars(currentEnv, desiredEnv, previousOwned)

						sts.Spec.Template = *updatedTemplate
						if len(sts.Spec.Template.Spec.Containers) > 0 {
							sts.Spec.Template.Spec.Containers[0].Env = merged
						}
						writeOwnedEnvVarNames(sts, desiredEnv)
					} else {
						logger := log.FromContext(ctx)
						logger.V(1).Info("Skipping StatefulSet template update - no significant changes detected",
							"statefulSet", sts.Name,
							"namespace", sts.Namespace)
						// Keep existing template to prevent unnecessary pod restarts.
						// The owned-env-vars annotation does NOT need updating here:
						// if no template change was applied, the previously-recorded
						// owned set is still accurate by definition.
					}
				} else {
					// If no selector exists, just use the desired template
					sts.Spec.Template = desiredSpec.Template
					if len(desiredSpec.Template.Spec.Containers) > 0 {
						writeOwnedEnvVarNames(sts, desiredSpec.Template.Spec.Containers[0].Env)
					}
				}
			} else {
				// This is a new StatefulSet, use the desired spec as-is
				logger := log.FromContext(ctx)
				logger.V(1).Info("Creating new StatefulSet with full template",
					"statefulSet", sts.Name,
					"namespace", sts.Namespace)
				sts.Spec = desiredSpec
				if len(desiredSpec.Template.Spec.Containers) > 0 {
					writeOwnedEnvVarNames(sts, desiredSpec.Template.Spec.Containers[0].Env)
				}
			}
		}
		return nil
	})

	return err
}

// isTemplateChangeSignificant determines if StatefulSet template changes warrant pod restarts
// This prevents unnecessary pod restarts during cluster formation due to resource version conflicts
func (r *Neo4jEnterpriseClusterReconciler) isTemplateChangeSignificant(ctx context.Context, currentTemplate, desiredTemplate corev1.PodTemplateSpec, sts *appsv1.StatefulSet) bool {
	logger := log.FromContext(ctx)

	// During initial cluster formation (not all pods ready), be more conservative about template updates
	if sts.Status.ReadyReplicas < *sts.Spec.Replicas {
		logger.V(1).Info("StatefulSet still forming - being conservative about template updates",
			"statefulSet", sts.Name,
			"readyReplicas", sts.Status.ReadyReplicas,
			"desiredReplicas", *sts.Spec.Replicas)

		// Only allow critical changes during cluster formation
		return r.hasCriticalTemplateChanges(currentTemplate, desiredTemplate)
	}

	// For stable clusters, allow more template changes
	return r.hasSignificantTemplateChanges(currentTemplate, desiredTemplate, readOwnedEnvVarNames(sts))
}

// hasCriticalTemplateChanges checks for changes that are essential during cluster formation
func (r *Neo4jEnterpriseClusterReconciler) hasCriticalTemplateChanges(current, desired corev1.PodTemplateSpec) bool {
	// Check for image changes (critical)
	if len(current.Spec.Containers) != len(desired.Spec.Containers) {
		return true
	}

	for i, currentContainer := range current.Spec.Containers {
		if i >= len(desired.Spec.Containers) {
			return true
		}
		desiredContainer := desired.Spec.Containers[i]

		// Image changes are critical
		if currentContainer.Image != desiredContainer.Image {
			return true
		}

		// Resource limit changes are critical
		if !r.resourcesEqual(currentContainer.Resources, desiredContainer.Resources) {
			return true
		}

		// Security context changes are critical
		if !r.containerSecurityContextEqual(currentContainer.SecurityContext, desiredContainer.SecurityContext) {
			return true
		}
	}

	// Check for security context changes (critical)
	if !r.securityContextEqual(current.Spec.SecurityContext, desired.Spec.SecurityContext) {
		return true
	}

	// Check for service account changes (critical for RBAC)
	if current.Spec.ServiceAccountName != desired.Spec.ServiceAccountName {
		return true
	}

	// No critical changes found
	return false
}

// hasSignificantTemplateChanges checks for any meaningful changes in stable clusters.
// previousOwned is the env-var name set this controller recorded as owned on the
// previous reconcile (read from the StatefulSet annotation by the caller). It is
// used by envVarsEqual to detect removals from the controller's own desired set.
func (r *Neo4jEnterpriseClusterReconciler) hasSignificantTemplateChanges(current, desired corev1.PodTemplateSpec, previousOwned map[string]struct{}) bool {
	// For stable clusters, allow more changes including:
	// - Environment variable updates
	// - Volume mount changes
	// - Label/annotation updates (beyond selector labels)
	// - Init container changes

	// First check critical changes
	if r.hasCriticalTemplateChanges(current, desired) {
		return true
	}

	// Check for environment variable changes
	if len(current.Spec.Containers) > 0 && len(desired.Spec.Containers) > 0 {
		if !r.envVarsEqual(current.Spec.Containers[0].Env, desired.Spec.Containers[0].Env, previousOwned) {
			return true
		}
	}

	// Check for volume changes
	if !r.volumesEqual(current.Spec.Volumes, desired.Spec.Volumes) {
		return true
	}

	// Check for init container changes
	if !r.initContainersEqual(current.Spec.InitContainers, desired.Spec.InitContainers) {
		return true
	}

	return false
}

// Helper functions for template comparison
func (r *Neo4jEnterpriseClusterReconciler) resourcesEqual(current, desired corev1.ResourceRequirements) bool {
	// Compare CPU and memory limits/requests
	currentCPU := current.Limits.Cpu()
	desiredCPU := desired.Limits.Cpu()
	if (currentCPU == nil) != (desiredCPU == nil) || (currentCPU != nil && !currentCPU.Equal(*desiredCPU)) {
		return false
	}

	currentMem := current.Limits.Memory()
	desiredMem := desired.Limits.Memory()
	if (currentMem == nil) != (desiredMem == nil) || (currentMem != nil && !currentMem.Equal(*desiredMem)) {
		return false
	}

	return true
}

func (r *Neo4jEnterpriseClusterReconciler) securityContextEqual(current, desired *corev1.PodSecurityContext) bool {
	return equality.Semantic.DeepEqual(current, desired)
}

func (r *Neo4jEnterpriseClusterReconciler) containerSecurityContextEqual(current, desired *corev1.SecurityContext) bool {
	return equality.Semantic.DeepEqual(current, desired)
}

// ownedEnvVarsAnnotation is written to the StatefulSet by the cluster
// controller and lists the env-var names that this controller considered
// "owned" on the most recent reconcile. The list lets a subsequent reconcile
// distinguish a removal-from-desired (name in previous-owned, absent from
// desired) from a foreign env var added by another controller (name not in
// previous-owned and not in desired) so that removals can be applied without
// disturbing plugin / fleet additions. Names are stored sorted, comma-
// separated. Same shape as kubectl's last-applied-configuration tracking.
const ownedEnvVarsAnnotation = "neo4j.com/cluster-controller-env-vars"

// readOwnedEnvVarNames returns the set of env-var names this controller
// recorded as owned on the StatefulSet's previous reconcile. An empty set is
// returned when the annotation is absent (first reconcile after upgrade or
// brand-new StatefulSet) — the caller must treat that as "no removals to
// reconcile this round" so the upgrade path is non-disruptive.
func readOwnedEnvVarNames(sts *appsv1.StatefulSet) map[string]struct{} {
	out := map[string]struct{}{}
	raw := sts.Annotations[ownedEnvVarsAnnotation]
	if raw == "" {
		return out
	}
	for _, name := range strings.Split(raw, ",") {
		if name = strings.TrimSpace(name); name != "" {
			out[name] = struct{}{}
		}
	}
	return out
}

// writeOwnedEnvVarNames records the controller-owned env-var names on the
// StatefulSet annotation. Names are extracted from desired and stored sorted
// for stable diffs.
func writeOwnedEnvVarNames(sts *appsv1.StatefulSet, desired []corev1.EnvVar) {
	names := make([]string, 0, len(desired))
	for _, e := range desired {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	if sts.Annotations == nil {
		sts.Annotations = map[string]string{}
	}
	sts.Annotations[ownedEnvVarsAnnotation] = strings.Join(names, ",")
}

// mergeEnvVars produces the env-var slice the cluster controller should write
// to the StatefulSet container after deciding to apply a template change:
//
//   - desired: applied (added or value-overwritten by name).
//   - previousOwned ∖ desired: removed from current (a key the controller
//     used to own and no longer wants — the case the old subset check missed).
//   - current ∖ previousOwned ∖ desired: preserved (foreign env vars added by
//     the plugin / fleet / Aura controllers, which the cluster controller
//     must not clobber to avoid cross-controller oscillation).
//
// Output order: foreign vars first (in their original relative order), then
// desired. Stable across reconciles for unchanged input.
func mergeEnvVars(current, desired []corev1.EnvVar, previousOwned map[string]struct{}) []corev1.EnvVar {
	desiredNames := make(map[string]struct{}, len(desired))
	for _, e := range desired {
		desiredNames[e.Name] = struct{}{}
	}
	final := make([]corev1.EnvVar, 0, len(current)+len(desired))
	for _, e := range current {
		if _, owned := previousOwned[e.Name]; owned {
			continue // owned-by-us — desired is the source of truth below.
		}
		if _, inDesired := desiredNames[e.Name]; inDesired {
			continue // in desired — desired wins below.
		}
		final = append(final, e)
	}
	final = append(final, desired...)
	return final
}

// envVarsEqual returns true when no env-var change is needed. It performs a
// subset check (every env var in desired must be present in current with the
// correct value) plus a removal check against previousOwned: a name in
// previousOwned that is no longer in desired but is still in current means
// the controller needs to apply a removal.
//
// The subset (rather than strict equality) on the desired side is intentional.
// The Neo4jPlugin controller (and reconcileAuraFleetManagement) live-patches
// the StatefulSet to add env vars (NEO4J_PLUGINS, NEO4J_APOC_*, fleet token
// vars) that are not part of the cluster controller's desired template. A
// strict count check would make those additions look like a "significant
// change", causing the cluster controller to overwrite the StatefulSet on
// every reconcile and creating an infinite oscillation between the
// controllers. The previousOwned-driven removal check closes the corollary
// gap that was tracked as a known limitation prior to this fix: removals
// from the cluster controller's own desired set are now detected and applied,
// but only for names this controller previously claimed. Names it never
// owned (foreign vars added by plugin/fleet) are still left alone.
func (r *Neo4jEnterpriseClusterReconciler) envVarsEqual(current, desired []corev1.EnvVar, previousOwned map[string]struct{}) bool {
	currentMap := make(map[string]corev1.EnvVar)
	for _, env := range current {
		currentMap[env.Name] = env
	}

	desiredNames := make(map[string]struct{}, len(desired))
	for _, env := range desired {
		desiredNames[env.Name] = struct{}{}
		currentEnv, exists := currentMap[env.Name]
		if !exists {
			// A desired env var is absent from current — needs update.
			return false
		}
		if currentEnv.Value != env.Value {
			// Desired env var present but with wrong value — needs update.
			return false
		}
		if !r.envVarSourceEqual(currentEnv.ValueFrom, env.ValueFrom) {
			// SecretKeyRef / ConfigMapKeyRef / FieldRef mismatch — needs update.
			return false
		}
	}

	// Removal check: a name in previousOwned that is no longer in desired
	// but is still in current means the controller used to manage it,
	// has dropped it from desired (e.g. user removed a spec.config key),
	// and needs to apply the removal.
	for name := range previousOwned {
		if _, inDesired := desiredNames[name]; inDesired {
			continue
		}
		if _, stillPresent := currentMap[name]; stillPresent {
			return false
		}
	}

	return true
}

// envVarSourceEqual compares two EnvVarSource pointers for the purposes of template
// change detection. Nil == Nil, and non-nil sources are compared structurally.
func (r *Neo4jEnterpriseClusterReconciler) envVarSourceEqual(a, b *corev1.EnvVarSource) bool {
	if a == nil && b == nil {
		return true
	}
	if (a == nil) != (b == nil) {
		return false
	}
	return equality.Semantic.DeepEqual(a, b)
}

func (r *Neo4jEnterpriseClusterReconciler) volumesEqual(current, desired []corev1.Volume) bool {
	if len(current) != len(desired) {
		return false
	}

	// Create maps for easier comparison
	currentMap := make(map[string]corev1.Volume)
	for _, vol := range current {
		currentMap[vol.Name] = vol
	}

	for _, vol := range desired {
		if _, exists := currentMap[vol.Name]; !exists {
			return false
		}
		// Note: We're doing basic name comparison here. For more sophisticated
		// comparison, we could compare volume sources, but this is sufficient
		// for our use case during cluster formation
	}

	return true
}

func (r *Neo4jEnterpriseClusterReconciler) initContainersEqual(current, desired []corev1.Container) bool {
	if len(current) != len(desired) {
		return false
	}

	for i, currentContainer := range current {
		if i >= len(desired) {
			return false
		}
		desiredContainer := desired[i]

		// Compare names and images
		if currentContainer.Name != desiredContainer.Name ||
			currentContainer.Image != desiredContainer.Image {
			return false
		}
	}

	return true
}

func (r *Neo4jEnterpriseClusterReconciler) updateClusterStatus(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster, phase, message string) bool {
	logger := log.FromContext(ctx)
	statusChanged := false

	update := func() error {
		// Get latest version
		latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
			return err
		}

		// Check if status is already exactly what we want
		readyBool := (phase == "Ready")
		statusNeedsUpdate := latest.Status.Phase != phase || latest.Status.Message != message ||
			latest.Status.Ready != readyBool || latest.Status.ObservedGeneration != latest.Generation

		// Determine standard condition status and reason
		condStatus, condReason := PhaseToConditionStatus(phase)
		conditionNeedsUpdate := true

		// Check existing condition against standardized values
		for _, cond := range latest.Status.Conditions {
			if cond.Type == ConditionTypeReady {
				if cond.Status == condStatus && cond.Reason == condReason && cond.Message == message {
					conditionNeedsUpdate = false
				}
				break
			}
		}

		// If neither status nor condition needs update, skip entirely
		if !statusNeedsUpdate && !conditionNeedsUpdate {
			logger.V(1).Info("Status and condition already correct, skipping update",
				"phase", phase, "message", message)
			statusChanged = false
			return nil
		}

		// Log what we're updating
		logger.V(1).Info("Updating cluster status",
			"phase", phase, "message", message,
			"statusNeedsUpdate", statusNeedsUpdate,
			"conditionNeedsUpdate", conditionNeedsUpdate)

		// Update status fields
		latest.Status.Phase = phase
		latest.Status.Message = message
		latest.Status.Ready = readyBool
		latest.Status.ObservedGeneration = latest.Generation

		// Populate connection endpoints + connection examples. Cluster
		// previously left status.endpoints unset; surfacing these lets users
		// `kubectl get neo4jenterprisecluster -o jsonpath='{.status.endpoints.connectionExamples.boltURI}'`
		// to copy a working connection string. Service-type and external-IP
		// resolution mirrors what the connection_helper helpers already do
		// for the standalone path.
		latest.Status.Endpoints = r.buildClusterEndpoints(ctx, cluster)

		// Replica count (desired vs ready). Pulls Ready directly off the
		// server StatefulSet status — the controller-runtime cache already
		// has the StatefulSet, so this is a free lookup.
		latest.Status.Replicas = r.buildReplicaStatus(ctx, cluster)

		// Update Ready condition using standard helper
		SetReadyCondition(&latest.Status.Conditions, latest.Generation, condStatus, condReason, message)

		// Record Prometheus phase metric on every phase transition
		clusterM := metrics.NewClusterMetrics(cluster.Name, cluster.Namespace)
		clusterM.RecordClusterPhase(phase)
		if phase == "Ready" {
			clusterM.RecordClusterHealth(true)
			var ready int32
			if latest.Status.Replicas != nil {
				ready = latest.Status.Replicas.Ready
			}
			clusterM.RecordClusterReplicas(cluster.Spec.Topology.Servers, ready)
		} else if phase == "Failed" || phase == "Degraded" {
			clusterM.RecordClusterHealth(false)
		} else if phase == "Forming" {
			clusterM.RecordClusterHealth(false)
		}

		statusChanged = true
		return r.Status().Update(ctx, latest)
	}

	err := retry.RetryOnConflict(retry.DefaultBackoff, update)
	if err != nil {
		logger.Error(err, "Failed to update cluster status")
		return false
	}
	return statusChanged
}

// buildReplicaStatus reports the desired vs ready server count by reading
// the live `{cluster}-server` StatefulSet's status.readyReplicas. Returns
// nil if the StatefulSet doesn't exist yet (early in reconcile, before the
// resource builder has run) — better than reporting fictional zeros.
func (r *Neo4jEnterpriseClusterReconciler) buildReplicaStatus(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *neo4jv1beta1.ReplicaStatus {
	sts := &appsv1.StatefulSet{}
	stsKey := types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      fmt.Sprintf("%s-server", cluster.Name),
	}
	if err := r.Get(ctx, stsKey, sts); err != nil {
		return nil
	}
	return &neo4jv1beta1.ReplicaStatus{
		Servers: cluster.Spec.Topology.Servers,
		Ready:   sts.Status.ReadyReplicas,
	}
}

// buildClusterEndpoints assembles the EndpointStatus surfaced via
// status.endpoints. Returns the in-cluster {cluster}-client Service URLs
// scheme-adjusted for TLS, plus the connection-example helper output keyed
// off the configured Service type. External-IP resolution looks at the live
// Service status and substitutes a `<external-ip>` placeholder when no IP
// has been assigned yet.
func (r *Neo4jEnterpriseClusterReconciler) buildClusterEndpoints(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *neo4jv1beta1.EndpointStatus {
	hasTLS := cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == "cert-manager"
	boltScheme := "bolt"
	if hasTLS {
		boltScheme = "bolt+s"
	}

	serviceType := corev1.ServiceTypeClusterIP
	if cluster.Spec.Service != nil && cluster.Spec.Service.Type != "" {
		serviceType = corev1.ServiceType(cluster.Spec.Service.Type)
	}
	externalIP := clusterServiceExternalIP(ctx, r.Client, cluster)

	return &neo4jv1beta1.EndpointStatus{
		Bolt:  fmt.Sprintf("%s://%s-client.%s.svc.cluster.local:7687", boltScheme, cluster.Name, cluster.Namespace),
		HTTP:  fmt.Sprintf("http://%s-client.%s.svc.cluster.local:7474", cluster.Name, cluster.Namespace),
		HTTPS: fmt.Sprintf("https://%s-client.%s.svc.cluster.local:7473", cluster.Name, cluster.Namespace),
		Internal: &neo4jv1beta1.InternalEndpoints{
			Headless: fmt.Sprintf("%s-headless.%s.svc.cluster.local", cluster.Name, cluster.Namespace),
			Client:   fmt.Sprintf("%s-client.%s.svc.cluster.local", cluster.Name, cluster.Namespace),
		},
		ConnectionExamples: GenerateConnectionExamples(cluster.Name, cluster.Namespace, serviceType, externalIP, hasTLS),
	}
}

// createExternalSecretForTLS creates an ExternalSecret resource for TLS certificates
func (r *Neo4jEnterpriseClusterReconciler) createExternalSecretForTLS(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	esData := resources.BuildExternalSecretForTLS(cluster)
	if esData == nil {
		return nil
	}

	// Convert map to unstructured object
	obj := &unstructured.Unstructured{}
	obj.SetUnstructuredContent(esData)

	return r.createOrUpdateUnstructuredResource(ctx, obj, cluster)
}

// createExternalSecretForAuth creates an ExternalSecret resource for authentication secrets
func (r *Neo4jEnterpriseClusterReconciler) createExternalSecretForAuth(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	esData := resources.BuildExternalSecretForAuth(cluster)
	if esData == nil {
		return nil
	}

	// Convert map to unstructured object
	obj := &unstructured.Unstructured{}
	obj.SetUnstructuredContent(esData)

	return r.createOrUpdateUnstructuredResource(ctx, obj, cluster)
}

// createOrUpdateUnstructuredResource handles unstructured resources like ExternalSecrets
func (r *Neo4jEnterpriseClusterReconciler) createOrUpdateUnstructuredResource(ctx context.Context, obj *unstructured.Unstructured, owner client.Object) error {
	// Set owner reference
	if err := controllerutil.SetControllerReference(owner, obj, r.Scheme); err != nil {
		return err
	}

	// Try to get the existing resource
	key := client.ObjectKeyFromObject(obj)
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())

	err := r.Get(ctx, key, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create the resource
			return r.Create(ctx, obj)
		}
		return err
	}

	// Update the resource
	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// isUpgradeRequired checks if an image upgrade is needed
func (r *Neo4jEnterpriseClusterReconciler) isUpgradeRequired(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) bool {
	// Skip upgrade check if cluster is not ready
	if cluster.Status.Phase != "Ready" {
		return false
	}

	// Skip if upgrade is already in progress
	if cluster.Status.UpgradeStatus != nil &&
		(cluster.Status.UpgradeStatus.Phase == "InProgress" || cluster.Status.UpgradeStatus.Phase == "Paused") {
		return false
	}

	// Check the unified server StatefulSet for image drift
	serverSts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-server", cluster.Name),
		Namespace: cluster.Namespace,
	}, serverSts); err != nil {
		return false // StatefulSet doesn't exist yet
	}

	// Compare current image with desired image
	if len(serverSts.Spec.Template.Spec.Containers) == 0 {
		return false // StatefulSet has no containers defined
	}
	currentImage := serverSts.Spec.Template.Spec.Containers[0].Image
	desiredImage := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)

	return currentImage != desiredImage
}

// handleRollingUpgrade manages the rolling upgrade process
func (r *Neo4jEnterpriseClusterReconciler) handleRollingUpgrade(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("rolling-upgrade-handler")

	// Check if upgrade strategy allows rolling upgrades
	if cluster.Spec.UpgradeStrategy != nil && cluster.Spec.UpgradeStrategy.Strategy == "Recreate" {
		logger.Info("Using recreate strategy, falling back to regular reconciliation")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Create Neo4j client for cluster health checks
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to create Neo4j client for upgrade")
		_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create Neo4j client: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}
	defer func() {
		if err := neo4jClient.Close(); err != nil {
			logger.Error(err, "Failed to close Neo4j client")
		}
	}()

	// Create rolling upgrade orchestrator
	upgrader := NewRollingUpgradeOrchestrator(r.Client, cluster.Name, cluster.Namespace)

	r.Recorder.Eventf(cluster, corev1.EventTypeNormal, EventReasonUpgradeStarted,
		"Rolling upgrade started: %s -> %s", cluster.Status.Version, cluster.Spec.Image.Tag)

	// Execute rolling upgrade
	if err := upgrader.ExecuteRollingUpgrade(ctx, cluster, neo4jClient); err != nil {
		logger.Error(err, "Rolling upgrade failed")

		// Check if auto-pause is enabled
		if cluster.Spec.UpgradeStrategy != nil && cluster.Spec.UpgradeStrategy.AutoPauseOnFailure {
			_ = r.updateClusterStatus(ctx, cluster, "Paused", "Upgrade paused due to failure - manual intervention required")
			r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonUpgradePaused, fmt.Sprintf("Upgrade paused: %v", err))
			return ctrl.Result{}, nil // Don't requeue automatically
		}

		r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonUpgradeFailed,
			"Rolling upgrade failed: %v", err)
		_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Rolling upgrade failed: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update cluster status and version
	_ = r.updateClusterStatus(ctx, cluster, "Ready", "Rolling upgrade completed successfully")
	cluster.Status.Version = cluster.Spec.Image.Tag
	if err := r.Status().Update(ctx, cluster); err != nil {
		logger.Error(err, "Failed to update cluster status")
	}

	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonUpgradeCompleted, "Rolling upgrade completed successfully")
	logger.Info("Rolling upgrade completed successfully")

	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

// createNeo4jClient creates a Neo4j client for cluster operations
func (r *Neo4jEnterpriseClusterReconciler) createNeo4jClient(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (*neo4jclient.Client, error) {
	// Get admin credentials
	adminSecretName := DefaultAdminSecretName
	if cluster.Spec.Auth != nil && cluster.Spec.Auth.AdminSecret != "" {
		adminSecretName = cluster.Spec.Auth.AdminSecret
	}

	// Create Neo4j client
	neo4jClient, err := neo4jclient.NewClientForEnterprise(cluster, r.Client, adminSecretName)
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j client: %w", err)
	}

	return neo4jClient, nil
}

// verifyNeo4jClusterFormation checks if Neo4j cluster formation is complete and detects split-brain scenarios
func (r *Neo4jEnterpriseClusterReconciler) verifyNeo4jClusterFormation(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (bool, string, error) {
	logger := log.FromContext(ctx)

	// Skip verification for single-server clusters (always formed)
	expectedServers := int(cluster.Spec.Topology.Servers)
	if expectedServers == 1 {
		return true, "Single server cluster - formation complete", nil
	}

	// First, check if Neo4j is ready to accept connections using legacy check
	// Only run split-brain detection if we can connect to Neo4j
	canConnect := false
	var connectError error
	var testError error
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err == nil {
		// Test the connection by trying to get server list
		_, testErr := neo4jClient.GetServerList(ctx)
		neo4jClient.Close()
		if testErr == nil {
			canConnect = true
		} else {
			testError = testErr
		}
	} else {
		connectError = err
	}

	if !canConnect {
		// Neo4j is not ready to accept connections yet, wait for it
		logger.Info("Neo4j not ready to accept connections, waiting...",
			"clientError", connectError, "testError", testError)
		return false, "Waiting for Neo4j to accept connections", nil
	}

	// Now perform split-brain detection since Neo4j is responsive
	logger.Info("Neo4j is responsive, performing split-brain detection")

	// Use the reconciler's SplitBrainDetector if available, otherwise create a new one
	splitBrainDetector := r.SplitBrainDetector
	if splitBrainDetector == nil {
		splitBrainDetector = NewSplitBrainDetector(r.Client)
	}
	analysis, err := splitBrainDetector.DetectSplitBrain(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to perform split-brain detection, falling back to legacy check")
		// Fall back to legacy cluster formation check
		isFormed, message, legacyErr := r.legacyClusterFormationCheck(ctx, cluster, expectedServers)
		logger.Info("Legacy cluster formation check result",
			"isFormed", isFormed, "message", message, "error", legacyErr)
		return isFormed, message, legacyErr
	}

	logger.Info("Split-brain analysis results",
		"isSplitBrain", analysis.IsSplitBrain,
		"repairAction", analysis.RepairAction,
		"orphanedPods", len(analysis.OrphanedPods),
		"errorMessage", analysis.ErrorMessage)

	// Handle split-brain scenarios
	if analysis.IsSplitBrain {
		logger.Info("Split-brain detected in Neo4j cluster",
			"orphanedPods", analysis.OrphanedPods,
			"repairAction", analysis.RepairAction)

		// Record event about split-brain detection
		r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonSplitBrainDetected,
			"Split-brain detected: %s", analysis.ErrorMessage)
		metrics.RecordSplitBrainDetected(cluster.Name, cluster.Namespace)

		// Attempt automatic repair if configured
		if analysis.RepairAction == RepairActionRestartPods {
			logger.Info("Attempting automatic split-brain repair by restarting orphaned pods",
				"orphanedPods", analysis.OrphanedPods)

			repairErr := splitBrainDetector.RepairSplitBrain(ctx, cluster, analysis)
			if repairErr != nil {
				logger.Error(repairErr, "Failed to repair split-brain automatically")
				r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonSplitBrainRepairFailed,
					"Automatic split-brain repair failed: %v", repairErr)
				return false, fmt.Sprintf("Split-brain repair failed: %v", repairErr), nil
			}

			r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonSplitBrainRepaired,
				"Split-brain automatically repaired by restarting orphaned pods")

			// After repair, cluster needs time to reform
			return false, "Split-brain repaired, waiting for cluster reformation", nil
		}

		// For other repair actions, report but don't auto-repair
		return false, fmt.Sprintf("Split-brain detected: %s", analysis.ErrorMessage), nil
	}

	// If no split-brain, check if cluster formation is complete
	if analysis.RepairAction == RepairActionWaitForming {
		return false, analysis.ErrorMessage, nil
	}

	// Verify we have the expected number of servers across all views
	if len(analysis.ClusterViews) > 0 && analysis.LargestCluster.ConnectionError == nil {
		availableServers := 0
		for _, server := range analysis.LargestCluster.Servers {
			if server.State == "Enabled" && server.Health == "Available" {
				availableServers++
			}
		}

		if availableServers >= expectedServers {
			return true, fmt.Sprintf("Cluster formation complete: %d/%d servers available", availableServers, expectedServers), nil
		} else {
			return false, fmt.Sprintf("Cluster forming: %d/%d servers available", availableServers, expectedServers), nil
		}
	}

	// Fall back to legacy cluster formation check if split-brain analysis was inconclusive
	logger.Info("Falling back to legacy cluster formation check after split-brain analysis")
	isFormed, message, legacyErr := r.legacyClusterFormationCheck(ctx, cluster, expectedServers)
	logger.Info("Final legacy cluster formation check result",
		"isFormed", isFormed, "message", message, "error", legacyErr)
	return isFormed, message, legacyErr
}

// legacyClusterFormationCheck performs the original cluster formation verification
func (r *Neo4jEnterpriseClusterReconciler) legacyClusterFormationCheck(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster, expectedServers int) (bool, string, error) {
	logger := log.FromContext(ctx)

	// Create Neo4j client to check cluster status
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		// If we can't connect to Neo4j yet, cluster is still forming
		return false, "Waiting for Neo4j to accept connections", nil
	}
	defer func() {
		if closeErr := neo4jClient.Close(); closeErr != nil {
			logger.Error(closeErr, "Failed to close Neo4j client")
		}
	}()

	// Check cluster formation using SHOW SERVERS with stability verification
	for attempt := 1; attempt <= 3; attempt++ {
		servers, err := neo4jClient.GetServerList(ctx)
		if err != nil {
			logger.Info("Cluster formation check failed", "attempt", attempt, "error", err)
			if attempt == 3 {
				// Final attempt failed
				return false, fmt.Sprintf("Cannot query server list after 3 attempts: %v", err), nil
			}
			// Wait 2 seconds before retry
			select {
			case <-ctx.Done():
				return false, "Context cancelled during cluster formation check", nil
			case <-time.After(2 * time.Second):
				continue
			}
		}

		// Count available servers
		availableServers := 0
		for _, server := range servers {
			if server.State == "Enabled" && server.Health == "Available" {
				availableServers++
			}
		}

		logger.Info("Cluster formation status",
			"attempt", attempt,
			"expectedServers", expectedServers,
			"availableServers", availableServers,
			"totalServers", len(servers))

		// Check if we have the expected number of servers
		if availableServers >= expectedServers {
			// Add a small stability delay to ensure service is consistently available
			if attempt == 1 {
				select {
				case <-ctx.Done():
					return false, "Context cancelled during stability check", nil
				case <-time.After(3 * time.Second):
					// Continue to verify stability
					continue
				}
			}
			// Cluster formation is complete and stable
			return true, fmt.Sprintf("Cluster formation complete and stable: %d/%d servers available", availableServers, expectedServers), nil
		}

		// Not enough servers, wait before retry
		if attempt < 3 {
			select {
			case <-ctx.Done():
				return false, "Context cancelled during cluster formation wait", nil
			case <-time.After(5 * time.Second):
				continue
			}
		}
	}

	// Should not reach here, but handle gracefully
	return false, "Cluster formation verification incomplete", nil
}

// NewQueryMonitor creates a new query monitor
func NewQueryMonitor(client client.Client, scheme *runtime.Scheme) *QueryMonitor {
	return &QueryMonitor{
		Client: client,
		Scheme: scheme,
	}
}

// QueryMonitor handles query performance monitoring
type QueryMonitor struct {
	client.Client
	Scheme *runtime.Scheme
}

// ReconcileMonitoring sets up query monitoring for the cluster
func (qm *QueryMonitor) ReconcileMonitoring(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Setting up query monitoring", "cluster", cluster.Name)

	// Set up metrics collection
	if err := qm.setupMetricsCollection(ctx, cluster); err != nil {
		return fmt.Errorf("failed to setup metrics: %w", err)
	}

	// Configure alerting rules if metrics export is enabled
	if cluster.Spec.Monitoring != nil && cluster.Spec.Monitoring.MetricsExport != nil && cluster.Spec.Monitoring.MetricsExport.Prometheus {
		if err := qm.setupAlertingRules(ctx, cluster); err != nil {
			return fmt.Errorf("failed to setup alerting rules: %w", err)
		}
	}

	logger.Info("Query monitoring setup completed successfully")
	return nil
}

// setupMetricsCollection sets up metrics collection for the cluster
func (qm *QueryMonitor) setupMetricsCollection(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Setting up metrics collection", "cluster", cluster.Name)

	// Create ServiceMonitor for Prometheus integration
	serviceMonitor := &unstructured.Unstructured{}
	serviceMonitor.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "ServiceMonitor",
	})
	serviceMonitor.SetName(cluster.Name + "-monitoring")
	serviceMonitor.SetNamespace(cluster.Namespace)
	serviceMonitor.SetLabels(map[string]string{
		"app":               "neo4j",
		"neo4j.com/cluster": cluster.Name,
	})

	// Set ServiceMonitor spec
	serviceMonitor.Object["spec"] = map[string]any{
		"selector": map[string]any{
			"matchLabels": map[string]any{
				"app.kubernetes.io/name": "neo4j",
				"neo4j.com/cluster":      cluster.Name,
				"neo4j.com/role":         "metrics",
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

	// Set owner reference
	if err := ctrl.SetControllerReference(cluster, serviceMonitor, qm.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create ServiceMonitor (ignore if CRD not available)
	if err := qm.Create(ctx, serviceMonitor); err != nil {
		if !errors.IsAlreadyExists(err) && !errors.IsNotFound(err) {
			logger.Info("ServiceMonitor creation failed (Prometheus Operator may not be installed)", "error", err.Error())
		}
	}

	logger.Info("Metrics collection setup completed")
	return nil
}

// setupAlertingRules sets up alerting rules for query monitoring
func (qm *QueryMonitor) setupAlertingRules(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Setting up alerting rules", "cluster", cluster.Name)

	// Create PrometheusRule for alerting
	prometheusRule := &unstructured.Unstructured{}
	prometheusRule.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "PrometheusRule",
	})
	prometheusRule.SetName(cluster.Name + "-query-alerts")
	prometheusRule.SetNamespace(cluster.Namespace)
	prometheusRule.SetLabels(map[string]string{
		"app":               "neo4j",
		"neo4j.com/cluster": cluster.Name,
		"prometheus":        "kube-prometheus",
		"role":              "alert-rules",
	})

	// Generate alerting rules using actual Neo4j Prometheus metric names.
	// Neo4j exposes query latency as a histogram in milliseconds and heap as gauges in bytes.
	rules := []map[string]any{
		{
			"alert": "Neo4jSlowQueries",
			"expr":  "histogram_quantile(0.99, rate(neo4j_db_query_execution_latency_millis_bucket[5m])) > 5000",
			"for":   "5m",
			"labels": map[string]any{
				"severity": "warning",
			},
			"annotations": map[string]any{
				"summary":     "Neo4j slow queries detected",
				"description": "Neo4j cluster {{ $labels.namespace }}/{{ $labels.job }} p99 query latency exceeds 5 seconds",
			},
		},
		{
			"alert": "Neo4jHighHeapUsage",
			"expr":  "neo4j_vm_heap_used / neo4j_vm_heap_max > 0.8",
			"for":   "5m",
			"labels": map[string]any{
				"severity": "warning",
			},
			"annotations": map[string]any{
				"summary":     "Neo4j high heap usage",
				"description": "Neo4j cluster {{ $labels.namespace }}/{{ $labels.job }} JVM heap usage is above 80%",
			},
		},
	}

	prometheusRule.Object["spec"] = map[string]any{
		"groups": []map[string]any{
			{
				"name":  "neo4j-monitoring",
				"rules": rules,
			},
		},
	}

	// Set owner reference
	if err := ctrl.SetControllerReference(cluster, prometheusRule, qm.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create PrometheusRule (ignore if CRD not available)
	if err := qm.Create(ctx, prometheusRule); err != nil {
		if !errors.IsAlreadyExists(err) && !errors.IsNotFound(err) {
			logger.Info("PrometheusRule creation failed (Prometheus Operator may not be installed)", "error", err.Error())
		}
	}

	logger.Info("Alerting rules setup completed")
	return nil
}

// CollectDiagnostics runs SHOW SERVERS and SHOW DATABASES against the cluster
// and writes the results into status.diagnostics and status.conditions.
// Non-blocking: all errors are surfaced in status but do not fail the reconciliation loop.
func (qm *QueryMonitor) CollectDiagnostics(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster, neo4jClient *neo4jclient.Client) error {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Collecting cluster diagnostics", "cluster", cluster.Name)

	diagnostics := &neo4jv1beta1.ClusterDiagnosticsStatus{}

	// Collect server list
	servers, serverErr := neo4jClient.GetServerList(ctx)
	if serverErr != nil {
		logger.Error(serverErr, "Failed to collect SHOW SERVERS")
		diagnostics.CollectionError = fmt.Sprintf("SHOW SERVERS failed: %v", serverErr)
	} else {
		for _, s := range servers {
			diagnostics.Servers = append(diagnostics.Servers, neo4jv1beta1.ServerDiagnosticInfo{
				Name:             s.Name,
				Address:          s.Address,
				State:            s.State,
				Health:           s.Health,
				HostingDatabases: len(s.Hosting),
			})
		}
		// Record per-server health metrics
		clusterM := metrics.NewClusterMetrics(cluster.Name, cluster.Namespace)
		serverHealthData := make([]metrics.ServerHealth, 0, len(servers))
		for _, s := range servers {
			serverHealthData = append(serverHealthData, metrics.ServerHealth{
				Name:      s.Name,
				Address:   s.Address,
				Enabled:   s.State == "Enabled",
				Available: s.Health == "Available",
			})
		}
		clusterM.RecordServerHealth(serverHealthData)
	}

	// Collect database list
	databases, dbErr := neo4jClient.GetDatabases(ctx)
	if dbErr != nil {
		logger.Error(dbErr, "Failed to collect SHOW DATABASES")
		if diagnostics.CollectionError == "" {
			diagnostics.CollectionError = fmt.Sprintf("SHOW DATABASES failed: %v", dbErr)
		} else {
			diagnostics.CollectionError += fmt.Sprintf("; SHOW DATABASES failed: %v", dbErr)
		}
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

	// Collect users + roles. Best-effort: if the connected user lacks
	// SHOW USER / SHOW ROLE privilege, we leave the lists empty rather than
	// failing the whole diagnostics pass.
	collectUsersAndRoles(ctx, neo4jClient,
		&diagnostics.Users, &diagnostics.UserCount,
		&diagnostics.Roles, &diagnostics.RoleCount,
		&diagnostics.CollectionError, logger)

	now := metav1.Now()
	diagnostics.LastCollected = &now

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		if err := qm.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
			return err
		}

		latest.Status.Diagnostics = diagnostics
		qm.updateServersCondition(latest, servers, serverErr)
		qm.updateDatabasesCondition(latest, databases, dbErr)

		return qm.Status().Update(ctx, latest)
	})
}

// updateServersCondition sets the ServersHealthy condition from SHOW SERVERS results.
func (qm *QueryMonitor) updateServersCondition(cluster *neo4jv1beta1.Neo4jEnterpriseCluster, servers []neo4jclient.ServerInfo, collectErr error) {
	if collectErr != nil {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeServersHealthy,
			cluster.Generation, metav1.ConditionUnknown,
			ConditionReasonDiagnosticsUnavailable,
			fmt.Sprintf("Could not collect server list: %v", collectErr))
		return
	}
	if len(servers) == 0 {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeServersHealthy,
			cluster.Generation, metav1.ConditionUnknown,
			ConditionReasonDiagnosticsUnavailable, "No servers returned by SHOW SERVERS")
		return
	}

	var degraded []string
	for _, s := range servers {
		if s.State != "Enabled" || s.Health != "Available" {
			degraded = append(degraded, fmt.Sprintf("%s (state=%s health=%s)", s.Name, s.State, s.Health))
		}
	}

	if len(degraded) > 0 {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeServersHealthy,
			cluster.Generation, metav1.ConditionFalse,
			ConditionReasonServerDegraded,
			fmt.Sprintf("%d server(s) unhealthy: %s", len(degraded), strings.Join(degraded, ", ")))
	} else {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeServersHealthy,
			cluster.Generation, metav1.ConditionTrue,
			ConditionReasonAllServersHealthy,
			fmt.Sprintf("All %d servers are Enabled and Available", len(servers)))
	}
}

// updateDatabasesCondition sets the DatabasesHealthy condition from SHOW DATABASES results.
func (qm *QueryMonitor) updateDatabasesCondition(cluster *neo4jv1beta1.Neo4jEnterpriseCluster, databases []neo4jclient.DatabaseInfo, collectErr error) {
	if collectErr != nil {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeDatabasesHealthy,
			cluster.Generation, metav1.ConditionUnknown,
			ConditionReasonDiagnosticsUnavailable,
			fmt.Sprintf("Could not collect database list: %v", collectErr))
		return
	}
	if len(databases) == 0 {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeDatabasesHealthy,
			cluster.Generation, metav1.ConditionUnknown,
			ConditionReasonDiagnosticsUnavailable, "No databases returned by SHOW DATABASES")
		return
	}

	var offline []string
	userDBCount := 0
	for _, d := range databases {
		if d.Name == "system" {
			continue
		}
		userDBCount++
		if d.RequestedStatus == "online" && d.Status != "online" {
			offline = append(offline, fmt.Sprintf("%s (status=%s)", d.Name, d.Status))
		}
	}

	if len(offline) > 0 {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeDatabasesHealthy,
			cluster.Generation, metav1.ConditionFalse,
			ConditionReasonDatabaseOffline,
			fmt.Sprintf("%d database(s) not online: %s", len(offline), strings.Join(offline, ", ")))
	} else {
		SetNamedCondition(&cluster.Status.Conditions, ConditionTypeDatabasesHealthy,
			cluster.Generation, metav1.ConditionTrue,
			ConditionReasonAllDatabasesOnline,
			fmt.Sprintf("All %d user database(s) are online", userDBCount))
	}
}

// validatePropertyShardingConfiguration validates property sharding settings
func (r *Neo4jEnterpriseClusterReconciler) validatePropertyShardingConfiguration(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)

	// Validate Neo4j version supports property sharding (2025.12+)
	if err := validatePropertyShardingVersion(cluster.Spec.Image.Tag); err != nil {
		return fmt.Errorf("property sharding version validation failed: %w", err)
	}

	// Validate minimum cluster size for property sharding
	if cluster.Spec.Topology.Servers < 1 {
		return fmt.Errorf("property sharding requires at least 1 server, got %d", cluster.Spec.Topology.Servers)
	}
	if cluster.Spec.Topology.Servers < 3 {
		logger.Info("Property sharding running without HA (recommended: 3+ graph shard primaries)",
			"servers", cluster.Spec.Topology.Servers)
	}

	// Validate required configuration settings
	requiredSettings := map[string]string{
		"internal.dbms.sharded_property_database.enabled":                     "true",
		"db.query.default_language":                                           "CYPHER_25",
		"internal.dbms.sharded_property_database.allow_external_shard_access": "false",
	}

	if cluster.Spec.PropertySharding.Config != nil {
		for key, expectedValue := range requiredSettings {
			if actualValue, exists := cluster.Spec.PropertySharding.Config[key]; exists && actualValue != expectedValue {
				return fmt.Errorf("property sharding requires %s=%s, got %s=%s", key, expectedValue, key, actualValue)
			}
		}
	} else {
		// If config is nil, create it with required settings
		logger.Info("Property sharding config is empty, will use default required settings")
	}

	// Validate resource requirements with lenient but realistic minimums.
	//
	// The 4GB hard floor is the operator's defensive minimum, not a Neo4j
	// JVM requirement. Below 4GB the JVM running multiple shards can OOM
	// or thrash under real workloads. For CI / smoke tests with empty
	// databases this is overly strict — the env var
	// `NEO4J_SHARDING_RELAX_MEMORY_MIN=true` downgrades the hard reject to
	// a warning so a minimal sharded smoke test can run in GitHub-hosted
	// runners (2-server × 1.5Gi clusters). Never set this in production.
	relaxMemoryMin := os.Getenv("NEO4J_SHARDING_RELAX_MEMORY_MIN") == "true"
	if cluster.Spec.Resources != nil && cluster.Spec.Resources.Requests != nil {
		if memory := cluster.Spec.Resources.Requests.Memory(); memory != nil {
			memoryMB := memory.Value() / (1024 * 1024)
			if memoryMB < 8192 { // 8GB recommended for production
				logger.Info("Property sharding memory below recommended 8GB for production, proceeding with caution",
					"requestedMB", memoryMB, "recommendedMB", 8192)
			}
			if memoryMB < 4096 { // 4GB absolute minimum for dev/test
				if relaxMemoryMin {
					logger.Info("Property sharding memory below 4GB minimum; relaxed via NEO4J_SHARDING_RELAX_MEMORY_MIN — DEV/TEST ONLY",
						"requestedMB", memoryMB)
				} else {
					return fmt.Errorf("property sharding requires minimum 4GB memory for basic operation, got %dMB (recommended: 8GB+ for production)", memoryMB)
				}
			}
		}

		// Check CPU requirements (lenient validation)
		if cpu := cluster.Spec.Resources.Requests.Cpu(); cpu != nil {
			cpuMillis := cpu.MilliValue()
			if cpuMillis < 2000 { // 2 cores recommended minimum
				logger.Info("Property sharding CPU below recommended 2+ cores, may impact cross-shard query performance",
					"requestedMillis", cpuMillis, "recommendedMillis", 2000)
			}
			if cpuMillis < 1000 { // 1 core absolute minimum
				if relaxMemoryMin {
					logger.Info("Property sharding CPU below 1-core minimum; relaxed via NEO4J_SHARDING_RELAX_MEMORY_MIN — DEV/TEST ONLY",
						"requestedMillis", cpuMillis)
				} else {
					return fmt.Errorf("property sharding requires minimum 1 CPU core, got %dm (recommended: 2+ cores)", cpuMillis)
				}
			}
		}
	}

	logger.Info("Property sharding configuration validation passed")
	return nil
}

// validatePropertyShardingVersion checks if Neo4j version supports property sharding
func validatePropertyShardingVersion(imageTag string) error {
	if imageTag == "" {
		return fmt.Errorf("image tag is required for property sharding")
	}

	if resources.IsNeo4jVersion202512OrHigher(imageTag) {
		return nil
	}

	return fmt.Errorf("property sharding requires Neo4j 2025.12+ Enterprise, got %s", imageTag)
}

// updatePropertyShardingStatus updates the property sharding ready status
func (r *Neo4jEnterpriseClusterReconciler) updatePropertyShardingStatus(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster, ready bool) error {
	logger := log.FromContext(ctx)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get latest version
		latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
			return err
		}

		// Update property sharding ready status
		if latest.Status.PropertyShardingReady == nil || *latest.Status.PropertyShardingReady != ready {
			latest.Status.PropertyShardingReady = &ready
			if err := r.Status().Update(ctx, latest); err != nil {
				return err
			}
			logger.Info("Updated property sharding readiness", "ready", ready)
		}

		return nil
	})
}

// reconcileRoute ensures an OpenShift Route exists when requested
func (r *Neo4jEnterpriseClusterReconciler) reconcileRoute(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)

	route := resources.BuildRouteForEnterprise(cluster)
	if route == nil {
		return nil
	}

	if err := controllerutil.SetControllerReference(cluster, route, r.Scheme); err != nil {
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
				r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonRouteAPINotFound, "route.openshift.io/v1 not available; skipping Route reconciliation")
			}
			return nil
		}
		return fmt.Errorf("failed to create or update Route: %w", err)
	}

	logger.Info("Successfully reconciled Route", "name", route.GetName())
	return nil
}

func (r *Neo4jEnterpriseClusterReconciler) reconcileMCP(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	if cluster.Spec.MCP == nil || !cluster.Spec.MCP.Enabled {
		return nil
	}

	if service := resources.BuildMCPServiceForCluster(cluster); service != nil {
		if err := r.createOrUpdateResource(ctx, service, cluster); err != nil {
			return fmt.Errorf("failed to reconcile MCP service: %w", err)
		}
	}

	if deployment := resources.BuildMCPDeploymentForCluster(cluster); deployment != nil {
		if err := r.createOrUpdateResource(ctx, deployment, cluster); err != nil {
			return fmt.Errorf("failed to reconcile MCP deployment: %w", err)
		}
	}

	if ingress := resources.BuildMCPIngressForCluster(cluster); ingress != nil {
		if err := r.createOrUpdateResource(ctx, ingress, cluster); err != nil {
			return fmt.Errorf("failed to reconcile MCP ingress: %w", err)
		}
	}

	if err := r.reconcileMCPRoute(ctx, cluster); err != nil {
		return err
	}

	r.warnIfMCPMissingAPOC(ctx, cluster)
	return nil
}

func (r *Neo4jEnterpriseClusterReconciler) reconcileMCPRoute(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)

	route := resources.BuildMCPRouteForCluster(cluster)
	if route == nil {
		return nil
	}

	if err := controllerutil.SetControllerReference(cluster, route, r.Scheme); err != nil {
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
				r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonRouteAPINotFound, "route.openshift.io/v1 not available; skipping MCP Route reconciliation")
			}
			return nil
		}
		return fmt.Errorf("failed to create or update MCP Route: %w", err)
	}

	logger.Info("Successfully reconciled MCP Route", "name", route.GetName())
	return nil
}

func (r *Neo4jEnterpriseClusterReconciler) warnIfMCPMissingAPOC(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) {
	if cluster.Spec.MCP == nil || !cluster.Spec.MCP.Enabled || r.Recorder == nil {
		return
	}

	plugins := &neo4jv1beta1.Neo4jPluginList{}
	if err := r.List(ctx, plugins, client.InNamespace(cluster.Namespace)); err != nil {
		log.FromContext(ctx).V(1).Info("Unable to list plugins for MCP APOC check", "error", err)
		return
	}

	for _, plugin := range plugins.Items {
		if plugin.Spec.ClusterRef != cluster.Name || !plugin.Spec.Enabled {
			continue
		}
		if isAPOCPluginName(plugin.Spec.Name) {
			return
		}
	}

	r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonMCPApocMissing,
		"MCP is enabled but APOC is not configured via Neo4jPlugin; MCP may fail in stdio mode and some tools may be unavailable.")
}

func isAPOCPluginName(name string) bool {
	return strings.EqualFold(name, "apoc") || strings.EqualFold(name, "apoc-extended")
}

// reconcileAuraFleetManagement handles Aura Fleet Management in two phases:
//
// Phase 1 — Plugin installation (runs unconditionally when enabled):
//
//	Merges "fleet-management" into the NEO4J_PLUGINS env var on the live StatefulSet via a
//	targeted patch. This is identical to how Neo4jPlugin controller installs plugins, so it
//	coexists safely: both controllers read the current list and append only if not present.
//	The updated StatefulSet triggers a rolling pod restart; the Docker entrypoint copies the
//	pre-bundled jar from /var/lib/neo4j/products/ to /plugins/ (no internet access needed).
//
// Phase 2 — Token registration (runs only when cluster is Ready):
//
//	Reads the Aura token from the referenced Kubernetes Secret and calls
//	CALL fleetManagement.registerToken($token). Registration is idempotent.
func (r *Neo4jEnterpriseClusterReconciler) reconcileAuraFleetManagement(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	spec := cluster.Spec.AuraFleetManagement

	// --- Phase 1: ensure the plugin jar is loaded ---
	// Merge "fleet-management" into NEO4J_PLUGINS on the live StatefulSet.
	// This is always done (regardless of Ready state) so pods restart promptly after
	// the feature is first enabled, before we attempt token registration.
	stsName := fmt.Sprintf("%s-server", cluster.Name)
	if err := r.mergeFleetManagementPlugin(ctx, stsName, cluster.Namespace); err != nil {
		// Non-fatal for the reconcile: the cluster is functional, plugin patching failed.
		logger.Error(err, "Failed to patch StatefulSet NEO4J_PLUGINS for fleet-management")
		r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonAuraFleetPluginPatchFailed,
			"Failed to add fleet-management to NEO4J_PLUGINS: %v", err)
		return nil
	}

	// --- Phase 2: token registration ---
	if spec.TokenSecretRef == nil {
		logger.Info("Aura Fleet Management enabled but no tokenSecretRef configured; plugin installed, registration deferred")
		return nil
	}

	if cluster.Status.Phase != "Ready" {
		logger.Info("Cluster not yet Ready; fleet-management plugin patch applied, registration deferred",
			"phase", cluster.Status.Phase)
		return nil
	}

	if cluster.Status.AuraFleetManagement != nil && cluster.Status.AuraFleetManagement.Registered {
		logger.V(1).Info("Aura Fleet Management already registered; nothing to do")
		return nil
	}

	secretName := spec.TokenSecretRef.Name
	secretKey := spec.TokenSecretRef.Key
	if secretKey == "" {
		secretKey = "token"
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: secretName}, secret); err != nil {
		return r.setFleetManagementStatus(ctx, cluster, false, fmt.Sprintf("cannot read token secret %s: %v", secretName, err))
	}

	tokenBytes, ok := secret.Data[secretKey]
	if !ok || len(tokenBytes) == 0 {
		return r.setFleetManagementStatus(ctx, cluster, false, fmt.Sprintf("key %q not found in secret %s", secretKey, secretName))
	}
	token := strings.TrimSpace(string(tokenBytes))

	neo4jClient, err := neo4jclient.NewClientForEnterprise(cluster, r.Client, getClusterAdminSecretName(cluster))
	if err != nil {
		return r.setFleetManagementStatus(ctx, cluster, false, fmt.Sprintf("cannot connect to Neo4j: %v", err))
	}
	defer neo4jClient.Close()

	installed, err := neo4jClient.IsFleetManagementInstalled(ctx)
	if err != nil {
		return r.setFleetManagementStatus(ctx, cluster, false, fmt.Sprintf("cannot check fleet management plugin: %v", err))
	}
	if !installed {
		// Pods may still be mid-restart after the NEO4J_PLUGINS patch above.
		logger.Info("Fleet management plugin not yet loaded; will retry on next reconcile")
		return nil
	}

	if err := neo4jClient.RegisterFleetManagementToken(ctx, token); err != nil {
		return r.setFleetManagementStatus(ctx, cluster, false, fmt.Sprintf("token registration failed: %v", err))
	}

	logger.Info("Aura Fleet Management token registered successfully")
	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonAuraFleetRegistered,
		"Successfully registered with Aura Fleet Management")
	return r.setFleetManagementStatus(ctx, cluster, true, "Registered with Aura Fleet Management")
}

// mergeFleetManagementPlugin patches the named StatefulSet to ensure "fleet-management"
// appears in the NEO4J_PLUGINS env var of the neo4j container. The merge is idempotent:
// if "fleet-management" is already present, the StatefulSet is not touched.
// Uses retry.RetryOnConflict to handle concurrent update conflicts.
func (r *Neo4jEnterpriseClusterReconciler) mergeFleetManagementPlugin(ctx context.Context, stsName, namespace string) error {
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

func (r *Neo4jEnterpriseClusterReconciler) setFleetManagementStatus(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster, registered bool, message string) error {
	now := metav1.Now()
	status := &neo4jv1beta1.AuraFleetManagementStatus{
		Registered: registered,
		Message:    message,
	}
	if registered {
		status.LastRegistrationTime = &now
	}
	cluster.Status.AuraFleetManagement = status
	return r.Status().Update(ctx, cluster)
}

// SetupWithManager sets up the controller with the Manager.
func (r *Neo4jEnterpriseClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1beta1.Neo4jEnterpriseCluster{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		// Note: Removed ConfigMap from Owns() to prevent reconciliation feedback loops
		// ConfigMaps are managed manually by ConfigMapManager with debounce
		Owns(&corev1.Secret{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1, // Limit concurrent reconciliations
			RateLimiter: workqueue.NewTypedMaxOfRateLimiter(
				// Exponential backoff starting at 5 seconds
				workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](5*time.Second, 30*time.Second),
				// Overall rate limiting (max 10 reconciliations per minute)
				&workqueue.TypedBucketRateLimiter[reconcile.Request]{
					Limiter: rate.NewLimiter(rate.Every(6*time.Second), 10),
				},
			),
		})

	routeGVK := schema.GroupVersionKind{Group: "route.openshift.io", Version: "v1", Kind: "Route"}
	if _, err := mgr.GetRESTMapper().RESTMapping(routeGVK.GroupKind(), routeGVK.Version); err == nil {
		routeObj := &unstructured.Unstructured{}
		routeObj.SetGroupVersionKind(routeGVK)
		routeHandler := handler.TypedEnqueueRequestForOwner[*unstructured.Unstructured](mgr.GetScheme(), mgr.GetRESTMapper(), &neo4jv1beta1.Neo4jEnterpriseCluster{}, handler.OnlyControllerOwner())
		builder = builder.WatchesRawSource(source.Kind(mgr.GetCache(), routeObj, routeHandler))
	}

	return builder.Complete(r)
}
