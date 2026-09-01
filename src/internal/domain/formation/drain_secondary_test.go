package formation

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
)

const readTailAddress = "prod-read-1.default.svc.cluster.local:7687"

// readScaleIn is the reported topology mid-scale-in: one primary, a read pool declared at 1 while
// its StatefulSet still runs 2, and the neo4j database already back to what the pools can host.
type readScaleIn struct {
	neo4j    *neo4jv1beta1.Neo4j
	r        *Reconciler
	admin    *fakeAdmin
	recorder *record.FakeRecorder
	client   client.Client
}

// newReadScaleIn builds that fixture around one departing member. The tail server is declared
// as Neo4j reports it rather than driven through fakeAdmin.DeallocateDatabases, which jumps
// straight to Deallocated and so cannot express a member that stays in Deallocating.
func newReadScaleIn(t *testing.T, tail intneo4j.Server, dbs ...intneo4j.DatabaseTopology) readScaleIn {
	t.Helper()
	neo4j := testClusterCR(1)
	neo4j.Spec.Topology.Secondaries = &neo4jv1beta1.SecondariesSpec{
		Read: &neo4jv1beta1.SecondaryPoolSpec{Members: 1},
	}
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	one, two := int32(1), int32(2)
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&neo4jv1beta1.Neo4j{}).WithObjects(
		neo4j.DeepCopy(),
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "prod-primary", Namespace: "default"},
			Spec: appsv1.StatefulSetSpec{Replicas: &one}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "prod-read", Namespace: "default"},
			Spec: appsv1.StatefulSetSpec{Replicas: &two}},
	).Build()

	if len(dbs) == 0 {
		// Requested == current on the remaining hosts, which is what the report showed: the drain
		// of the user database succeeded, only the server state never moved.
		dbs = []intneo4j.DatabaseTopology{{
			Name: "neo4j", Type: "standard", HasTopology: true,
			RequestedPrimaries: 1, RequestedSecondaries: 1,
			CurrentPrimaries: 1, CurrentSecondaries: 1,
		}}
	}
	admin := &fakeAdmin{
		servers: []intneo4j.Server{
			{Name: "p0", Address: "prod-primary-0.default.svc.cluster.local:7687",
				State: "Enabled", Hosting: []string{"neo4j", "system"}},
			{Name: "r0", Address: "prod-read-0.default.svc.cluster.local:7687",
				State: "Enabled", Hosting: []string{"neo4j", "system"}},
			tail,
		},
		dbs: dbs,
	}
	recorder := record.NewFakeRecorder(10)
	return readScaleIn{
		neo4j:    neo4j,
		admin:    admin,
		recorder: recorder,
		client:   c,
		r: &Reconciler{
			Client:   c,
			Recorder: recorder,
			Connect: func(context.Context, *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
				return admin, nil
			},
		},
	}
}

// settle runs passes until the read pool is cleared for shrinking, refreshing the CR between them
// the way the controller does.
func (f readScaleIn) settle(t *testing.T) {
	t.Helper()
	for i := 0; i < 5; i++ {
		if out := f.r.Reconcile(t.Context(), f.neo4j); out.Err != nil {
			t.Fatalf("pass %d: %v", i, out.Err)
		}
		if ParseDrainOK(f.neo4j)["read"] == 1 {
			return
		}
		if err := f.client.Get(t.Context(), types.NamespacedName{Name: "prod", Namespace: "default"}, f.neo4j); err != nil {
			t.Fatal(err)
		}
	}
}

func (f readScaleIn) tailStillRegistered(t *testing.T) bool {
	t.Helper()
	servers, err := f.admin.ShowServers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, ok := intneo4j.FindByAddress(servers, readTailAddress)
	return ok
}

// drainCondition is ServersPendingDrain, the single condition a scale-in reports through.
func drainCondition(t *testing.T, neo4j *neo4jv1beta1.Neo4j) *metav1.Condition {
	t.Helper()
	c := meta.FindStatusCondition(neo4j.Status.Conditions, oracle.ConditionServersPendingDrain.String())
	if c == nil {
		t.Fatalf("no %s condition; got %v", oracle.ConditionServersPendingDrain, neo4j.Status.Conditions)
	}
	return c
}

// The reported defect. On a single-primary cluster Neo4j leaves a drained read member in
// Deallocating for good: hosting is down to system, the user database is back to its requested
// topology, and the label never flips. The operator used to wait on that label alone, so it
// requeued every 15s forever and held the StatefulSet at its old size.
func TestScaleInDropsDrainedSecondaryStuckDeallocating(t *testing.T) {
	f := newReadScaleIn(t, intneo4j.Server{
		Name: "r1", Address: readTailAddress,
		State: "Deallocating", Hosting: []string{"system"},
	})

	f.settle(t)

	if ParseDrainOK(f.neo4j)["read"] != 1 {
		t.Fatalf("drain-ok = %v gen=%d, want read=1 — without it the StatefulSet is never allowed to shrink",
			f.neo4j.Status.DrainOK, f.neo4j.Status.DrainOKGeneration)
	}
	if f.tailStillRegistered(t) {
		t.Error("read-1 is still registered in Neo4j: it hosts nothing but system, which is Neo4j's own definition of a droppable server")
	}
}

// The evidence has to be read, not assumed: a member Neo4j still reports hosting a user database
// is genuinely mid-handover, and dropping it could take the copy with it.
func TestScaleInWaitsWhileDrainingSecondaryStillHostsAUserDatabase(t *testing.T) {
	f := newReadScaleIn(t, intneo4j.Server{
		Name: "r1", Address: readTailAddress,
		State: "Deallocating", Hosting: []string{"neo4j", "system"},
	})

	if out := f.r.Reconcile(t.Context(), f.neo4j); out.Err != nil {
		t.Fatal(out.Err)
	}

	if !f.tailStillRegistered(t) {
		t.Fatal("read-1 was dropped while Neo4j still reports it hosting neo4j")
	}
	if got := drainCondition(t, f.neo4j).Reason; got != oracle.ReasonDraining.String() {
		t.Errorf("ServersPendingDrain reason = %q, want %q", got, oracle.ReasonDraining)
	}
}

