/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"strings"
	"testing"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

func TestUserDatabasesFromArtifacts_ExcludesSystem(t *testing.T) {
	arts := []neo4jv1beta1.DatabaseArtifact{
		{Database: "neo4j", Filename: "neo4j-t.backup"},
		{Database: "system", Filename: "system-t.backup"},
		{Database: "customers", Filename: "customers-t.backup"},
	}
	got := userDatabasesFromArtifacts(arts)
	want := []string{"neo4j", "customers"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFilenameForDB(t *testing.T) {
	arts := []neo4jv1beta1.DatabaseArtifact{
		{Database: "neo4j", Filename: "neo4j-t.backup"},
		{Database: "customers", Filename: "customers-t.backup"},
	}
	if got := filenameForDB(arts, "customers"); got != "customers-t.backup" {
		t.Errorf("filenameForDB(customers) = %q, want customers-t.backup", got)
	}
	if got := filenameForDB(arts, "missing"); got != "" {
		t.Errorf("filenameForDB(missing) = %q, want empty", got)
	}
}

func TestEnsureDatabaseResults_SeedsAndIsIdempotent(t *testing.T) {
	r := &Neo4jRestoreReconciler{}
	restore := &neo4jv1beta1.Neo4jRestore{}

	r.ensureDatabaseResults(restore, []string{"neo4j", "customers"})
	if len(restore.Status.DatabaseResults) != 2 {
		t.Fatalf("expected 2 results, got %d", len(restore.Status.DatabaseResults))
	}
	for i := range restore.Status.DatabaseResults {
		if restore.Status.DatabaseResults[i].Phase != StatusPending {
			t.Errorf("result %d phase = %q, want Pending", i, restore.Status.DatabaseResults[i].Phase)
		}
	}

	// Mark one done, then re-run: existing results must be preserved (no reset,
	// no duplicates) and a newly-discovered DB appended.
	restore.Status.DatabaseResults[0].Phase = StatusCompleted
	r.ensureDatabaseResults(restore, []string{"neo4j", "customers", "orders"})
	if len(restore.Status.DatabaseResults) != 3 {
		t.Fatalf("expected 3 results after re-run, got %d", len(restore.Status.DatabaseResults))
	}
	if restore.Status.DatabaseResults[0].Phase != StatusCompleted {
		t.Errorf("existing Completed result was reset to %q", restore.Status.DatabaseResults[0].Phase)
	}
}

// buildAllDatabasesRestoreCommand drives the STANDALONE all-databases offline
// restore (#288): one `neo4j-admin database restore` per user database, from its
// exact .backup file in the resolved artifact map, system excluded, temp dir
// reset per database, and --overwrite-destination gated on spec.force.
func TestBuildAllDatabasesRestoreCommand_PVC(t *testing.T) {
	r := &Neo4jRestoreReconciler{}
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
		},
	}
	storage := &neo4jv1beta1.StorageLocation{Type: "pvc", PVC: &neo4jv1beta1.PVCSpec{Name: "sa-backup-store"}}
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			InstanceRef:  "sa",
			AllDatabases: true,
			Force:        true,
			Source:       neo4jv1beta1.RestoreSource{Type: "storage", Storage: storage, BackupPath: "sa-all-backup"},
		},
		Status: neo4jv1beta1.Neo4jRestoreStatus{
			ResolvedSource: &neo4jv1beta1.ResolvedRestoreSource{
				Storage:    storage,
				BackupPath: "sa-all-backup",
				DatabaseArtifacts: []neo4jv1beta1.DatabaseArtifact{
					{Database: "system", Filename: "system-t.backup"},
					{Database: "neo4j", Filename: "neo4j-t.backup"},
					{Database: "customers", Filename: "customers-t.backup"},
					{Database: "inventory", Filename: "inventory-t.backup"},
				},
			},
		},
	}

	cmd, err := r.buildAllDatabasesRestoreCommand(restore, cluster)
	if err != nil {
		t.Fatalf("buildAllDatabasesRestoreCommand: %v", err)
	}

	// system must never appear; every user DB must reference its EXACT file under
	// the chain-root directory (no `ls` glob — we have the precise filename).
	if strings.Contains(cmd, "system-t.backup") || strings.Contains(cmd, "'system'") {
		t.Errorf("system database must be excluded; got:\n%s", cmd)
	}
	for _, db := range []string{"neo4j", "customers", "inventory"} {
		want := "--from-path='/backup/sa-all-backup/" + db + "-t.backup' '" + db + "'"
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing exact restore for %q\nwant substring: %s\ngot:\n%s", db, want, cmd)
		}
	}
	// force → overwrite confirmed; per-database temp dir reset + explicit temp path.
	if !strings.Contains(cmd, "--overwrite-destination=true") {
		t.Errorf("spec.force should add --overwrite-destination=true; got:\n%s", cmd)
	}
	if strings.Count(cmd, "rm -rf /tmp/restore-tmp") != 3 {
		t.Errorf("expected one temp-dir reset per user database (3); got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--temp-path=/tmp/restore-tmp") {
		t.Errorf("expected explicit --temp-path; got:\n%s", cmd)
	}
}

// Without spec.force (and without options.replaceExisting) the command must NOT
// pass --overwrite-destination — a non-confirming all-databases restore against
// existing databases fails at neo4j-admin rather than silently clobbering.
func TestBuildAllDatabasesRestoreCommand_NoForceNoOverwrite(t *testing.T) {
	r := &Neo4jRestoreReconciler{}
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{Image: neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"}},
	}
	storage := &neo4jv1beta1.StorageLocation{Type: "pvc", PVC: &neo4jv1beta1.PVCSpec{Name: "store"}}
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			AllDatabases: true,
			Source:       neo4jv1beta1.RestoreSource{Type: "storage", Storage: storage, BackupPath: "b"},
		},
		Status: neo4jv1beta1.Neo4jRestoreStatus{
			ResolvedSource: &neo4jv1beta1.ResolvedRestoreSource{
				Storage:           storage,
				BackupPath:        "b",
				DatabaseArtifacts: []neo4jv1beta1.DatabaseArtifact{{Database: "neo4j", Filename: "neo4j-t.backup"}},
			},
		},
	}
	cmd, err := r.buildAllDatabasesRestoreCommand(restore, cluster)
	if err != nil {
		t.Fatalf("buildAllDatabasesRestoreCommand: %v", err)
	}
	if strings.Contains(cmd, "--overwrite-destination") {
		t.Errorf("no force/replaceExisting must not add --overwrite-destination; got:\n%s", cmd)
	}
}

// A resolved source carrying no per-database artifacts (e.g. the backup wasn't an
// all-databases backup) must be a clear error, not an empty/no-op command.
func TestBuildAllDatabasesRestoreCommand_NoArtifactsErrors(t *testing.T) {
	r := &Neo4jRestoreReconciler{}
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{Image: neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"}},
	}
	storage := &neo4jv1beta1.StorageLocation{Type: "pvc", PVC: &neo4jv1beta1.PVCSpec{Name: "store"}}
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{AllDatabases: true, Source: neo4jv1beta1.RestoreSource{Type: "storage", Storage: storage, BackupPath: "b"}},
		Status: neo4jv1beta1.Neo4jRestoreStatus{
			ResolvedSource: &neo4jv1beta1.ResolvedRestoreSource{Storage: storage, BackupPath: "b"},
		},
	}
	if _, err := r.buildAllDatabasesRestoreCommand(restore, cluster); err == nil {
		t.Fatalf("expected an error when no per-database artifacts are recorded")
	}
}
