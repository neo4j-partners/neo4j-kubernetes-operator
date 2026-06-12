/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"fmt"
	"regexp"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/log"
	ctrl "sigs.k8s.io/controller-runtime/pkg/reconcile"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	neo4jclient "github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/validation"
)

// shardedShardNamePattern matches the per-shard naming convention Neo4j uses
// for property-sharded databases: {logical-name}-g000 (graph shard) and
// {logical-name}-p000…p{N-1} (property shards). Used by the glob-safety check
// to confirm every database matching the backup glob is actually a shard of the
// target sharded DB and not an unrelated database that happens to share the
// logical name as a prefix.
//
// Build the regex per-target since the logical name is user-supplied.
func shardedShardNamePattern(logicalName string) *regexp.Regexp {
	// QuoteMeta escapes any regex special chars in the user-supplied name so
	// e.g. a logical name containing "." can't widen the match.
	return regexp.MustCompile(`^` + regexp.QuoteMeta(logicalName) + `-(g|p)\d{3}$`)
}

// preflightAction tells the caller how to route after the sharded preflight.
type preflightAction int

const (
	preflightContinue preflightAction = iota // checks passed, continue normally
	preflightWait                            // not yet ready, requeue without changing phase to terminal
	preflightFail                            // terminal failure, set Phase=Failed and stop retrying
)

