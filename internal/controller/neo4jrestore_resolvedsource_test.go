/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

// Unit tests for the #188 fix: a `source.type: backup` restore pins the
// resolved backup location onto status.ResolvedSource on first resolution and
// restores from that snapshot thereafter, so it no longer depends on the
// referenced Neo4jBackup CR continuing to exist.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func newResolvedSourceReconciler(t *testing.T, objs ...client.Object) *Neo4jRestoreReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&neo4jv1beta1.Neo4jRestore{}).
		Build()
	return &Neo4jRestoreReconciler{
		Client:       c,
		Scheme:       scheme,
		Recorder:     record.NewFakeRecorder(16),
		RequeueAfter: time.Second,
	}
}

func pvcStorage(name string) *neo4jv1beta1.StorageLocation {
	return &neo4jv1beta1.StorageLocation{Type: "pvc", PVC: &neo4jv1beta1.PVCSpec{Name: name}}
}

func backupCRForRestore(name, ns string, succeeded bool) *neo4jv1beta1.Neo4jBackup {
	b := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Storage: *pvcStorage("backup-storage"),
		},
	}
	if succeeded {
		b.Status.History = []neo4jv1beta1.BackupRun{{
			Status:           "Succeeded",
			BackupsPath:      name + "/",
			ArtifactFilename: "neo4j-2026-06-11T10-00-00.backup",
		}}
	}
	return b
}

func restoreWithBackupRef(name, ns, backupRef string) *neo4jv1beta1.Neo4jRestore {
	return &neo4jv1beta1.Neo4jRestore{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			ClusterRef:   "standalone-neo4j",
			DatabaseName: "neo4j",
			Source:       neo4jv1beta1.RestoreSource{Type: SourceTypeBackup, BackupRef: backupRef},
		},
	}
}

func TestResolvedBackupSnapshot_NilWhenAbsentOrEmpty(t *testing.T) {
	r := restoreWithBackupRef("simple-restore", "default", "simple-backup")

	assert.Nil(t, resolvedBackupSnapshot(r), "no status.ResolvedSource yet")

	r.Status.ResolvedSource = &neo4jv1beta1.ResolvedRestoreSource{BackupRef: "simple-backup"}
	assert.Nil(t, resolvedBackupSnapshot(r), "ResolvedSource without Storage is not usable")

	r.Status.ResolvedSource.Storage = pvcStorage("backup-storage")
	assert.NotNil(t, resolvedBackupSnapshot(r), "ResolvedSource with Storage is usable")
}

func TestResolveRestoreSource_PrefersSnapshotWithoutBackupCR(t *testing.T) {
	r := restoreWithBackupRef("simple-restore", "default", "gone-backup")
	r.Status.ResolvedSource = &neo4jv1beta1.ResolvedRestoreSource{
		BackupRef:        "gone-backup",
		Storage:          pvcStorage("backup-storage"),
		BackupPath:       "gone-backup/",
		ArtifactFilename: "neo4j-2026-06-11T10-00-00.backup",
	}
	// Note: NO Neo4jBackup CR in the fake client — proves the resolver does
	// not touch it once a snapshot is pinned.
	rec := newResolvedSourceReconciler(t)

	src, err := rec.resolveRestoreSource(context.Background(), r)
	require.NoError(t, err)
	assert.Equal(t, "storage", src.Type)
	require.NotNil(t, src.Storage)
	assert.Equal(t, "pvc", src.Storage.Type)
	assert.Equal(t, "gone-backup/", src.BackupPath)
}

func TestEnsureResolvedBackupSource_NonBackupTypeIsNoOp(t *testing.T) {
	r := &neo4jv1beta1.Neo4jRestore{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{Type: "storage", Storage: pvcStorage("backup-storage"), BackupPath: "x/y.backup"},
		},
	}
	rec := newResolvedSourceReconciler(t, r)

	_, done, err := rec.ensureResolvedBackupSource(context.Background(), r)
	require.NoError(t, err)
	assert.False(t, done, "type=storage needs no pinning")
	assert.Nil(t, r.Status.ResolvedSource)
}

