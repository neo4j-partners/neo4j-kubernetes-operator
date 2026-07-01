/*
Copyright 2025 Priyo Lahiri.

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
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/neo4j"
)

// startAllDatabasesRestore drives an all-databases restore (spec.allDatabases):
// it restores every USER database recorded in the resolved backup's per-database
// artifact map (the system database is always excluded), reporting per-database
// progress in status.databaseResults. It restores ONE database per reconcile
// pass and requeues — never blocking the worker on the asynchronous seed (the
// same non-blocking contract the single-database cluster path holds, #218/#227).
//
// This path drives CLUSTER targets only (cloud s3/gcs/azure or PVC-backed
// backups; the PVC path uses the in-cluster seed proxy). STANDALONE
// all-databases restore (#288) takes the offline Job path instead — the
// dispatch in startRestore gates this function on isTrueCluster, so a
// standalone never reaches here. The isTrueCluster guard below is therefore
// defensive (a direct call would still be rejected with an actionable message).
func (r *Neo4jRestoreReconciler) startAllDatabasesRestore(
	ctx context.Context,
	restore *neo4jv1beta1.Neo4jRestore,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
	isTrueCluster bool,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// The resolved source (pinned by ensureResolvedBackupSource, issue #188)
	// carries the per-database artifact map. Without it we can't enumerate.
	snap := resolvedBackupSnapshot(restore)
	if snap == nil || snap.Storage == nil {
		r.updateRestoreStatus(ctx, restore, StatusPending, "Waiting for the backup source to resolve before all-databases restore")
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	dbs := userDatabasesFromArtifacts(snap.DatabaseArtifacts)
	if len(dbs) == 0 {
		msg := "all-databases restore: the resolved backup recorded no per-database artifacts. The source must be an all-databases backup (spec.allDatabases / target.kind=Cluster) whose per-database map was captured in status.history[*].databaseArtifacts"
		r.updateRestoreStatus(ctx, restore, StatusFailed, msg)
		return ctrl.Result{}, fmt.Errorf("%s", msg)
	}

	// Surface (once) any property-sharded databases this restore's per-database
	// loop does not recreate — sharded DBs need the SET GRAPH/PROPERTY SHARDS
	// CREATE clauses only Neo4jShardedDatabase emits. They are NOT lost: an
	// all-databases backup taken by a recent operator catalogues each family's
	// shard artifacts (status.shardedFamilies), so each is restorable FROM THIS
	// BACKUP by pointing its Neo4jShardedDatabase CR's seedBackupRef at it.
	// Emitted on the first pass (before any per-database result is recorded) so
	// it doesn't repeat each reconcile.
	if len(snap.ShardedDatabasesExcluded) > 0 && len(restore.Status.DatabaseResults) == 0 {
		seedHint := "its Neo4jShardedDatabase CR (spec.seedBackupRef)"
		if snap.BackupRef != "" {
			seedHint = fmt.Sprintf("its Neo4jShardedDatabase CR with spec.seedBackupRef: %s", snap.BackupRef)
		}
		r.Recorder.Event(restore, corev1.EventTypeWarning, EventReasonRestoreShardedNotCovered,
			fmt.Sprintf("all-databases restore does not recreate property-sharded database(s) %s in its per-database loop — restore each via %s", strings.Join(snap.ShardedDatabasesExcluded, ", "), seedHint))
		logger.Info("all-databases restore: sharded databases restore via their Neo4jShardedDatabase CRs (seedBackupRef → this backup)",
			"shardedDatabases", snap.ShardedDatabasesExcluded, "backupRef", snap.BackupRef)
	}

	// Bounded scope for this release.
	if !isTrueCluster {
		msg := "all-databases restore currently supports cluster targets only; for a standalone, restore each database individually with spec.database"
		r.updateRestoreStatus(ctx, restore, StatusFailed, msg)
		return ctrl.Result{}, fmt.Errorf("%s", msg)
	}
	storage := *snap.Storage

	// Initialize per-database results once (idempotent across reconciles).
	r.ensureDatabaseResults(restore, dbs)

	// ONE-TIME, storage-specific seed setup the per-database loop depends on,
	// plus a per-database seedURI builder. The server pods fetch each seed, so
	// this gates the whole restore until ready: one rolling restart for cloud
	// credentials/endpoint (#190/#252), or one proxy rollout for PVC (#227).
	var seedURIFor func(filename string) string
	switch storage.Type {
	case "s3", "gcs", "azure":
		if res, ready, err := r.ensureClusterSeedConfigReady(ctx, restore, cluster, storage); !ready {
			return res, err
		}
		dirURI, err := buildSeedURIFromBackupStorage(storage, snap.BackupPath)
		if err != nil {
			r.updateRestoreStatus(ctx, restore, StatusFailed, err.Error())
			return ctrl.Result{}, err
		}
		dirURI = strings.TrimRight(dirURI, "/")
		seedURIFor = func(filename string) string { return dirURI + "/" + filename }
	case "pvc":
		if res, ready, err := r.ensurePVCSeedProxyReady(ctx, restore, storage); !ready {
			return res, err
		}
		seedURIFor = func(filename string) string {
			return pvcSeedProxyURL(restore.Name, restore.Namespace, snap.BackupPath, filename)
		}
	default:
		msg := fmt.Sprintf("all-databases restore does not support storage type %q (expected s3, gcs, azure, or pvc)", storage.Type)
		r.updateRestoreStatus(ctx, restore, StatusFailed, msg)
		return ctrl.Result{}, fmt.Errorf("%s", msg)
	}

	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.Error(err, "Failed to connect to cluster for all-databases restore")
		r.updateRestoreStatus(ctx, restore, StatusPending, fmt.Sprintf("Waiting to connect to cluster: %v", err))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}
	defer func() { _ = neo4jClient.Close() }()

	imageTag := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	version, vErr := neo4j.GetImageVersion(imageTag)
	if vErr != nil {
		version = &neo4j.Version{Major: 5, Minor: 26, Patch: 0}
	}

	// Drive ONE database action per pass, then requeue (non-blocking).
	for i := range restore.Status.DatabaseResults {
		res := &restore.Status.DatabaseResults[i]
		if res.Phase == StatusCompleted || res.Phase == StatusFailed {
			continue
		}
		db := res.Database
		fname := filenameForDB(snap.DatabaseArtifacts, db)
		if fname == "" {
			r.markDatabaseResult(ctx, restore, db, StatusFailed,
				"no .backup artifact filename recorded for this database in the source backup")
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
		seedURI := seedURIFor(fname)

		switch res.Phase {
		case "", StatusPending:
			// Issue the create/recreate exactly ONCE, then flip to Running and
			// persist BEFORE returning — re-issuing would wipe a partially-seeded
			// database. Re-entry finds Running and only polls (below).
			exists, exErr := neo4jClient.DatabaseExists(ctx, db)
			if exErr != nil {
				r.updateRestoreStatus(ctx, restore, StatusPending, fmt.Sprintf("Checking database %q: %v", db, exErr))
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
			}
			if exists {
				// Recreating an existing database is destructive — gate on the
				// same explicit opt-in as the single-database path (#218).
				if !restoreOverwriteConfirmed(restore) {
					r.markDatabaseResult(ctx, restore, db, StatusFailed,
						"database already exists; set spec.options.replaceExisting=true to overwrite it during an all-databases restore")
					return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
				}
				applied, rErr := neo4jClient.RecreateDatabaseWithSeedURI(ctx, version, db, seedURI)
				if rErr != nil {
					r.markDatabaseResult(ctx, restore, db, StatusFailed, fmt.Sprintf("recreateDatabase failed: %v", rErr))
					return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
				}
				if !applied {
					r.markDatabaseResult(ctx, restore, db, StatusFailed,
						fmt.Sprintf("Neo4j %d.%d does not support dbms.recreateDatabase; DROP DATABASE %q and re-run", version.Major, version.Minor, db))
					return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
				}
			} else if cErr := neo4jClient.CreateDatabaseWithSeedURIOptions(ctx, db, seedURI, true); cErr != nil {
				r.markDatabaseResult(ctx, restore, db, StatusFailed, fmt.Sprintf("CREATE DATABASE with seedURI failed: %v", cErr))
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
			}
			r.Recorder.Event(restore, corev1.EventTypeNormal, EventReasonRestoreStarted,
				fmt.Sprintf("All-databases restore: seeding %q from %s", db, seedURI))
			r.markDatabaseResult(ctx, restore, db, StatusRunning, "seeding from backup")
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil

		case StatusRunning:
			online, total, _, stErr := neo4jClient.DatabaseOnlineState(ctx, db)
			if stErr == nil && total > 0 && online == total {
				r.markDatabaseResult(ctx, restore, db, StatusCompleted, "online")
				return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
			}
			// Still seeding — requeue without blocking.
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
	}

	// No database is still pending/running — aggregate the terminal outcome.
	failed := 0
	for i := range restore.Status.DatabaseResults {
		if restore.Status.DatabaseResults[i].Phase == StatusFailed {
			failed++
		}
	}
	if failed > 0 {
		r.updateRestoreStatus(ctx, restore, StatusFailed,
			fmt.Sprintf("%d of %d databases failed to restore; see status.databaseResults", failed, len(restore.Status.DatabaseResults)))
		return ctrl.Result{}, nil
	}
	r.updateRestoreStatus(ctx, restore, StatusCompleted,
		fmt.Sprintf("Restored %d databases", len(restore.Status.DatabaseResults)))
	return ctrl.Result{}, nil
}

// ensureClusterSeedConfigReady projects the cloud seed credentials / custom
// endpoint onto the cluster once and gates on the resulting rolling restart, so
// every per-database seed runs against pods that can reach the backup store.
// Returns ready=false (with the result the caller must return) until the config
// has rolled out.
func (r *Neo4jRestoreReconciler) ensureClusterSeedConfigReady(
	ctx context.Context,
	restore *neo4jv1beta1.Neo4jRestore,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
	storage neo4jv1beta1.StorageLocation,
) (ctrl.Result, bool, error) {
	credsSecret := ""
	if storage.Cloud != nil {
		credsSecret = storage.Cloud.CredentialsSecretRef
	}
	hasCustomEndpoint := storage.Type == "s3" && storage.Cloud != nil && storage.Cloud.EndpointURL != ""

	projected, missing, projErr := r.projectClusterSeedConfig(ctx, cluster, credsSecret, storage.Cloud)
	if projErr != nil {
		if stderrors.Is(projErr, errSeedConfigNotAutoInherited) {
			msg := fmt.Sprintf("cluster %q's server pods can't reach the seed source: missing %s. The server JVM fetches each seed itself. Provide these on the cluster CR, or set annotation %s=\"true\" to let the operator inject them (one rolling restart).",
				cluster.Name, strings.Join(missing, "; "), AutoInheritSeedCredsAnnotation)
			r.updateRestoreStatus(ctx, restore, StatusFailed, msg)
			r.Recorder.Event(restore, corev1.EventTypeWarning, EventReasonSeedEndpointNotProjected, msg)
			return ctrl.Result{}, false, nil
		}
		r.updateRestoreStatus(ctx, restore, StatusPending,
			fmt.Sprintf("Retrying projection of seed configuration onto cluster %q: %v", cluster.Name, projErr))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, false, nil
	}
	if projected {
		r.updateRestoreStatus(ctx, restore, StatusPending,
			fmt.Sprintf("Projected seed configuration onto cluster %q; waiting for the rolling restart", cluster.Name))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, false, nil
	}
	if credsSecret != "" {
		rolled, rErr := r.seedCredsRolledOut(ctx, cluster, credsSecret)
		if rErr != nil || !rolled {
			r.updateRestoreStatus(ctx, restore, StatusPending,
				fmt.Sprintf("Waiting for cluster %q server pods to roll out seed credentials Secret %q", cluster.Name, credsSecret))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, false, nil
		}
	}
	if hasCustomEndpoint && clusterSpecEnvHasSeedEndpoint(cluster) {
		rolled, rErr := r.specEnvEndpointRolledOut(ctx, cluster)
		if rErr != nil || !rolled {
			r.updateRestoreStatus(ctx, restore, StatusPending,
				fmt.Sprintf("Waiting for cluster %q server pods to roll out the S3 endpoint", cluster.Name))
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, false, nil
		}
	}
	return ctrl.Result{}, true, nil
}

// ensurePVCSeedProxyReady spawns (idempotently) the in-cluster HTTP proxy in
// front of the backup PVC and gates on its readiness, so the per-database loop
// can build seedURIs against it via pvcSeedProxyURL. Mirrors the single-database
// PVC setup in resolveClusterPVCRestoreURI (bounded wait + diagnosis on a proxy
// that never starts, #227). Returns ready=false (with the result to return)
// until the proxy Deployment is Ready.
func (r *Neo4jRestoreReconciler) ensurePVCSeedProxyReady(
	ctx context.Context,
	restore *neo4jv1beta1.Neo4jRestore,
	storage neo4jv1beta1.StorageLocation,
) (ctrl.Result, bool, error) {
	logger := log.FromContext(ctx)
	if storage.PVC == nil || storage.PVC.Name == "" {
		msg := "PVC-backed all-databases restore requires storage.pvc.name to be set"
		r.updateRestoreStatus(ctx, restore, StatusFailed, msg)
		return ctrl.Result{}, false, fmt.Errorf("%s", msg)
	}

	proxyAvailable, err := ensurePVCSeedProxyResources(ctx, r.Client, r.Scheme, restore, restore.Name, storage.PVC.Name)
	if err != nil {
		r.updateRestoreStatus(ctx, restore, StatusFailed, fmt.Sprintf("ensure PVC seed proxy: %v", err))
		return ctrl.Result{}, false, fmt.Errorf("ensure PVC seed proxy: %w", err)
	}
	// Restrict the proxy (which serves the whole backup PVC) to the target
	// cluster's server pods (#219). Best-effort: only enforcing CNIs apply it.
	if npErr := ensurePVCSeedProxyNetworkPolicy(ctx, r.Client, r.Scheme, restore, restore.Name, restore.Spec.ClusterRef); npErr != nil {
		logger.Error(npErr, "Failed to ensure seed-proxy NetworkPolicy (non-fatal)")
	}

	if !proxyAvailable {
		// Bounded wait (#227): a proxy that can never start (RWO backup PVC
		// attached elsewhere, unpullable image) must not requeue forever.
		waitStart, haveAnchor := r.seedProxyWaitStart(ctx, restore)
		diagnosis := pvcSeedProxyDiagnosis(ctx, r.Client, restore.Namespace, restore.Name)
		budget := seedProxyWaitTimeout
		if restore.Spec.Timeout != "" {
			if d, perr := time.ParseDuration(restore.Spec.Timeout); perr == nil && d > 0 {
				budget = d
			}
		}
		if haveAnchor && time.Since(waitStart) > budget {
			msg := fmt.Sprintf("backup-seed-proxy Deployment did not become Ready within %s: %s — a common cause is the backup PVC (%s) being ReadWriteOnce and still attached elsewhere; fix the cause and re-trigger by bumping the spec",
				budget, diagnosis, storage.PVC.Name)
			r.updateRestoreStatus(ctx, restore, StatusFailed, msg)
			r.Recorder.Event(restore, corev1.EventTypeWarning, EventReasonRestoreFailed, msg)
			return ctrl.Result{}, false, nil
		}
		r.updateRestoreStatus(ctx, restore, StatusPending,
			fmt.Sprintf("Waiting for backup-seed-proxy Deployment to become Ready (%s)", diagnosis))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, false, nil
	}
	// Proxy is up — drop the wait anchor so any later wait starts fresh.
	if err := r.clearSeedProxyWaitStarted(ctx, restore); err != nil {
		logger.V(1).Info("Failed to clear seed-proxy wait anchor (non-fatal)", "error", err.Error())
	}
	return ctrl.Result{}, true, nil
}

// buildAllDatabasesRestoreCommand builds the offline `neo4j-admin database
// restore` command for a STANDALONE all-databases restore: one restore
// invocation per user database (system excluded), each from its exact .backup
// file in the resolved source, sharing one writable temp dir reset between
// databases. The instance is scaled to 0 while this Job runs (the standalone
// Job machinery, reused via createRestoreJob → buildRestoreCommand).
func (r *Neo4jRestoreReconciler) buildAllDatabasesRestoreCommand(restore *neo4jv1beta1.Neo4jRestore, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (string, error) {
	snap := resolvedBackupSnapshot(restore)
	if snap == nil || snap.Storage == nil {
		return "", fmt.Errorf("all-databases restore: resolved backup source not available")
	}
	dbs := userDatabasesFromArtifacts(snap.DatabaseArtifacts)
	if len(dbs) == 0 {
		return "", fmt.Errorf("all-databases restore: the resolved backup recorded no per-database artifacts")
	}

	imageTag := fmt.Sprintf("%s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	version, err := neo4j.GetImageVersion(imageTag)
	if err != nil {
		version = &neo4j.Version{Major: 5, Minor: 26, Patch: 0}
	}

	// dir is the chain-root directory (cloud URI like s3://.../<chain-root>, or
	// the local PVC mount /backup/<chain-root>). resolveRestoreSource has
	// normalized Source to type=storage before buildRestoreCommand runs, so
	// buildRestoreFromPath yields that directory; the exact per-database file is
	// dir + "/" + <filename> (we have the precise filename from the artifact map,
	// so no `ls` glob is needed).
	dir := strings.TrimRight(r.buildRestoreFromPath(restore), "/")
	overwrite := restoreOverwriteConfirmed(restore)

	var b strings.Builder
	b.WriteString("set -e; ")
	for _, db := range dbs {
		fname := filenameForDB(snap.DatabaseArtifacts, db)
		if fname == "" {
			return "", fmt.Errorf("all-databases restore: no artifact filename recorded for database %q", db)
		}
		// Reset the writable temp dir per database (neo4j-admin requires it
		// empty; the backup mount is ReadOnly), then restore that database.
		b.WriteString("rm -rf /tmp/restore-tmp && mkdir -p /tmp/restore-tmp && ")
		b.WriteString(neo4j.GetRestoreCommand(version, db, shellQuote(dir+"/"+fname)))
		if overwrite {
			b.WriteString(" --overwrite-destination=true")
		}
		b.WriteString(" --temp-path=/tmp/restore-tmp; ")
	}
	return b.String(), nil
}

// registerAllDatabasesAfterRestore brings every restored user database online
// after a standalone all-databases restore Job completes (the offline restore
// writes store files; the database must be CREATE'd/START'ed to be served), and
// records per-database results in status.databaseResults.
func (r *Neo4jRestoreReconciler) registerAllDatabasesAfterRestore(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore) error {
	snap := resolvedBackupSnapshot(restore)
	if snap == nil {
		return fmt.Errorf("all-databases restore: resolved source missing during registration")
	}
	dbs := userDatabasesFromArtifacts(snap.DatabaseArtifacts)
	r.ensureDatabaseResults(restore, dbs)

	neo4jClient, err := r.newStandaloneRestoreClient(ctx, restore)
	if err != nil {
		return fmt.Errorf("failed to create Neo4j client: %w", err)
	}
	defer func() { _ = neo4jClient.Close() }()

	var firstErr error
	for _, db := range dbs {
		exists, exErr := neo4jClient.DatabaseExists(ctx, db)
		if exErr == nil {
			if exists {
				exErr = neo4jClient.StartDatabase(ctx, db, false)
			} else {
				exErr = neo4jClient.CreateDatabase(ctx, db, nil, false, false)
			}
		}
		if exErr != nil {
			if firstErr == nil {
				firstErr = exErr
			}
			r.markDatabaseResult(ctx, restore, db, StatusFailed, fmt.Sprintf("register after restore: %v", exErr))
			continue
		}
		r.markDatabaseResult(ctx, restore, db, StatusCompleted, "restored and online")
	}
	return firstErr
}

// userDatabasesFromArtifacts returns the restorable user-database names from a
// per-database artifact map, excluding the system database (never user-restored).
func userDatabasesFromArtifacts(arts []neo4jv1beta1.DatabaseArtifact) []string {
	var out []string
	for _, a := range arts {
		if strings.EqualFold(a.Database, "system") {
			continue
		}
		out = append(out, a.Database)
	}
	return out
}

// filenameForDB returns the `.backup` filename recorded for a database.
func filenameForDB(arts []neo4jv1beta1.DatabaseArtifact, db string) string {
	for _, a := range arts {
		if a.Database == db {
			return a.Filename
		}
	}
	return ""
}

// ensureDatabaseResults seeds status.databaseResults with a Pending entry per
// database the first time, preserving any results already recorded.
func (r *Neo4jRestoreReconciler) ensureDatabaseResults(restore *neo4jv1beta1.Neo4jRestore, dbs []string) {
	existing := map[string]bool{}
	for i := range restore.Status.DatabaseResults {
		existing[restore.Status.DatabaseResults[i].Database] = true
	}
	for _, db := range dbs {
		if !existing[db] {
			restore.Status.DatabaseResults = append(restore.Status.DatabaseResults,
				neo4jv1beta1.DatabaseRestoreResult{Database: db, Phase: StatusPending})
		}
	}
}

// markDatabaseResult sets a database's per-DB phase/message in memory and
// durably persists status.databaseResults (refetch + RetryOnConflict — the
// project's status-write pattern; updateRestoreStatus copies only
// phase/message/conditions and would drop databaseResults, #227 item 4).
func (r *Neo4jRestoreReconciler) markDatabaseResult(ctx context.Context, restore *neo4jv1beta1.Neo4jRestore, db, phase, message string) {
	for i := range restore.Status.DatabaseResults {
		if restore.Status.DatabaseResults[i].Database == db {
			restore.Status.DatabaseResults[i].Phase = phase
			restore.Status.DatabaseResults[i].Message = message
			if phase == StatusCompleted || phase == StatusFailed {
				now := metav1.Now()
				restore.Status.DatabaseResults[i].CompletionTime = &now
			}
			break
		}
	}
	results := restore.Status.DatabaseResults
	_ = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &neo4jv1beta1.Neo4jRestore{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(restore), latest); err != nil {
			return err
		}
		latest.Status.DatabaseResults = results
		if latest.Status.Phase != StatusCompleted && latest.Status.Phase != StatusFailed {
			latest.Status.Phase = StatusRunning
		}
		return r.Status().Update(ctx, latest)
	})
}
