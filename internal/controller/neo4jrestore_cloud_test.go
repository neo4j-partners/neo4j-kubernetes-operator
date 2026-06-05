package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
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
