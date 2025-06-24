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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	neo4jclient "github.com/neo4j-labs/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/resources"
)

// Neo4jEnterpriseClusterReconciler reconciles a Neo4jEnterpriseCluster object
type Neo4jEnterpriseClusterReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	RequeueAfter      time.Duration
	TopologyScheduler *TopologyScheduler
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

	// Update cluster status to "Initializing"
	r.updateClusterStatus(ctx, cluster, "Initializing", "Starting cluster reconciliation")

	// Create Certificate if cert-manager is enabled
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == "cert-manager" {
		certificate := resources.BuildCertificateForEnterprise(cluster)
		if certificate != nil {
			if err := r.createOrUpdateResource(ctx, certificate, cluster); err != nil {
				logger.Error(err, "Failed to create Certificate")
				r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create Certificate: %v", err))
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
			}
		}
	}

	// Create External Secrets if enabled
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.ExternalSecrets != nil && cluster.Spec.TLS.ExternalSecrets.Enabled {
		if err := r.createExternalSecretForTLS(ctx, cluster); err != nil {
			logger.Error(err, "Failed to create TLS ExternalSecret")
			r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create TLS ExternalSecret: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	if cluster.Spec.Auth != nil && cluster.Spec.Auth.ExternalSecrets != nil && cluster.Spec.Auth.ExternalSecrets.Enabled {
		if err := r.createExternalSecretForAuth(ctx, cluster); err != nil {
			logger.Error(err, "Failed to create Auth ExternalSecret")
			r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create Auth ExternalSecret: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Create ConfigMap
	configMap := resources.BuildConfigMapForEnterprise(cluster)
	if err := r.createOrUpdateResource(ctx, configMap, cluster); err != nil {
		logger.Error(err, "Failed to create ConfigMap")
		r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create ConfigMap: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Create Services
	services := []*corev1.Service{
		resources.BuildHeadlessServiceForEnterprise(cluster),
		resources.BuildClientServiceForEnterprise(cluster),
	}
	for _, service := range services {
		if err := r.createOrUpdateResource(ctx, service, cluster); err != nil {
			logger.Error(err, "Failed to create Service", "service", service.Name)
			r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create Service %s: %v", service.Name, err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Calculate topology placement if topology scheduler is available
	var topologyPlacement *TopologyPlacement
	if r.TopologyScheduler != nil {
		placement, err := r.TopologyScheduler.CalculateTopologyPlacement(ctx, cluster)
		if err != nil {
			logger.Error(err, "Failed to calculate topology placement")
			r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to calculate topology placement: %v", err))
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
			r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to apply topology constraints: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	if err := r.createOrUpdateResource(ctx, primarySts, cluster); err != nil {
		logger.Error(err, "Failed to create primary StatefulSet")
		r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create primary StatefulSet: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	if cluster.Spec.Topology.Secondaries > 0 {
		secondarySts := resources.BuildSecondaryStatefulSetForEnterprise(cluster)

		// Apply topology constraints to secondary StatefulSet
		if r.TopologyScheduler != nil && topologyPlacement != nil {
			if err := r.TopologyScheduler.ApplyTopologyConstraints(ctx, secondarySts, cluster, topologyPlacement); err != nil {
				logger.Error(err, "Failed to apply topology constraints to secondary StatefulSet")
				r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to apply topology constraints: %v", err))
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
			}
		}

		if err := r.createOrUpdateResource(ctx, secondarySts, cluster); err != nil {
			logger.Error(err, "Failed to create secondary StatefulSet")
			r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create secondary StatefulSet: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Handle Auto-scaling for primaries and secondaries
	if cluster.Spec.AutoScaling != nil && cluster.Spec.AutoScaling.Enabled {
		autoScaler := NewAutoScaler(r.Client)
		if err := autoScaler.ReconcileAutoScaling(ctx, cluster); err != nil {
			logger.Error(err, "Failed to reconcile auto-scaling")
			r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Auto-scaling failed: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Handle Multi-cluster deployment
	if cluster.Spec.MultiCluster != nil && cluster.Spec.MultiCluster.Enabled {
		multiClusterController := NewMultiClusterController(r.Client, r.Scheme)
		if err := multiClusterController.ReconcileMultiCluster(ctx, cluster); err != nil {
			logger.Error(err, "Failed to reconcile multi-cluster deployment")
			r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Multi-cluster deployment failed: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Handle Query Performance Monitoring
	if cluster.Spec.QueryMonitoring != nil && cluster.Spec.QueryMonitoring.Enabled {
		queryMonitor := NewQueryMonitor(r.Client)
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
			r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Plugin management failed: %v", err))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Update status to "Ready"
	r.updateClusterStatus(ctx, cluster, "Ready", "Cluster is ready")
	r.Recorder.Event(cluster, "Normal", "ClusterReady", "Neo4j Enterprise cluster is ready")

	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Neo4jEnterpriseClusterReconciler) handleDeletion(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(cluster, ClusterFinalizer) {
		logger.Info("Finalizer not present, nothing to do", "finalizers", cluster.Finalizers, "deletionTimestamp", cluster.DeletionTimestamp)
		return ctrl.Result{}, nil
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

func (r *Neo4jEnterpriseClusterReconciler) createOrUpdateResource(ctx context.Context, obj client.Object, owner client.Object) error {
	// Set owner reference
	if err := controllerutil.SetControllerReference(owner, obj, r.Scheme); err != nil {
		return err
	}

	// Try to get the existing resource
	key := client.ObjectKeyFromObject(obj)
	existing := obj.DeepCopyObject().(client.Object)

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

func (r *Neo4jEnterpriseClusterReconciler) updateClusterStatus(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster, phase, message string) {
	update := func() error {
		// Get latest version
		latest := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
			return err
		}
		latest.Status.Phase = phase
		latest.Status.Message = message

		// Update the condition
		condition := metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             phase,
			Message:            message,
		}
		if phase == "Failed" {
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
		log.FromContext(ctx).Error(err, "Failed to update cluster status")
	}
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
		r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Failed to create Neo4j client: %v", err))
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
			r.updateClusterStatus(ctx, cluster, "Paused", "Upgrade paused due to failure - manual intervention required")
			r.Recorder.Event(cluster, "Warning", "UpgradePaused", fmt.Sprintf("Upgrade paused: %v", err))
			return ctrl.Result{}, nil // Don't requeue automatically
		}

		r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Rolling upgrade failed: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update cluster status and version
	r.updateClusterStatus(ctx, cluster, "Ready", "Rolling upgrade completed successfully")
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
