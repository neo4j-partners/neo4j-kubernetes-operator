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
	stderrors "errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
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
				Target:  neo4jv1beta1.BackupTarget{Kind: neo4jv1beta1.BackupTargetKindShardedDatabase, Name: "products", ClusterRef: "ec"},
				Storage: storage,
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
			name:          "PVC backup is rejected (no cloud URI)",
			seedBackupRef: "products-backup",
			seedObjects: []runtime.Object{
				mkBackup("products-backup",
					neo4jv1beta1.StorageLocation{Type: "pvc"},
					[]neo4jv1beta1.BackupRun{
						{RunID: "uid-1", Status: "Succeeded", BackupsPath: "products-backup", CompletionTime: &completionTime},
					}),
			},
			wantErrSubstr: "requires cloud-backed",
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
			uri, err := r.resolveShardedSeed(context.Background(), shardedDB)
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
