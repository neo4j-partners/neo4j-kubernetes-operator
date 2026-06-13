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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// Pins the #227 shared-SA conflict detection: new keys and identical values
// are NOT conflicts; only overwriting a different existing value is.
func TestServiceAccountAnnotationConflicts(t *testing.T) {
	existing := map[string]string{
		"eks.amazonaws.com/role-arn": "arn:aws:iam::1:role/backup-a",
		"unrelated.io/by-user":       "keep",
	}
	desired := map[string]string{
		"eks.amazonaws.com/role-arn":     "arn:aws:iam::1:role/backup-b", // conflict
		"unrelated.io/by-user":           "keep",                         // identical — no conflict
		"iam.gke.io/gcp-service-account": "new@p.iam",                    // new key — no conflict
	}
	conflicts := serviceAccountAnnotationConflicts(existing, desired)
	require.Len(t, conflicts, 1)
	assert.Contains(t, conflicts[0], "eks.amazonaws.com/role-arn")
	assert.Contains(t, conflicts[0], "backup-a")
	assert.Contains(t, conflicts[0], "backup-b")

	assert.Empty(t, serviceAccountAnnotationConflicts(nil, desired))
	assert.Empty(t, serviceAccountAnnotationConflicts(existing, nil))
}

// End-to-end on the restore side: a second CR declaring a DIFFERENT identity
// on the shared SA must win the write (documented last-writer-wins) AND emit
// the ServiceAccountAnnotationConflict warning so the fight is visible.
func TestEnsureRestoreServiceAccount_ConflictEmitsWarning(t *testing.T) {
	existingSA := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreServiceAccountName,
			Namespace: "default",
			Annotations: map[string]string{
				"eks.amazonaws.com/role-arn": "arn:aws:iam::1:role/other-restore",
			},
		},
	}
	restore := restoreWithBackupRef("r1", "default", "nightly")
	restore.Spec.Source.Type = "storage"
	restore.Spec.Source.Storage = &neo4jv1beta1.StorageLocation{
		Type: "s3",
		Cloud: &neo4jv1beta1.CloudBlock{
			Provider: "aws",
			Identity: &neo4jv1beta1.CloudIdentity{
				AutoCreate: &neo4jv1beta1.AutoCreateSpec{
					Annotations: map[string]string{
						"eks.amazonaws.com/role-arn": "arn:aws:iam::1:role/this-restore",
					},
				},
			},
		},
	}

	r := newResolvedSourceReconciler(t, restore, existingSA)
	require.NoError(t, r.ensureRestoreServiceAccount(context.Background(), restore))

	rec, ok := r.Recorder.(*record.FakeRecorder)
	require.True(t, ok, "test reconciler must use a FakeRecorder")
	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, EventReasonServiceAccountAnnotationConflict)
		assert.Contains(t, ev, "other-restore")
		assert.Contains(t, ev, "this-restore")
		assert.True(t, strings.HasPrefix(ev, corev1.EventTypeWarning), "must be a Warning event: %s", ev)
	default:
		t.Fatal("expected a ServiceAccountAnnotationConflict warning event")
	}

	// Same identity re-applied: no conflict, no event. (The previous ensure
	// already wrote this-restore's value onto the SA.)
	require.NoError(t, r.ensureRestoreServiceAccount(context.Background(), restore))
	select {
	case ev := <-rec.Events:
		t.Fatalf("no event expected for an identical re-apply, got: %s", ev)
	default:
	}
}

// #252: a cluster Cypher restore from a custom-endpoint S3 store (MinIO)
// auto-projects AWS_ENDPOINT_URL_S3 onto the cluster (gated by the
// auto-inherit annotation), and recognises an endpoint already supplied via
// spec.env or a projected Secret. The projector does NOT wait — the caller
// batches it with the creds projection for a single rolling restart.
func TestEnsureClusterSeedEndpointProjected(t *testing.T) {
	cloud := &neo4jv1beta1.CloudBlock{EndpointURL: "http://minio.minio.svc:9000", ForcePathStyle: true}
	baseCluster := func() *neo4jv1beta1.Neo4jEnterpriseCluster {
		return &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "ec", Namespace: "default"},
		}
	}

	t.Run("absent + no annotation -> Failed, sentinel error", func(t *testing.T) {
		restore := restoreWithBackupRef("r1", "default", "nightly")
		cluster := baseCluster()
		r := newResolvedSourceReconciler(t, restore, cluster)
		projected, err := r.ensureClusterSeedEndpointProjected(context.Background(), restore, cluster, cloud)
		require.False(t, projected)
		require.ErrorIs(t, err, errSeedEndpointNotConfigured)
	})

	t.Run("absent + auto-inherit annotation -> patches spec.env + JVM opt", func(t *testing.T) {
		restore := restoreWithBackupRef("r1", "default", "nightly")
		cluster := baseCluster()
		cluster.Annotations = map[string]string{AutoInheritSeedCredsAnnotation: "true"}
		r := newResolvedSourceReconciler(t, restore, cluster)
		projected, err := r.ensureClusterSeedEndpointProjected(context.Background(), restore, cluster, cloud)
		require.True(t, projected)
		require.NoError(t, err)
		got := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		require.NoError(t, r.Get(context.Background(), client.ObjectKeyFromObject(cluster), got))
		var endpoint, jto string
		for _, e := range got.Spec.Env {
			if e.Name == "AWS_ENDPOINT_URL_S3" {
				endpoint = e.Value
			}
			if e.Name == "JAVA_TOOL_OPTIONS" {
				jto = e.Value
			}
		}
		assert.Equal(t, "http://minio.minio.svc:9000", endpoint, "endpoint must be projected to spec.env")
		assert.Contains(t, jto, "aws.s3.forcePathStyle=true", "forcePathStyle must be projected as a JVM opt")
	})

	t.Run("already in spec.env -> nothing to project", func(t *testing.T) {
		restore := restoreWithBackupRef("r1", "default", "nightly")
		cluster := baseCluster()
		cluster.Spec.Env = []corev1.EnvVar{{Name: "AWS_ENDPOINT_URL_S3", Value: cloud.EndpointURL}}
		r := newResolvedSourceReconciler(t, restore, cluster)
		projected, err := r.ensureClusterSeedEndpointProjected(context.Background(), restore, cluster, cloud)
		require.False(t, projected)
		require.NoError(t, err)
	})

	t.Run("reachable via projected Secret -> nothing to project", func(t *testing.T) {
		restore := restoreWithBackupRef("r1", "default", "nightly")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "minio-creds", Namespace: "default"},
			Data:       map[string][]byte{"AWS_ENDPOINT_URL_S3": []byte(cloud.EndpointURL)},
		}
		cluster := baseCluster()
		cluster.Spec.ExtraEnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: "minio-creds"},
		}}}
		r := newResolvedSourceReconciler(t, restore, cluster, secret)
		projected, err := r.ensureClusterSeedEndpointProjected(context.Background(), restore, cluster, cloud)
		require.False(t, projected, "endpoint reachable via the projected creds Secret -> nothing to project")
		require.NoError(t, err)
	})

	t.Run("unreadable referenced Secret -> assume present", func(t *testing.T) {
		restore := restoreWithBackupRef("r1", "default", "nightly")
		cluster := baseCluster()
		cluster.Spec.ExtraEnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: "missing-secret"},
		}}}
		r := newResolvedSourceReconciler(t, restore, cluster)
		projected, err := r.ensureClusterSeedEndpointProjected(context.Background(), restore, cluster, cloud)
		require.False(t, projected, "incomplete view -> conservative, no spurious projection")
		require.NoError(t, err)
	})
}