func TestEnsureResolvedBackupSource_AlreadyPinnedIsNoOp(t *testing.T) {
	r := restoreWithBackupRef("r", "default", "gone-backup")
	r.Status.ResolvedSource = &neo4jv1beta1.ResolvedRestoreSource{Storage: pvcStorage("backup-storage"), BackupPath: "p/"}
	rec := newResolvedSourceReconciler(t) // no backup CR

	_, done, err := rec.ensureResolvedBackupSource(context.Background(), r)
	require.NoError(t, err)
	assert.False(t, done, "already-pinned restore proceeds without re-resolving")
}

func TestEnsureResolvedBackupSource_BackupNotReadyRoutesPending(t *testing.T) {
	backup := backupCRForRestore("simple-backup", "default", false) // no Succeeded run
	r := restoreWithBackupRef("simple-restore", "default", "simple-backup")
	rec := newResolvedSourceReconciler(t, backup, r)

	res, done, err := rec.ensureResolvedBackupSource(context.Background(), r)
	require.NoError(t, err, "not-ready is transient, not an error")
	assert.True(t, done)
	assert.Positive(t, res.RequeueAfter)

	got := &neo4jv1beta1.Neo4jRestore{}
	require.NoError(t, rec.Get(context.Background(), client.ObjectKeyFromObject(r), got))
	assert.Equal(t, StatusPending, got.Status.Phase)
	assert.Nil(t, got.Status.ResolvedSource, "nothing pinned until a Succeeded run exists")
}

func TestEnsureResolvedBackupSource_MissingCRFailsWithStorageHint(t *testing.T) {
	r := restoreWithBackupRef("simple-restore", "default", "deleted-backup")
	rec := newResolvedSourceReconciler(t, r) // backup CR absent

	_, done, err := rec.ensureResolvedBackupSource(context.Background(), r)
	require.Error(t, err)
	assert.True(t, done)
	assert.Contains(t, err.Error(), "source.type=storage", "error must point at the escape hatch")
	assert.Contains(t, err.Error(), "do NOT recreate", "error must warn against recreating the CR")

	got := &neo4jv1beta1.Neo4jRestore{}
	require.NoError(t, rec.Get(context.Background(), client.ObjectKeyFromObject(r), got))
	assert.Equal(t, StatusFailed, got.Status.Phase)
}

func TestEnsureResolvedBackupSource_SuccessPinsSnapshot(t *testing.T) {
	backup := backupCRForRestore("simple-backup", "default", true)
	r := restoreWithBackupRef("simple-restore", "default", "simple-backup")
	rec := newResolvedSourceReconciler(t, backup, r)

	_, done, err := rec.ensureResolvedBackupSource(context.Background(), r)
	require.NoError(t, err)
	assert.False(t, done, "resolved successfully — restore proceeds")

	// In-memory restore is updated…
	require.NotNil(t, r.Status.ResolvedSource)
	assert.Equal(t, "simple-backup", r.Status.ResolvedSource.BackupRef)
	assert.Equal(t, "simple-backup/", r.Status.ResolvedSource.BackupPath)
	assert.Equal(t, "neo4j-2026-06-11T10-00-00.backup", r.Status.ResolvedSource.ArtifactFilename)
	require.NotNil(t, r.Status.ResolvedSource.Storage)
	assert.Equal(t, "pvc", r.Status.ResolvedSource.Storage.Type)

	// …and the snapshot is durably persisted.
	got := &neo4jv1beta1.Neo4jRestore{}
	require.NoError(t, rec.Get(context.Background(), client.ObjectKeyFromObject(r), got))
	require.NotNil(t, got.Status.ResolvedSource)
	assert.Equal(t, "neo4j-2026-06-11T10-00-00.backup", got.Status.ResolvedSource.ArtifactFilename)
}

