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

func TestValidateRestore_MissingCRWithoutSnapshotPointsAtStorage(t *testing.T) {
	r := restoreWithBackupRef("simple-restore", "default", "deleted-backup")
	rec := newResolvedSourceReconciler(t, r) // backup CR absent, no snapshot

	err := rec.validateRestore(context.Background(), r)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "source.type=storage"))
}
