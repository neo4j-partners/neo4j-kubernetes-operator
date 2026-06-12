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
// This controller manages Neo4j property-sharded databases using Neo4j 2025.12+ Cypher 25 syntax.
//
// Key Implementation Details:
//
// 1. Database Creation Approach:
//    - Uses single Cypher 25 CREATE DATABASE command (not separate shard creation)
//    - Command format: CYPHER 25 CREATE DATABASE `name` IF NOT EXISTS
//                      SET DEFAULT LANGUAGE CYPHER 25
//                      SET GRAPH SHARD { TOPOLOGY n PRIMARIES m SECONDARIES }
//                      SET PROPERTY SHARDS { COUNT n TOPOLOGY m REPLICAS }
//                      OPTIONS { seedURI: ..., seedConfig: ..., seedSourceDatabase: ..., seedRestoreUntil: ..., txLogEnrichment: ... } WAIT
//
// 2. Status Verification:
//    - Uses SHOW DATABASES command to verify graph shard creation
//    - Avoids non-existent SHOW SHARDED DATABASES command
//    - Looks for {database}-g000 shard to confirm successful creation
//
// 3. Resource Requirements (Updated 2025-09-07):
//    - Minimum: 4GB memory per server (reduced from 12-16GB)
//    - Recommended: 8GB memory per server for production workloads
//    - CPU: 2+ cores per server for cross-shard query performance
//    - Servers: 1+ server (3+ recommended for HA graph shard primaries)
//
// 4. Error Handling:
//    - Graceful degradation when virtual database not immediately visible
//    - Retry logic for Neo4j client operations
//    - Comprehensive status reporting and event recording
//

package controller