// Composite databases have no store and Neo4j reports them on every server, so counting one as
// hosted data would pin the drain exactly as the state label does.
func TestScaleInDropsDrainedSecondaryHostingOnlyAComposite(t *testing.T) {
	f := newReadScaleIn(t,
		intneo4j.Server{
			Name: "r1", Address: readTailAddress,
			State: "Deallocating", Hosting: []string{"system", "shards"},
		},
		intneo4j.DatabaseTopology{
			Name: "neo4j", Type: "standard", HasTopology: true,
			RequestedPrimaries: 1, RequestedSecondaries: 1,
			CurrentPrimaries: 1, CurrentSecondaries: 1,
		},
		intneo4j.DatabaseTopology{Name: "shards", Type: "composite"},
	)

	f.settle(t)

	if f.tailStillRegistered(t) {
		t.Error("read-1 is still registered: a composite database is not data it can hand over")
	}
}

// The evidence path stops at secondaries. A primary's copy of system votes in the raft group and
// its database copies are not replicas, so a primary stuck in Deallocating is a case for the
// timeout below, never for an operator-decided DROP.
func TestScaleInDoesNotDropPrimaryOnHostingEvidence(t *testing.T) {
	neo4j := testClusterCR(1)
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	two := int32(2)
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&neo4jv1beta1.Neo4j{}).WithObjects(
		neo4j.DeepCopy(),
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "prod-primary", Namespace: "default"},
			Spec: appsv1.StatefulSetSpec{Replicas: &two}},
	).Build()
	admin := &fakeAdmin{
		servers: []intneo4j.Server{
			{Name: "p0", Address: "prod-primary-0.default.svc.cluster.local:7687",
				State: "Enabled", Hosting: []string{"neo4j", "system"}},
			{Name: "p1", Address: "prod-primary-1.default.svc.cluster.local:7687",
				State: "Deallocating", Hosting: []string{"system"}},
		},
		dbs: []intneo4j.DatabaseTopology{{
			Name: "neo4j", Type: "standard", HasTopology: true,
			RequestedPrimaries: 1, CurrentPrimaries: 1,
		}},
	}
	r := &Reconciler{
		Client: c,
		Connect: func(context.Context, *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
			return admin, nil
		},
	}

	if out := r.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatal(out.Err)
	}

	servers, _ := admin.ShowServers(t.Context())
	if _, ok := intneo4j.FindByAddress(servers, "prod-primary-1.default.svc.cluster.local:7687"); !ok {
		t.Fatal("a primary was dropped on hosting evidence: its system copy votes, so only Neo4j may decide it has left")
	}
}

// Whatever keeps Neo4j from releasing a member, the wait must stop being silent: the operator
// forces nothing, it says what it is waiting on and slows down.
func TestScaleInSurfacesDrainTimeout(t *testing.T) {
	f := newReadScaleIn(t, intneo4j.Server{
		Name: "r1", Address: readTailAddress,
		State: "Deallocating", Hosting: []string{"neo4j", "system"},
	})
	// The condition dates the episode, so backdating it past the budget is what a stuck drain
	// looks like on the next pass.
	meta.SetStatusCondition(&f.neo4j.Status.Conditions, metav1.Condition{
		Type:               oracle.ConditionServersPendingDrain.String(),
		Status:             metav1.ConditionTrue,
		Reason:             oracle.ReasonDraining.String(),
		LastTransitionTime: metav1.NewTime(time.Now().Add(-drainBudget - time.Minute)),
	})

	out := f.r.Reconcile(t.Context(), f.neo4j)
	if out.Err != nil {
		t.Fatal(out.Err)
	}

	cond := drainCondition(t, f.neo4j)
	if cond.Reason != oracle.ReasonDrainTimeout.String() {
		t.Fatalf("ServersPendingDrain reason = %q, want %q after %s of draining",
			cond.Reason, oracle.ReasonDrainTimeout, drainBudget)
	}
	// Naming the member and what Neo4j reports of it is the whole point — otherwise the reader is
	// sent back to the operator logs to find out which pod is stuck and why.
	for _, want := range []string{"prod-read-1", "Deallocating", "neo4j"} {
		if !strings.Contains(cond.Message, want) {
			t.Errorf("condition message %q should mention %q", cond.Message, want)
		}
	}
	event := findEvent(f.recorder, oracle.ReasonDrainTimeout.String())
	if event == "" {
		t.Fatalf("no %s Event: a drain that outlived its budget has to reach whoever watches Events",
			oracle.ReasonDrainTimeout)
	}
	if !strings.Contains(event, "Warning") {
		t.Errorf("event %q should be a Warning", event)
	}
	// Nothing forced: the member stays registered, the pool keeps its size, and the pass backs off.
	if !f.tailStillRegistered(t) {
		t.Error("the timeout dropped the member — it reports, it does not act")
	}
	if ParseDrainOK(f.neo4j)["read"] != 0 {
		t.Errorf("drain-ok = %v: the StatefulSet must not be cleared to shrink on a timeout", f.neo4j.Status.DrainOK)
	}
	if out.Result.RequeueAfter <= requeueAfter {
		t.Errorf("requeue after %s, want a slower cadence than the %s drain poll", out.Result.RequeueAfter, requeueAfter)
	}
}
