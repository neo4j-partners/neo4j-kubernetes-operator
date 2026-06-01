/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// Tests for the cluster-coordination machinery introduced for issue #117:
// the restore-in-progress annotation, the stopCluster=false preflight, and
// the volume builder. These are pure-controller unit tests against a fake
// client — no envtest — because the end-to-end "restore controller and
// cluster controller cooperate on STS Replicas" path runs into the same
// cluster-controller-fights-status problem that surfaced on #118, and the
// coordination logic itself is fully covered by these unit tests.

func newRestoreTestReconciler(t *testing.T, objs ...runtime.Object) *Neo4jRestoreReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, neo4jv1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		Build()
	return &Neo4jRestoreReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(16),
	}
}

func minimalClusterForRestore(name, namespace string) *neo4jv1beta1.Neo4jEnterpriseCluster {
	return &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			Image:    neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
			Topology: neo4jv1beta1.TopologyConfiguration{Servers: 2},
		},
	}
}

func minimalRestore(name, namespace, clusterRef string) *neo4jv1beta1.Neo4jRestore {
	return &neo4jv1beta1.Neo4jRestore{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: neo4jv1beta1.Neo4jRestoreSpec{
			ClusterRef:   clusterRef,
			DatabaseName: "neo4j",
		},
	}
}

func TestSetRestoreInProgressAnnotation(t *testing.T) {
	ctx := context.Background()
	ns := "default"

	t.Run("sets annotation on a clean cluster", func(t *testing.T) {
		cluster := minimalClusterForRestore("c", ns)
		restore := minimalRestore("r1", ns, "c")
		r := newRestoreTestReconciler(t, cluster, restore)

		require.NoError(t, r.setRestoreInProgressAnnotation(ctx, restore, cluster))

		got := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		require.NoError(t, r.Get(ctx, types.NamespacedName{Name: "c", Namespace: ns}, got))
		assert.Equal(t, "r1", got.Annotations[RestoreInProgressAnnotation])
	})

	t.Run("idempotent when annotation already names this restore", func(t *testing.T) {
		cluster := minimalClusterForRestore("c", ns)
		cluster.Annotations = map[string]string{RestoreInProgressAnnotation: "r1"}
		restore := minimalRestore("r1", ns, "c")
		r := newRestoreTestReconciler(t, cluster, restore)

		require.NoError(t, r.setRestoreInProgressAnnotation(ctx, restore, cluster))

		got := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		require.NoError(t, r.Get(ctx, types.NamespacedName{Name: "c", Namespace: ns}, got))
		assert.Equal(t, "r1", got.Annotations[RestoreInProgressAnnotation])
	})

	t.Run("refuses when a different restore is already in progress", func(t *testing.T) {
		cluster := minimalClusterForRestore("c", ns)
		cluster.Annotations = map[string]string{RestoreInProgressAnnotation: "older-restore"}
		restore := minimalRestore("r1", ns, "c")
		r := newRestoreTestReconciler(t, cluster, restore)

		err := r.setRestoreInProgressAnnotation(ctx, restore, cluster)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "older-restore")
		assert.Contains(t, err.Error(), "r1")

		// Annotation must NOT have been overwritten.
		got := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		require.NoError(t, r.Get(ctx, types.NamespacedName{Name: "c", Namespace: ns}, got))
		assert.Equal(t, "older-restore", got.Annotations[RestoreInProgressAnnotation])
	})
}

func TestClearRestoreInProgressAnnotation(t *testing.T) {
	ctx := context.Background()
	ns := "default"

	t.Run("clears annotation set by this restore", func(t *testing.T) {
		cluster := minimalClusterForRestore("c", ns)
		cluster.Annotations = map[string]string{RestoreInProgressAnnotation: "r1"}
		restore := minimalRestore("r1", ns, "c")
		r := newRestoreTestReconciler(t, cluster, restore)

		require.NoError(t, r.clearRestoreInProgressAnnotation(ctx, restore, "c", ns))

		got := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		require.NoError(t, r.Get(ctx, types.NamespacedName{Name: "c", Namespace: ns}, got))
		_, present := got.Annotations[RestoreInProgressAnnotation]
		assert.False(t, present, "annotation should be cleared")
	})

	t.Run("leaves annotation set by a DIFFERENT restore alone", func(t *testing.T) {
		cluster := minimalClusterForRestore("c", ns)
		cluster.Annotations = map[string]string{RestoreInProgressAnnotation: "other-restore"}
		restore := minimalRestore("r1", ns, "c")
		r := newRestoreTestReconciler(t, cluster, restore)

		require.NoError(t, r.clearRestoreInProgressAnnotation(ctx, restore, "c", ns))

		got := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		require.NoError(t, r.Get(ctx, types.NamespacedName{Name: "c", Namespace: ns}, got))
		assert.Equal(t, "other-restore", got.Annotations[RestoreInProgressAnnotation],
			"clear must be a no-op when the annotation belongs to a different restore")
	})

	t.Run("no-op when annotation is absent", func(t *testing.T) {
		cluster := minimalClusterForRestore("c", ns)
		restore := minimalRestore("r1", ns, "c")
		r := newRestoreTestReconciler(t, cluster, restore)

		require.NoError(t, r.clearRestoreInProgressAnnotation(ctx, restore, "c", ns))
	})

	t.Run("no-op when cluster CR is gone", func(t *testing.T) {
		restore := minimalRestore("r1", ns, "gone")
		r := newRestoreTestReconciler(t, restore)

		// Cleanly handles a missing cluster — the restore finalizer must be
		// able to release even after the cluster has already been deleted.
		require.NoError(t, r.clearRestoreInProgressAnnotation(ctx, restore, "gone", ns))
	})
}

