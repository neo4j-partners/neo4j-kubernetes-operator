package neo4j

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/status"
)

// A CR the pipeline refuses at its first step: storage is mandatory, so validation fails before
// any domain reconciler runs and the error branch is reached without wiring the whole pipeline.
func neo4jRejectedByValidation() *neo4jv1beta1.Neo4j {
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dev",
			Namespace: "default",
			// Third spec revision, mirroring the reported reproduction. The finalizer is already
			// present so the reconcile reaches the pipeline instead of adding it and requeueing.
			Generation: 3,
			Finalizers: []string{FinalizerName},
		},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
		},
	}
}

// observedGeneration is the operator's acknowledgement that a generation was *fully reconciled*
// (ADR-004), and the field the user guide tells people to gate their automation on. The error
// branch used to advance it next to MarkPipelineError, so a `kubectl wait` on
// observedGeneration == generation returned immediately over a workload the operator had just
// given up on. A failed pass must leave the field on the last generation that really was applied.
func TestFailedPipelineDoesNotAdvanceObservedGeneration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := neo4jv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	cr := neo4jRejectedByValidation()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(cr).
		WithStatusSubresource(&neo4jv1beta1.Neo4j{}).Build()

	key := types.NamespacedName{Name: "dev", Namespace: "default"}
	stored := &neo4jv1beta1.Neo4j{}
	if err := c.Get(t.Context(), key, stored); err != nil {
		t.Fatalf("Get: %v", err)
	}
	// Generation 2 was applied and served; generation 3 is the one about to fail. Status lives on
	// a subresource, so it has to be seeded through its own write.
	const priorObserved int64 = 2
	stored.Status.ObservedGeneration = priorObserved
	stored.Status.Phase = neo4jv1beta1.Neo4jPhaseRunning
	if err := c.Status().Update(t.Context(), stored); err != nil {
		t.Fatalf("seed status: %v", err)
	}
	if stored.Generation != 3 {
		t.Fatalf("fixture generation = %d, want 3", stored.Generation)
	}

	r := &Neo4jReconciler{Client: c, StatusWriter: status.NewWriter(c)}
	if _, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: key}); err == nil {
		t.Fatal("expected the reconcile to surface the pipeline error")
	}

	got := &neo4jv1beta1.Neo4j{}
	if err := c.Get(t.Context(), key, got); err != nil {
		t.Fatalf("Get after reconcile: %v", err)
	}

	if got.Status.ObservedGeneration != priorObserved {
		t.Errorf("observedGeneration = %d, want %d — a failed pass must not claim generation %d was applied",
			got.Status.ObservedGeneration, priorObserved, got.Generation)
	}
	// The failure still has to be visible, or not advancing the field would just be silence.
	if got.Status.Phase != neo4jv1beta1.Neo4jPhaseFailed {
		t.Errorf("phase = %q, want %q", got.Status.Phase, neo4jv1beta1.Neo4jPhaseFailed)
	}
	if cond := meta.FindStatusCondition(got.Status.Conditions, oracle.ConditionError.String()); cond == nil ||
		cond.Status != metav1.ConditionTrue {
		t.Errorf("Error condition = %+v, want True", cond)
	}
}
