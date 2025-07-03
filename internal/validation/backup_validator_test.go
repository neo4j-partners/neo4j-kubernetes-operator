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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestBackupValidator_ValidateFixed(t *testing.T) {
	validator := NewBackupValidator()

	tests := []struct {
		name        string
		backup      *neo4jv1alpha1.Neo4jBackup
		expectError bool
		errorCount  int
	}{
		{
			name: "valid cluster backup to S3",
			backup: &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster",
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type:   "s3",
						Bucket: "backup-bucket",
						Path:   "neo4j-backups",
						Cloud: &neo4jv1alpha1.CloudBlock{
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
			backup: &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Database",
						Name: "test-database",
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type:   "gcs",
						Bucket: "backup-bucket",
						Path:   "neo4j-backups",
						Cloud: &neo4jv1alpha1.CloudBlock{
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
			backup: &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "InvalidKind",
						Name: "test-target",
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type:   "s3",
						Bucket: "backup-bucket",
					},
				},
			},
			expectError: true,
			errorCount:  2, // Invalid kind + missing cloud config
		},
		{
			name: "missing target name",
			backup: &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						// Missing name
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type: "pvc",
						PVC: &neo4jv1alpha1.PVCSpec{
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
			backup: &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster",
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type: "invalid-type",
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "S3 without cloud provider",
			backup: &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster",
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type:   "s3",
						Bucket: "backup-bucket",
						// Missing Cloud configuration
					},
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "invalid cron schedule",
			backup: &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster",
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type:   "s3",
						Bucket: "backup-bucket",
						Cloud: &neo4jv1alpha1.CloudBlock{
							Provider: "aws",
						},
					},
					Schedule: "invalid-cron",
				},
			},
			expectError: true,
			errorCount:  1,
		},
		{
			name: "valid backup with encryption",
			backup: &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster",
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type:   "s3",
						Bucket: "backup-bucket",
						Cloud: &neo4jv1alpha1.CloudBlock{
							Provider: "aws",
						},
					},
					Options: &neo4jv1alpha1.BackupOptions{
						Compress: true,
						Verify:   true,
						Encryption: &neo4jv1alpha1.EncryptionSpec{
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
			backup: &neo4jv1alpha1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-backup",
				},
				Spec: neo4jv1alpha1.Neo4jBackupSpec{
					Target: neo4jv1alpha1.BackupTarget{
						Kind: "Cluster",
						Name: "test-cluster",
					},
					Storage: neo4jv1alpha1.StorageLocation{
						Type: "pvc",
						PVC: &neo4jv1alpha1.PVCSpec{
							Name: "backup-pvc",
						},
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
			} else {
				if len(errors) > 0 {
					t.Errorf("expected no validation errors but got %d: %v", len(errors), errors)
				}
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
