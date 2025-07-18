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
	"time"

	"golang.org/x/time/rate"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
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
	neo4jclient "github.com/neo4j-labs/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/resources"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/validation"
)

// Neo4jEnterpriseClusterReconciler reconciles a Neo4jEnterpriseCluster object
type Neo4jEnterpriseClusterReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	RequeueAfter      time.Duration
	TopologyScheduler *TopologyScheduler
	Validator         *validation.ClusterValidator
	ConfigMapManager  *ConfigMapManager
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
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
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

	// Create StatefulSets
	primarySts := resources.BuildPrimaryStatefulSetForEnterprise(cluster)

	// Apply topology constraints to primary StatefulSet
	if r.TopologyScheduler != nil && topologyPlacement != nil {
		if err := r.TopologyScheduler.ApplyTopologyConstraints(ctx, primarySts, cluster, topologyPlacement); err != nil {
			logger.Error(err, "Failed to apply topology constraints to primary StatefulSet")
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to apply topology constraints: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	if err := r.createOrUpdateResource(ctx, primarySts, cluster); err != nil {
		logger.Error(err, "Failed to create primary StatefulSet")
		_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create primary StatefulSet: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	if cluster.Spec.Topology.Secondaries > 0 {
		secondarySts := resources.BuildSecondaryStatefulSetForEnterprise(cluster)

		// Apply topology constraints to secondary StatefulSet
		if r.TopologyScheduler != nil && topologyPlacement != nil {
			if err := r.TopologyScheduler.ApplyTopologyConstraints(ctx, secondarySts, cluster, topologyPlacement); err != nil {
				logger.Error(err, "Failed to apply topology constraints to secondary StatefulSet")
				_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to apply topology constraints: %v", err))
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
			}
		}

		if err := r.createOrUpdateResource(ctx, secondarySts, cluster); err != nil {
			logger.Error(err, "Failed to create secondary StatefulSet")
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create secondary StatefulSet: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Handle Auto-scaling for primaries and secondaries
	if cluster.Spec.AutoScaling != nil && cluster.Spec.AutoScaling.Enabled {
		autoScaler := NewAutoScaler(r.Client)
		if err := autoScaler.ReconcileAutoScaling(ctx, cluster); err != nil {
			logger.Error(err, "Failed to reconcile auto-scaling")
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Auto-scaling failed: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
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

	// Handle Plugin management
	if len(cluster.Spec.Plugins) > 0 {
		if err := r.reconcilePlugins(ctx, cluster); err != nil {
			logger.Error(err, "Failed to reconcile plugins")
			_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Plugin management failed: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Update status to "Ready" only if it changed
	statusChanged := r.updateClusterStatus(ctx, cluster, "Ready", "Cluster is ready")

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

func (r *Neo4jEnterpriseClusterReconciler) createOrUpdateResource(ctx context.Context, obj client.Object, owner client.Object) error {
	// Set owner reference
	if err := controllerutil.SetControllerReference(owner, obj, r.Scheme); err != nil {
		return err
	}

	// Capture the desired spec if this is a StatefulSet
	var desiredSpec appsv1.StatefulSetSpec
	if sts, ok := obj.(*appsv1.StatefulSet); ok {
		desiredSpec = *sts.Spec.DeepCopy()
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		if sts, ok := obj.(*appsv1.StatefulSet); ok {
			// Check if this is an update (object already exists)
			if sts.ResourceVersion != "" {
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

				// For template updates, ensure labels match the existing selector
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
					sts.Spec.Template = *updatedTemplate
				} else {
					// If no selector exists, just use the desired template
					sts.Spec.Template = desiredSpec.Template
				}
			} else {
				// This is a new StatefulSet, use the desired spec as-is
				sts.Spec = desiredSpec
			}
		}
		return nil
	})

	return err
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

	// Check if primary StatefulSet exists and has different image
	primarySts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      cluster.Name + "-primary",
		Namespace: cluster.Namespace,
	}, primarySts); err != nil {
		return false // StatefulSet doesn't exist yet
	}

	// Compare current image with desired image
	if len(primarySts.Spec.Template.Spec.Containers) == 0 {
		return false // StatefulSet has no containers defined
	}
	currentImage := primarySts.Spec.Template.Spec.Containers[0].Image
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

// reconcilePlugins handles plugin installation and management
func (r *Neo4jEnterpriseClusterReconciler) reconcilePlugins(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)

	// Create plugin controller
	pluginController := NewPluginController(r.Client, r.Scheme)

	// Reconcile each plugin
	for _, plugin := range cluster.Spec.Plugins {
		if err := pluginController.ReconcilePlugin(ctx, cluster, plugin); err != nil {
			logger.Error(err, "Failed to reconcile plugin", "plugin", plugin.Name)
			return fmt.Errorf("failed to reconcile plugin %s: %w", plugin.Name, err)
		}
	}

	return nil
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

// NewPluginController creates a new plugin controller
func NewPluginController(client client.Client, scheme *runtime.Scheme) *PluginController {
	return &PluginController{
		Client: client,
		Scheme: scheme,
	}
}

// PluginController handles plugin management
type PluginController struct {
	client.Client
	Scheme *runtime.Scheme
}

// ReconcilePlugin manages a single plugin
func (pc *PluginController) ReconcilePlugin(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, plugin neo4jv1alpha1.PluginSpec) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling plugin", "plugin", plugin.Name, "cluster", cluster.Name)

	// Skip if plugin is disabled
	if !plugin.Enabled {
		logger.Info("Plugin is disabled, skipping", "plugin", plugin.Name)
		return pc.uninstallPlugin(ctx, cluster, plugin)
	}

	// Create plugin installation job
	job, err := pc.createPluginInstallJob(ctx, cluster, plugin)
	if err != nil {
		return fmt.Errorf("failed to create plugin installation job: %w", err)
	}

	// Monitor job status
	return pc.monitorPluginInstallation(ctx, cluster, plugin, job)
}

// createPluginInstallJob creates a Kubernetes job to install the plugin
func (pc *PluginController) createPluginInstallJob(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, plugin neo4jv1alpha1.PluginSpec) (*batchv1.Job, error) {
	logger := log.FromContext(ctx)
	logger.Info("Creating plugin installation job", "plugin", plugin.Name)

	jobName := fmt.Sprintf("%s-%s-plugin-install", cluster.Name, plugin.Name)

	// Check if job already exists
	existingJob := &batchv1.Job{}
	err := pc.Get(ctx, types.NamespacedName{Name: jobName, Namespace: cluster.Namespace}, existingJob)
	if err == nil {
		logger.Info("Plugin installation job already exists", "job", jobName)
		return existingJob, nil
	}

	// Build plugin installation command
	installCmd := pc.buildPluginInstallCommand(cluster, plugin)

	// Create job
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app":                "neo4j",
				"neo4j.com/cluster":  cluster.Name,
				"neo4j.com/plugin":   plugin.Name,
				"neo4j.com/job-type": "plugin-install",
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":                "neo4j",
						"neo4j.com/cluster":  cluster.Name,
						"neo4j.com/plugin":   plugin.Name,
						"neo4j.com/job-type": "plugin-install",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:    "plugin-installer",
							Image:   cluster.Spec.Image.Repo + ":" + cluster.Spec.Image.Tag,
							Command: []string{"/bin/bash", "-c"},
							Args:    []string{installCmd},
							Env:     pc.buildPluginEnvironment(cluster, plugin),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "neo4j-plugins",
									MountPath: "/plugins",
								},
								{
									Name:      "neo4j-conf",
									MountPath: "/conf",
								},
							},
						},
					},
					Volumes: pc.buildPluginVolumes(cluster, plugin),
				},
			},
		},
	}

	// Set owner reference
	if err := ctrl.SetControllerReference(cluster, job, pc.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create job
	if err := pc.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create plugin installation job: %w", err)
	}

	logger.Info("Plugin installation job created", "job", jobName)
	return job, nil
}

