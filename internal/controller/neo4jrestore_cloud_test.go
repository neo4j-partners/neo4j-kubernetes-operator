package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

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
