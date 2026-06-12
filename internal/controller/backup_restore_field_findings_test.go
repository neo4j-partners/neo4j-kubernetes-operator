/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

// Pinning tests for the v1.12.0 field findings (#251, #253-#256): bugs that
// shipped because the documented paths were never the tested paths. Each test
// here executes the path the DOCS teach, not the path the original specs took.

import (
	"context"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func fieldFindingsCluster(tag string) *neo4jv1beta1.Neo4jEnterpriseCluster {
	return &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "ec", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image:    neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: tag},
			Topology: neo4jv1beta1.TopologyConfiguration{Servers: 2},
		},
	}
}

func fieldFindingsBackup(opts *neo4jv1beta1.BackupOptions) *neo4jv1beta1.Neo4jBackup {
	return &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "bk", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Target: neo4jv1beta1.BackupTarget{
				Kind:       neo4jv1beta1.BackupTargetKindDatabase,
				Name:       "neo4j",
				ClusterRef: "ec",
			},
			Storage: neo4jv1beta1.StorageLocation{
				Type: "pvc",
				PVC:  &neo4jv1beta1.PVCSpec{Name: "backup-pvc"},
			},
			Options: opts,
		},
	}
}

// #256: a user-supplied options.tempPath must get a mkdir prelude — neo4j-admin
// refuses a staging path that doesn't exist, and nothing else creates it (the
// shipped MinIO example failed on exactly this).
func TestBuildBackupCommand_TempPathGetsMkdirPrelude(t *testing.T) {
	r := newShardedTestReconciler(t)
	backup := fieldFindingsBackup(&neo4jv1beta1.BackupOptions{TempPath: "/tmp/neo4j-backup-staging"})

	cmd, err := r.buildBackupCommand(context.Background(), backup, fieldFindingsCluster("5.26.0-enterprise"))
	if err != nil {
		t.Fatalf("buildBackupCommand: %v", err)
	}
	if !strings.Contains(cmd, "mkdir -p '/tmp/neo4j-backup-staging' && ") {
		t.Errorf("tempPath must be created before neo4j-admin runs; got: %q", cmd)
	}
	if !strings.Contains(cmd, "--temp-path='/tmp/neo4j-backup-staging'") {
		t.Errorf("expected quoted --temp-path; got: %q", cmd)
	}
}

// #255: `neo4j-admin backup validate` exists only on CalVer images. On 5.26
// the clause must be SKIPPED (previously it was emitted, rejected by the CLI,
// and swallowed by `|| true` — the user silently got no validation).
func TestBuildBackupCommand_ValidateGatedOnCalver(t *testing.T) {
	vTrue := true
	r := newShardedTestReconciler(t)
	backup := fieldFindingsBackup(&neo4jv1beta1.BackupOptions{Validate: &vTrue})

	cmd, err := r.buildBackupCommand(context.Background(), backup, fieldFindingsCluster("5.26.0-enterprise"))
	if err != nil {
		t.Fatalf("buildBackupCommand (5.26): %v", err)
	}
	if strings.Contains(cmd, "backup validate") {
		t.Errorf("validate clause must be skipped on 5.26 (subcommand doesn't exist); got: %q", cmd)
	}

	cmd, err = r.buildBackupCommand(context.Background(), backup, fieldFindingsCluster("2026.04-enterprise"))
	if err != nil {
		t.Fatalf("buildBackupCommand (calver): %v", err)
	}
	if !strings.Contains(cmd, "backup validate") {
		t.Errorf("validate clause expected on CalVer; got: %q", cmd)
	}
}

// #253: both spec.force and options.replaceExisting are documented as the
// overwrite confirmation; the Job command builders must honor BOTH (only
// force was wired, so the documented replaceExisting path failed with
// "Database ... already exists").
func TestRestoreOverwriteConfirmed(t *testing.T) {
	cases := []struct {
		name string
		spec neo4jv1beta1.Neo4jRestoreSpec
		want bool
	}{
		{"force only", neo4jv1beta1.Neo4jRestoreSpec{Force: true}, true},
		{"replaceExisting only", neo4jv1beta1.Neo4jRestoreSpec{Options: &neo4jv1beta1.RestoreOptionsSpec{ReplaceExisting: true}}, true},
		{"both", neo4jv1beta1.Neo4jRestoreSpec{Force: true, Options: &neo4jv1beta1.RestoreOptionsSpec{ReplaceExisting: true}}, true},
		{"neither", neo4jv1beta1.Neo4jRestoreSpec{}, false},
		{"nil options", neo4jv1beta1.Neo4jRestoreSpec{Options: nil}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &neo4jv1beta1.Neo4jRestore{Spec: tc.spec}
			if got := restoreOverwriteConfirmed(r); got != tc.want {
				t.Errorf("restoreOverwriteConfirmed = %v, want %v", got, tc.want)
			}
		})
	}
}

