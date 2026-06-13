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

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
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

// #252: projectClusterSeedConfig applies the seed-credentials Secret
// (extraEnvFrom) AND a custom S3 endpoint (spec.env) in a SINGLE cluster
// Update — gated by the auto-inherit annotation — so the cluster rolls once.
func TestProjectClusterSeedConfig(t *testing.T) {
	cloud := &neo4jv1beta1.CloudBlock{EndpointURL: "http://minio.minio.svc:9000", ForcePathStyle: true, CredentialsSecretRef: "minio-creds"}
	baseCluster := func() *neo4jv1beta1.Neo4jEnterpriseCluster {
		return &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "ec", Namespace: "default"},
		}
	}

	t.Run("missing creds+endpoint, no annotation -> sentinel + both listed", func(t *testing.T) {
		cluster := baseCluster()
		r := newResolvedSourceReconciler(t, cluster)
		projected, missing, err := r.projectClusterSeedConfig(context.Background(), cluster, "minio-creds", cloud)
		require.False(t, projected)
		require.ErrorIs(t, err, errSeedConfigNotAutoInherited)
		require.Len(t, missing, 2)
		joined := strings.Join(missing, " ")
		assert.Contains(t, joined, "minio-creds")
		assert.Contains(t, joined, "AWS_ENDPOINT_URL_S3")
	})

	t.Run("missing both + annotation -> ONE update sets extraEnvFrom + spec.env", func(t *testing.T) {
		cluster := baseCluster()
		cluster.Annotations = map[string]string{AutoInheritSeedCredsAnnotation: "true"}
		r := newResolvedSourceReconciler(t, cluster)
		projected, _, err := r.projectClusterSeedConfig(context.Background(), cluster, "minio-creds", cloud)
		require.True(t, projected)
		require.NoError(t, err)
		got := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		require.NoError(t, r.Get(context.Background(), client.ObjectKeyFromObject(cluster), got))
		// creds projected via extraEnvFrom
		assert.True(t, clusterHasSecretEnvFrom(got, "minio-creds"), "creds Secret must be in extraEnvFrom")
		// endpoint + forcePathStyle projected via spec.env
		var endpoint, jto string
		for _, e := range got.Spec.Env {
			switch e.Name {
			case "AWS_ENDPOINT_URL_S3":
				endpoint = e.Value
			case "JAVA_TOOL_OPTIONS":
				jto = e.Value
			}
		}
		assert.Equal(t, "http://minio.minio.svc:9000", endpoint)
		assert.Contains(t, jto, "aws.s3.forcePathStyle=true")
		// Single Update: STS generation effect is one bump — asserted by the
		// fact that both landed in the same Get/Update cycle (this fake-client
		// call is atomic). Re-running is now a no-op.
		again, _, err := r.projectClusterSeedConfig(context.Background(), got, "minio-creds", cloud)
		require.NoError(t, err)
		assert.False(t, again, "already projected -> nothing to do (no second roll)")
	})

	t.Run("endpoint reachable via projected Secret + creds present -> nothing to project", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "minio-creds", Namespace: "default"},
			Data:       map[string][]byte{"AWS_ENDPOINT_URL_S3": []byte(cloud.EndpointURL)},
		}
		cluster := baseCluster()
		cluster.Spec.ExtraEnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: "minio-creds"},
		}}}
		r := newResolvedSourceReconciler(t, cluster, secret)
		projected, _, err := r.projectClusterSeedConfig(context.Background(), cluster, "minio-creds", cloud)
		require.False(t, projected, "creds present + endpoint in the projected Secret -> nothing to do")
		require.NoError(t, err)
	})
}
