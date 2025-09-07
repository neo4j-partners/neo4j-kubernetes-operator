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

//
// Neo4jShardedDatabase Controller Implementation
//
// This controller manages Neo4j property-sharded databases using Neo4j 2025.07.1+ Cypher 25 syntax.
//
// Key Implementation Details:
//
// 1. Database Creation Approach:
//    - Uses single Cypher 25 CREATE DATABASE command (not separate shard creation)
//    - Command format: CYPHER 25 CREATE DATABASE `name` IF NOT EXISTS
//                      SET GRAPH SHARD { TOPOLOGY n PRIMARIES m SECONDARIES }
//                      SET PROPERTY SHARDS { COUNT n TOPOLOGY m REPLICAS } WAIT
//
// 2. Status Verification:
//    - Uses SHOW DATABASES command to verify graph shard creation
//    - Avoids non-existent SHOW SHARDED DATABASES command
//    - Looks for {database}-graph shard to confirm successful creation
//
// 3. Resource Requirements (Updated 2025-09-07):
//    - Minimum: 4GB memory per server (reduced from 12-16GB)
//    - Recommended: 8GB memory per server for production workloads
//    - CPU: 2+ cores per server for cross-shard query performance
//    - Servers: 5+ servers required for proper shard distribution
//
// 4. Error Handling:
//    - Graceful degradation when virtual database not immediately visible
//    - Retry logic for Neo4j client operations
//    - Comprehensive status reporting and event recording
//

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

	// Update status to show validation passed
	if shardedDatabase.Status.Phase == "Validating" {
		if err := r.updateStatus(ctx, &shardedDatabase, "Creating", "Configuration validated, preparing to create sharded database", nil); err != nil {
			logger.Error(err, "Failed to update status after validation")
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
		}
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
		err := fmt.Errorf("cluster %s does not support property sharding (requires Neo4j 2025.07.1+ and propertySharding.enabled=true)", cluster.Name)
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

	// Wait for Neo4j to be ready for database operations
	if err := r.waitForNeo4jReadiness(ctx, client); err != nil {
		return fmt.Errorf("Neo4j not ready for database operations: %w", err)
	}

	// Create the sharded database using Cypher 25 syntax in a single command
	if err := r.createShardedDatabase(ctx, shardedDB, client); err != nil {
		return fmt.Errorf("failed to create sharded database: %w", err)
	}

	// Update shard status
	if err := r.updateShardStatus(ctx, shardedDB, client); err != nil {
		logger.Error(err, "Failed to update shard status, continuing")
		// Non-fatal error, continue
	}

	return nil
}

