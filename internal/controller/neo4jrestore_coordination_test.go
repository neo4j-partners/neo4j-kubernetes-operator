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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
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

// minimalStandaloneForRestore builds a Neo4jEnterpriseStandalone for the
// Job-based restore path. That path is standalone-only (clusters use the
// Cypher seed/recreate path, rule 75), so the coordination helpers
// (setRestoreInProgressAnnotation, stopCluster pod-wait, refuse preflight)
// operate on a Neo4jEnterpriseStandalone + the `app=<name>` pod selector.
func minimalStandaloneForRestore(name, namespace string) *neo4jv1beta1.Neo4jEnterpriseStandalone {
	return &neo4jv1beta1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
			Image: neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
		},
	}
}

// The Job-based restore path (and thus the restore-in-progress marker) is
// standalone-only — clusters use the Cypher path (rule 75). #196: the marker
// must be written on the Neo4jEnterpriseStandalone; the pre-fix code Got/Updated
// a Neo4jEnterpriseCluster that doesn't exist for a standalone target and failed
// with "Neo4jEnterpriseCluster ... not found". `cluster` here is the
// standaloneAsCluster wrapper, exactly as the controller passes it.
func TestSetRestoreInProgressAnnotation(t *testing.T) {
	ctx := context.Background()
	ns := "default"

	t.Run("sets annotation on a clean standalone (#196)", func(t *testing.T) {
		sa := minimalStandaloneForRestore("s", ns)
		restore := minimalRestore("r1", ns, "s")
		r := newRestoreTestReconciler(t, sa, restore)

		require.NoError(t, r.setRestoreInProgressAnnotation(ctx, restore, standaloneAsCluster(sa)))

		got := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
		require.NoError(t, r.Get(ctx, types.NamespacedName{Name: "s", Namespace: ns}, got))
		assert.Equal(t, "r1", got.Annotations[RestoreInProgressAnnotation])
	})

	t.Run("idempotent when annotation already names this restore", func(t *testing.T) {
		sa := minimalStandaloneForRestore("s", ns)
		sa.Annotations = map[string]string{RestoreInProgressAnnotation: "r1"}
		restore := minimalRestore("r1", ns, "s")
		r := newRestoreTestReconciler(t, sa, restore)

		require.NoError(t, r.setRestoreInProgressAnnotation(ctx, restore, standaloneAsCluster(sa)))

		got := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
		require.NoError(t, r.Get(ctx, types.NamespacedName{Name: "s", Namespace: ns}, got))
		assert.Equal(t, "r1", got.Annotations[RestoreInProgressAnnotation])
	})

	t.Run("refuses when a different restore is already in progress", func(t *testing.T) {
		sa := minimalStandaloneForRestore("s", ns)
		sa.Annotations = map[string]string{RestoreInProgressAnnotation: "older-restore"}
		restore := minimalRestore("r1", ns, "s")
		r := newRestoreTestReconciler(t, sa, restore)

		err := r.setRestoreInProgressAnnotation(ctx, restore, standaloneAsCluster(sa))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "older-restore")
		assert.Contains(t, err.Error(), "r1")

		// Annotation must NOT have been overwritten.
		got := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
		require.NoError(t, r.Get(ctx, types.NamespacedName{Name: "s", Namespace: ns}, got))
		assert.Equal(t, "older-restore", got.Annotations[RestoreInProgressAnnotation])
	})
}

