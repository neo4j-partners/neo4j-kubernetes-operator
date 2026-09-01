package status

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

// served is the status of a CR that has already been fully ready once — status.version is what
// records that, so it is what makes a phase regression detectable (ADR-004).
func served() neo4jv1beta1.Neo4jStatus {
	return neo4jv1beta1.Neo4jStatus{Phase: neo4jv1beta1.Neo4jPhaseRunning, Version: "2026.05.0"}
}

// nextPhase is the whole phase contract in one function: a wrong answer either tells the user their
// cluster is installing itself again or calls a routine roll a degradation.
func TestNextPhase(t *testing.T) {
	cases := []struct {
		name                                     string
		prior                                    neo4jv1beta1.Neo4jStatus
		offline, allReady, anySTSFound, changing bool
		want                                     neo4jv1beta1.Neo4jPhase
	}{
		{
			name:  "offline maintenance outranks everything, ready included",
			prior: served(), offline: true, allReady: true, anySTSFound: true,
			want: neo4jv1beta1.Neo4jPhaseMaintenance,
		},
		{
			name:  "everything the CR asked for is serving",
			prior: served(), allReady: true, anySTSFound: true,
			want: neo4jv1beta1.Neo4jPhaseRunning,
		},
		{
			name:  "no StatefulSet observed yet",
			prior: neo4jv1beta1.Neo4jStatus{},
			want:  neo4jv1beta1.Neo4jPhaseProvisioning,
		},
		{
			name:  "first install, pods still starting",
			prior: neo4jv1beta1.Neo4jStatus{Phase: neo4jv1beta1.Neo4jPhaseBootstrapping}, anySTSFound: true,
			want: neo4jv1beta1.Neo4jPhaseBootstrapping,
		},
		{
			// Bootstrapping outranks the in-flight branch: a CR that has never served is installing,
			// whatever else is going on.
			name:  "first install while the spec is already changing again",
			prior: neo4jv1beta1.Neo4jStatus{}, anySTSFound: true, changing: true,
			want: neo4jv1beta1.Neo4jPhaseBootstrapping,
		},
		{
			// The regression this decision exists to remove.
			name:  "a roll, scale or upgrade we asked for keeps Running",
			prior: served(), anySTSFound: true, changing: true,
			want: neo4jv1beta1.Neo4jPhaseRunning,
		},
		{
			name:  "members lost with nothing in flight is a degradation",
			prior: served(), anySTSFound: true,
			want: neo4jv1beta1.Neo4jPhaseDegraded,
		},
		{
			// Documented, not incidental: the no-StatefulSet branch is tested before establishment,
			// so a CR whose StatefulSet was deleted by hand does report Provisioning again. The
			// pipeline re-applies it before the writer runs, so this is a hand-edit only.
			name:  "an established CR whose StatefulSet vanished falls back to Provisioning",
			prior: served(),
			want:  neo4jv1beta1.Neo4jPhaseProvisioning,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextPhase(tc.prior, tc.offline, tc.allReady, tc.anySTSFound, tc.changing)
			if got != tc.want {
				t.Errorf("nextPhase() = %q, want %q", got, tc.want)
			}
		})
	}
}

// changeInFlight is what separates "the operator is working on what you asked for" from "something
// broke", so each of its three sources has to count on its own.
func TestChangeInFlight(t *testing.T) {
	cases := []struct {
		name                  string
		generation, observed  int64
		rolling, drainPending bool
		want                  bool
	}{
		{name: "spec changed and not absorbed yet", generation: 4, observed: 3, want: true},
		{name: "StatefulSet still moving pods to a new revision", generation: 3, observed: 3, rolling: true, want: true},
		{name: "scale-in waiting on Neo4j to release a member", generation: 3, observed: 3, drainPending: true, want: true},
		{name: "settled", generation: 3, observed: 3, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			neo4j := &neo4jv1beta1.Neo4j{
				ObjectMeta: metav1.ObjectMeta{Generation: tc.generation},
				Status:     neo4jv1beta1.Neo4jStatus{ObservedGeneration: tc.observed},
			}
			if got := changeInFlight(neo4j, tc.rolling, tc.drainPending); got != tc.want {
				t.Errorf("changeInFlight() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStsRolling(t *testing.T) {
	cases := []struct {
		name            string
		current, update string
		want            bool
	}{
		{name: "mid-roll", current: "rev-1", update: "rev-2", want: true},
		{name: "settled on one revision", current: "rev-1", update: "rev-1", want: false},
		{name: "being created — only the update revision exists, which is an install", update: "rev-1", want: false},
		{name: "status not populated yet", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sts := appsv1.StatefulSet{Status: appsv1.StatefulSetStatus{
				CurrentRevision: tc.current,
				UpdateRevision:  tc.update,
			}}
			if got := stsRolling(sts); got != tc.want {
				t.Errorf("stsRolling(current=%q update=%q) = %v, want %v", tc.current, tc.update, got, tc.want)
			}
		})
	}
}

// Wiring, not logic: nextPhase can be right while the signals never reach it. Both cases are a
// served CR with zero ready members — only the StatefulSet revisions differ.
func TestObserveAndWritePhaseFromRevisions(t *testing.T) {
	cases := []struct {
		name            string
		current, update string
		want            neo4jv1beta1.Neo4jPhase
	}{
		{name: "rolling after a config change", current: "rev-1", update: "rev-2", want: neo4jv1beta1.Neo4jPhaseRunning},
		{name: "not rolling — a member was lost", current: "rev-1", update: "rev-1", want: neo4jv1beta1.Neo4jPhaseDegraded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)
			_ = neo4jv1beta1.AddToScheme(scheme)

			neo4j := standaloneWithDynamicSC("dev", "default", "")
			neo4j.Spec.Version = "2026.05.0"
			neo4j.Status = served()
			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "dev-server", Namespace: "default"},
				Status: appsv1.StatefulSetStatus{
					ReadyReplicas:   0,
					CurrentRevision: tc.current,
					UpdateRevision:  tc.update,
				},
			}
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "data-dev-server-0", Namespace: "default"},
				Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
			}
			c := fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(neo4j, sts, pvc).
				WithStatusSubresource(&neo4jv1beta1.Neo4j{}).Build()

			got := &neo4jv1beta1.Neo4j{}
			if err := c.Get(t.Context(), types.NamespacedName{Name: "dev", Namespace: "default"}, got); err != nil {
				t.Fatalf("Get: %v", err)
			}
			// The spec change is already absorbed, so the revisions are the only signal left —
			// otherwise a generation the fake client assigns would decide the outcome for us.
			got.Status.ObservedGeneration = got.Generation

			if err := NewWriter(c).ObserveAndWrite(t.Context(), got); err != nil {
				t.Fatalf("ObserveAndWrite: %v", err)
			}
			if got.Status.Phase != tc.want {
				t.Errorf("phase = %q, want %q", got.Status.Phase, tc.want)
			}
		})
	}
}
