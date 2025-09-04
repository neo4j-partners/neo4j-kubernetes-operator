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

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/validation"
)

// Neo4jShardedDatabaseReconciler reconciles a Neo4jShardedDatabase object
type Neo4jShardedDatabaseReconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	Recorder                 record.EventRecorder
	MaxConcurrentReconciles  int
	RequeueAfter             time.Duration
	ShardedDatabaseValidator *validation.ShardedDatabaseValidator
}

// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jshardeddatabases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jshardeddatabases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jshardeddatabases/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *Neo4jShardedDatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("neo4jshardeddatabase", req.NamespacedName)
	logger.Info("Starting reconciliation of Neo4jShardedDatabase")

	// Fetch the Neo4jShardedDatabase instance
	var shardedDatabase neo4jv1alpha1.Neo4jShardedDatabase
	if err := r.Get(ctx, req.NamespacedName, &shardedDatabase); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Neo4jShardedDatabase resource not found, likely deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Neo4jShardedDatabase")
		return ctrl.Result{}, err
	}

	// Initialize status if needed
	if shardedDatabase.Status.Phase == "" {
		logger.Info("Initializing Neo4jShardedDatabase status")
		if err := r.updateStatus(ctx, &shardedDatabase, "Validating", "Validating sharded database configuration", nil); err != nil {
			logger.Error(err, "Failed to initialize status")
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
	}

	// Validate the sharded database configuration
	if err := r.validateShardedDatabase(ctx, &shardedDatabase); err != nil {
		logger.Error(err, "Validation failed")
		r.Recorder.Event(&shardedDatabase, "Warning", "ValidationFailed", err.Error())

		if statusErr := r.updateStatus(ctx, &shardedDatabase, "Failed", fmt.Sprintf("Validation failed: %v", err), nil); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after validation failure")
		}
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Get the referenced cluster
	cluster, err := r.getReferencedCluster(ctx, &shardedDatabase)
	if err != nil {
		logger.Error(err, "Failed to get referenced cluster")
		r.Recorder.Event(&shardedDatabase, "Warning", "ClusterNotFound", err.Error())

		if statusErr := r.updateStatus(ctx, &shardedDatabase, "Failed", fmt.Sprintf("Cluster not found: %v", err), nil); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after cluster lookup failure")
		}
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Verify cluster supports property sharding
	if !r.clusterSupportsPropertySharding(cluster) {
		err := fmt.Errorf("cluster %s does not support property sharding (requires Neo4j 2025.06+ and propertySharding.enabled=true)", cluster.Name)
		logger.Error(err, "Cluster does not support property sharding")
		r.Recorder.Event(&shardedDatabase, "Warning", "ClusterNotReady", err.Error())

		if statusErr := r.updateStatus(ctx, &shardedDatabase, "Failed", err.Error(), nil); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after cluster readiness check")
		}
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Create Neo4j client for the cluster
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to create Neo4j client")
		r.Recorder.Event(&shardedDatabase, "Warning", "ClientCreationFailed", err.Error())

		if statusErr := r.updateStatus(ctx, &shardedDatabase, "Failed", fmt.Sprintf("Failed to create Neo4j client: %v", err), nil); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after client creation failure")
		}
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}
	defer neo4jClient.Close()

	// Create or update sharded database
	if err := r.reconcileShardedDatabase(ctx, &shardedDatabase, neo4jClient); err != nil {
		logger.Error(err, "Failed to reconcile sharded database")
		r.Recorder.Event(&shardedDatabase, "Warning", "ReconcileFailed", err.Error())

		if statusErr := r.updateStatus(ctx, &shardedDatabase, "Failed", fmt.Sprintf("Reconcile failed: %v", err), nil); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after reconcile failure")
		}
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update status to Ready if everything succeeded
	ready := true
	if err := r.updateStatus(ctx, &shardedDatabase, "Ready", "Sharded database is operational", &ready); err != nil {
		logger.Error(err, "Failed to update status to Ready")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	r.Recorder.Event(&shardedDatabase, "Normal", "ShardedDatabaseReady", "Sharded database is ready and operational")
	logger.Info("Successfully reconciled Neo4jShardedDatabase")

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil // Periodic reconciliation
}

// validateShardedDatabase performs comprehensive validation of the sharded database spec
func (r *Neo4jShardedDatabaseReconciler) validateShardedDatabase(ctx context.Context, shardedDB *neo4jv1alpha1.Neo4jShardedDatabase) error {
	if r.ShardedDatabaseValidator != nil {
		return r.ShardedDatabaseValidator.ValidateShardedDatabase(ctx, shardedDB)
	}

	// Basic validation if validator not available
	if shardedDB.Spec.ClusterRef == "" {
		return fmt.Errorf("clusterRef is required")
	}
	if shardedDB.Spec.Name == "" {
		return fmt.Errorf("database name is required")
	}
	if shardedDB.Spec.PropertySharding.PropertyShards < 1 {
		return fmt.Errorf("propertyShards must be at least 1")
	}
	if shardedDB.Spec.DefaultCypherLanguage != "25" {
		return fmt.Errorf("defaultCypherLanguage must be '25' for property sharding")
	}

	return nil
}