func TestRefuseRestoreIfPodsRunning(t *testing.T) {
	ctx := context.Background()
	ns := "default"

	serverPod := func(podName string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: ns,
				Labels: map[string]string{
					"app.kubernetes.io/instance":  "c",
					"app.kubernetes.io/component": "database",
				},
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "neo4j", Image: "neo4j:5.26-enterprise"}}},
		}
	}

	t.Run("no pods → allowed", func(t *testing.T) {
		cluster := minimalClusterForRestore("c", ns)
		restore := minimalRestore("r1", ns, "c")
		r := newRestoreTestReconciler(t, cluster, restore)

		require.NoError(t, r.refuseRestoreIfPodsRunning(ctx, restore, cluster))
	})

	t.Run("server pods exist → refused with actionable error", func(t *testing.T) {
		cluster := minimalClusterForRestore("c", ns)
		restore := minimalRestore("r1", ns, "c")
		r := newRestoreTestReconciler(t, cluster, restore, serverPod("c-server-0"), serverPod("c-server-1"))

		err := r.refuseRestoreIfPodsRunning(ctx, restore, cluster)
		require.Error(t, err)
		msg := err.Error()
		assert.Contains(t, msg, "2 server pod(s)")
		assert.Contains(t, msg, "stopCluster=true")
		assert.Contains(t, msg, "\"r1\"")
	})

	t.Run("pods of an unrelated cluster don't trigger refusal", func(t *testing.T) {
		cluster := minimalClusterForRestore("c", ns)
		restore := minimalRestore("r1", ns, "c")
		other := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-server-0",
				Namespace: ns,
				Labels: map[string]string{
					"app.kubernetes.io/instance":  "other",
					"app.kubernetes.io/component": "database",
				},
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "neo4j", Image: "neo4j:5.26-enterprise"}}},
		}
		r := newRestoreTestReconciler(t, cluster, restore, other)

		require.NoError(t, r.refuseRestoreIfPodsRunning(ctx, restore, cluster))
	})
}

func TestReplicasReconciliationPaused(t *testing.T) {
	// The single check that makes the cluster controller yield on
	// sts.Spec.Replicas during a restore (issue #117). Extracted as a
	// helper so this gate can be unit-tested without standing up an
	// envtest and the full createOrUpdateResource machinery.
	cases := []struct {
		name     string
		owner    *neo4jv1beta1.Neo4jEnterpriseCluster
		expected bool
	}{
		{
			name:     "nil owner is not paused",
			owner:    nil,
			expected: false,
		},
		{
			name:     "no annotations is not paused",
			owner:    &neo4jv1beta1.Neo4jEnterpriseCluster{},
			expected: false,
		},
		{
			name: "unrelated annotation is not paused",
			owner: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"some.other/annotation": "yes"},
				},
			},
			expected: false,
		},
		{
			name: "restore-in-progress annotation pauses",
			owner: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{RestoreInProgressAnnotation: "r1"},
				},
			},
			expected: true,
		},
		{
			name: "restore-in-progress with empty value still pauses",
			// Defensive: an empty value still indicates a restore controller
			// has marked the cluster. Treat "present" as the gate, not
			// "non-empty".
			owner: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{RestoreInProgressAnnotation: ""},
				},
			},
			expected: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var owner interface{ GetAnnotations() map[string]string }
			if tc.owner == nil {
				assert.Equal(t, tc.expected, replicasReconciliationPaused(nil))
				return
			}
			owner = tc.owner
			_ = owner
			assert.Equal(t, tc.expected, replicasReconciliationPaused(tc.owner))
		})
	}
}

func TestBuildRestoreVolumesAlwaysMountsDataPVC(t *testing.T) {
	ctx := context.Background()
	ns := "default"

	// Pre-fix, stopCluster=false silently mounted an EmptyDir at /data, so the
	// restore wrote to ephemeral storage. Regression-lock that this never
	// happens again regardless of stopCluster.
	cases := []struct {
		name        string
		stopCluster bool
	}{
		{"stopCluster=true mounts data PVC", true},
		{"stopCluster=false also mounts data PVC (issue #117)", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := minimalClusterForRestore("c", ns)
			restore := minimalRestore("r1", ns, "c")
			restore.Spec.StopCluster = tc.stopCluster
			r := newRestoreTestReconciler(t, cluster, restore)

			vols := r.buildRestoreVolumes(ctx, restore)

			var dataVolume *corev1.Volume
			for i := range vols {
				if vols[i].Name == "neo4j-data" {
					dataVolume = &vols[i]
					break
				}
			}
			require.NotNil(t, dataVolume, "neo4j-data volume must be present")
			require.Nil(t, dataVolume.EmptyDir, "neo4j-data must NOT be an EmptyDir")
			require.NotNil(t, dataVolume.PersistentVolumeClaim, "neo4j-data must mount a PVC")
			assert.True(t,
				strings.Contains(dataVolume.PersistentVolumeClaim.ClaimName, "c"),
				"PVC claim name should reference the cluster: got %q", dataVolume.PersistentVolumeClaim.ClaimName)
		})
	}
}
