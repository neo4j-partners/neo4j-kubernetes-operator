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

package validation

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func TestBackupValidator_Validate(t *testing.T) {
	validator := NewBackupValidator()

	tests := []struct {
		name        string
		backup      *neo4jv1beta1.Neo4jBackup
		expectError bool
		errorCount  int
		// expectedErrorContains lists message fragments. For each entry,
		// at least one returned validation error must contain that
		// substring (independent matches — one error can satisfy multiple
		// entries, but each entry must hit at least one error). Counting
		// errors alone doesn't tell you WHICH validator fired; pinning
		// fragments catches regressions where two different validators
		// happen to fire to produce the same total count.
		//
		// Multi-entry usage is for cases like "invalid target kind"
		// where a single CR is designed to trip TWO validators
		// simultaneously — each substring asserts a distinct expected
		// failure mode, so a regression that drops one of them surfaces
		// instead of being absorbed into the count.
		expectedErrorContains []string
	}{
		{
			name: "valid cluster backup to S3",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster",
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type:   "s3",
						Bucket: "backup-bucket",
						Path:   "neo4j-backups",
						Cloud: &neo4jv1beta1.CloudBlock{
							Provider: "aws",
						},
					},
					Schedule: "0 2 * * *", // Daily at 2 AM
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "valid database backup to GCS",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind:       "Database",
						Name:       "test-database",
						ClusterRef: "test-cluster",
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type:   "gcs",
						Bucket: "backup-bucket",
						Path:   "neo4j-backups",
						Cloud: &neo4jv1beta1.CloudBlock{
							Provider: "gcp",
						},
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "invalid target kind",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind: "InvalidKind",
						Name: "test-target",
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type:   "s3",
						Bucket: "backup-bucket",
					},
				},
			},
			expectError: true,
			errorCount:  2,
			// Both validators MUST fire — invalid `target.kind` AND the
			// S3 storage requiring a cloud block. The count alone could
			// be satisfied by any two errors; pinning one fragment per
			// expected validator catches a regression that swaps either
			// firing site for an unrelated one and still produces 2.
			expectedErrorContains: []string{
				"target.kind", // field.NotSupported on Spec.Target.Kind
				"cloud",       // S3 storage requires cloud provider
			},
		},
		{
			name: "missing target name",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind: "Cluster",
						// Missing name
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type: "pvc",
						PVC: &neo4jv1beta1.PVCSpec{
							Name: "backup-pvc",
						},
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "invalid storage type",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster",
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type: "invalid-type",
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "PVC storage with empty pvc.name is rejected",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-backup"},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{Kind: "Cluster", Name: "test-cluster"},
					Storage: neo4jv1beta1.StorageLocation{
						Type: "pvc",
						PVC:  &neo4jv1beta1.PVCSpec{}, // PVCSpec set but Name is empty
					},
				},
			},
			// Without a PVC name, /backup in the Pod is an EmptyDir;
			// the backup "succeeds" but artifacts are discarded when
			// the Job's TTL elapses (matches the validator's error text).
			expectError:           true,
			errorCount:            1,
			expectedErrorContains: []string{"spec.storage.pvc.name"},
		},
		{
			name: "PVC storage with whitespace-only pvc.name is rejected",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-backup"},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{Kind: "Cluster", Name: "test-cluster"},
					Storage: neo4jv1beta1.StorageLocation{
						Type: "pvc",
						// "   " would slip past a naive == "" check; the
						// validator trims first so K8s never sees a
						// malformed PVC name and the user gets a clear
						// rejection at apply time, not a Pod
						// MountVolume.SetUp failure later.
						PVC: &neo4jv1beta1.PVCSpec{Name: "   "},
					},
				},
			},
			expectError:           true,
			errorCount:            1,
			expectedErrorContains: []string{"spec.storage.pvc.name"},
		},
		{
			name: "PVC storage with nil PVC is rejected",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-backup"},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target:  neo4jv1beta1.BackupTarget{Kind: "Cluster", Name: "test-cluster"},
					Storage: neo4jv1beta1.StorageLocation{Type: "pvc"}, // no PVC block at all
				},
			},
			expectError:           true,
			errorCount:            1,
			expectedErrorContains: []string{"spec.storage.pvc"},
		},
		{
			name: "S3 without cloud provider",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster",
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type:   "s3",
						Bucket: "backup-bucket",
						// Missing Cloud configuration
					},
				},
			},
			expectError:           true,
			errorCount:            1,
			expectedErrorContains: []string{"cloud"},
		},
		{
			name: "invalid cron schedule",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster",
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type:   "s3",
						Bucket: "backup-bucket",
						Cloud: &neo4jv1beta1.CloudBlock{
							Provider: "aws",
						},
					},
					Schedule: "invalid-cron",
				},
			},
			expectError: true,
			errorCount:  1,
			// Pin the message fragment to prove validateSchedule is the
			// validator firing — without this, a regression where some
			// other check happens to produce one error would silently
			// pass on the wrong assertion.
			expectedErrorContains: []string{"cron schedule"},
		},
		{
			name: "valid backup with encryption",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster",
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type:   "s3",
						Bucket: "backup-bucket",
						Cloud: &neo4jv1beta1.CloudBlock{
							Provider: "aws",
						},
					},
					Options: &neo4jv1beta1.BackupOptions{
						Compress: true,
						Verify:   true,
						Encryption: &neo4jv1beta1.EncryptionSpec{
							Enabled:   true,
							KeySecret: "encryption-key",
							Algorithm: "AES256",
						},
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "valid PVC backup",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster",
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type: "pvc",
						PVC: &neo4jv1beta1.PVCSpec{
							Name: "backup-pvc",
						},
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "valid sharded database backup",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-backup", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind:       "ShardedDatabase",
						Name:       "products",
						ClusterRef: "my-cluster",
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type: "pvc",
						PVC:  &neo4jv1beta1.PVCSpec{Name: "backup-pvc"},
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
		{
			name: "ShardedDatabase missing clusterRef",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-backup", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind: "ShardedDatabase",
						Name: "products",
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type: "pvc",
						PVC:  &neo4jv1beta1.PVCSpec{Name: "backup-pvc"},
					},
				},
			},
			expectError:           true,
			errorCount:            1,
			expectedErrorContains: []string{"clusterRef is required when target.kind=ShardedDatabase"},
		},
		{
			name: "ShardedDatabase with cross-namespace target.namespace",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-backup", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind:       "ShardedDatabase",
						Name:       "products",
						ClusterRef: "my-cluster",
						Namespace:  "other-ns",
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type: "pvc",
						PVC:  &neo4jv1beta1.PVCSpec{Name: "backup-pvc"},
					},
				},
			},
			expectError:           true,
			errorCount:            1,
			expectedErrorContains: []string{"cross-namespace target references are not supported"},
		},
		{
			name: "ShardedDatabase with matching target.namespace allowed",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-backup", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind:       "ShardedDatabase",
						Name:       "products",
						ClusterRef: "my-cluster",
						Namespace:  "default", // same as backup ns
					},
					Storage: neo4jv1beta1.StorageLocation{
						Type: "pvc",
						PVC:  &neo4jv1beta1.PVCSpec{Name: "backup-pvc"},
					},
				},
			},
			expectError: false,
			errorCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := validator.Validate(tt.backup)

			if tt.expectError {
				if len(errors) == 0 {
					t.Errorf("expected validation errors but got none")
				}
				if len(errors) != tt.errorCount {
					t.Errorf("expected %d errors but got %d: %v", tt.errorCount, len(errors), errors)
				}
				// Each expected fragment must appear in at least one error.
				// One error can satisfy multiple fragments, but every
				// fragment must hit — so dropping any of the expected
				// validators fails the test.
				for _, want := range tt.expectedErrorContains {
					found := false
					for _, err := range errors {
						if strings.Contains(err.Error(), want) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected an error containing %q but got: %v", want, errors)
					}
				}
			} else if len(errors) > 0 {
				t.Errorf("expected no validation errors but got %d: %v", len(errors), errors)
			}
		})
	}
}

func TestBackupValidator_validateStorageType(t *testing.T) {
	validator := NewBackupValidator()

	tests := []struct {
		name        string
		storageType string
		expectError bool
	}{
		{
			name:        "valid S3 type",
			storageType: "s3",
			expectError: false,
		},
		{
			name:        "valid GCS type",
			storageType: "gcs",
			expectError: false,
		},
		{
			name:        "valid Azure type",
			storageType: "azure",
			expectError: false,
		},
		{
			name:        "valid PVC type",
			storageType: "pvc",
			expectError: false,
		},
		{
			name:        "invalid type",
			storageType: "invalid-type",
			expectError: true,
		},
		{
			name:        "empty type",
			storageType: "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateStorageType(tt.storageType)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
			}
		})
	}
}