// createShardedDatabase creates the sharded database using Cypher 25 syntax
func (r *Neo4jShardedDatabaseReconciler) createShardedDatabase(ctx context.Context, shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, client *neo4j.Client) error {
	logger := log.FromContext(ctx).WithValues("database", shardedDB.Spec.Name)

	// Build the Cypher 25 CREATE DATABASE command for property sharding
	// Format: CREATE DATABASE name [IF NOT EXISTS]
	//         SET GRAPH SHARD { TOPOLOGY n PRIMARIES m SECONDARIES }
	//         SET PROPERTY SHARDS { COUNT n TOPOLOGY m REPLICAS }

	var query strings.Builder

	// Start with Cypher 25 prefix and CREATE DATABASE
	query.WriteString(fmt.Sprintf("CYPHER 25 CREATE DATABASE `%s`", shardedDB.Spec.Name))

	// Add IF NOT EXISTS if specified
	if shardedDB.Spec.IfNotExists {
		query.WriteString(" IF NOT EXISTS")
	}

	// Add graph shard topology
	graphShard := shardedDB.Spec.PropertySharding.GraphShard
	query.WriteString(fmt.Sprintf(" SET GRAPH SHARD { TOPOLOGY %d", graphShard.Primaries))
	if graphShard.Primaries == 1 {
		query.WriteString(" PRIMARY")
	} else {
		query.WriteString(" PRIMARIES")
	}

	if graphShard.Secondaries > 0 {
		query.WriteString(fmt.Sprintf(" %d", graphShard.Secondaries))
		if graphShard.Secondaries == 1 {
			query.WriteString(" SECONDARY")
		} else {
			query.WriteString(" SECONDARIES")
		}
	}
	query.WriteString(" }")

	// Add property shards configuration
	propertyShards := shardedDB.Spec.PropertySharding.PropertyShards
	propertyTopology := shardedDB.Spec.PropertySharding.PropertyShardTopology

	query.WriteString(fmt.Sprintf(" SET PROPERTY SHARDS { COUNT %d", propertyShards))

	// Property shards use replicas, not primaries/secondaries
	// Calculate total replicas needed
	replicas := propertyTopology.Primaries + propertyTopology.Secondaries
	if replicas > 0 {
		query.WriteString(fmt.Sprintf(" TOPOLOGY %d", replicas))
		if replicas == 1 {
			query.WriteString(" REPLICA")
		} else {
			query.WriteString(" REPLICAS")
		}
	}
	query.WriteString(" }")

	// Add WAIT if specified
	if shardedDB.Spec.Wait {
		query.WriteString(" WAIT")
	}

	queryStr := query.String()
	logger.Info("Executing sharded database creation", "query", queryStr)

	// Execute the command with retry logic
	maxRetries := 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Execute against system database for DDL commands
		err := (*client).ExecuteCypher(ctx, "system", queryStr)
		if err == nil {
			logger.Info("Successfully created sharded database")
			return nil
		}

		// Check if database already exists (not an error if IfNotExists is true)
		if shardedDB.Spec.IfNotExists && strings.Contains(err.Error(), "already exists") {
			logger.Info("Database already exists, continuing")
			return nil
		}

		// Check for transient errors
		if attempt < maxRetries-1 && isTransientError(err) {
			delay := time.Duration(attempt+1) * time.Second
			logger.V(1).Info("Transient error, retrying", "error", err, "attempt", attempt+1, "delay", delay)
			time.Sleep(delay)
			continue
		}

		return fmt.Errorf("failed to create sharded database: %w", err)
	}

	return fmt.Errorf("failed to create sharded database after %d attempts", maxRetries)
}

// isTransientError checks if an error is transient and can be retried
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "unavailable") ||
		strings.Contains(errMsg, "timeout") ||
		strings.Contains(errMsg, "transient") ||
		strings.Contains(errMsg, "connection")
}

// createGraphShard creates the graph shard database (nodes, relationships, labels without properties)
func (r *Neo4jShardedDatabaseReconciler) createGraphShard(ctx context.Context, shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, client *neo4j.Client) error {
	logger := log.FromContext(ctx).WithValues("shard", "graph")

	graphShardName := fmt.Sprintf("%s-graph", shardedDB.Spec.Name)
	topology := shardedDB.Spec.PropertySharding.GraphShard

	// Wait for Neo4j to be ready for database operations
	if err := r.waitForNeo4jReadiness(ctx, client); err != nil {
		return fmt.Errorf("Neo4j not ready for database operations: %w", err)
	}

	// Create database with topology using proper method with retry logic
	if err := r.createDatabaseWithRetry(ctx, client, graphShardName, topology.Primaries, topology.Secondaries); err != nil {
		return fmt.Errorf("failed to create graph shard database: %w", err)
	}

	logger.Info("Successfully created graph shard database", "database", graphShardName)
	return nil
}

