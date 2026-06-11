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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// These tests pin the operator's half of the pod-identity (IRSA / GKE Workload
// Identity / Azure Workload Identity) contract for cloud backup/restore, which
// is fully verifiable WITHOUT a real cloud:
//
//   1. When credentialsSecretRef is empty, the operator injects NO static
//      cloud creds (it defers to the SDK default credential chain).
//   2. The operator creates the backup/restore ServiceAccount carrying the
//      caller's cloud.identity.autoCreate.annotations (the IRSA role-arn / GKE
//      iam.gke.io / azure.workload.identity annotations the cloud webhook then
//      acts on), preserving any foreign annotations and staying idempotent.
//
// The actual OIDC token exchange (STS AssumeRoleWithWebIdentity etc.) is the
// only piece left for real-cloud verification.

func wiTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, neo4jv1beta1.AddToScheme(s))
	return s
}

func cloudWithIdentity(provider string, annotations map[string]string) *neo4jv1beta1.CloudBlock {
	return &neo4jv1beta1.CloudBlock{
		Provider: provider,
		Identity: &neo4jv1beta1.CloudIdentity{
			AutoCreate: &neo4jv1beta1.AutoCreateSpec{Annotations: annotations},
		},
	}
}

func TestEnsureBackupServiceAccount_WorkloadIdentity(t *testing.T) {
	scheme := wiTestScheme(t)
	fc := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &Neo4jBackupReconciler{Client: fc}

	roleAnnotations := map[string]string{"eks.amazonaws.com/role-arn": "arn:aws:iam::123456789012:role/neo4j-backup"}
	backup := backupWithCloud(cloudWithIdentity("aws", roleAnnotations))
	backup.Namespace = "ns"

	// No static creds are injected on the workload-identity path (layer 1).
	assert.Nil(t, r.buildCloudEnvVars(backup), "workload identity must not inject static cloud creds")

	// The operator creates the backup SA carrying the IRSA annotation (layer 2).
	require.NoError(t, r.ensureBackupServiceAccount(context.Background(), backup))

	sa := &corev1.ServiceAccount{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: backupServiceAccountName, Namespace: "ns"}, sa))
	assert.Equal(t, "arn:aws:iam::123456789012:role/neo4j-backup", sa.Annotations["eks.amazonaws.com/role-arn"])

	// Idempotent: a second reconcile must not error or drift the annotation.
	require.NoError(t, r.ensureBackupServiceAccount(context.Background(), backup))
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: backupServiceAccountName, Namespace: "ns"}, sa))
	assert.Equal(t, "arn:aws:iam::123456789012:role/neo4j-backup", sa.Annotations["eks.amazonaws.com/role-arn"])
}

func TestEnsureBackupServiceAccount_PreservesForeignAnnotations(t *testing.T) {
	scheme := wiTestScheme(t)
	// A SA already exists (e.g. created by the cloud controller or the user)
	// with a foreign annotation the operator must not clobber.
	existing := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:        backupServiceAccountName,
			Namespace:   "ns",
			Annotations: map[string]string{"example.com/owner": "platform-team"},
		},
	}
	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	r := &Neo4jBackupReconciler{Client: fc}

	backup := backupWithCloud(cloudWithIdentity("gcp", map[string]string{"iam.gke.io/gcp-service-account": "neo4j@proj.iam.gserviceaccount.com"}))
	backup.Namespace = "ns"
	require.NoError(t, r.ensureBackupServiceAccount(context.Background(), backup))

	sa := &corev1.ServiceAccount{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: backupServiceAccountName, Namespace: "ns"}, sa))
	assert.Equal(t, "platform-team", sa.Annotations["example.com/owner"], "foreign annotation must be preserved")
	assert.Equal(t, "neo4j@proj.iam.gserviceaccount.com", sa.Annotations["iam.gke.io/gcp-service-account"], "WI annotation must be applied")
}

func TestEnsureBackupServiceAccount_StaticCredsNoAnnotations(t *testing.T) {
	scheme := wiTestScheme(t)
	fc := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &Neo4jBackupReconciler{Client: fc}

	// Static-credential path: a credsSecret, no identity block. The SA is still
	// created (the Job runs under it) but carries no workload-identity
	// annotations, and static creds ARE injected.
	backup := backupWithCloud(&neo4jv1beta1.CloudBlock{Provider: "aws", CredentialsSecretRef: "s3-creds"})
	backup.Namespace = "ns"

	assert.NotNil(t, r.buildCloudEnvVars(backup), "static-cred path must inject cloud creds")
	require.NoError(t, r.ensureBackupServiceAccount(context.Background(), backup))

	sa := &corev1.ServiceAccount{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: backupServiceAccountName, Namespace: "ns"}, sa))
	assert.Empty(t, sa.Annotations, "static-cred SA must not carry workload-identity annotations")
}

func TestEnsureRestoreServiceAccount_WorkloadIdentity(t *testing.T) {
	scheme := wiTestScheme(t)
	fc := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &Neo4jRestoreReconciler{Client: fc}

	azureAnnotations := map[string]string{"azure.workload.identity/client-id": "00000000-0000-0000-0000-000000000000"}
	restore := &neo4jv1beta1.Neo4jRestore{ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "ns"}}
	restore.Spec.Source.Storage = &neo4jv1beta1.StorageLocation{
		Type:  "azure",
		Cloud: cloudWithIdentity("azure", azureAnnotations),
	}

	require.NoError(t, r.ensureRestoreServiceAccount(context.Background(), restore))

	sa := &corev1.ServiceAccount{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: restoreServiceAccountName, Namespace: "ns"}, sa))
	assert.Equal(t, "00000000-0000-0000-0000-000000000000", sa.Annotations["azure.workload.identity/client-id"])

	// Idempotent.
	require.NoError(t, r.ensureRestoreServiceAccount(context.Background(), restore))
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: restoreServiceAccountName, Namespace: "ns"}, sa))
	assert.Equal(t, "00000000-0000-0000-0000-000000000000", sa.Annotations["azure.workload.identity/client-id"])
}