func TestValidateRestore_PinnedSnapshotSurvivesMissingCR(t *testing.T) {
	r := restoreWithBackupRef("simple-restore", "default", "deleted-backup")
	r.Status.ResolvedSource = &neo4jv1beta1.ResolvedRestoreSource{
		BackupRef:  "deleted-backup",
		Storage:    pvcStorage("backup-storage"),
		BackupPath: "deleted-backup/",
	}
	rec := newResolvedSourceReconciler(t, r) // backup CR absent

	// validateRestore must NOT fail on the missing CR because the source is
	// already pinned. (No Neo4jShardedDatabase named "neo4j" exists, so the
	// sharded guard is a no-op.)
	err := rec.validateRestore(context.Background(), r)
	require.NoError(t, err)
}

// #269: the system database holds cluster topology, users, and roles and is
// owned by Neo4j — restoring over it is unsupported and must be rejected up
// front (case-insensitively), regardless of source. A pinned snapshot lets
// validateRestore get past source resolution to the databaseName check.
func TestValidateRestore_RejectsSystemDatabase(t *testing.T) {
	for _, name := range []string{"system", "System", "SYSTEM"} {
		r := restoreWithBackupRef("simple-restore", "default", "deleted-backup")
		r.Spec.DatabaseName = name
		r.Status.ResolvedSource = &neo4jv1beta1.ResolvedRestoreSource{
			BackupRef:  "deleted-backup",
			Storage:    pvcStorage("backup-storage"),
			BackupPath: "deleted-backup/",
		}
		rec := newResolvedSourceReconciler(t, r)

		err := rec.validateRestore(context.Background(), r)
		require.Error(t, err, "databaseName %q must be rejected", name)
		assert.Contains(t, err.Error(), "system database",
			"the error must explain why system is not restorable")
	}
}

func TestValidateRestore_MissingCRWithoutSnapshotPointsAtStorage(t *testing.T) {
	r := restoreWithBackupRef("simple-restore", "default", "deleted-backup")
	rec := newResolvedSourceReconciler(t, r) // backup CR absent, no snapshot

	err := rec.validateRestore(context.Background(), r)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "source.type=storage"))
}

// --- #227 pre-release items 3 + 4 ---

// TestLatestSucceededArtifactFilename_NewestRunWithoutFilenameIsRefused pins
// #227 item 3: when the MOST RECENT Succeeded run has no captured
// ArtifactFilename, the resolver must error actionably instead of silently
// falling through to an OLDER Succeeded run (which would restore stale data
// under a green status).
func TestLatestSucceededArtifactFilename_NewestRunWithoutFilenameIsRefused(t *testing.T) {
	backup := backupCRForRestore("nightly", "default", false)
	backup.Status.History = []neo4jv1beta1.BackupRun{
		{RunID: "nightly-backup-new", Status: "Succeeded", ArtifactFilename: ""}, // newest — capture missed
		{RunID: "nightly-backup-old", Status: "Succeeded", ArtifactFilename: "neo4j-2026-06-10T10-00-00.backup"},
	}
	r := newResolvedSourceReconciler(t, backup)

	_, err := r.latestSucceededArtifactFilename(context.Background(), "nightly", "default")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nightly-backup-new", "error must name the run whose filename capture missed")
	assert.Contains(t, err.Error(), "type=storage", "error must point at the explicit-path escape hatch")
	assert.NotContains(t, err.Error(), "2026-06-10", "must not leak/choose the older artifact")
}

// TestLatestSucceededArtifactFilename_NewestSucceededWins pins the happy path:
// the newest Succeeded run's filename is returned even when older runs exist,
// and Failed runs ahead of it are skipped.
func TestLatestSucceededArtifactFilename_NewestSucceededWins(t *testing.T) {
	backup := backupCRForRestore("nightly", "default", false)
	backup.Status.History = []neo4jv1beta1.BackupRun{
		{RunID: "nightly-backup-failed", Status: "Failed"},
		{RunID: "nightly-backup-new", Status: "Succeeded", ArtifactFilename: "neo4j-2026-06-11T10-00-00.backup"},
		{RunID: "nightly-backup-old", Status: "Succeeded", ArtifactFilename: "neo4j-2026-06-10T10-00-00.backup"},
	}
	r := newResolvedSourceReconciler(t, backup)

	fn, err := r.latestSucceededArtifactFilename(context.Background(), "nightly", "default")
	require.NoError(t, err)
	assert.Equal(t, "neo4j-2026-06-11T10-00-00.backup", fn)
}