import (
	"context"
	stderrors "errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/validation"
	corev1 "k8s.io/api/core/v1"
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
// PVC seed proxy resources (F5): the operator creates a busybox httpd
// Deployment + ClusterIP Service per Neo4jShardedDatabase whose seedBackupRef
// resolves to a PVC-backed backup. Owner reference on the sharded DB CR
// handles GC on delete, so no explicit delete verb is needed.
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *Neo4jShardedDatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("neo4jshardeddatabase", req.NamespacedName)
	logger.Info("Starting reconciliation of Neo4jShardedDatabase")

	// Fetch the Neo4jShardedDatabase instance
	var shardedDatabase neo4jv1beta1.Neo4jShardedDatabase
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
		r.Recorder.Event(&shardedDatabase, corev1.EventTypeWarning, EventReasonValidationFailed, err.Error())

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
		r.Recorder.Event(&shardedDatabase, corev1.EventTypeWarning, EventReasonClusterNotFound, err.Error())

		if statusErr := r.updateStatus(ctx, &shardedDatabase, "Failed", fmt.Sprintf("Cluster not found: %v", err), nil); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after cluster lookup failure")
		}
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Verify cluster supports property sharding
	if !r.clusterSupportsPropertySharding(cluster) {
		err := fmt.Errorf("cluster %s does not support property sharding (requires Neo4j 2025.12+ and propertySharding.enabled=true)", cluster.Name)
		logger.Error(err, "Cluster does not support property sharding")
		r.Recorder.Event(&shardedDatabase, corev1.EventTypeWarning, EventReasonClusterNotReady, err.Error())

		if statusErr := r.updateStatus(ctx, &shardedDatabase, "Failed", err.Error(), nil); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after cluster readiness check")
		}
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Create Neo4j client for the cluster
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to create Neo4j client")
		r.Recorder.Event(&shardedDatabase, corev1.EventTypeWarning, EventReasonClientCreationFailed, err.Error())

		if statusErr := r.updateStatus(ctx, &shardedDatabase, "Failed", fmt.Sprintf("Failed to create Neo4j client: %v", err), nil); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after client creation failure")
		}
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}
	defer neo4jClient.Close()

	// Phase 2 seed-from-backup: if spec.seedBackupRef is set, resolve it into
	// a concrete seedURI before building the CREATE DATABASE Cypher. Errors
	// from the resolver have two meanings:
	//   - ErrBackupNotReady (backup exists but has no Succeeded run yet) →
	//     transient; route to Pending + requeue. Mirrors CLAUDE.md rule 72
	//     for the restore controller.
	//   - Anything else (missing CR, PVC storage, unsupported type) →
	//     permanent; route to Failed.
	// Seed resolution only matters while the seed can still be CONSUMED:
	// initial creation (not ShardingReady yet) or a pending destructive
	// replaceExisting re-trigger (rule 64). Once the sharded DB is Ready,
	// resolving again would re-create the PVC seed proxy that the Ready
	// transition just tore down — oscillating Ready→Pending and re-exposing
	// the backup PVC on every periodic reconcile (#224 review).
	seedConsumable := shardedDatabase.Status.ShardingReady == nil || !*shardedDatabase.Status.ShardingReady ||
		(shardedDatabase.Spec.ReplaceExisting && shardedDatabase.Status.LastDestructiveRestoreGeneration < shardedDatabase.Generation)
	var resolved *ResolvedShardedSeed
	var seedErr error
	if seedConsumable {
		resolved, seedErr = r.resolveShardedSeed(ctx, &shardedDatabase)
	}
	if resolved != nil || seedErr != nil {
		if seedErr != nil {
			if stderrors.Is(seedErr, ErrBackupNotReady) {
				logger.Info("seedBackupRef target has no Succeeded run yet, requeuing",
					"seedBackupRef", shardedDatabase.Spec.SeedBackupRef, "error", seedErr.Error())
				r.Recorder.Event(&shardedDatabase, corev1.EventTypeNormal, "SeedBackupPending",
					"Waiting for referenced Neo4jBackup to complete a successful run")
				if statusErr := r.updateStatus(ctx, &shardedDatabase, "Pending",
					fmt.Sprintf("Waiting for Neo4jBackup %q to produce a Succeeded run", shardedDatabase.Spec.SeedBackupRef), nil); statusErr != nil {
					logger.Error(statusErr, "Failed to update status to Pending")
				}
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
			}
			logger.Error(seedErr, "Failed to resolve seedBackupRef", "seedBackupRef", shardedDatabase.Spec.SeedBackupRef)
			r.Recorder.Event(&shardedDatabase, corev1.EventTypeWarning, "SeedBackupResolutionFailed", seedErr.Error())
			if statusErr := r.updateStatus(ctx, &shardedDatabase, "Failed",
				fmt.Sprintf("seedBackupRef resolution failed: %v", seedErr), nil); statusErr != nil {
				logger.Error(statusErr, "Failed to update status to Failed")
			}
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
		// Populate the in-memory spec so downstream Cypher builders use the
		// resolved URI(s) without needing to know about SeedBackupRef. The
		// CR is NOT Updated — only the in-memory copy is mutated for this
		// reconcile. URI vs PerShardURIs is mutually exclusive: cloud
		// backups populate the single URI; PVC backups populate the
		// per-shard map served by the operator-managed seed proxy.
		switch {
		case resolved.URI != "":
			shardedDatabase.Spec.SeedURI = resolved.URI
			logger.Info("Resolved seedBackupRef to cloud seedURI",
				"seedBackupRef", shardedDatabase.Spec.SeedBackupRef, "seedURI", resolved.URI)
		case len(resolved.PerShardURIs) > 0:
			if !resolved.ProxyAvailable {
				logger.Info("PVC seed proxy not yet Ready; requeuing",
					"seedBackupRef", shardedDatabase.Spec.SeedBackupRef)
				r.Recorder.Event(&shardedDatabase, corev1.EventTypeNormal, "SeedProxyStarting",
					"Waiting for backup-seed-proxy Deployment to become Ready")
				if statusErr := r.updateStatus(ctx, &shardedDatabase, "Pending",
					"Waiting for PVC seed proxy to become Ready", nil); statusErr != nil {
					logger.Error(statusErr, "Failed to update status to Pending")
				}
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
			}
			shardedDatabase.Spec.SeedURIs = resolved.PerShardURIs
			// Clear SeedURI so the Cypher options builder uses seedURIs map.
			shardedDatabase.Spec.SeedURI = ""
			// seedSourceDatabase is incompatible with the seedURIs map form
			// — Neo4j rejects CREATE DATABASE with "OPTIONS specify
			// 'seedSourceDatabase' expecting 'seedURI' to be present" when
			// both are emitted. The per-shard URL map carries the source
			// shard identity in its key (target-shard-name → source-shard
			// URL), so seedSourceDatabase would just be redundant anyway.
			// Clear it in-memory on the PVC path.
			shardedDatabase.Spec.SeedSourceDatabase = ""
			logger.Info("Resolved seedBackupRef to PVC per-shard URIs",
				"seedBackupRef", shardedDatabase.Spec.SeedBackupRef, "shardCount", len(resolved.PerShardURIs))
		}

		// Phase 2b: ensure the referenced cluster has the backup's
		// credentials Secret projected onto its server pods (cloud only;
		// PVC seed uses in-cluster HTTP, no creds needed).
		if resolved.CredsSecretName != "" {
			autoInherited, credsErr := EnsureClusterHasSeedCreds(ctx, r.Client, cluster, resolved.CredsSecretName)
			if credsErr != nil {
				logger.Error(credsErr, "Cluster missing seed credentials projection")
				r.Recorder.Event(&shardedDatabase, corev1.EventTypeWarning, "SeedCredsMissing", credsErr.Error())
				if statusErr := r.updateStatus(ctx, &shardedDatabase, "Failed", credsErr.Error(), nil); statusErr != nil {
					logger.Error(statusErr, "Failed to update status to Failed")
				}
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
			}
			if autoInherited {
				logger.Info("Auto-inherited seed credentials onto cluster; waiting for rolling restart",
					"cluster", cluster.Name, "credentialsSecret", resolved.CredsSecretName)
				r.Recorder.Event(&shardedDatabase, corev1.EventTypeNormal, "SeedCredsAutoInherited",
					fmt.Sprintf("Patched cluster %q spec.extraEnvFrom with %q; waiting for rolling restart", cluster.Name, resolved.CredsSecretName))
				if statusErr := r.updateStatus(ctx, &shardedDatabase, "Pending",
					fmt.Sprintf("Auto-inherited seed credentials Secret %q onto cluster %q; waiting for cluster pods to restart", resolved.CredsSecretName, cluster.Name), nil); statusErr != nil {
					logger.Error(statusErr, "Failed to update status to Pending")
				}
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
			}
		}
	}

	// Create or update sharded database
	destructive, err := r.reconcileShardedDatabase(ctx, &shardedDatabase, neo4jClient)
	if err != nil {
		logger.Error(err, "Failed to reconcile sharded database")
		r.Recorder.Event(&shardedDatabase, corev1.EventTypeWarning, EventReasonReconcileFailed, err.Error())

		if statusErr := r.updateStatus(ctx, &shardedDatabase, "Failed", fmt.Sprintf("Reconcile failed: %v", err), nil); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after reconcile failure")
		}
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}

	// Update status to Ready if everything succeeded. When the destructive
	// path fired AND succeeded, also stamp Status.LastDestructiveRestoreGeneration
	// so the next reconcile at the same generation skips the destructive
	// branch (otherwise the controller would re-drop on every poll cycle
	// since IF EXISTS makes the drop idempotent).
	ready := true
	// Seeding is complete once the sharded DB is operational — tear down the
	// PVC seed proxy stack (if any) so the backup PVC stops being served
	// cluster-wide (#219). Idempotent NotFound no-op after the first pass.
	if err := r.teardownPVCSeedProxy(ctx, &shardedDatabase); err != nil {
		logger.Error(err, "Failed to tear down seed proxy after sharded DB became Ready (non-fatal)")
	}
	if err := r.updateStatus(ctx, &shardedDatabase, "Ready", "Sharded database is operational", &ready); err != nil {
		logger.Error(err, "Failed to update status to Ready")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, err
	}
	if destructive {
		if err := r.recordDestructiveRestoreGeneration(ctx, &shardedDatabase); err != nil {
			// Non-fatal: re-running drop+create at the same generation is
			// safe-but-wasteful; the user can still recover by editing the
			// CR. Log and continue.
			logger.Error(err, "Failed to record LastDestructiveRestoreGeneration; reconciler may re-drop on next poll")
		}
	}

	r.Recorder.Event(&shardedDatabase, corev1.EventTypeNormal, EventReasonShardedDatabaseReady, "Sharded database is ready and operational")
	logger.Info("Successfully reconciled Neo4jShardedDatabase")

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil // Periodic reconciliation
}

