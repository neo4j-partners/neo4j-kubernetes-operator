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

	"golang.org/x/time/rate"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/metrics"
	neo4jclient "github.com/neo4j-labs/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/resources"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/validation"
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
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

func (r *Neo4jEnterpriseClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Neo4jEnterpriseCluster instance
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Neo4jEnterpriseCluster resource not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Neo4jEnterpriseCluster")
		return ctrl.Result{}, err
	}

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
			currentCluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
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
					r.Recorder.Eventf(cluster, corev1.EventTypeWarning, "TopologyWarning", warning)
				}

				// Check for validation errors
				if len(result.Errors) > 0 {
					err := fmt.Errorf("validation failed: %s", result.Errors.ToAggregate().Error())
					logger.Error(err, "Cluster update validation failed")
					r.Recorder.Eventf(cluster, corev1.EventTypeWarning, "ValidationFailed", "Cluster update validation failed: %v", err)
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
				r.Recorder.Eventf(cluster, corev1.EventTypeWarning, "TopologyWarning", warning)
			}

			// Check for validation errors
			if len(result.Errors) > 0 {
				err := fmt.Errorf("validation failed: %s", result.Errors.ToAggregate().Error())
				logger.Error(err, "Cluster validation failed")
				r.Recorder.Eventf(cluster, corev1.EventTypeWarning, "ValidationFailed", "Cluster validation failed: %v", err)
				_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Validation failed: %v", err))
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
			}
		}

		// Validate server role hints for both create and update
		roleHintErrors := resources.ValidateServerRoleHints(cluster)
		if len(roleHintErrors) > 0 {
			for _, roleError := range roleHintErrors {
				logger.Error(fmt.Errorf("server role validation error"), roleError)
				r.Recorder.Eventf(cluster, corev1.EventTypeWarning, "ServerRoleValidationFailed", "Server role hint validation failed: %s", roleError)
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
			r.Recorder.Eventf(cluster, corev1.EventTypeWarning, "PropertyShardingValidationFailed", "Property sharding validation failed: %v", err)
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Property sharding validation failed: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
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

	// Only set to "Initializing" if cluster is not already Ready
	if cluster.Status.Phase != "Ready" {
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

	// Calculate topology placement if topology scheduler is available
	var topologyPlacement *TopologyPlacement
	if r.TopologyScheduler != nil {
		placement, err := r.TopologyScheduler.CalculateTopologyPlacement(ctx, cluster)
		if err != nil {
			logger.Error(err, "Failed to calculate topology placement")
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to calculate topology placement: %v", err))
			r.Recorder.Event(cluster, "Warning", "TopologyPlacementFailed", fmt.Sprintf("Failed to calculate topology placement: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
		topologyPlacement = placement
		logger.Info("Calculated topology placement",
			"useTopologySpread", placement.UseTopologySpread,
			"useAntiAffinity", placement.UseAntiAffinity,
			"zones", len(placement.AvailabilityZones),
			"enforceDistribution", placement.EnforceDistribution)

		if len(placement.AvailabilityZones) > 0 {
			r.Recorder.Event(cluster, "Normal", "TopologyPlacementCalculated",
				fmt.Sprintf("Calculated topology placement across %d zones", len(placement.AvailabilityZones)))
		}
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

	// Create centralized backup StatefulSet if backups are enabled
	if cluster.Spec.Backups != nil {
		backupSts := resources.BuildBackupStatefulSet(cluster)
		if backupSts != nil {
			if err := r.createOrUpdateResource(ctx, backupSts, cluster); err != nil {
				logger.Error(err, "Failed to create backup StatefulSet")
				_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create backup StatefulSet: %v", err))
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
			}
		}
	}

	// Handle Query Performance Monitoring
	if cluster.Spec.QueryMonitoring != nil && cluster.Spec.QueryMonitoring.Enabled {
		queryMonitor := NewQueryMonitor(r.Client, r.Scheme)
		if err := queryMonitor.ReconcileQueryMonitoring(ctx, cluster); err != nil {
			logger.Error(err, "Failed to reconcile query monitoring")
			// Don't fail the entire reconciliation for monitoring issues
			logger.Info("Query monitoring setup failed, continuing with cluster reconciliation")
		}
	}

	// Plugin management is now handled by the separate Neo4jPlugin CRD and controller

	// Verify Neo4j cluster formation before marking as Ready
	clusterFormed, formationMessage, err := r.verifyNeo4jClusterFormation(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to verify cluster formation")
		_ = r.updateClusterStatus(ctx, cluster, "Forming", fmt.Sprintf("Verifying cluster formation: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	if !clusterFormed {
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
		r.Recorder.Event(cluster, "Normal", "ClusterReady", "Neo4j Enterprise cluster is ready")
	}

	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jEnterpriseClusterReconciler) handleDeletion(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (ctrl.Result, error) {
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

func (r *Neo4jEnterpriseClusterReconciler) cleanupPVCs(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)

	// List PVCs that belong to this cluster
	pvcList := &corev1.PersistentVolumeClaimList{}
	labelSelector := client.MatchingLabels{
		"app.kubernetes.io/name":     "neo4j",
		"app.kubernetes.io/instance": cluster.Name,
	}

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

				// Apply desired spec
				sts.Spec.Replicas = desiredSpec.Replicas
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
						sts.Spec.Template = *updatedTemplate
					} else {
						logger := log.FromContext(ctx)
						logger.V(1).Info("Skipping StatefulSet template update - no significant changes detected",
							"statefulSet", sts.Name,
							"namespace", sts.Namespace)
						// Keep existing template to prevent unnecessary pod restarts
					}
				} else {
					// If no selector exists, just use the desired template
					sts.Spec.Template = desiredSpec.Template
				}
			} else {
				// This is a new StatefulSet, use the desired spec as-is
				logger := log.FromContext(ctx)
				logger.V(1).Info("Creating new StatefulSet with full template",
					"statefulSet", sts.Name,
					"namespace", sts.Namespace)
				sts.Spec = desiredSpec
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
	return r.hasSignificantTemplateChanges(currentTemplate, desiredTemplate)
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

// hasSignificantTemplateChanges checks for any meaningful changes in stable clusters
func (r *Neo4jEnterpriseClusterReconciler) hasSignificantTemplateChanges(current, desired corev1.PodTemplateSpec) bool {
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
		if !r.envVarsEqual(current.Spec.Containers[0].Env, desired.Spec.Containers[0].Env) {
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
	if (current == nil) != (desired == nil) {
		return false
	}
	if current == nil && desired == nil {
		return true
	}

	// Compare critical security context fields
	if current.RunAsUser != desired.RunAsUser ||
		current.RunAsGroup != desired.RunAsGroup ||
		current.RunAsNonRoot != desired.RunAsNonRoot {
		return false
	}

	return true
}

func (r *Neo4jEnterpriseClusterReconciler) envVarsEqual(current, desired []corev1.EnvVar) bool {
	if len(current) != len(desired) {
		return false
	}

	// Create maps for easier comparison
	currentMap := make(map[string]corev1.EnvVar)
	for _, env := range current {
		currentMap[env.Name] = env
	}

	for _, env := range desired {
		if currentEnv, exists := currentMap[env.Name]; !exists || currentEnv.Value != env.Value {
			return false
		}
	}

	return true
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

func (r *Neo4jEnterpriseClusterReconciler) updateClusterStatus(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, phase, message string) bool {
	logger := log.FromContext(ctx)
	statusChanged := false

	update := func() error {
		// Get latest version
		latest := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
			return err
		}

		// Determine expected condition status
		expectedConditionStatus := metav1.ConditionTrue
		if phase == "Failed" {
			expectedConditionStatus = metav1.ConditionFalse
		}

		// Check if status is already exactly what we want
		statusNeedsUpdate := latest.Status.Phase != phase || latest.Status.Message != message
		conditionNeedsUpdate := true

		// Check existing condition
		for _, cond := range latest.Status.Conditions {
			if cond.Type == "Ready" {
				if cond.Status == expectedConditionStatus &&
					cond.Reason == phase &&
					cond.Message == message {
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

		// Update the condition
		condition := metav1.Condition{
			Type:               "Ready",
			Status:             expectedConditionStatus,
			LastTransitionTime: metav1.Now(),
			Reason:             phase,
			Message:            message,
		}

		// Update or add condition
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

// createExternalSecretForTLS creates an ExternalSecret resource for TLS certificates
func (r *Neo4jEnterpriseClusterReconciler) createExternalSecretForTLS(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
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
func (r *Neo4jEnterpriseClusterReconciler) createExternalSecretForAuth(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
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
func (r *Neo4jEnterpriseClusterReconciler) isUpgradeRequired(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) bool {
	// Skip upgrade check if cluster is not ready
	if cluster.Status.Phase != "Ready" {
		return false
	}

	// Skip if upgrade is already in progress
	if cluster.Status.UpgradeStatus != nil &&
		(cluster.Status.UpgradeStatus.Phase == "InProgress" || cluster.Status.UpgradeStatus.Phase == "Paused") {
		return false
	}

	// Check if any server StatefulSet exists and has different image
	// Check server-0 as a representative
	serverSts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-server-0", cluster.Name),
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
func (r *Neo4jEnterpriseClusterReconciler) handleRollingUpgrade(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (ctrl.Result, error) {
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

	// Execute rolling upgrade
	if err := upgrader.ExecuteRollingUpgrade(ctx, cluster, neo4jClient); err != nil {
		logger.Error(err, "Rolling upgrade failed")

		// Check if auto-pause is enabled
		if cluster.Spec.UpgradeStrategy != nil && cluster.Spec.UpgradeStrategy.AutoPauseOnFailure {
			_ = r.updateClusterStatus(ctx, cluster, "Paused", "Upgrade paused due to failure - manual intervention required")
			r.Recorder.Event(cluster, "Warning", "UpgradePaused", fmt.Sprintf("Upgrade paused: %v", err))
			return ctrl.Result{}, nil // Don't requeue automatically
		}

		_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Rolling upgrade failed: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update cluster status and version
	_ = r.updateClusterStatus(ctx, cluster, "Ready", "Rolling upgrade completed successfully")
	cluster.Status.Version = cluster.Spec.Image.Tag
	if err := r.Status().Update(ctx, cluster); err != nil {
		logger.Error(err, "Failed to update cluster status")
	}

	r.Recorder.Event(cluster, "Normal", "UpgradeCompleted", "Rolling upgrade completed successfully")
	logger.Info("Rolling upgrade completed successfully")

	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

// createNeo4jClient creates a Neo4j client for cluster operations
func (r *Neo4jEnterpriseClusterReconciler) createNeo4jClient(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*neo4jclient.Client, error) {
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
func (r *Neo4jEnterpriseClusterReconciler) verifyNeo4jClusterFormation(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (bool, string, error) {
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
		r.Recorder.Eventf(cluster, "Warning", "SplitBrainDetected",
			"Split-brain detected: %s", analysis.ErrorMessage)

		// Attempt automatic repair if configured
		if analysis.RepairAction == RepairActionRestartPods {
			logger.Info("Attempting automatic split-brain repair by restarting orphaned pods",
				"orphanedPods", analysis.OrphanedPods)

			repairErr := splitBrainDetector.RepairSplitBrain(ctx, cluster, analysis)
			if repairErr != nil {
				logger.Error(repairErr, "Failed to repair split-brain automatically")
				r.Recorder.Eventf(cluster, "Warning", "SplitBrainRepairFailed",
					"Automatic split-brain repair failed: %v", repairErr)
				return false, fmt.Sprintf("Split-brain repair failed: %v", repairErr), nil
			}

			r.Recorder.Event(cluster, "Normal", "SplitBrainRepaired",
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
func (r *Neo4jEnterpriseClusterReconciler) legacyClusterFormationCheck(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, expectedServers int) (bool, string, error) {
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

// ReconcileQueryMonitoring sets up query monitoring for the cluster
func (qm *QueryMonitor) ReconcileQueryMonitoring(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Setting up query monitoring", "cluster", cluster.Name)

	// Create monitoring ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name + "-query-monitoring",
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app":                       "neo4j",
				"neo4j.com/cluster":         cluster.Name,
				"neo4j.com/component":       "monitoring",
				"neo4j.com/monitoring-type": "query",
			},
		},
		Data: map[string]string{
			"neo4j.conf": generateQueryMonitoringConfig(cluster),
		},
	}

	// Set owner reference
	if err := ctrl.SetControllerReference(cluster, configMap, qm.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create or update ConfigMap
	if err := qm.Create(ctx, configMap); err != nil {
		if errors.IsAlreadyExists(err) {
			// Update existing ConfigMap
			existing := &corev1.ConfigMap{}
			if err := qm.Get(ctx, types.NamespacedName{Name: configMap.Name, Namespace: configMap.Namespace}, existing); err != nil {
				return fmt.Errorf("failed to get existing ConfigMap: %w", err)
			}
			existing.Data = configMap.Data
			if err := qm.Update(ctx, existing); err != nil {
				return fmt.Errorf("failed to update ConfigMap: %w", err)
			}
		} else {
			return fmt.Errorf("failed to create ConfigMap: %w", err)
		}
	}

	// Set up metrics collection
	if err := qm.setupMetricsCollection(ctx, cluster); err != nil {
		return fmt.Errorf("failed to setup metrics: %w", err)
	}

	// Configure alerting rules if metrics export is enabled
	if cluster.Spec.QueryMonitoring != nil && cluster.Spec.QueryMonitoring.MetricsExport != nil && cluster.Spec.QueryMonitoring.MetricsExport.Prometheus {
		if err := qm.setupAlertingRules(ctx, cluster); err != nil {
			return fmt.Errorf("failed to setup alerting rules: %w", err)
		}
	}

	logger.Info("Query monitoring setup completed successfully")
	return nil
}

// generateQueryMonitoringConfig generates Neo4j configuration for query monitoring
func generateQueryMonitoringConfig(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) string {
	config := `# Query Monitoring Configuration
# Generated by Neo4j Kubernetes Operator

# Enable query logging
dbms.logs.query.enabled=true
dbms.logs.query.threshold=1s

# Enable query statistics
dbms.query_statistics.enabled=true

# Enable slow query logging
dbms.logs.query.slow_threshold=5s

# Enable query plan logging
dbms.logs.query.plan_description_enabled=true

# Enable query parameter logging
dbms.logs.query.parameter_logging_enabled=true

# Query monitoring metrics
dbms.metrics.enabled=true
dbms.metrics.neo4j.enabled=true
dbms.metrics.neo4j.query.enabled=true

# Performance monitoring
dbms.metrics.neo4j.cypher.enabled=true
dbms.metrics.neo4j.transaction.enabled=true
dbms.metrics.neo4j.bolt.enabled=true

# Memory monitoring
dbms.metrics.neo4j.memory.enabled=true
dbms.metrics.neo4j.memory.pool.enabled=true

# Connection monitoring
dbms.metrics.neo4j.connection.enabled=true
dbms.metrics.neo4j.connection.accepted.enabled=true
dbms.metrics.neo4j.connection.active.enabled=true
`

	// Add custom configuration if specified
	if cluster.Spec.QueryMonitoring != nil {
		if cluster.Spec.QueryMonitoring.SlowQueryThreshold != "" {
			config += fmt.Sprintf("dbms.logs.query.slow_threshold=%s\n", cluster.Spec.QueryMonitoring.SlowQueryThreshold)
		}
		if cluster.Spec.QueryMonitoring.ExplainPlan {
			config += "dbms.logs.query.plan_description_enabled=true\n"
		}
		if cluster.Spec.QueryMonitoring.IndexRecommendations {
			config += "dbms.index.recommendations.enabled=true\n"
		}
	}

	return config
}

// setupMetricsCollection sets up metrics collection for the cluster
func (qm *QueryMonitor) setupMetricsCollection(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Setting up metrics collection", "cluster", cluster.Name)

	// Create ServiceMonitor for Prometheus integration
	serviceMonitor := &unstructured.Unstructured{}
	serviceMonitor.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "ServiceMonitor",
	})
	serviceMonitor.SetName(cluster.Name + "-query-monitoring")
	serviceMonitor.SetNamespace(cluster.Namespace)
	serviceMonitor.SetLabels(map[string]string{
		"app":               "neo4j",
		"neo4j.com/cluster": cluster.Name,
	})

	// Set ServiceMonitor spec
	serviceMonitor.Object["spec"] = map[string]interface{}{
		"selector": map[string]interface{}{
			"matchLabels": map[string]interface{}{
				"app":               "neo4j",
				"neo4j.com/cluster": cluster.Name,
			},
		},
		"endpoints": []map[string]interface{}{
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
func (qm *QueryMonitor) setupAlertingRules(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
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

	// Generate alerting rules
	rules := []map[string]interface{}{
		{
			"alert": "Neo4jSlowQueries",
			"expr":  "neo4j_query_duration_seconds > 5",
			"for":   "5m",
			"labels": map[string]interface{}{
				"severity": "warning",
			},
			"annotations": map[string]interface{}{
				"summary":     "Neo4j slow queries detected",
				"description": "Neo4j cluster {{ $labels.cluster }} has slow queries taking longer than 5 seconds",
			},
		},
		{
			"alert": "Neo4jHighMemoryUsage",
			"expr":  "neo4j_memory_usage_bytes / neo4j_memory_total_bytes > 0.8",
			"for":   "5m",
			"labels": map[string]interface{}{
				"severity": "warning",
			},
			"annotations": map[string]interface{}{
				"summary":     "Neo4j high memory usage",
				"description": "Neo4j cluster {{ $labels.cluster }} memory usage is above 80%",
			},
		},
	}

	prometheusRule.Object["spec"] = map[string]interface{}{
		"groups": []map[string]interface{}{
			{
				"name":  "neo4j-query-monitoring",
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

// validatePropertyShardingConfiguration validates property sharding settings
func (r *Neo4jEnterpriseClusterReconciler) validatePropertyShardingConfiguration(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)

	// Validate Neo4j version supports property sharding (2025.06+)
	if err := validatePropertyShardingVersion(cluster.Spec.Image.Tag); err != nil {
		return fmt.Errorf("property sharding version validation failed: %w", err)
	}

	// Validate minimum cluster size for property sharding
	if cluster.Spec.Topology.Servers < 5 {
		return fmt.Errorf("property sharding requires minimum 5 servers for proper shard distribution, got %d", cluster.Spec.Topology.Servers)
	}

	// Validate required configuration settings
	requiredSettings := map[string]string{
		"internal.dbms.sharded_property_database.enabled":                     "true",
		"db.query.default_language":                                           "CYPHER_25",
		"internal.dbms.cluster.experimental_protocol_version.dbms_enabled":    "true",
		"internal.dbms.sharded_property_database.allow_external_shard_access": "false",
	}

	if cluster.Spec.PropertySharding.Config != nil {
		for key, expectedValue := range requiredSettings {
			if actualValue, exists := cluster.Spec.PropertySharding.Config[key]; !exists || actualValue != expectedValue {
				return fmt.Errorf("property sharding requires %s=%s, got %s=%s", key, expectedValue, key, actualValue)
			}
		}
	} else {
		// If config is nil, create it with required settings
		logger.Info("Property sharding config is empty, will use default required settings")
	}

	// Validate resource requirements with lenient but realistic minimums
	if cluster.Spec.Resources != nil && cluster.Spec.Resources.Requests != nil {
		if memory := cluster.Spec.Resources.Requests.Memory(); memory != nil {
			memoryMB := memory.Value() / (1024 * 1024)
			if memoryMB < 8192 { // 8GB lenient minimum (implementation report recommends 12GB+)
				logger.Info("Property sharding memory below recommended 12GB+, proceeding with caution",
					"requestedMB", memoryMB, "recommendedMB", 12288)
			}
			if memoryMB < 6144 { // 6GB absolute minimum
				return fmt.Errorf("property sharding requires minimum 6GB memory for basic operation, got %dMB (recommended: 12GB+)", memoryMB)
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
				return fmt.Errorf("property sharding requires minimum 1 CPU core, got %dm (recommended: 2+ cores)", cpuMillis)
			}
		}
	}

	logger.Info("Property sharding configuration validation passed")
	return nil
}

// validatePropertyShardingVersion checks if Neo4j version supports property sharding
func validatePropertyShardingVersion(imageTag string) error {
	// Property sharding requires Neo4j 2025.06+
	if imageTag == "" {
		return fmt.Errorf("image tag is required for property sharding")
	}

	// Handle calver format (2025.MM.DD)
	if strings.HasPrefix(imageTag, "2025.") {
		// Extract month portion
		parts := strings.Split(imageTag, ".")
		if len(parts) >= 2 {
			month := parts[1]
			if month < "06" {
				return fmt.Errorf("property sharding requires Neo4j 2025.06+, got %s", imageTag)
			}
		}
		return nil
	}

	// Handle future years (2026+)
	if contains([]string{"2026.", "2027.", "2028.", "2029."}, imageTag) {
		return nil
	}

	return fmt.Errorf("property sharding requires Neo4j 2025.06+, got %s", imageTag)
}

// contains checks if string starts with any of the prefixes
func contains(prefixes []string, s string) bool {
	for _, prefix := range prefixes {
		if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// updatePropertyShardingStatus updates the property sharding ready status
func (r *Neo4jEnterpriseClusterReconciler) updatePropertyShardingStatus(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, ready bool) error {
	logger := log.FromContext(ctx)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get latest version
		latest := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
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

// SetupWithManager sets up the controller with the Manager.
func (r *Neo4jEnterpriseClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1alpha1.Neo4jEnterpriseCluster{}).
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
		}).
		Complete(r)
}