// TestUpdateRestoreStatus_PersistsCallerTimestamps pins #227 item 4: the
// status writer refetches the CR, so StartTime/CompletionTime stamped on the
// caller's in-memory object were silently dropped. They must be carried over —
// and an already-persisted value must not be moved by a later requeue.
func TestUpdateRestoreStatus_PersistsCallerTimestamps(t *testing.T) {
	restore := restoreWithBackupRef("r1", "default", "nightly")
	r := newResolvedSourceReconciler(t, restore)
	ctx := context.Background()

	start := metav1.NewTime(time.Now().Add(-2 * time.Minute).Truncate(time.Second))
	completion := metav1.NewTime(time.Now().Truncate(time.Second))
	restore.Status.StartTime = &start
	restore.Status.CompletionTime = &completion

	r.updateRestoreStatus(ctx, restore, StatusCompleted, "done")

	persisted := &neo4jv1beta1.Neo4jRestore{}
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(restore), persisted))
	require.NotNil(t, persisted.Status.StartTime, "StartTime must survive the refetch-based status writer")
	require.NotNil(t, persisted.Status.CompletionTime, "CompletionTime must survive the refetch-based status writer")
	assert.True(t, persisted.Status.StartTime.Equal(&start))
	assert.True(t, persisted.Status.CompletionTime.Equal(&completion))

	// A second write with different in-memory stamps must NOT move the
	// persisted timestamps (earlier persisted values win).
	later := metav1.NewTime(time.Now().Add(time.Hour))
	restore.Status.StartTime = &later
	restore.Status.CompletionTime = &later
	r.updateRestoreStatus(ctx, restore, StatusCompleted, "done again")

	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(restore), persisted))
	assert.True(t, persisted.Status.StartTime.Equal(&start), "persisted StartTime must not be moved by a requeue")
	assert.True(t, persisted.Status.CompletionTime.Equal(&completion), "persisted CompletionTime must not be moved by a requeue")
}

// Pins the #227 stale-online guard: dbms.recreateDatabase is asynchronous, so
// an all-online SHOW DATABASE right after issue can be the PRE-recreate
// allocations. Acceptance requires a prior offline observation or the grace
// window to have elapsed.
func TestCypherRestoreOnlineAcceptable_StaleOnlineGuard(t *testing.T) {
	now := time.Now()
	stamp := func(issuedAgo time.Duration) map[string]string {
		return map[string]string{
			AnnotationCypherRestoreIssued: now.Add(-issuedAgo).UTC().Format(time.RFC3339),
		}
	}
	restore := func(ann map[string]string) *neo4jv1beta1.Neo4jRestore {
		r := restoreWithBackupRef("r1", "default", "nightly")
		r.Annotations = ann
		return r
	}

	// Fresh issue, no offline observation: all-online is suspect — refuse.
	assert.False(t, cypherRestoreOnlineAcceptable(restore(stamp(time.Second)), now),
		"all-online 1s after issue without an offline observation must be refused")

	// Offline was observed: trust the next all-online immediately.
	ann := stamp(time.Second)
	ann[AnnotationCypherRestoreObservedOffline] = "true"
	assert.True(t, cypherRestoreOnlineAcceptable(restore(ann), now),
		"observed-offline marker must unlock acceptance inside the grace window")

	// Grace window elapsed: accept even without an offline observation
	// (small store seeded entirely between two polls).
	assert.True(t, cypherRestoreOnlineAcceptable(restore(stamp(cypherRestoreStaleOnlineGrace+time.Second)), now),
		"all-online past the grace window must be accepted")

	// Missing or malformed issue stamps fail open (deadline logic tolerates
	// them too; blocking forever would be worse than the race).
	assert.True(t, cypherRestoreOnlineAcceptable(restore(nil), now))
	assert.True(t, cypherRestoreOnlineAcceptable(restore(map[string]string{
		AnnotationCypherRestoreIssued: "not-a-timestamp",
	}), now))
}