func TestClearRestoreInProgressAnnotation(t *testing.T) {
	ctx := context.Background()
	ns := "default"

	t.Run("clears annotation set by this restore", func(t *testing.T) {
		sa := minimalStandaloneForRestore("s", ns)
		sa.Annotations = map[string]string{RestoreInProgressAnnotation: "r1"}
		restore := minimalRestore("r1", ns, "s")
		r := newRestoreTestReconciler(t, sa, restore)

		require.NoError(t, r.clearRestoreInProgressAnnotation(ctx, restore, "s", ns))

		got := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
		require.NoError(t, r.Get(ctx, types.NamespacedName{Name: "s", Namespace: ns}, got))
		_, present := got.Annotations[RestoreInProgressAnnotation]
		assert.False(t, present, "annotation should be cleared")
	})

	t.Run("leaves annotation set by a DIFFERENT restore alone", func(t *testing.T) {
		sa := minimalStandaloneForRestore("s", ns)
		sa.Annotations = map[string]string{RestoreInProgressAnnotation: "other-restore"}
		restore := minimalRestore("r1", ns, "s")
		r := newRestoreTestReconciler(t, sa, restore)

		require.NoError(t, r.clearRestoreInProgressAnnotation(ctx, restore, "s", ns))

		got := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
		require.NoError(t, r.Get(ctx, types.NamespacedName{Name: "s", Namespace: ns}, got))
		assert.Equal(t, "other-restore", got.Annotations[RestoreInProgressAnnotation],
			"clear must be a no-op when the annotation belongs to a different restore")
	})

	t.Run("no-op when annotation is absent", func(t *testing.T) {
		sa := minimalStandaloneForRestore("s", ns)
		restore := minimalRestore("r1", ns, "s")
		r := newRestoreTestReconciler(t, sa, restore)

		require.NoError(t, r.clearRestoreInProgressAnnotation(ctx, restore, "s", ns))
	})

	t.Run("no-op when target CR is gone (or was a cluster, which never sets it)", func(t *testing.T) {
		restore := minimalRestore("r1", ns, "gone")
		r := newRestoreTestReconciler(t, restore)

		// Cleanly handles a missing standalone — the restore finalizer must be
		// able to release even after the standalone is deleted, and a true
		// cluster (which never sets this marker) is a no-op too.
		require.NoError(t, r.clearRestoreInProgressAnnotation(ctx, restore, "gone", ns))
	})
}

// The Job restore path is standalone-only, so the preflight lists standalone
// pods via the `app=<name>` selector (StandalonePodSelector) — not the cluster
// ServerPodSelector. This pins that swap (part of #196/#187).
// TestSeedCredsRolledOut pins the #190 gate: a cluster Cypher restore must not
// issue the seedURI recreate until the StatefulSet has rolled out the
// seed-credentials Secret. The decisive check is "the pod template references
// the Secret AND every pod is updated+ready" — a fully-ready OLD template
// (without the creds) must NOT count as rolled out.
func TestSeedCredsRolledOut(t *testing.T) {
	ctx := context.Background()
	ns := "default"
	const secret = "s3-creds"

	mkSTS := func(mut func(*appsv1.StatefulSet)) *appsv1.StatefulSet {
		replicas := int32(2)
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "c-server", Namespace: ns, Generation: 3},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &replicas,
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "neo4j"}}}},
			},
			Status: appsv1.StatefulSetStatus{ObservedGeneration: 3, UpdatedReplicas: 2, ReadyReplicas: 2},
		}
		if mut != nil {
			mut(sts)
		}
		return sts
	}
	withCreds := func(sts *appsv1.StatefulSet) {
		sts.Spec.Template.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{
			{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: secret}}},
		}
	}

	cases := []struct {
		name string
		sts  *appsv1.StatefulSet
		want bool
	}{
		{"creds rolled out + all pods ready", mkSTS(withCreds), true},
		{"creds in spec but template not yet updated (the #190 window)", mkSTS(nil), false},
		{"creds in template but mid-roll", mkSTS(func(s *appsv1.StatefulSet) { withCreds(s); s.Status.UpdatedReplicas = 1 }), false},
		{"creds in template, ready, but observedGeneration stale", mkSTS(func(s *appsv1.StatefulSet) { withCreds(s); s.Status.ObservedGeneration = 2 }), false},
		{"creds in template but not all ready", mkSTS(func(s *appsv1.StatefulSet) { withCreds(s); s.Status.ReadyReplicas = 1 }), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := minimalClusterForRestore("c", ns)
			r := newRestoreTestReconciler(t, cluster, tc.sts)
			got, err := r.seedCredsRolledOut(ctx, cluster, secret)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("StatefulSet missing returns error", func(t *testing.T) {
		cluster := minimalClusterForRestore("c", ns)
		r := newRestoreTestReconciler(t, cluster)
		_, err := r.seedCredsRolledOut(ctx, cluster, secret)
		require.Error(t, err)
	})
}

func TestRefuseRestoreIfPodsRunning(t *testing.T) {
	ctx := context.Background()
	ns := "default"

	standalonePod := func(podName, app string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: ns,
				Labels:    map[string]string{"app": app},
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "neo4j", Image: "neo4j:5.26-enterprise"}}},
		}
	}

	t.Run("no pods → allowed", func(t *testing.T) {
		sa := minimalStandaloneForRestore("s", ns)
		restore := minimalRestore("r1", ns, "s")
		r := newRestoreTestReconciler(t, sa, restore)

		require.NoError(t, r.refuseRestoreIfPodsRunning(ctx, restore, standaloneAsCluster(sa)))
	})

	t.Run("standalone pod exists → refused with actionable error", func(t *testing.T) {
		sa := minimalStandaloneForRestore("s", ns)
		restore := minimalRestore("r1", ns, "s")
		r := newRestoreTestReconciler(t, sa, restore, standalonePod("s-0", "s"))

		err := r.refuseRestoreIfPodsRunning(ctx, restore, standaloneAsCluster(sa))
		require.Error(t, err)
		msg := err.Error()
		assert.Contains(t, msg, "server pod(s)")
		assert.Contains(t, msg, "stopCluster=true")
		assert.Contains(t, msg, "\"r1\"")
	})

	t.Run("pods of an unrelated instance don't trigger refusal", func(t *testing.T) {
		sa := minimalStandaloneForRestore("s", ns)
		restore := minimalRestore("r1", ns, "s")
		r := newRestoreTestReconciler(t, sa, restore, standalonePod("other-0", "other"))

		require.NoError(t, r.refuseRestoreIfPodsRunning(ctx, restore, standaloneAsCluster(sa)))
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
			if tc.owner == nil {
				assert.Equal(t, tc.expected, replicasReconciliationPaused(nil))
				return
			}
			assert.Equal(t, tc.expected, replicasReconciliationPaused(tc.owner))
		})
	}

	// The gate must also pause a Neo4jEnterpriseStandalone owner — the standalone
	// controller honours it so a stopCluster=true standalone restore can quiesce
	// (it scales the STS to 0) without the controller racing it back to 1.
	t.Run("standalone with restore-in-progress annotation pauses", func(t *testing.T) {
		sa := &neo4jv1beta1.Neo4jEnterpriseStandalone{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{RestoreInProgressAnnotation: "r1"}},
		}
		assert.True(t, replicasReconciliationPaused(sa))
	})
	t.Run("standalone without the annotation is not paused", func(t *testing.T) {
		assert.False(t, replicasReconciliationPaused(&neo4jv1beta1.Neo4jEnterpriseStandalone{}))
	})
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

