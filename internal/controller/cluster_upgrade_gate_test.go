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

package controller

// Pinning tests for #262: an image change while the cluster is not Ready must
// be HELD (never silently applied by the normal config-restart path), Ready
// must not be declared mid-rollout, and these guarantees are what makes the
// status.version backfill at Ready truthful.

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

func gateTestReconciler(t *testing.T, objs ...runtime.Object) *Neo4jEnterpriseClusterReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = neo4jv1beta1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).
		WithStatusSubresource(&neo4jv1beta1.Neo4jEnterpriseCluster{}).Build()
	return &Neo4jEnterpriseClusterReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(16)}
}

func gateTestSTS(image string, replicas int32, currentRev, updateRev string, updated, ready int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "c-server", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "neo4j", Image: image}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			CurrentRevision: currentRev,
			UpdateRevision:  updateRev,
			UpdatedReplicas: updated,
			ReadyReplicas:   ready,
		},
	}
}

func gateTestCluster(tag, phase string) *neo4jv1beta1.Neo4jEnterpriseCluster {
	return &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			AcceptLicenseAgreement: "eval",
			Image:                  neo4jv1beta1.ImageSpec{Repo: "neo4j", Tag: tag},
			Topology:               neo4jv1beta1.TopologyConfiguration{Servers: 3},
		},
		Status: neo4jv1beta1.Neo4jEnterpriseClusterStatus{Phase: phase},
	}
}

// The core #262 pin: image drift + cluster NOT Ready → the in-memory spec is
// pinned to the StatefulSet's current image, so the normal path cannot stomp
// the version change in as a config restart.
func TestHoldImageDriftUntilReady_PinsSpecWhileNotReady(t *testing.T) {
	sts := gateTestSTS("neo4j:5.26.0-enterprise", 3, "rev1", "rev1", 3, 3)
	cluster := gateTestCluster("2026.04-enterprise", "Forming")
	r := gateTestReconciler(t, sts, cluster)

	r.holdImageDriftUntilReady(context.Background(), cluster)

	if cluster.Spec.Image.Tag != "5.26.0-enterprise" || cluster.Spec.Image.Repo != "neo4j" {
		t.Errorf("spec must be pinned to the running image while not Ready; got %s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	}
}

func TestHoldImageDriftUntilReady_NoDriftNoTouch(t *testing.T) {
	sts := gateTestSTS("neo4j:5.26.0-enterprise", 3, "rev1", "rev1", 3, 3)
	cluster := gateTestCluster("5.26.0-enterprise", "Forming")
	r := gateTestReconciler(t, sts, cluster)
	r.holdImageDriftUntilReady(context.Background(), cluster)
	if cluster.Spec.Image.Tag != "5.26.0-enterprise" {
		t.Errorf("no drift: spec must be untouched; got %s", cluster.Spec.Image.Tag)
	}
}

func TestHoldImageDriftUntilReady_NoStatefulSetNoTouch(t *testing.T) {
	cluster := gateTestCluster("2026.04-enterprise", "")
	r := gateTestReconciler(t, cluster)
	r.holdImageDriftUntilReady(context.Background(), cluster)
	if cluster.Spec.Image.Tag != "2026.04-enterprise" {
		t.Errorf("initial install (no STS): spec must be untouched; got %s", cluster.Spec.Image.Tag)
	}
}

// Ready must not be declared while a rollout is in flight (#262: 'Ready' was
// reported over a mixed-version cluster mid-roll).
func TestServerStatefulSetFullyRolled(t *testing.T) {
	cases := []struct {
		name string
		sts  *appsv1.StatefulSet
		want bool
	}{
		{"fully rolled", gateTestSTS("neo4j:x", 3, "rev2", "rev2", 3, 3), true},
		{"revision mismatch (mid-roll)", gateTestSTS("neo4j:x", 3, "rev1", "rev2", 1, 3), false},
		{"updated lagging", gateTestSTS("neo4j:x", 3, "rev2", "rev2", 2, 3), false},
		{"ready lagging", gateTestSTS("neo4j:x", 3, "rev2", "rev2", 3, 2), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := gateTestCluster("x", "Forming")
			r := gateTestReconciler(t, tc.sts, cluster)
			got, detail := r.serverStatefulSetFullyRolled(context.Background(), cluster)
			if got != tc.want {
				t.Errorf("fullyRolled = %v (detail %q), want %v", got, detail, tc.want)
			}
		})
	}
}

// Escape hatch: a first deploy wedged on an unpullable image (0 ready
// replicas, every pod ImagePullBackOff) has no quorum for the hold to
// protect — the user's corrected tag must flow to the StatefulSet instead
// of being deferred until a Ready that can never come (found by the
// v1.12.1 fresh-eyes journey: a bad example tag permanently wedged the
// cluster even after fixing spec.image).
func TestHoldImageDriftUntilReady_ZeroReadyReplicasEscapesHold(t *testing.T) {
	sts := gateTestSTS("neo4j:2025.12.0-enterprise", 3, "rev1", "rev1", 0, 0)
	cluster := gateTestCluster("2026.04-enterprise", "Forming")
	r := gateTestReconciler(t, sts, cluster)

	r.holdImageDriftUntilReady(context.Background(), cluster)

	if cluster.Spec.Image.Tag != "2026.04-enterprise" {
		t.Errorf("with zero ready replicas the corrected image must NOT be held; got %s:%s", cluster.Spec.Image.Repo, cluster.Spec.Image.Tag)
	}
}