// Retention-on-delete field findings: the cleanup Job was (1) owner-ref'd to
// the CR being deleted — the GC raced (and beat) the prune script — and
// (2) built on `find -printf`, which busybox/alpine find doesn't implement,
// so even a surviving Job died under `set -e`. Verified live: maxCount=2 with
// 7 artifacts pruned nothing.
func TestBuildRetentionScript_BusyboxPortable(t *testing.T) {
	policy := &neo4jv1beta1.RetentionPolicy{MaxCount: 2, MaxAge: "7d"}
	script := buildRetentionScript(policy, "chain")
	if strings.Contains(script, "-printf") {
		t.Errorf("retention script must not use find -printf (unsupported on busybox/alpine):\n%s", script)
	}
	if !strings.Contains(script, "stat -c '%Y %n'") {
		t.Errorf("expected busybox-portable stat -c mtime listing:\n%s", script)
	}
	if !strings.Contains(script, "MAX_COUNT=2") {
		t.Errorf("maxCount not rendered:\n%s", script)
	}
}

func TestCleanupJobHasNoOwnerReference(t *testing.T) {
	r := newShardedTestReconciler(t)
	backup := fieldFindingsBackup(nil)
	backup.Spec.Retention = &neo4jv1beta1.RetentionPolicy{MaxCount: 2}
	backup.Spec.Options = &neo4jv1beta1.BackupOptions{BackupType: "FULL"}

	if err := r.cleanupBackupArtifacts(context.Background(), backup); err != nil {
		t.Fatalf("cleanupBackupArtifacts: %v", err)
	}
	jobs := &batchv1.JobList{}
	if err := r.List(context.Background(), jobs); err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("expected exactly one cleanup Job, got %d", len(jobs.Items))
	}
	if len(jobs.Items[0].OwnerReferences) != 0 {
		t.Errorf("cleanup Job must NOT be owner-ref'd to the CR being deleted (GC races the prune script); got: %v", jobs.Items[0].OwnerReferences)
	}
}

// #227: PVC-backed backup Jobs must serialize on the chain directory via
// flock — the operator's Job-creation gate can't stop two CronJob children
// (FULL parent + DIFF child sharing the dir via chainFromBackup) firing into
// the same reconcile gap. The lock is fd-based (no nested quoting) and held
// for the whole command chain; cloud targets have no shared fs to lock.
func TestBuildBackupCommand_PVCChainDirFlock(t *testing.T) {
	r := newShardedTestReconciler(t)
	backup := fieldFindingsBackup(nil)

	cmd, err := r.buildBackupCommand(context.Background(), backup, fieldFindingsCluster("5.26.0-enterprise"))
	if err != nil {
		t.Fatalf("buildBackupCommand: %v", err)
	}
	wantPrefix := "mkdir -p '/backup/bk/' && exec 9>'/backup/bk/.chain.lock' && flock -w 3600 9 && "
	if !strings.HasPrefix(cmd, wantPrefix) {
		t.Fatalf("PVC backup command must take the chain-dir flock before neo4j-admin:\nwant prefix %q\ngot %q", wantPrefix, cmd)
	}

	// A DIFF child chained into another CR's dir locks the PARENT's chain
	// root — that's the directory the two CRs contend on.
	backup.Spec.ChainFromBackup = "daily-full"
	cmd, err = r.buildBackupCommand(context.Background(), backup, fieldFindingsCluster("5.26.0-enterprise"))
	if err != nil {
		t.Fatalf("buildBackupCommand (chained): %v", err)
	}
	if !strings.Contains(cmd, "exec 9>'/backup/daily-full/.chain.lock'") {
		t.Fatalf("chained backup must lock the parent chain root, got %q", cmd)
	}

	// Cloud targets: no flock (nothing to lock on object storage).
	backup = fieldFindingsBackup(nil)
	backup.Spec.Storage = neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "bkt"}
	cmd, err = r.buildBackupCommand(context.Background(), backup, fieldFindingsCluster("5.26.0-enterprise"))
	if err != nil {
		t.Fatalf("buildBackupCommand (s3): %v", err)
	}
	if strings.Contains(cmd, "flock") {
		t.Fatalf("cloud backup must not attempt flock, got %q", cmd)
	}
}
