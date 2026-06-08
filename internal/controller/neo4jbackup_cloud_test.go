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

package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// envVarMap converts a slice of EnvVar to a name→value map for easy assertions.
func envVarMap(envs []corev1.EnvVar) map[string]string {
	m := make(map[string]string, len(envs))
	for _, e := range envs {
		if e.Value != "" {
			m[e.Name] = e.Value
		} else {
			m[e.Name] = "<from-secret>"
		}
	}
	return m
}

// envVarSecretKey returns the secret key referenced by a named env var, or "".
func envVarSecretKey(envs []corev1.EnvVar, name string) string {
	for _, e := range envs {
		if e.Name == name && e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			return e.ValueFrom.SecretKeyRef.Key
		}
	}
	return ""
}

func newReconcilerForCloudTest() *Neo4jBackupReconciler {
	return &Neo4jBackupReconciler{}
}

func backupWithCloud(cloud *neo4jv1beta1.CloudBlock) *neo4jv1beta1.Neo4jBackup {
	b := &neo4jv1beta1.Neo4jBackup{}
	b.Spec.Storage.Cloud = cloud
	return b
}

// ─── AWS / S3 ───────────────────────────────────────────────────────────────

func TestBuildCloudEnvVars_AWS_WithCredentials(t *testing.T) {
	r := newReconcilerForCloudTest()
	backup := backupWithCloud(&neo4jv1beta1.CloudBlock{
		Provider:             "aws",
		CredentialsSecretRef: "my-secret",
	})

	envs := r.buildCloudEnvVars(backup)

	require.NotNil(t, envs)
	m := envVarMap(envs)
	assert.Equal(t, "<from-secret>", m["AWS_ACCESS_KEY_ID"])
	assert.Equal(t, "<from-secret>", m["AWS_SECRET_ACCESS_KEY"])
	assert.Equal(t, "<from-secret>", m["AWS_REGION"])

	assert.Equal(t, "AWS_ACCESS_KEY_ID", envVarSecretKey(envs, "AWS_ACCESS_KEY_ID"))
	assert.Equal(t, "AWS_SECRET_ACCESS_KEY", envVarSecretKey(envs, "AWS_SECRET_ACCESS_KEY"))
	assert.Equal(t, "AWS_REGION", envVarSecretKey(envs, "AWS_REGION"))

	// No MinIO-specific vars when endpointURL is not set
	assert.NotContains(t, m, "AWS_ENDPOINT_URL_S3")
	assert.NotContains(t, m, "JAVA_TOOL_OPTIONS")
}

func TestBuildCloudEnvVars_AWS_WithoutCredentials(t *testing.T) {
	r := newReconcilerForCloudTest()
	// No credentialsSecretRef → workload identity; expect nil (no env injection)
	backup := backupWithCloud(&neo4jv1beta1.CloudBlock{
		Provider: "aws",
	})

	envs := r.buildCloudEnvVars(backup)
	assert.Nil(t, envs, "workload identity path should return nil env vars")
}

// ─── MinIO / S3-compatible ──────────────────────────────────────────────────

func TestBuildCloudEnvVars_MinIO_EndpointOnly(t *testing.T) {
	r := newReconcilerForCloudTest()
	backup := backupWithCloud(&neo4jv1beta1.CloudBlock{
		Provider:             "aws",
		CredentialsSecretRef: "minio-secret",
		EndpointURL:          "http://minio.minio.svc:9000",
	})

	envs := r.buildCloudEnvVars(backup)

	require.NotNil(t, envs)
	m := envVarMap(envs)

	// Standard AWS credentials must still be present
	assert.Contains(t, m, "AWS_ACCESS_KEY_ID")
	assert.Contains(t, m, "AWS_SECRET_ACCESS_KEY")
	assert.Contains(t, m, "AWS_REGION")

	// Custom endpoint must be injected
	assert.Equal(t, "http://minio.minio.svc:9000", m["AWS_ENDPOINT_URL_S3"],
		"AWS_ENDPOINT_URL_S3 should match the configured endpointURL")

	// forcePathStyle not requested → no JAVA_TOOL_OPTIONS
	assert.NotContains(t, m, "JAVA_TOOL_OPTIONS")
}