// shardedPreflightStatic runs the cheap, no-Neo4j-connection checks for a
// ShardedDatabase backup. Returns:
//   - preflightContinue if the backup is OK to proceed.
//   - preflightWait with a message if the sharded DB CR exists but is not yet
//     Ready. Caller should set status to "Pending" / "Waiting" and requeue.
//   - preflightFail with an error if the backup target is fundamentally
//     misconfigured (sharding disabled on cluster, version too old, sharded DB
//     CR missing, clusterRef mismatch). Caller should set Phase=Failed without
//     retrying.
//
// Returns preflightContinue immediately for non-ShardedDatabase kinds so the
// helper is safe to call unconditionally.
func (r *Neo4jBackupReconciler) shardedPreflightStatic(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (preflightAction, string, error) {
	if backup.Spec.Target.Kind != neo4jv1beta1.BackupTargetKindShardedDatabase {
		return preflightContinue, "", nil
	}

	if err := validation.IsClusterShardingReady(cluster); err != nil {
		return preflightFail, "", fmt.Errorf("cluster preflight failed: %w", err)
	}

	ns := backup.Spec.Target.Namespace
	if ns == "" {
		ns = backup.Namespace
	}
	shardedDB := &neo4jv1beta1.Neo4jShardedDatabase{}
	if err := r.Get(ctx, types.NamespacedName{Name: backup.Spec.Target.Name, Namespace: ns}, shardedDB); err != nil {
		if errors.IsNotFound(err) {
			return preflightFail, "", fmt.Errorf("Neo4jShardedDatabase %q not found in namespace %q", backup.Spec.Target.Name, ns)
		}
		return preflightFail, "", fmt.Errorf("failed to fetch Neo4jShardedDatabase %q: %w", backup.Spec.Target.Name, err)
	}

	if shardedDB.Spec.ClusterRef != backup.Spec.Target.ClusterRef {
		return preflightFail, "", fmt.Errorf("Neo4jShardedDatabase %q references cluster %q but backup target.clusterRef is %q", shardedDB.Name, shardedDB.Spec.ClusterRef, backup.Spec.Target.ClusterRef)
	}

	if shardedDB.Status.ShardingReady == nil || !*shardedDB.Status.ShardingReady {
		return preflightWait, fmt.Sprintf("Neo4jShardedDatabase %q is not yet Ready", shardedDB.Name), nil
	}

	return preflightContinue, "", nil
}

// shardedPreflightGlobSafety runs the expensive `SHOW DATABASES` check that
// guards against the neo4j-admin glob `{logical-name}*` pulling in unrelated
// databases (e.g. a backup for "products" silently including "productsales").
// Only call this for ShardedDatabase kinds AND only at Job-creation time —
// running it on every reconcile would issue a Bolt query every poll.
//
// Returns nil if the glob is safe. Returns an error naming the offending
// database(s) otherwise; the caller should route the backup to Phase=Failed.
func (r *Neo4jBackupReconciler) shardedPreflightGlobSafety(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	if backup.Spec.Target.Kind != neo4jv1beta1.BackupTargetKindShardedDatabase {
		return nil
	}

	client, err := neo4jclient.NewClientForEnterprise(cluster, r.Client, cluster.Spec.Auth.AdminSecret)
	if err != nil {
		// Connect failures are indistinguishable from a cluster that is
		// momentarily unreachable — transient (#217), not a glob violation.
		return fmt.Errorf("failed to open Neo4j client for glob-safety check: %v: %w", err, errBackupTransient)
	}
	defer client.Close()

	dbs, err := client.GetDatabases(ctx)
	if err != nil {
		return fmt.Errorf("failed to list databases for glob-safety check: %v: %w", err, errBackupTransient)
	}

	logical := backup.Spec.Target.Name
	shardPattern := shardedShardNamePattern(logical)
	prefix := logical
	var poisoning []string
	for _, db := range dbs {
		// SHOW DATABASES returns duplicate rows per database (one per role).
		// Filter by exact name match against the prefix and shard regex.
		if len(db.Name) < len(prefix) || db.Name[:len(prefix)] != prefix {
			continue
		}
		if db.Name == logical {
			// The "virtual" aggregated database — same name as the logical
			// sharded DB. Today this doesn't appear in SHOW DATABASES until
			// first access (per neo4jshardeddatabase_controller.go), but if a
			// future Neo4j release surfaces it we still want to allow it
			// since it's part of the same sharded family.
			continue
		}
		if shardPattern.MatchString(db.Name) {
			continue
		}
		// Deduplicate names that appear once per role row.
		if !contains(poisoning, db.Name) {
			poisoning = append(poisoning, db.Name)
		}
	}
	if len(poisoning) > 0 {
		return fmt.Errorf("backup glob %q would also match unrelated database(s): %v — rename or drop the conflicting database(s) before running this backup", logical+"*", poisoning)
	}
	return nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// expectedShardArtifactsForBackup returns the per-shard artifact stubs for a
// sharded backup run by reading the referenced Neo4jShardedDatabase CR's spec.
// Returns nil (no error) for:
//   - non-ShardedDatabase backups,
//   - missing CR (sharded DB was deleted between backup creation and run
//     completion),
//   - any fetch error (caller treats this as a UX-only signal, not a hard
//     failure).
//
// Phase 3 deliberately leaves ShardArtifact.Filename + Size empty: capturing
// the actual neo4j-admin output filenames + bytes would require Pod-log
// access (kubernetes.Clientset) the operator doesn't currently wire in.
// ShardName alone is the audit-load-bearing field ("did all shards get
// backed up?") and can be filled from the spec without log parsing. A
// future enhancement can populate Filename/Size from Pod logs without a CRD
// break.
func (r *Neo4jBackupReconciler) expectedShardArtifactsForBackup(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup) []neo4jv1beta1.ShardArtifact {
	if backup.Spec.Target.Kind != neo4jv1beta1.BackupTargetKindShardedDatabase {
		return nil
	}

	ns := backup.Spec.Target.Namespace
	if ns == "" {
		ns = backup.Namespace
	}
	shardedDB := &neo4jv1beta1.Neo4jShardedDatabase{}
	if err := r.Get(ctx, types.NamespacedName{Name: backup.Spec.Target.Name, Namespace: ns}, shardedDB); err != nil {
		log.FromContext(ctx).Info("expectedShardArtifactsForBackup: sharded DB CR not found, returning empty artifact list",
			"shardedDB", backup.Spec.Target.Name, "namespace", ns)
		return nil
	}

	logical := backup.Spec.Target.Name
	count := int(shardedDB.Spec.PropertySharding.PropertyShards)
	if count < 0 {
		count = 0
	}
	artifacts := make([]neo4jv1beta1.ShardArtifact, 0, 1+count)
	artifacts = append(artifacts, neo4jv1beta1.ShardArtifact{ShardName: fmt.Sprintf("%s-g000", logical)})
	for i := 0; i < count; i++ {
		artifacts = append(artifacts, neo4jv1beta1.ShardArtifact{ShardName: fmt.Sprintf("%s-p%03d", logical, i)})
	}
	return artifacts
}

// updateShardedDBLastBackup is the Phase 3 reverse-lookup that updates a
// Neo4jShardedDatabase CR's status.lastBackup when a sharded backup run
// completes successfully. Non-fatal: any failure (CR not found, status patch
// conflict beyond the retry budget) is logged and swallowed — the backup's
// own status.history remains the source of truth, this is just a UX hint on
// the sharded DB CR so operators can audit backup health without grepping
// Neo4jBackup CRs.
//
// No-op when:
//   - backup target.kind is not ShardedDatabase,
//   - the run is not Succeeded (Failed runs do not overwrite lastBackup),
//   - the target Neo4jShardedDatabase CR doesn't exist (e.g. user deleted it
//     while a backup was still in flight, or the CR is managed externally).
//
// Conflict-on-update: the sharded DB controller writes to its own status on
// every reconcile, so concurrent writes are normal. Use the standard
// retry.RetryOnConflict pattern (matches CLAUDE.md rule 1).
func (r *Neo4jBackupReconciler) updateShardedDBLastBackup(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup, run neo4jv1beta1.BackupRun) {
	if backup.Spec.Target.Kind != neo4jv1beta1.BackupTargetKindShardedDatabase {
		return
	}
	if run.Status != "Succeeded" {
		return
	}

	logger := log.FromContext(ctx)

	ns := backup.Spec.Target.Namespace
	if ns == "" {
		ns = backup.Namespace
	}

	update := func() error {
		shardedDB := &neo4jv1beta1.Neo4jShardedDatabase{}
		if err := r.Get(ctx, types.NamespacedName{Name: backup.Spec.Target.Name, Namespace: ns}, shardedDB); err != nil {
			return err
		}
		shardedDB.Status.LastBackup = &neo4jv1beta1.ShardedDatabaseBackupReference{
			BackupRef:   backup.Name,
			RunID:       run.RunID,
			BackupsPath: run.BackupsPath,
			Timestamp:   run.CompletionTime,
		}
		return r.Status().Update(ctx, shardedDB)
	}
	if err := retry.RetryOnConflict(retry.DefaultBackoff, update); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Skipping Neo4jShardedDatabase.status.lastBackup update — CR not found",
				"shardedDB", backup.Spec.Target.Name, "namespace", ns)
			return
		}
		logger.Error(err, "Failed to update Neo4jShardedDatabase.status.lastBackup (non-fatal)",
			"shardedDB", backup.Spec.Target.Name, "namespace", ns, "backup", backup.Name)
	}
}

// applyShardedPreflight is the convenience wrapper used by the reconcile flow:
// runs the static preflight, translates results to status/result/error tuples
// the caller can return verbatim. Returns (done=true, result, err) when the
// caller should return immediately. Returns (false, _, _) to continue.
func (r *Neo4jBackupReconciler) applyShardedPreflight(ctx context.Context, backup *neo4jv1beta1.Neo4jBackup, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) (bool, ctrl.Result, error) {
	action, waitMsg, err := r.shardedPreflightStatic(ctx, backup, cluster)
	switch action {
	case preflightContinue:
		return false, ctrl.Result{}, nil
	case preflightWait:
		r.updateBackupStatus(ctx, backup, "Waiting", waitMsg)
		return true, ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	case preflightFail:
		r.updateBackupStatus(ctx, backup, "Failed", err.Error())
		// Returning nil error so controller-runtime doesn't requeue with
		// backoff. The terminal-phase guard in handleOneTimeBackup keeps the
		// CR pinned to Failed.
		return true, ctrl.Result{}, nil
	}
	return false, ctrl.Result{}, nil
}
