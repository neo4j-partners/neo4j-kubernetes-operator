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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// TestBuildSeedURIFromBackupStorage pins the per-storage-type URI shape.
// The trailing slash is load-bearing — CloudSeedProvider treats values
// without a trailing slash as a single artifact path rather than a directory.
func TestBuildSeedURIFromBackupStorage(t *testing.T) {
	cases := []struct {
		name       string
		storage    neo4jv1beta1.StorageLocation
		backupPath string
		want       string
		wantErr    string
	}{
		{
			name:       "S3 with storage.path and backupPath joined",
			storage:    neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "my-bucket", Path: "neo4j-backups"},
			backupPath: "products-backup",
			want:       "s3://my-bucket/neo4j-backups/products-backup/",
		},
		{
			name:       "GCS empty storage.path, backupPath only",
			storage:    neo4jv1beta1.StorageLocation{Type: "gcs", Bucket: "neo4j-prod"},
			backupPath: "products-backup-cron-28832400",
			want:       "gs://neo4j-prod/products-backup-cron-28832400/",
		},
		{
			name:       "Azure full URI",
			storage:    neo4jv1beta1.StorageLocation{Type: "azure", Bucket: "container1", Path: "backups"},
			backupPath: "run-001",
			want:       "azb://container1/backups/run-001/",
		},
		{
			name:       "storage.path with trailing slash doesn't double",
			storage:    neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "b", Path: "neo4j-backups/"},
			backupPath: "p-backup",
			want:       "s3://b/neo4j-backups/p-backup/",
		},
		{
			name:       "PVC storage is rejected (cluster pods can't read backup PVC)",
			storage:    neo4jv1beta1.StorageLocation{Type: "pvc"},
			backupPath: "p-backup",
			wantErr:    "requires cloud-backed backup storage",
		},
		{
			name:       "empty storage type rejected",
			storage:    neo4jv1beta1.StorageLocation{},
			backupPath: "p-backup",
			wantErr:    "requires cloud-backed backup storage",
		},
		{
			name:       "unsupported storage type rejected",
			storage:    neo4jv1beta1.StorageLocation{Type: "ftp"},
			backupPath: "p-backup",
			wantErr:    "does not support storage type",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildSeedURIFromBackupStorage(tc.storage, tc.backupPath)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if !strings.HasSuffix(got, "/") {
				t.Errorf("seed URI %q must end with / (directory semantics)", got)
			}
		})
	}
}