func TestBuildCloudEnvVars_MinIO_EndpointWithForcePathStyle(t *testing.T) {
	r := newReconcilerForCloudTest()
	backup := backupWithCloud(&neo4jv1beta1.CloudBlock{
		Provider:             "aws",
		CredentialsSecretRef: "minio-secret",
		EndpointURL:          "http://minio.minio.svc:9000",
		ForcePathStyle:       true,
	})

	envs := r.buildCloudEnvVars(backup)

	require.NotNil(t, envs)
	m := envVarMap(envs)

	assert.Equal(t, "http://minio.minio.svc:9000", m["AWS_ENDPOINT_URL_S3"])
	assert.Equal(t, "-Daws.s3.forcePathStyle=true", m["JAVA_TOOL_OPTIONS"],
		"JAVA_TOOL_OPTIONS should carry the path-style JVM property")
}

func TestBuildCloudEnvVars_MinIO_ExternalHTTPS(t *testing.T) {
	r := newReconcilerForCloudTest()
	backup := backupWithCloud(&neo4jv1beta1.CloudBlock{
		Provider:             "aws",
		CredentialsSecretRef: "minio-secret",
		EndpointURL:          "https://minio.example.com",
		ForcePathStyle:       true,
	})

	envs := r.buildCloudEnvVars(backup)

	require.NotNil(t, envs)
	m := envVarMap(envs)
	assert.Equal(t, "https://minio.example.com", m["AWS_ENDPOINT_URL_S3"])
	assert.Equal(t, "-Daws.s3.forcePathStyle=true", m["JAVA_TOOL_OPTIONS"])
}

func TestBuildCloudEnvVars_MinIO_ForcePathStyleWithoutEndpoint(t *testing.T) {
	// forcePathStyle=true without an endpointURL is unusual but should not panic.
	// It just sets JAVA_TOOL_OPTIONS; the AWS SDK will use the standard endpoint.
	r := newReconcilerForCloudTest()
	backup := backupWithCloud(&neo4jv1beta1.CloudBlock{
		Provider:             "aws",
		CredentialsSecretRef: "my-secret",
		ForcePathStyle:       true,
	})

	envs := r.buildCloudEnvVars(backup)
	require.NotNil(t, envs)
	m := envVarMap(envs)
	assert.NotContains(t, m, "AWS_ENDPOINT_URL_S3")
	assert.Equal(t, "-Daws.s3.forcePathStyle=true", m["JAVA_TOOL_OPTIONS"])
}

// ─── GCP ────────────────────────────────────────────────────────────────────

func TestBuildCloudEnvVars_GCP_WithCredentials(t *testing.T) {
	r := newReconcilerForCloudTest()
	backup := backupWithCloud(&neo4jv1beta1.CloudBlock{
		Provider:             "gcp",
		CredentialsSecretRef: "gcp-secret",
	})

	envs := r.buildCloudEnvVars(backup)

	require.NotNil(t, envs)
	m := envVarMap(envs)
	assert.Equal(t, "/var/secrets/gcp/credentials.json", m["GOOGLE_APPLICATION_CREDENTIALS"])

	// GCP path must not inject any AWS vars
	assert.NotContains(t, m, "AWS_ACCESS_KEY_ID")
	assert.NotContains(t, m, "AWS_ENDPOINT_URL_S3")
	assert.NotContains(t, m, "JAVA_TOOL_OPTIONS")
}

// ─── Azure ──────────────────────────────────────────────────────────────────

func TestBuildCloudEnvVars_Azure_WithCredentials(t *testing.T) {
	r := newReconcilerForCloudTest()
	backup := backupWithCloud(&neo4jv1beta1.CloudBlock{
		Provider:             "azure",
		CredentialsSecretRef: "azure-secret",
	})

	envs := r.buildCloudEnvVars(backup)

	require.NotNil(t, envs)
	m := envVarMap(envs)
	assert.Equal(t, "<from-secret>", m["AZURE_STORAGE_ACCOUNT"])
	assert.Equal(t, "<from-secret>", m["AZURE_STORAGE_KEY"])

	assert.NotContains(t, m, "AWS_ENDPOINT_URL_S3")
	assert.NotContains(t, m, "JAVA_TOOL_OPTIONS")
}

// ─── No cloud config ────────────────────────────────────────────────────────

func TestBuildCloudEnvVars_NoCloudConfig(t *testing.T) {
	r := newReconcilerForCloudTest()
	backup := &neo4jv1beta1.Neo4jBackup{} // no cloud block at all

	envs := r.buildCloudEnvVars(backup)
	assert.Nil(t, envs)
}

func TestBuildCloudEnvVars_NilCredentialsSecretRef(t *testing.T) {
	r := newReconcilerForCloudTest()
	// Cloud block present but no credentialsSecretRef → workload identity
	backup := backupWithCloud(&neo4jv1beta1.CloudBlock{
		Provider: "aws",
	})

	envs := r.buildCloudEnvVars(backup)
	assert.Nil(t, envs)
}