// buildPluginInstallCommand builds the command to install the plugin
func (pc *PluginController) buildPluginInstallCommand(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, plugin neo4jv1alpha1.PluginSpec) string {
	// Base command to install plugin
	cmd := fmt.Sprintf(`
set -e
echo "Installing plugin %s version %s..."

# Create plugin directories
mkdir -p /plugins
mkdir -p /conf

# Download plugin based on source type
`, plugin.Name, plugin.Version)

	// Add download logic based on source type
	if plugin.Source != nil {
		switch plugin.Source.Type {
		case "url":
			cmd += fmt.Sprintf(`
# Download from URL
echo "Downloading plugin from URL: %s"
curl -L -o /plugins/%s.jar %s
`, plugin.Source.URL, plugin.Name, plugin.Source.URL)
		case "official":
			cmd += fmt.Sprintf(`
# Download from official Neo4j plugin repository
echo "Downloading official plugin: %s"
curl -L -o /plugins/%s.jar https://github.com/neo4j-contrib/neo4j-apoc-procedures/releases/download/%s/apoc-%s-core.jar
`, plugin.Name, plugin.Name, plugin.Version, plugin.Version)
		case "community":
			cmd += fmt.Sprintf(`
# Download from community repository
echo "Downloading community plugin: %s"
curl -L -o /plugins/%s.jar https://github.com/neo4j-contrib/neo4j-%s/releases/download/%s/%s-%s.jar
`, plugin.Name, plugin.Name, plugin.Name, plugin.Version, plugin.Name, plugin.Version)
		default:
			cmd += fmt.Sprintf(`
# Custom plugin source
echo "Using custom plugin source for: %s"
# Add custom download logic here
`, plugin.Name)
		}

		// Add checksum verification if provided
		if plugin.Source.Checksum != "" {
			cmd += fmt.Sprintf(`
# Verify checksum
echo "Verifying plugin checksum..."
echo "%s /plugins/%s.jar" | sha256sum -c
`, plugin.Source.Checksum, plugin.Name)
		}
	}

	// Add plugin configuration
	cmd += fmt.Sprintf(`
# Configure plugin
echo "Configuring plugin %s..."
`, plugin.Name)

	// Add plugin-specific configuration
	if len(plugin.Config) > 0 {
		for key, value := range plugin.Config {
			cmd += fmt.Sprintf("echo '%s=%s' >> /conf/neo4j.conf\n", key, value)
		}
	}

	// Add plugin installation verification
	cmd += fmt.Sprintf(`
# Verify plugin installation
echo "Verifying plugin installation..."
if [ -f "/plugins/%s.jar" ]; then
    echo "Plugin %s installed successfully"
    exit 0
else
    echo "Plugin installation failed"
    exit 1
fi
`, plugin.Name, plugin.Name)

	return cmd
}