// clearCypherRestoreIssued must clear BOTH cypher-restore markers — a stale
// observed-offline annotation surviving into a fresh attempt would defeat the
// stale-online guard on the next recreate.
func TestClearCypherRestoreIssued_AlsoClearsObservedOffline(t *testing.T) {
	restore := restoreWithBackupRef("r1", "default", "nightly")
	restore.Annotations = map[string]string{
		AnnotationCypherRestoreIssued:          time.Now().UTC().Format(time.RFC3339),
		AnnotationCypherRestoreObservedOffline: "true",
	}
	r := newResolvedSourceReconciler(t, restore)
	ctx := context.Background()

	require.NoError(t, r.clearCypherRestoreIssued(ctx, restore))

	persisted := &neo4jv1beta1.Neo4jRestore{}
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(restore), persisted))
	assert.NotContains(t, persisted.Annotations, AnnotationCypherRestoreIssued)
	assert.NotContains(t, persisted.Annotations, AnnotationCypherRestoreObservedOffline)
	assert.NotContains(t, restore.Annotations, AnnotationCypherRestoreObservedOffline, "in-memory copy must be cleared too")
}

// markCypherRestoreObservedOffline persists the marker and is idempotent.
func TestMarkCypherRestoreObservedOffline(t *testing.T) {
	restore := restoreWithBackupRef("r1", "default", "nightly")
	r := newResolvedSourceReconciler(t, restore)
	ctx := context.Background()

	require.NoError(t, r.markCypherRestoreObservedOffline(ctx, restore))
	require.NoError(t, r.markCypherRestoreObservedOffline(ctx, restore))

	persisted := &neo4jv1beta1.Neo4jRestore{}
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(restore), persisted))
	assert.Equal(t, "true", persisted.Annotations[AnnotationCypherRestoreObservedOffline])
}

// #242: source.backupPath was the undiscoverable field — empty (or a bare
// "/", which resolves to the storage root and fails opaquely downstream)
// must produce an error that names where the value lives
// (status.history[*].backupsPath on the originating Neo4jBackup) and the
// backupRef alternative.
func TestValidateRestore_StorageBackupPathActionable(t *testing.T) {
	r := newResolvedSourceReconciler(t)
	ctx := context.Background()

	for _, bad := range []string{"", "/", "//", "  "} {
		restore := restoreWithBackupRef("r1", "default", "")
		restore.Spec.Source.Type = "storage"
		restore.Spec.Source.BackupRef = ""
		restore.Spec.Source.BackupPath = bad
		restore.Spec.Source.Storage = pvcStorage("backup-pvc")

		err := r.validateRestore(ctx, restore)
		require.Error(t, err, "backupPath %q must be rejected", bad)
		assert.Contains(t, err.Error(), "status.history[*].backupsPath",
			"the error must tell the user WHERE to find the path")
		assert.Contains(t, err.Error(), "backupRef",
			"the error must mention the backupRef alternative")
	}
}

// Pins the v1.12.1 release-verify finding: a kind:Cluster (all-databases)
// backup never captures a single ArtifactFilename (one artifact per DB), so a
// cluster restore referencing it must fail with the STRUCTURAL explanation —
// not the misleading "Pod-log capture is best-effort, re-run the backup"
// message, which can't help.
func TestLatestSucceededArtifactFilename_ClusterKindGivesStructuralError(t *testing.T) {
	backup := backupCRForRestore("clusterwide", "default", false)
	backup.Spec.Target.Kind = neo4jv1beta1.BackupTargetKindCluster
	backup.Status.History = []neo4jv1beta1.BackupRun{
		{RunID: "clusterwide-backup", Status: "Succeeded", ArtifactFilename: ""}, // kind:Cluster never captures one
	}
	r := newResolvedSourceReconciler(t, backup)

	_, err := r.latestSucceededArtifactFilename(context.Background(), "clusterwide", "default")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kind:Cluster", "error must name the structural cause")
	assert.Contains(t, err.Error(), "kind:Database", "error must point at the kind:Database remedy")
	assert.Contains(t, err.Error(), "type=storage", "error must offer the explicit-path escape hatch")
	assert.NotContains(t, err.Error(), "re-run the backup", "must not suggest re-running (structural, not flaky)")
}