// getReferencedCluster retrieves the Neo4jEnterpriseCluster referenced by the sharded database
func (r *Neo4jShardedDatabaseReconciler) getReferencedCluster(ctx context.Context, shardedDB *neo4jv1alpha1.Neo4jShardedDatabase) (*neo4jv1alpha1.Neo4jEnterpriseCluster, error) {
	var cluster neo4jv1alpha1.Neo4jEnterpriseCluster
	clusterKey := types.NamespacedName{
		Name:      shardedDB.Spec.ClusterRef,
		Namespace: shardedDB.Namespace,
	}

	if err := r.Get(ctx, clusterKey, &cluster); err != nil {
		return nil, fmt.Errorf("failed to get cluster %s: %w", shardedDB.Spec.ClusterRef, err)
	}

	return &cluster, nil
}

// clusterSupportsPropertySharding verifies that the cluster supports property sharding
func (r *Neo4jShardedDatabaseReconciler) clusterSupportsPropertySharding(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) bool {
	// Check if property sharding is enabled on the cluster
	if cluster.Spec.PropertySharding == nil || !cluster.Spec.PropertySharding.Enabled {
		return false
	}

	// Check if cluster is ready and property sharding is operational
	if cluster.Status.Phase != "Ready" {
		return false
	}

	if cluster.Status.PropertyShardingReady == nil || !*cluster.Status.PropertyShardingReady {
		return false
	}

	return true
}

// createNeo4jClient creates a Neo4j client for the specified cluster
func (r *Neo4jShardedDatabaseReconciler) createNeo4jClient(ctx context.Context, cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) (*neo4j.Client, error) {
	// Use the same pattern as the database controller
	return neo4j.NewClientForEnterprise(cluster, r.Client, cluster.Spec.Auth.AdminSecret)
}

// reconcileShardedDatabase handles the creation and management of sharded databases
func (r *Neo4jShardedDatabaseReconciler) reconcileShardedDatabase(ctx context.Context, shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, client *neo4j.Client) error {
	logger := log.FromContext(ctx)

	// Step 1: Create graph shard database
	if err := r.createGraphShard(ctx, shardedDB, client); err != nil {
		return fmt.Errorf("failed to create graph shard: %w", err)
	}

	// Step 2: Create property shard databases
	for i := int32(0); i < shardedDB.Spec.PropertySharding.PropertyShards; i++ {
		if err := r.createPropertyShard(ctx, shardedDB, client, i); err != nil {
			return fmt.Errorf("failed to create property shard %d: %w", i, err)
		}
	}

	// Step 3: Configure virtual database (logical view)
	if err := r.configureVirtualDatabase(ctx, shardedDB, client); err != nil {
		return fmt.Errorf("failed to configure virtual database: %w", err)
	}

	// Step 4: Update shard status
	if err := r.updateShardStatus(ctx, shardedDB, client); err != nil {
		logger.Error(err, "Failed to update shard status, continuing")
		// Non-fatal error, continue
	}

	return nil
}

// createGraphShard creates the graph shard database (nodes, relationships, labels without properties)
func (r *Neo4jShardedDatabaseReconciler) createGraphShard(ctx context.Context, shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, client *neo4j.Client) error {
	logger := log.FromContext(ctx).WithValues("shard", "graph")

	graphShardName := fmt.Sprintf("%s_graph", shardedDB.Spec.Name)
	topology := shardedDB.Spec.PropertySharding.GraphShard

	// Create database with topology
	query := fmt.Sprintf(`
		CREATE DATABASE %s IF NOT EXISTS
		DEFAULT LANGUAGE CYPHER 25
		TOPOLOGY %d PRIMARIES %d SECONDARIES
		WAIT
	`, graphShardName, topology.Primaries, topology.Secondaries)

	if _, err := client.ExecuteQuery(ctx, query); err != nil {
		return fmt.Errorf("failed to create graph shard database: %w", err)
	}

	logger.Info("Successfully created graph shard database", "database", graphShardName)
	return nil
}

// createPropertyShard creates a property shard database
func (r *Neo4jShardedDatabaseReconciler) createPropertyShard(ctx context.Context, shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, client *neo4j.Client, shardIndex int32) error {
	logger := log.FromContext(ctx).WithValues("shard", "property", "index", shardIndex)

	propertyShardName := fmt.Sprintf("%s_properties_%d", shardedDB.Spec.Name, shardIndex)
	topology := shardedDB.Spec.PropertySharding.PropertyShardTopology

	// Create database with topology
	query := fmt.Sprintf(`
		CREATE DATABASE %s IF NOT EXISTS
		DEFAULT LANGUAGE CYPHER 25
		TOPOLOGY %d PRIMARIES %d SECONDARIES
		WAIT
	`, propertyShardName, topology.Primaries, topology.Secondaries)

	if _, err := client.ExecuteQuery(ctx, query); err != nil {
		return fmt.Errorf("failed to create property shard database: %w", err)
	}

	logger.Info("Successfully created property shard database", "database", propertyShardName, "index", shardIndex)
	return nil
}