// buildPluginEnvironment builds environment variables for plugin installation
func (pc *PluginController) buildPluginEnvironment(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, plugin neo4jv1alpha1.PluginSpec) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{
			Name:  "NEO4J_PLUGIN_NAME",
			Value: plugin.Name,
		},
		{
			Name:  "NEO4J_PLUGIN_VERSION",
			Value: plugin.Version,
		},
		{
			Name:  "NEO4J_CLUSTER_NAME",
			Value: cluster.Name,
		},
	}

	// Add authentication if plugin source requires it
	if plugin.Source != nil && plugin.Source.AuthSecret != "" {
		env = append(env, corev1.EnvVar{
			Name: "PLUGIN_AUTH_SECRET",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: plugin.Source.AuthSecret,
					},
					Key: "auth",
				},
			},
		})
	}

	return env
}

// buildPluginVolumes builds volumes for plugin installation
func (pc *PluginController) buildPluginVolumes(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, plugin neo4jv1alpha1.PluginSpec) []corev1.Volume {
	volumes := []corev1.Volume{
		{
			Name: "neo4j-plugins",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: "neo4j-conf",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	// Add secret volume if authentication is required
	if plugin.Source != nil && plugin.Source.AuthSecret != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "plugin-auth",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: plugin.Source.AuthSecret,
				},
			},
		})
	}

	return volumes
}