// TestResolveShardedSeed_Matrix exercises the end-to-end resolver path that
// dereferences spec.seedBackupRef into a concrete seedURI via the shared
// ResolveBackupRef helper. Sentinel errors (ErrBackupNotReady) must remain
// detectable via errors.Is so the controller routes them to Pending instead
// of Failed.
func TestResolveShardedSeed_Matrix(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("clientgoscheme: %v", err)
	}
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("neo4j scheme: %v", err)
	}

	completionTime := metav1.Now()

	mkBackup := func(name string, storage neo4jv1beta1.StorageLocation, history []neo4jv1beta1.BackupRun) *neo4jv1beta1.Neo4jBackup {
		return &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				InstanceRef:     "ec",
				ShardedDatabase: "products",
				Storage:         storage,
			},
			Status: neo4jv1beta1.Neo4jBackupStatus{History: history},
		}
	}

	cases := []struct {
		name          string
		seedBackupRef string
		seedObjects   []runtime.Object
		wantURI       string
		wantErrIs     error
		wantErrSubstr string
	}{
		{
			name:          "empty seedBackupRef → empty URI, no error",
			seedBackupRef: "",
			seedObjects:   nil,
			wantURI:       "",
		},
		{
			name:          "backup with Succeeded run → resolves to s3 directory URI",
			seedBackupRef: "products-backup",
			seedObjects: []runtime.Object{
				mkBackup("products-backup",
					neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "b", Path: "backups"},
					[]neo4jv1beta1.BackupRun{
						{RunID: "uid-1", Status: "Succeeded", BackupsPath: "products-backup", CompletionTime: &completionTime},
					}),
			},
			wantURI: "s3://b/backups/products-backup/",
		},
		{
			name:          "backup with no Succeeded run → ErrBackupNotReady (transient)",
			seedBackupRef: "products-backup",
			seedObjects: []runtime.Object{
				mkBackup("products-backup",
					neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "b"},
					[]neo4jv1beta1.BackupRun{
						{RunID: "uid-1", Status: "Running"},
					}),
			},
			wantErrIs: ErrBackupNotReady,
		},
		{
			name:          "backup CR missing → permanent error (not ErrBackupNotReady)",
			seedBackupRef: "missing-backup",
			seedObjects:   nil,
			wantErrSubstr: "failed to get Neo4jBackup",
		},
		{
			name:          "PVC backup without storage.PVC.Name → permanent error",
			seedBackupRef: "products-backup",
			seedObjects: []runtime.Object{
				mkBackup("products-backup",
					neo4jv1beta1.StorageLocation{Type: "pvc"}, // PVC.Name missing
					[]neo4jv1beta1.BackupRun{
						{RunID: "uid-1", Status: "Succeeded", BackupsPath: "products-backup", CompletionTime: &completionTime},
					}),
			},
			wantErrSubstr: "PVC-backed seedBackupRef requires the backup's storage.pvc.name",
		},
		{
			name:          "PVC backup without shardArtifacts metadata → permanent error",
			seedBackupRef: "products-backup",
			seedObjects: []runtime.Object{
				mkBackup("products-backup",
					neo4jv1beta1.StorageLocation{Type: "pvc", PVC: &neo4jv1beta1.PVCSpec{Name: "backup-pvc"}},
					[]neo4jv1beta1.BackupRun{
						{RunID: "uid-1", Status: "Succeeded", BackupsPath: "products-backup", CompletionTime: &completionTime},
					}),
			},
			wantErrSubstr: "no shard artifacts resolved",
		},
		{
			name:          "PVC backup with shardArtifacts but empty Filenames → permanent error",
			seedBackupRef: "products-backup",
			seedObjects: []runtime.Object{
				mkBackup("products-backup",
					neo4jv1beta1.StorageLocation{Type: "pvc", PVC: &neo4jv1beta1.PVCSpec{Name: "backup-pvc"}},
					[]neo4jv1beta1.BackupRun{
						{
							RunID: "uid-1", Status: "Succeeded",
							BackupsPath:    "products-backup",
							CompletionTime: &completionTime,
							ShardArtifacts: []neo4jv1beta1.ShardArtifact{
								{ShardName: "products-g000"}, // no Filename
							},
						},
					}),
			},
			wantErrSubstr: "have empty Filename",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tc.seedObjects...).
				Build()
			r := &Neo4jShardedDatabaseReconciler{Client: c}
			shardedDB := &neo4jv1beta1.Neo4jShardedDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "products", Namespace: "default"},
				Spec:       neo4jv1beta1.Neo4jShardedDatabaseSpec{SeedBackupRef: tc.seedBackupRef},
			}
			resolved, err := r.resolveShardedSeed(context.Background(), shardedDB)
			uri := ""
			if resolved != nil {
				uri = resolved.URI
			}
			if tc.wantErrIs != nil {
				if err == nil || !stderrors.Is(err, tc.wantErrIs) {
					t.Fatalf("err=%v, want errors.Is(%v)", err, tc.wantErrIs)
				}
				return
			}
			if tc.wantErrSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrSubstr) {
					t.Fatalf("err=%v, want substring %q", err, tc.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if uri != tc.wantURI {
				t.Errorf("uri=%q, want %q", uri, tc.wantURI)
			}
		})
	}
}