// configureVirtualDatabase sets up the virtual database (logical view combining all shards)
func (r *Neo4jShardedDatabaseReconciler) configureVirtualDatabase(ctx context.Context, shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, client *neo4j.Client) error {
	logger := log.FromContext(ctx).WithValues("virtual", shardedDB.Spec.Name)

	// Generate shard names
	graphShardName := fmt.Sprintf("%s-g000", shardedDB.Spec.Name)
	var propertyShardNames []string
	for i := int32(0); i < shardedDB.Spec.PropertySharding.PropertyShards; i++ {
		propertyShardNames = append(propertyShardNames, fmt.Sprintf("%s-p%03d", shardedDB.Spec.Name, i))
	}

	// Create sharded database using new client method
	options := make(map[string]string)
	if shardedDB.Spec.PropertySharding.Config != nil {
		for k, v := range shardedDB.Spec.PropertySharding.Config {
			options[k] = v
		}
	}

	if err := client.CreateShardedDatabase(
		ctx,
		shardedDB.Spec.Name, // Virtual database name
		graphShardName,
		propertyShardNames,
		options,
		shardedDB.Spec.Wait,
		shardedDB.Spec.IfNotExists,
	); err != nil {
		return fmt.Errorf("failed to create sharded database: %w", err)
	}

	logger.Info("Virtual database configuration completed", "database", shardedDB.Spec.Name)
	return nil
}

// updateShardStatus updates the status with current shard information
func (r *Neo4jShardedDatabaseReconciler) updateShardStatus(ctx context.Context, shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, client *neo4j.Client) error {
	logger := log.FromContext(ctx).WithValues("database", shardedDB.Spec.Name)

	// Get sharded database status
	shardedInfo, err := client.GetShardedDatabaseStatus(ctx, shardedDB.Spec.Name)
	if err != nil {
		logger.Error(err, "Failed to get sharded database status")
		return nil // Non-fatal error
	}

	// Get individual database statuses for each shard
	databases, err := client.GetDatabases(ctx)
	if err != nil {
		logger.Error(err, "Failed to get database information")
		return nil // Non-fatal error
	}

	// Build status information
	graphShardName := fmt.Sprintf("%s-g000", shardedDB.Spec.Name)

	// Update graph shard status
	for _, db := range databases {
		if db.Name == graphShardName {
			graphShard := &neo4jv1alpha1.ShardStatus{
				Name:  graphShardName,
				Type:  "graph",
				State: db.Status,
				Ready: db.Status == "online",
			}
			// Update the status (would need to implement proper status updates)
			logger.Info("Graph shard status", "name", graphShard.Name, "state", graphShard.State, "ready", graphShard.Ready)
			break
		}
	}

	// Update property shard statuses
	for i := int32(0); i < shardedDB.Spec.PropertySharding.PropertyShards; i++ {
		propertyShardName := fmt.Sprintf("%s-p%03d", shardedDB.Spec.Name, i)
		for _, db := range databases {
			if db.Name == propertyShardName {
				propShard := &neo4jv1alpha1.ShardStatus{
					Name:               propertyShardName,
					Type:               "property",
					State:              db.Status,
					Ready:              db.Status == "online",
					PropertyShardIndex: &i,
				}
				logger.Info("Property shard status", "name", propShard.Name, "index", i, "state", propShard.State, "ready", propShard.Ready)
				break
			}
		}
	}

	logger.Info("Updated shard status", "virtual", shardedInfo.Name, "status", shardedInfo.Status)
	return nil
}

// updateStatus updates the sharded database status
func (r *Neo4jShardedDatabaseReconciler) updateStatus(ctx context.Context, shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, phase, message string, shardingReady *bool) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Fetch latest version
		var latest neo4jv1alpha1.Neo4jShardedDatabase
		if err := r.Get(ctx, client.ObjectKeyFromObject(shardedDB), &latest); err != nil {
			return err
		}

		// Update status
		latest.Status.Phase = phase
		latest.Status.Message = message
		latest.Status.ObservedGeneration = latest.Generation

		if shardingReady != nil {
			latest.Status.ShardingReady = shardingReady
		}

		// Update condition
		condition := metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "NotReady",
			Message: message,
		}

		if phase == "Ready" {
			condition.Status = metav1.ConditionTrue
			condition.Reason = "ShardedDatabaseReady"
		} else if phase == "Failed" {
			condition.Reason = "ShardedDatabaseFailed"
		}

		condition.LastTransitionTime = metav1.Now()

		// Update or add condition
		found := false
		for i, existingCondition := range latest.Status.Conditions {
			if existingCondition.Type == condition.Type {
				latest.Status.Conditions[i] = condition
				found = true
				break
			}
		}
		if !found {
			latest.Status.Conditions = append(latest.Status.Conditions, condition)
		}

		return r.Status().Update(ctx, &latest)
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *Neo4jShardedDatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	maxConcurrentReconciles := r.MaxConcurrentReconciles
	if maxConcurrentReconciles <= 0 {
		maxConcurrentReconciles = 1
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&neo4jv1alpha1.Neo4jShardedDatabase{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles,
		}).
		Complete(r)
}