// validateShardedDatabase performs comprehensive validation of the sharded database spec
func (r *Neo4jShardedDatabaseReconciler) validateShardedDatabase(ctx context.Context, shardedDB *neo4jv1beta1.Neo4jShardedDatabase) error {
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
	if shardedDB.Spec.PropertySharding.PropertyShardTopology.Replicas < 1 {
		return fmt.Errorf("propertyShardTopology.replicas must be at least 1")
	}
	if shardedDB.Spec.SeedURI != "" && len(shardedDB.Spec.SeedURIs) > 0 {
		return fmt.Errorf("seedURI and seedURIs cannot be specified together")
	}
	if shardedDB.Spec.SeedSourceDatabase != "" && shardedDB.Spec.SeedURI == "" && len(shardedDB.Spec.SeedURIs) == 0 {
		return fmt.Errorf("seedSourceDatabase requires seedURI or seedURIs")
	}
	if shardedDB.Spec.SeedCredentials != nil && shardedDB.Spec.SeedURI == "" && len(shardedDB.Spec.SeedURIs) == 0 {
		return fmt.Errorf("seedCredentials requires seedURI or seedURIs")
	}

	return nil
}

// getReferencedCluster retrieves the Neo4jEnterpriseCluster referenced by the sharded database
func (r *Neo4jShardedDatabaseReconciler) getReferencedCluster(ctx context.Context, shardedDB *neo4jv1beta1.Neo4jShardedDatabase) (*neo4jv1beta1.Neo4jEnterpriseCluster, error) {
	var cluster neo4jv1beta1.Neo4jEnterpriseCluster
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
func (r *Neo4jShardedDatabaseReconciler) clusterSupportsPropertySharding(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) bool {
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
func (r *Neo4jShardedDatabaseReconciler) createNeo4jClient(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (*neo4j.Client, error) {
	// Use the same pattern as the database controller
	return neo4j.NewClientForEnterprise(cluster, r.Client, cluster.Spec.Auth.AdminSecret)
}

// reconcileShardedDatabase handles the creation and management of sharded
// databases. Returns destructive=true when the Phase 2c
// replaceExisting+force path fired at the current spec generation; the
// caller is expected to record that generation in
// Status.LastDestructiveRestoreGeneration so future reconciles at the
// same generation skip the destructive branch.
func (r *Neo4jShardedDatabaseReconciler) reconcileShardedDatabase(ctx context.Context, shardedDB *neo4jv1beta1.Neo4jShardedDatabase, client *neo4j.Client) (destructive bool, err error) {
	logger := log.FromContext(ctx)

	// Wait for Neo4j to be ready for database operations
	if err := r.waitForNeo4jReadiness(ctx, client); err != nil {
		return false, fmt.Errorf("Neo4j not ready for database operations: %w", err)
	}

	// Phase 2c: destructive drop-and-recreate path. Validator already
	// ensured replaceExisting+force pair correctly and that a seed source
	// is present. Generation guard: only run DROP if the current spec
	// generation hasn't already been destructively restored — without this
	// the controller would re-drop on every reconcile (and re-seed from
	// the backup), since IF EXISTS keeps the DROP idempotent. The guard
	// records Generation on Status.LastDestructiveRestoreGeneration once
	// the drop+create cycle succeeds; any subsequent reconcile at the same
	// generation skips the destructive branch and falls through to the
	// standard create path.
	destructive = shardedDB.Spec.ReplaceExisting && shardedDB.Spec.Force &&
		shardedDB.Status.LastDestructiveRestoreGeneration < shardedDB.Generation
	if destructive {
		if dropErr := r.dropShardedDatabaseIfExists(ctx, shardedDB, client); dropErr != nil {
			return false, fmt.Errorf("failed to DROP DATABASE %q for replaceExisting: %w", shardedDB.Spec.Name, dropErr)
		}
		logger.Info("Dropped existing sharded database for replaceExisting", "database", shardedDB.Spec.Name)
		r.Recorder.Event(shardedDB, corev1.EventTypeNormal, "ShardedDatabaseDropped",
			fmt.Sprintf("Dropped existing sharded database %q before recreating from seed", shardedDB.Spec.Name))
	}

	// Create the sharded database using Cypher 25 syntax in a single command
	if (shardedDB.Spec.SeedURI != "" || len(shardedDB.Spec.SeedURIs) > 0) && shardedDB.Spec.SeedCredentials != nil {
		if credErr := client.PrepareCloudCredentialsForShardedDatabase(ctx, r.Client, shardedDB); credErr != nil {
			return destructive, fmt.Errorf("failed to prepare cloud credentials: %w", credErr)
		}
	}

	if createErr := r.createShardedDatabase(ctx, shardedDB, client); createErr != nil {
		return destructive, fmt.Errorf("failed to create sharded database: %w", createErr)
	}

	// Update shard status
	if statusErr := r.updateShardStatus(ctx, shardedDB, client); statusErr != nil {
		logger.Error(statusErr, "Failed to update shard status, continuing")
		// Non-fatal error, continue
	}

	return destructive, nil
}

// dropShardedDatabaseIfExists runs `CYPHER 25 DROP DATABASE name IF EXISTS
// DESTROY DATA WAIT` against the system database, idempotently. Used by the
// Phase 2c replaceExisting+force path before re-creating from seed.
//
// IF EXISTS makes the call a no-op when the logical sharded database isn't
// present, so the helper is safe to invoke on every reconcile while the
// caller still has replaceExisting=true. The WAIT clause blocks until the
// drop completes (including all per-shard databases the sharded DB owns)
// so the subsequent CREATE doesn't race against in-flight cleanup.
//
// CYPHER 25 prefix matches createShardedDatabase's invocation — the rest
// of the sharded DB Cypher path is Cypher 25 so the prefix stays
// consistent (CLAUDE.md rule 30 territory).
func (r *Neo4jShardedDatabaseReconciler) dropShardedDatabaseIfExists(ctx context.Context, shardedDB *neo4jv1beta1.Neo4jShardedDatabase, client *neo4j.Client) error {
	query := fmt.Sprintf("CYPHER 25 DROP DATABASE `%s` IF EXISTS DESTROY DATA WAIT", shardedDB.Spec.Name)
	logger := log.FromContext(ctx).WithValues("database", shardedDB.Spec.Name, "query", query)
	logger.Info("Executing destructive DROP DATABASE for replaceExisting")
	return client.ExecuteCypher(ctx, "system", query)
}

// createShardedDatabase creates the sharded database using Cypher 25 syntax
func (r *Neo4jShardedDatabaseReconciler) createShardedDatabase(ctx context.Context, shardedDB *neo4jv1beta1.Neo4jShardedDatabase, client *neo4j.Client) error {
	logger := log.FromContext(ctx).WithValues("database", shardedDB.Spec.Name)

	// Build the Cypher 25 CREATE DATABASE command for property sharding
	// Format: CREATE DATABASE name [IF NOT EXISTS]
	//         SET DEFAULT LANGUAGE CYPHER 25
	//         SET GRAPH SHARD { TOPOLOGY n PRIMARIES m SECONDARIES }
	//         SET PROPERTY SHARDS { COUNT n TOPOLOGY m REPLICAS }
	//         OPTIONS { seedURI: ..., seedConfig: ..., seedSourceDatabase: ..., seedRestoreUntil: ..., txLogEnrichment: ... }

	var query strings.Builder

	// Start with Cypher 25 prefix and CREATE DATABASE
	query.WriteString(fmt.Sprintf("CYPHER 25 CREATE DATABASE `%s`", shardedDB.Spec.Name))

	// Add IF NOT EXISTS if specified
	if shardedDB.Spec.IfNotExistsEffective() {
		query.WriteString(" IF NOT EXISTS")
	}

	// Add default Cypher language
	if shardedDB.Spec.DefaultCypherLanguage != "" {
		query.WriteString(fmt.Sprintf(" SET DEFAULT LANGUAGE CYPHER %s", shardedDB.Spec.DefaultCypherLanguage))
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
	replicas := propertyTopology.Replicas
	if replicas > 0 {
		query.WriteString(fmt.Sprintf(" TOPOLOGY %d", replicas))
		if replicas == 1 {
			query.WriteString(" REPLICA")
		} else {
			query.WriteString(" REPLICAS")
		}
	}
	query.WriteString(" }")

	// Add supported options for sharded databases. Values are bound as driver
	// parameters (params) so seed URIs / config can't inject Cypher (#170).
	options, params, err := buildShardedDatabaseOptions(shardedDB)
	if err != nil {
		return err
	}
	if options != "" {
		query.WriteString(" OPTIONS { ")
		query.WriteString(options)
		query.WriteString(" }")
	}

	// Add WAIT if specified
	if shardedDB.Spec.Wait {
		query.WriteString(" WAIT")
	}

	queryStr := query.String()
	logger.Info("Executing sharded database creation", "query", queryStr)

	// Execute the command with retry logic
	maxRetries := 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Execute against system database for DDL commands (params bind the
		// user-supplied seed values — see buildShardedDatabaseOptions).
		err := (*client).ExecuteCypherWithParams(ctx, "system", queryStr, params)
		if err == nil {
			logger.Info("Successfully created sharded database")
			return nil
		}

		// Check if database already exists (not an error if IfNotExists is true)
		if shardedDB.Spec.IfNotExistsEffective() && strings.Contains(err.Error(), "already exists") {
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

// buildShardedDatabaseOptions builds the inner OPTIONS entries for a sharded
// CREATE DATABASE plus the driver parameters they reference. Every user-supplied
// value (seedURI, per-shard seedURIs, seedSourceDatabase, seedConfig,
// seedRestoreUntil, txLogEnrichment) is bound as a parameter so it cannot inject
// Cypher (issue #170). Only the per-shard seedURI map KEYS are interpolated —
// backtick-escaped, and constrained by the validator. seedConfig is the
// documented comma-separated string and seedRestoreUntil follows the documented
// integer/`datetime()` forms, matching the standard seed path.
func buildShardedDatabaseOptions(shardedDB *neo4jv1beta1.Neo4jShardedDatabase) (string, map[string]any, error) {
	var options []string
	params := map[string]any{}

	if shardedDB.Spec.SeedURI != "" && len(shardedDB.Spec.SeedURIs) > 0 {
		return "", nil, fmt.Errorf("seedURI and seedURIs cannot be specified together")
	}

	if shardedDB.Spec.SeedURI != "" {
		options = append(options, "seedURI: $seed_uri")
		params["seed_uri"] = shardedDB.Spec.SeedURI
	}

	if len(shardedDB.Spec.SeedURIs) > 0 {
		keys := make([]string, 0, len(shardedDB.Spec.SeedURIs))
		for key := range shardedDB.Spec.SeedURIs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		entries := make([]string, 0, len(keys))
		for i, key := range keys {
			p := fmt.Sprintf("seed_uri_%d", i)
			entries = append(entries, fmt.Sprintf("`%s`: $%s", neo4j.EscapeBackticks(key), p))
			params[p] = shardedDB.Spec.SeedURIs[key]
		}
		options = append(options, "seedURI: { "+strings.Join(entries, ", ")+" }")
	}

	if shardedDB.Spec.SeedSourceDatabase != "" {
		options = append(options, "seedSourceDatabase: $seed_source_database")
		params["seed_source_database"] = shardedDB.Spec.SeedSourceDatabase
	}

	if shardedDB.Spec.SeedConfig != nil {
		if restoreUntil := shardedDB.Spec.SeedConfig.RestoreUntil; restoreUntil != "" {
			if txid, ok := strings.CutPrefix(restoreUntil, "txId:"); ok {
				// The sharded validator (isValidRestoreUntilTxID) rejects a txId
				// that doesn't fit int64, so ParseInt succeeds for any spec that
				// reached here; the err==nil guard never silently drops it.
				if n, err := strconv.ParseInt(txid, 10, 64); err == nil {
					options = append(options, "seedRestoreUntil: $seed_restore_until")
					params["seed_restore_until"] = n
				}
			} else {
				options = append(options, "seedRestoreUntil: datetime($seed_restore_until)")
				params["seed_restore_until"] = restoreUntil
			}
		}

		if cfg := neo4j.SerializeSeedConfig(shardedDB.Spec.SeedConfig.Config); cfg != "" {
			options = append(options, "seedConfig: $seed_config")
			params["seed_config"] = cfg
		}
	}

	if shardedDB.Spec.TxLogEnrichment != "" {
		options = append(options, "txLogEnrichment: $tx_log_enrichment")
		params["tx_log_enrichment"] = shardedDB.Spec.TxLogEnrichment
	}

	return strings.Join(options, ", "), params, nil
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

// updateShardStatus updates the status with current shard information
func (r *Neo4jShardedDatabaseReconciler) updateShardStatus(ctx context.Context, shardedDB *neo4jv1beta1.Neo4jShardedDatabase, client *neo4j.Client) error {
	logger := log.FromContext(ctx).WithValues("database", shardedDB.Spec.Name)

	// Get individual database statuses for each shard
	databases, err := client.GetDatabases(ctx)
	if err != nil {
		logger.Error(err, "Failed to get database information")
		return fmt.Errorf("failed to get database information: %w", err)
	}

	// Check if the graph shard was successfully created
	graphShardName := fmt.Sprintf("%s-g000", shardedDB.Spec.Name)
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
func (r *Neo4jShardedDatabaseReconciler) updateStatus(ctx context.Context, shardedDB *neo4jv1beta1.Neo4jShardedDatabase, phase, message string, shardingReady *bool) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Fetch latest version
		var latest neo4jv1beta1.Neo4jShardedDatabase
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

		// Update Ready condition using standard helper
		condStatus, condReason := PhaseToConditionStatus(phase)
		SetReadyCondition(&latest.Status.Conditions, latest.Generation, condStatus, condReason, message)

		return r.Status().Update(ctx, &latest)
	})
}

// recordDestructiveRestoreGeneration stamps Status.LastDestructiveRestoreGeneration
// with the current spec generation after a Phase 2c destructive restore
// completes successfully. Used as a guard against re-triggering the
// drop-and-recreate cycle on every reconcile while
// spec.replaceExisting=true (which is the steady state after a destructive
// restore — users typically leave the field set rather than racing to
// unset it after Ready). Done as a separate Status.Update from the Ready
// transition so a single retry-on-conflict cycle covers it; failure here
// is non-fatal because a re-trigger is just a wasteful drop+create at
// the same generation, not data loss.
func (r *Neo4jShardedDatabaseReconciler) recordDestructiveRestoreGeneration(ctx context.Context, shardedDB *neo4jv1beta1.Neo4jShardedDatabase) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &neo4jv1beta1.Neo4jShardedDatabase{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(shardedDB), latest); err != nil {
			return err
		}
		latest.Status.LastDestructiveRestoreGeneration = latest.Generation
		return r.Status().Update(ctx, latest)
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
		For(&neo4jv1beta1.Neo4jShardedDatabase{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles,
		}).
		Complete(r)
}