// TestResolveShardedSeed_AllDatabasesFamily exercises seeding a single sharded
// family from an ALL-DATABASES backup (run.ShardedFamilies). Cloud all-DB seeds
// via explicit per-shard cloud URIs (the directory holds every DB, so a
// directory-scan URI can't isolate one family); the family is selected by
// spec.seedSourceDatabase (or the target name when restoring in place), with
// shard names remapped to the target.
func TestResolveShardedSeed_AllDatabasesFamily(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("clientgoscheme: %v", err)
	}
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("neo4j scheme: %v", err)
	}
	completionTime := metav1.Now()

	// An all-databases backup: ShardArtifacts empty, ShardedFamilies populated
	// for two families. Each family's shards carry captured filenames.
	allDBBackup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "all-backup", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			InstanceRef:  "ec",
			AllDatabases: true,
			Storage:      neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "b", Path: "backups"},
		},
		Status: neo4jv1beta1.Neo4jBackupStatus{History: []neo4jv1beta1.BackupRun{{
			RunID: "uid-1", Status: "Succeeded", BackupsPath: "all-backup", CompletionTime: &completionTime,
			ShardedFamilies: []neo4jv1beta1.ShardedFamilyArtifacts{
				{Family: "orders", ShardArtifacts: []neo4jv1beta1.ShardArtifact{
					{ShardName: "orders-g000", Filename: "orders-g000-2026-06-08T01-21-00.backup"},
				}},
				{Family: "products", ShardArtifacts: []neo4jv1beta1.ShardArtifact{
					{ShardName: "products-g000", Filename: "products-g000-2026-06-08T01-20-00.backup"},
					{ShardName: "products-p000", Filename: "products-p000-2026-06-08T01-20-30.backup"},
				}},
			},
		}}},
	}

	t.Run("cloud all-DB restore in place → per-shard cloud URIs", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(allDBBackup).Build()
		r := &Neo4jShardedDatabaseReconciler{Client: c}
		sd := &neo4jv1beta1.Neo4jShardedDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "products", Namespace: "default"},
			Spec:       neo4jv1beta1.Neo4jShardedDatabaseSpec{Name: "products", SeedBackupRef: "all-backup"},
		}
		resolved, err := r.resolveShardedSeed(context.Background(), sd)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if resolved.URI != "" {
			t.Errorf("expected per-shard URIs (no single directory URI), got URI=%q", resolved.URI)
		}
		if !resolved.ProxyAvailable {
			t.Errorf("cloud seed needs no proxy; ProxyAvailable should be true")
		}
		want := map[string]string{
			"products-g000": "s3://b/backups/all-backup/products-g000-2026-06-08T01-20-00.backup",
			"products-p000": "s3://b/backups/all-backup/products-p000-2026-06-08T01-20-30.backup",
		}
		if len(resolved.PerShardURIs) != len(want) {
			t.Fatalf("got %d per-shard URIs %+v, want %d", len(resolved.PerShardURIs), resolved.PerShardURIs, len(want))
		}
		for k, v := range want {
			if resolved.PerShardURIs[k] != v {
				t.Errorf("shard %q: got %q, want %q", k, resolved.PerShardURIs[k], v)
			}
		}
	})

	t.Run("cloud all-DB renamed target via seedSourceDatabase → shards remapped", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(allDBBackup).Build()
		r := &Neo4jShardedDatabaseReconciler{Client: c}
		sd := &neo4jv1beta1.Neo4jShardedDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "products-restored", Namespace: "default"},
			Spec: neo4jv1beta1.Neo4jShardedDatabaseSpec{
				Name: "products-restored", SeedBackupRef: "all-backup", SeedSourceDatabase: "products",
			},
		}
		resolved, err := r.resolveShardedSeed(context.Background(), sd)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		// Target shard keys carry the NEW name; URIs point at the SOURCE files.
		if got := resolved.PerShardURIs["products-restored-g000"]; got != "s3://b/backups/all-backup/products-g000-2026-06-08T01-20-00.backup" {
			t.Errorf("renamed graph shard: got %q", got)
		}
		if _, ok := resolved.PerShardURIs["products-restored-p000"]; !ok {
			t.Errorf("expected remapped property shard products-restored-p000, got %+v", resolved.PerShardURIs)
		}
	})

	t.Run("family not present → actionable error", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(allDBBackup).Build()
		r := &Neo4jShardedDatabaseReconciler{Client: c}
		sd := &neo4jv1beta1.Neo4jShardedDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory", Namespace: "default"},
			Spec:       neo4jv1beta1.Neo4jShardedDatabaseSpec{Name: "inventory", SeedBackupRef: "all-backup"},
		}
		_, err := r.resolveShardedSeed(context.Background(), sd)
		if err == nil || !strings.Contains(err.Error(), "did not capture sharded family") {
			t.Fatalf("err=%v, want 'did not capture sharded family'", err)
		}
	})
}