// ─── Per-run subfolder (issue #129) ─────────────────────────────────────────

// TestBuildToPath_NoTrailingRunSubfolder verifies that buildToPath returns
// the storage BASE — no trailing slash, no per-run component. The per-run
// "${BACKUP_RUN_ID}" subfolder is appended by buildBackupCommand at
// command-construction time and expanded by the shell at runtime via the
// downward-API env var.
// TestBuildToPath_SharedDirectoryPerCR pins the shared-directory layout
// (CLAUDE.md rule 40): all runs of one Neo4jBackup CR write to the SAME
// `<base>/<cr-name>` directory so neo4j-admin can chain `--type=DIFF`
// backups off the prior FULL. The CR name acts as the per-CR isolation
// segment — multiple CRs pointed at the same storage location stay
// separated. NO per-run subfolder is appended at command-build time; the
// shell command in buildBackupCommand only adds a trailing slash.
func TestBuildToPath_SharedDirectoryPerCR(t *testing.T) {
	r := newReconcilerForCloudTest()

	tests := []struct {
		name     string
		storage  neo4jv1beta1.StorageLocation
		backupCR string
		expected string
	}{
		{
			name:     "s3 with explicit path",
			storage:  neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "my-bucket", Path: "neo4j/prod"},
			backupCR: "daily-prod-backup",
			expected: "s3://my-bucket/neo4j/prod/daily-prod-backup",
		},
		{
			name:     "gcs with explicit path",
			storage:  neo4jv1beta1.StorageLocation{Type: "gcs", Bucket: "my-bucket", Path: "neo4j/prod"},
			backupCR: "daily-prod-backup",
			expected: "gs://my-bucket/neo4j/prod/daily-prod-backup",
		},
		{
			name:     "azure with explicit path",
			storage:  neo4jv1beta1.StorageLocation{Type: "azure", Bucket: "my-container", Path: "neo4j/prod"},
			backupCR: "weekly-prod-backup",
			expected: "azb://my-container/neo4j/prod/weekly-prod-backup",
		},
		{
			name:     "s3 empty path falls back to 'backups'",
			storage:  neo4jv1beta1.StorageLocation{Type: "s3", Bucket: "my-bucket"},
			backupCR: "ad-hoc-backup",
			expected: "s3://my-bucket/backups/ad-hoc-backup",
		},
		{
			name:     "pvc returns per-CR directory under /backup",
			storage:  neo4jv1beta1.StorageLocation{Type: "pvc"},
			backupCR: "ad-hoc-backup",
			expected: "/backup/ad-hoc-backup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backup := &neo4jv1beta1.Neo4jBackup{
				ObjectMeta: metav1.ObjectMeta{Name: tt.backupCR},
				Spec:       neo4jv1beta1.Neo4jBackupSpec{Storage: tt.storage},
			}
			got := r.buildToPath(backup)
			assert.Equal(t, tt.expected, got,
				"buildToPath must embed the CR name as the per-CR shared segment (rule 40)")
			// Critical: no trailing slash. The shell command in
			// buildBackupCommand appends "/" so neo4j-admin sees directory
			// semantics (required for cloud targets per the docs).
			assert.NotEqual(t, "/", string(got[len(got)-1]),
				"buildToPath must not return a trailing slash; the separator is added by the caller")
		})
	}
}

// TestBackupRunIDEnvVar verifies the downward-API env var that exposes the
// backing Job's name to the backup Pod. This is the source of truth for the
// per-run subfolder in --to-path (see buildBackupCommand). The operator
// records the same Job name in BackupRun.BackupsPath, so the Pod and the
// status history agree on the artifact directory without any out-of-band
// coordination (no log parsing, no separate Job patch, no race window).
func TestBackupRunIDEnvVar(t *testing.T) {
	env := backupRunIDEnvVar()
	assert.Equal(t, "BACKUP_RUN_ID", env.Name)
	require.NotNil(t, env.ValueFrom)
	require.NotNil(t, env.ValueFrom.FieldRef)
	// `batch.kubernetes.io/job-name` is the canonical Kubernetes 1.27+ label
	// the Job controller stamps onto every Pod it spawns. Don't fall back to
	// the legacy bare `job-name` key — the project requires K8s 1.30+ and
	// the canonical key is what's documented.
	assert.Equal(t,
		"metadata.labels['batch.kubernetes.io/job-name']",
		env.ValueFrom.FieldRef.FieldPath)
}