// TestWarnIfChainParent pins the advisory that restoring via a FULL+DIFF chain
// PARENT seeds from its full snapshot, not the latest diff state (rule 78).
// The warning must fire only when differential children with a Succeeded run
// actually exist — so a restore intending "latest" via the parent FULL CR is
// surfaced, not silently downgraded.
func TestWarnIfChainParent(t *testing.T) {
	const ns = "chain-ns"
	mkBackup := func(name, chainFrom, status string) *neo4jv1beta1.Neo4jBackup {
		b := &neo4jv1beta1.Neo4jBackup{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec:       neo4jv1beta1.Neo4jBackupSpec{ChainFromBackup: chainFrom},
		}
		if status != "" {
			b.Status.History = []neo4jv1beta1.BackupRun{{Status: status}}
		}
		return b
	}
	restore := &neo4jv1beta1.Neo4jRestore{ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: ns}}

	drain := func(r *Neo4jRestoreReconciler) []string {
		fr, ok := r.Recorder.(*record.FakeRecorder)
		if !ok {
			t.Fatalf("expected *record.FakeRecorder, got %T", r.Recorder)
		}
		var out []string
		for {
			select {
			case e := <-fr.Events:
				out = append(out, e)
			default:
				return out
			}
		}
	}

	t.Run("parent with a Succeeded diff child warns and names the child", func(t *testing.T) {
		r := newRestoreTestReconciler(t,
			mkBackup("daily", "", "Succeeded"),
			mkBackup("hourly", "daily", "Succeeded"))
		r.warnIfChainParent(context.Background(), restore, "daily")
		ev := drain(r)
		require.Len(t, ev, 1)
		assert.Contains(t, ev[0], EventReasonRestoreFromChainParent)
		assert.Contains(t, ev[0], "hourly")
	})

	t.Run("no chain children means no warning", func(t *testing.T) {
		r := newRestoreTestReconciler(t, mkBackup("daily", "", "Succeeded"))
		r.warnIfChainParent(context.Background(), restore, "daily")
		assert.Empty(t, drain(r))
	})

	t.Run("child with no Succeeded run does not warn", func(t *testing.T) {
		r := newRestoreTestReconciler(t,
			mkBackup("daily", "", "Succeeded"),
			mkBackup("hourly", "daily", "Failed"))
		r.warnIfChainParent(context.Background(), restore, "daily")
		assert.Empty(t, drain(r))
	})
}