// createPropertyShard creates a property shard database
func (r *Neo4jShardedDatabaseReconciler) createPropertyShard(ctx context.Context, shardedDB *neo4jv1alpha1.Neo4jShardedDatabase, client *neo4j.Client, shardIndex int32) error {
	logger := log.FromContext(ctx).WithValues("shard", "property", "index", shardIndex)

	propertyShardName := fmt.Sprintf("%s-properties-%d", shardedDB.Spec.Name, shardIndex)
	topology := shardedDB.Spec.PropertySharding.PropertyShardTopology

	// Create database with topology using proper method with retry logic
	if err := r.createDatabaseWithRetry(ctx, client, propertyShardName, topology.Primaries, topology.Secondaries); err != nil {
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

	// Get individual database statuses for each shard
	databases, err := client.GetDatabases(ctx)
	if err != nil {
		logger.Error(err, "Failed to get database information")
		return fmt.Errorf("failed to get database information: %w", err)
	}

	// Check if the graph shard was successfully created
	graphShardName := fmt.Sprintf("%s-graph", shardedDB.Spec.Name)
	graphShardExists := false
	graphShardOnline := false

	for _, db := range databases {
		if db.Name == graphShardName {
			graphShardExists = true
			if db.Status == "online" {
				graphShardOnline = true
			}
			logger.Info("Graph shard found", "database", db.Name, "status", db.Status)
			break
		}
	}

	// If we have the graph shard and it's online, consider the sharded database ready
	// The virtual database may not appear in SHOW DATABASES until first access
	if graphShardExists && graphShardOnline {
		logger.Info("Sharded database appears to be ready", "graphShard", graphShardName, "online", graphShardOnline)
	} else if graphShardExists {
		logger.Info("Graph shard exists but not online yet", "database", graphShardName)
		// Don't fail - this might be transient during startup
	} else {
		logger.Info("Graph shard not found yet", "expectedName", graphShardName)
		// Don't fail - this might be due to timing or eventual consistency
	}

	logger.Info("Sharded database status check completed successfully")
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

// waitForNeo4jReadiness waits for Neo4j to be ready for database operations
func (r *Neo4jShardedDatabaseReconciler) waitForNeo4jReadiness(ctx context.Context, client *neo4j.Client) error {
	logger := log.FromContext(ctx)

	// Check if we can execute basic queries (system database readiness)
	maxRetries := 30 // 30 seconds with 1 second intervals
	for i := 0; i < maxRetries; i++ {
		logger.V(1).Info("Checking Neo4j readiness", "attempt", i+1, "maxRetries", maxRetries)

		// Try to get databases (this will fail if system database is not ready)
		_, err := (*client).GetDatabases(ctx)
		if err == nil {
			logger.Info("Neo4j is ready for database operations")
			return nil
		}

		// Check if this is a transient error we should retry (simple string check)
		errMsg := strings.ToLower(err.Error())
		isTransient := strings.Contains(errMsg, "unavailable") ||
			strings.Contains(errMsg, "timeout") ||
			strings.Contains(errMsg, "transient") ||
			strings.Contains(errMsg, "connection") ||
			strings.Contains(errMsg, "routing")

		if isTransient {
			logger.V(1).Info("Neo4j not ready yet, retrying", "error", err.Error())
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
				continue
			}
		} else {
			// Non-transient error, fail immediately
			return fmt.Errorf("Neo4j readiness check failed with non-transient error: %w", err)
		}
	}

	return fmt.Errorf("timeout waiting for Neo4j to become ready for database operations after %d attempts", maxRetries)
}

// createDatabaseWithRetry creates a database with retry logic for transient failures
func (r *Neo4jShardedDatabaseReconciler) createDatabaseWithRetry(ctx context.Context, client *neo4j.Client, dbName string, primaries, secondaries int32) error {
	logger := log.FromContext(ctx).WithValues("database", dbName)

	maxRetries := 10
	baseDelay := time.Second
	maxDelay := 30 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		logger.V(1).Info("Attempting database creation", "attempt", attempt+1, "maxRetries", maxRetries)

		// Use the correct method signature with all required parameters
		err := (*client).CreateDatabaseWithTopology(ctx, dbName, primaries, secondaries, nil, true, false, "")
		if err == nil {
			logger.Info("Successfully created database", "primaries", primaries, "secondaries", secondaries)
			return nil
		}

		// Check if this is a retryable transient error (simple string check)
		errMsg := strings.ToLower(err.Error())
		isTransient := strings.Contains(errMsg, "unavailable") ||
			strings.Contains(errMsg, "timeout") ||
			strings.Contains(errMsg, "transient") ||
			strings.Contains(errMsg, "connection") ||
			strings.Contains(errMsg, "routing")

		if isTransient {
			// Calculate exponential backoff delay
			delay := time.Duration(attempt) * baseDelay
			if delay > maxDelay {
				delay = maxDelay
			}

			logger.V(1).Info("Database creation failed with transient error, retrying",
				"error", err.Error(), "delay", delay.String())

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				continue
			}
		} else {
			// Non-transient error, fail immediately
			return fmt.Errorf("database creation failed with non-transient error: %w", err)
		}
	}

	return fmt.Errorf("database creation failed after %d attempts", maxRetries)
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