// monitorPluginInstallation monitors the plugin installation job
func (pc *PluginController) monitorPluginInstallation(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, plugin neo4jv1alpha1.PluginSpec, job *batchv1.Job) error {
	logger := log.FromContext(ctx)
	logger.Info("Monitoring plugin installation", "plugin", plugin.Name, "job", job.Name)

	// Check job status
	if job.Status.Succeeded > 0 {
		logger.Info("Plugin installation completed successfully", "plugin", plugin.Name)
		return pc.configurePlugin(ctx, cluster, plugin)
	}

	if job.Status.Failed > 0 {
		logger.Error(fmt.Errorf("plugin installation failed"), "Plugin installation job failed", "plugin", plugin.Name, "job", job.Name)
		return fmt.Errorf("plugin installation failed for %s", plugin.Name)
	}

	// Job is still running
	logger.Info("Plugin installation in progress", "plugin", plugin.Name)
	return nil
}

// configurePlugin configures the installed plugin
func (pc *PluginController) configurePlugin(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, plugin neo4jv1alpha1.PluginSpec) error {
	logger := log.FromContext(ctx)
	logger.Info("Configuring plugin", "plugin", plugin.Name)

	// Create ConfigMap for plugin configuration
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-plugin-config", cluster.Name, plugin.Name),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app":                 "neo4j",
				"neo4j.com/cluster":   cluster.Name,
				"neo4j.com/plugin":    plugin.Name,
				"neo4j.com/component": "plugin-config",
			},
		},
		Data: plugin.Config,
	}

	// Set owner reference
	if err := ctrl.SetControllerReference(cluster, configMap, pc.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create or update ConfigMap
	if err := pc.Create(ctx, configMap); err != nil {
		if errors.IsAlreadyExists(err) {
			// Update existing ConfigMap
			existing := &corev1.ConfigMap{}
			if err := pc.Get(ctx, types.NamespacedName{Name: configMap.Name, Namespace: configMap.Namespace}, existing); err != nil {
				return fmt.Errorf("failed to get existing ConfigMap: %w", err)
			}
			existing.Data = configMap.Data
			if err := pc.Update(ctx, existing); err != nil {
				return fmt.Errorf("failed to update ConfigMap: %w", err)
			}
		} else {
			return fmt.Errorf("failed to create ConfigMap: %w", err)
		}
	}

	logger.Info("Plugin configuration completed", "plugin", plugin.Name)
	return nil
}

// uninstallPlugin removes a plugin from the cluster
func (pc *PluginController) uninstallPlugin(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, plugin neo4jv1alpha1.PluginSpec) error {
	logger := log.FromContext(ctx)
	logger.Info("Uninstalling plugin", "plugin", plugin.Name)

	// Delete plugin installation job
	jobName := fmt.Sprintf("%s-%s-plugin-install", cluster.Name, plugin.Name)
	job := &batchv1.Job{}
	if err := pc.Get(ctx, types.NamespacedName{Name: jobName, Namespace: cluster.Namespace}, job); err == nil {
		if err := pc.Delete(ctx, job); err != nil {
			logger.Error(err, "Failed to delete plugin installation job", "job", jobName)
		}
	}

	// Delete plugin configuration ConfigMap
	configMapName := fmt.Sprintf("%s-%s-plugin-config", cluster.Name, plugin.Name)
	configMap := &corev1.ConfigMap{}
	if err := pc.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: cluster.Namespace}, configMap); err == nil {
		if err := pc.Delete(ctx, configMap); err != nil {
			logger.Error(err, "Failed to delete plugin configuration", "configMap", configMapName)
		}
	}

	logger.Info("Plugin uninstallation completed", "plugin", plugin.Name)
	return nil
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
