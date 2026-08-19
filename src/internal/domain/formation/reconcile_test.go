package formation

import (
	"context"
	"strings"
	"sync"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	intneo4j "github.com/neo4j/neo4j-kubernetes-operator/src/internal/neo4j"
)

type fakeAdmin struct {
	mu      sync.Mutex
	servers []intneo4j.Server
	dbs     []intneo4j.DatabaseTopology
	// defaultAlloc records the last dbms.setDefaultAllocationNumbers call, nil until called.
	defaultAlloc *[2]int64
	alters       int
}

func (f *fakeAdmin) ShowServers(context.Context) ([]intneo4j.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]intneo4j.Server, len(f.servers))
	copy(out, f.servers)
	return out, nil
}

func (f *fakeAdmin) ShowDatabaseTopologies(context.Context) ([]intneo4j.DatabaseTopology, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]intneo4j.DatabaseTopology, len(f.dbs))
	copy(out, f.dbs)
	return out, nil
}

func (f *fakeAdmin) SetDatabaseTopology(_ context.Context, name string, primaries, secondaries int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alters++
	for i := range f.dbs {
		if f.dbs[i].Name == name {
			f.dbs[i].RequestedPrimaries = primaries
			f.dbs[i].RequestedSecondaries = secondaries
			f.dbs[i].CurrentPrimaries = primaries
			f.dbs[i].CurrentSecondaries = secondaries
		}
	}
	return nil
}

func (f *fakeAdmin) SetDefaultAllocationNumbers(_ context.Context, primaries, secondaries int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defaultAlloc = &[2]int64{primaries, secondaries}
	return nil
}

func (f *fakeAdmin) EnableServer(_ context.Context, name, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.servers {
		if f.servers[i].Name == name {
			f.servers[i].State = "Enabled"
		}
	}
	return nil
}

func (f *fakeAdmin) DeallocateDatabases(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.servers {
		if f.servers[i].Name == name {
			f.servers[i].State = "Deallocated"
		}
	}
	return nil
}

func (f *fakeAdmin) DropServer(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := f.servers[:0]
	for _, s := range f.servers {
		if s.Name != name {
			next = append(next, s)
		}
	}
	f.servers = next
	return nil
}

func (f *fakeAdmin) Close(context.Context) error { return nil }

func testClusterCR(primaries int32) *neo4jv1beta1.Neo4j {
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition:  neo4jv1beta1.EditionEnterprise,
			Version:  "2026.05.0",
			License:  neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{
				Mode:      neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{Members: primaries},
			},
		},
	}
}

func ptr[T any](v T) *T { return &v }

func TestReconcileEnablesAllFreeInOnePass(t *testing.T) {
	neo4j := testClusterCR(3)
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	replicas := int32(3)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-primary", Namespace: "default"},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&neo4jv1beta1.Neo4j{}).WithObjects(neo4j.DeepCopy(), sts).Build()

	admin := &fakeAdmin{servers: []intneo4j.Server{
		{Name: "s0", Address: "prod-primary-0.default.svc.cluster.local:7687", State: "Enabled"},
		{Name: "s1", Address: "prod-primary-1.default.svc.cluster.local:7687", State: "Free"},
		{Name: "s2", Address: "prod-primary-2.default.svc.cluster.local:7687", State: "Free"},
	}}
	r := &Reconciler{
		Client: c,
		Connect: func(context.Context, *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
			return admin, nil
		},
	}

	out := r.Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	servers, _ := admin.ShowServers(t.Context())
	for _, addr := range []string{
		"prod-primary-1.default.svc.cluster.local:7687",
		"prod-primary-2.default.svc.cluster.local:7687",
	} {
		s, ok := intneo4j.FindByAddress(servers, addr)
		if !ok || !intneo4j.IsEnabled(s.State) {
			t.Fatalf("%s = %#v", addr, s)
		}
	}
}

// topology.minimumMembers is immutable, so a pool scaled below it must still form: the gate was a
// bootstrap bar, not a permanent quorum floor.
func TestReconcileFormsWhenGateExceedsShrunkPool(t *testing.T) {
	neo4j := testClusterCR(3)
	neo4j.Spec.Topology.MinimumMembers = ptr(int32(5))
	if got := systemQuorumFloor(neo4j); got != 3 {
		t.Fatalf("quorum floor = %d, want the pool size 3", got)
	}

	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	replicas := int32(3)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-primary", Namespace: "default"},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&neo4jv1beta1.Neo4j{}).WithObjects(neo4j.DeepCopy(), sts).Build()
	admin := &fakeAdmin{servers: []intneo4j.Server{
		{Name: "s0", Address: "prod-primary-0.default.svc.cluster.local:7687", State: "Enabled"},
		{Name: "s1", Address: "prod-primary-1.default.svc.cluster.local:7687", State: "Enabled"},
		{Name: "s2", Address: "prod-primary-2.default.svc.cluster.local:7687", State: "Enabled"},
	}}
	r := &Reconciler{
		Client: c,
		Connect: func(context.Context, *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
			return admin, nil
		},
	}

	out := r.Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	cond := meta.FindStatusCondition(neo4j.Status.Conditions, ConditionClusterFormed)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("ClusterFormed = %#v, want True (a gate above the pool must not wedge formation)", cond)
	}
}

func TestReconcileDrainsTail(t *testing.T) {
	neo4j := testClusterCR(1)
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	replicas := int32(2)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-primary", Namespace: "default"},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&neo4jv1beta1.Neo4j{}).WithObjects(neo4j.DeepCopy(), sts).Build()

	admin := &fakeAdmin{servers: []intneo4j.Server{
		{Name: "s0", Address: "prod-primary-0.default.svc.cluster.local:7687", State: "Enabled"},
		{Name: "s1", Address: "prod-primary-1.default.svc.cluster.local:7687", State: "Enabled"},
	}}
	r := &Reconciler{
		Client: c,
		Connect: func(context.Context, *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
			return admin, nil
		},
	}

	for i := 0; i < 5; i++ {
		out := r.Reconcile(t.Context(), neo4j)
		if out.Err != nil {
			t.Fatalf("pass %d: %v", i, out.Err)
		}
		if ParseDrainOK(neo4j)["primary"] == 1 {
			break
		}
		if err := c.Get(t.Context(), types.NamespacedName{Name: "prod", Namespace: "default"}, neo4j); err != nil {
			t.Fatal(err)
		}
	}
	if ParseDrainOK(neo4j)["primary"] != 1 {
		t.Fatalf("drain-ok = %v gen=%d", neo4j.Status.DrainOK, neo4j.Status.DrainOKGeneration)
	}
	servers, _ := admin.ShowServers(t.Context())
	if _, ok := intneo4j.FindByAddress(servers, "prod-primary-1.default.svc.cluster.local:7687"); ok {
		t.Fatal("tail server should be dropped")
	}
}

func TestReconcileShrinksTopologyBeforeDrain(t *testing.T) {
	neo4j := testClusterCR(3)
	// defaultPrimariesCount stays 1; scale-in must shrink hosting to pool size (3), not to 1.
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	replicas := int32(5)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-primary", Namespace: "default"},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&neo4jv1beta1.Neo4j{}).WithObjects(neo4j.DeepCopy(), sts).Build()

	admin := &fakeAdmin{
		servers: []intneo4j.Server{
			{Name: "s0", Address: "prod-primary-0.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s1", Address: "prod-primary-1.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s2", Address: "prod-primary-2.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s3", Address: "prod-primary-3.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s4", Address: "prod-primary-4.default.svc.cluster.local:7687", State: "Enabled"},
		},
		dbs: []intneo4j.DatabaseTopology{{
			Name: "neo4j", Type: "standard", HasTopology: true,
			RequestedPrimaries: 5, RequestedSecondaries: 0,
			CurrentPrimaries: 5, CurrentSecondaries: 0,
		}},
	}
	r := &Reconciler{
		Client: c,
		Connect: func(context.Context, *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
			return admin, nil
		},
	}

	out := r.Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	dbs, _ := admin.ShowDatabaseTopologies(t.Context())
	if dbs[0].RequestedPrimaries != 3 {
		t.Fatalf("topology not shrunk: %#v", dbs[0])
	}

	for i := 0; i < 8; i++ {
		out = r.Reconcile(t.Context(), neo4j)
		if out.Err != nil {
			t.Fatalf("pass %d: %v", i, out.Err)
		}
		if ParseDrainOK(neo4j)["primary"] == 3 {
			break
		}
		_ = c.Get(t.Context(), types.NamespacedName{Name: "prod", Namespace: "default"}, neo4j)
	}
	if ParseDrainOK(neo4j)["primary"] != 3 {
		t.Fatalf("drain-ok = %v gen=%d", neo4j.Status.DrainOK, neo4j.Status.DrainOKGeneration)
	}
}

// Shrinking hosting rewrites a topology the user may have set with their own ALTER DATABASE, so it
// must never be silent: one Warning Event under the catalogued reason, naming the database and the
// counts on both sides.
func TestReconcileReportsDatabaseTopologyResize(t *testing.T) {
	neo4j := testClusterCR(3)
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	replicas := int32(5)
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&neo4jv1beta1.Neo4j{}).WithObjects(
		neo4j.DeepCopy(),
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "prod-primary", Namespace: "default"}, Spec: appsv1.StatefulSetSpec{Replicas: &replicas}},
	).Build()

	admin := &fakeAdmin{
		servers: []intneo4j.Server{
			{Name: "s0", Address: "prod-primary-0.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s1", Address: "prod-primary-1.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s2", Address: "prod-primary-2.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s3", Address: "prod-primary-3.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s4", Address: "prod-primary-4.default.svc.cluster.local:7687", State: "Enabled"},
		},
		dbs: []intneo4j.DatabaseTopology{{
			Name: "orders", Type: "standard", HasTopology: true,
			RequestedPrimaries: 5, RequestedSecondaries: 0,
			CurrentPrimaries: 5, CurrentSecondaries: 0,
		}},
	}
	recorder := record.NewFakeRecorder(10)
	r := &Reconciler{
		Client:   c,
		Recorder: recorder,
		Connect: func(context.Context, *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
			return admin, nil
		},
	}

	if out := r.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatal(out.Err)
	}

	event := findEvent(recorder, ReasonDatabaseTopologyResized)
	if event == "" {
		t.Fatalf("no %s Event after the operator shrunk a database topology", ReasonDatabaseTopologyResized)
	}
	if !strings.Contains(event, "Warning") {
		t.Errorf("event %q should be a Warning", event)
	}
	// Which database, from what, to what — enough to audit the rewrite without reading the log.
	for _, want := range []string{"orders", "5 primaries", "to 3"} {
		if !strings.Contains(event, want) {
			t.Errorf("event %q should mention %q", event, want)
		}
	}
}

func TestReconcileNoResizeEventWhenTopologiesAlreadyFit(t *testing.T) {
	neo4j := testClusterCR(3)
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	replicas := int32(3)
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&neo4jv1beta1.Neo4j{}).WithObjects(
		neo4j.DeepCopy(),
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "prod-primary", Namespace: "default"}, Spec: appsv1.StatefulSetSpec{Replicas: &replicas}},
	).Build()

	admin := &fakeAdmin{
		servers: []intneo4j.Server{
			{Name: "s0", Address: "prod-primary-0.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s1", Address: "prod-primary-1.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s2", Address: "prod-primary-2.default.svc.cluster.local:7687", State: "Enabled"},
		},
		// defaultPrimariesCount is unset, so 1 primary is the target and nothing has to move.
		dbs: []intneo4j.DatabaseTopology{{
			Name: "neo4j", Type: "standard", HasTopology: true,
			RequestedPrimaries: 1, CurrentPrimaries: 1,
		}},
	}
	recorder := record.NewFakeRecorder(10)
	r := &Reconciler{
		Client:   c,
		Recorder: recorder,
		Connect: func(context.Context, *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
			return admin, nil
		},
	}

	if out := r.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatal(out.Err)
	}
	if event := findEvent(recorder, ReasonDatabaseTopologyResized); event != "" {
		t.Errorf("unexpected resize Event while every topology already fits: %q", event)
	}
}

// findEvent drains the buffered events and returns the first one carrying reason, "" if none.
func findEvent(recorder *record.FakeRecorder, reason string) string {
	for {
		select {
		case e := <-recorder.Events:
			if strings.Contains(e, reason) {
				return e
			}
		default:
			return ""
		}
	}
}

// Pools wider than a database are not a reason to rewrite it: defaultPrimariesCount and the
// secondary pools decide what a *new* database gets, and the operator carries that intent through
// the DBMS creation defaults instead of ALTER DATABASE (TOPO-006).
func TestReconcileLeavesTopologyAloneWhenPoolsAreWider(t *testing.T) {
	neo4j := testClusterCR(3)
	neo4j.Spec.Topology.DefaultPrimariesCount = ptr(int32(3))
	neo4j.Spec.Topology.Secondaries = &neo4jv1beta1.SecondariesSpec{
		Analytics: &neo4jv1beta1.SecondaryPoolSpec{Members: 1},
		Read:      &neo4jv1beta1.SecondaryPoolSpec{Members: 1},
	}
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	r3, r1 := int32(3), int32(1)
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&neo4jv1beta1.Neo4j{}).WithObjects(
		neo4j.DeepCopy(),
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "prod-primary", Namespace: "default"}, Spec: appsv1.StatefulSetSpec{Replicas: &r3}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "prod-analytics", Namespace: "default"}, Spec: appsv1.StatefulSetSpec{Replicas: &r1}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "prod-read", Namespace: "default"}, Spec: appsv1.StatefulSetSpec{Replicas: &r1}},
	).Build()

	admin := &fakeAdmin{
		servers: []intneo4j.Server{
			{Name: "s0", Address: "prod-primary-0.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s1", Address: "prod-primary-1.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s2", Address: "prod-primary-2.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "a0", Address: "prod-analytics-0.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "r0", Address: "prod-read-0.default.svc.cluster.local:7687", State: "Enabled"},
		},
		dbs: []intneo4j.DatabaseTopology{{
			Name: "neo4j", Type: "standard", HasTopology: true,
			RequestedPrimaries: 1, RequestedSecondaries: 0,
			CurrentPrimaries: 1, CurrentSecondaries: 0,
		}},
	}
	r := &Reconciler{
		Client: c,
		Connect: func(context.Context, *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
			return admin, nil
		},
	}

	out := r.Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if admin.alters != 0 {
		t.Errorf("operator ran %d ALTER DATABASE while no pool shrank", admin.alters)
	}
	if admin.dbs[0].RequestedPrimaries != 1 || admin.dbs[0].RequestedSecondaries != 0 {
		t.Errorf("database rewritten behind its owner's back: %#v", admin.dbs[0])
	}
	// The intent is not lost, it moved to where it belongs: the next CREATE DATABASE.
	if admin.defaultAlloc == nil || *admin.defaultAlloc != [2]int64{3, 2} {
		t.Errorf("creation defaults = %v, want 3 primaries / 2 secondaries", admin.defaultAlloc)
	}
}

// A running cluster must keep honouring the field: initial.dbms.default_*_count is only read when
// the DBMS is initialised, so an edited defaultPrimariesCount reaches Neo4j through the procedure.
func TestReconcileAppliesCreationDefaults(t *testing.T) {
	neo4j := testClusterCR(3)
	neo4j.Spec.Topology.DefaultPrimariesCount = ptr(int32(3))
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	replicas := int32(3)
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&neo4jv1beta1.Neo4j{}).WithObjects(
		neo4j.DeepCopy(),
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "prod-primary", Namespace: "default"}, Spec: appsv1.StatefulSetSpec{Replicas: &replicas}},
	).Build()

	admin := &fakeAdmin{
		servers: []intneo4j.Server{
			{Name: "s0", Address: "prod-primary-0.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s1", Address: "prod-primary-1.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s2", Address: "prod-primary-2.default.svc.cluster.local:7687", State: "Enabled"},
		},
		dbs: []intneo4j.DatabaseTopology{{
			Name: "neo4j", Type: "standard", HasTopology: true,
			RequestedPrimaries: 3, CurrentPrimaries: 3,
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
	if admin.defaultAlloc == nil || *admin.defaultAlloc != [2]int64{3, 0} {
		t.Fatalf("creation defaults = %v, want 3 primaries / 0 secondaries", admin.defaultAlloc)
	}
}

func TestReconcileScaleInLeavesFittingTopologyAlone(t *testing.T) {
	// User ALTER'd neo4j to 3 primaries with defaultPrimariesCount=1; scaling servers 5→3
	// must drain without trying to shrink the DB to 1.
	neo4j := testClusterCR(3)
	neo4j.Spec.Topology.DefaultPrimariesCount = ptr(int32(1))
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	replicas := int32(5)
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&neo4jv1beta1.Neo4j{}).WithObjects(
		neo4j.DeepCopy(),
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "prod-primary", Namespace: "default"}, Spec: appsv1.StatefulSetSpec{Replicas: &replicas}},
	).Build()

	admin := &fakeAdmin{
		servers: []intneo4j.Server{
			{Name: "s0", Address: "prod-primary-0.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s1", Address: "prod-primary-1.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s2", Address: "prod-primary-2.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s3", Address: "prod-primary-3.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s4", Address: "prod-primary-4.default.svc.cluster.local:7687", State: "Enabled"},
		},
		dbs: []intneo4j.DatabaseTopology{{
			Name: "neo4j", Type: "standard", HasTopology: true,
			RequestedPrimaries: 3, RequestedSecondaries: 0,
			CurrentPrimaries: 3, CurrentSecondaries: 0,
		}},
	}
	r := &Reconciler{
		Client: c,
		Connect: func(context.Context, *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
			return admin, nil
		},
	}

	out := r.Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if admin.dbs[0].RequestedPrimaries != 3 {
		t.Fatalf("must not shrink fitting topology toward defaultPrimariesCount: %#v", admin.dbs[0])
	}
	for i := 0; i < 8; i++ {
		out = r.Reconcile(t.Context(), neo4j)
		if out.Err != nil {
			t.Fatalf("pass %d: %v", i, out.Err)
		}
		if ParseDrainOK(neo4j)["primary"] == 3 {
			break
		}
		_ = c.Get(t.Context(), types.NamespacedName{Name: "prod", Namespace: "default"}, neo4j)
	}
	if ParseDrainOK(neo4j)["primary"] != 3 {
		t.Fatalf("expected drain of tails with DB still at 3 primaries; drain-ok=%v gen=%d cond=%v",
			neo4j.Status.DrainOK, neo4j.Status.DrainOKGeneration, neo4j.Status.Conditions)
	}
}

// The narrow topology a user asked for with their own ALTER DATABASE must survive every pass, even
// while defaultPrimariesCount says something wider. Regression guard: the operator used to pull it
// back on every reconcile, undoing the change in a loop.
func TestReconcileKeepsUserTopologyAcrossPasses(t *testing.T) {
	neo4j := testClusterCR(3)
	neo4j.Spec.Topology.DefaultPrimariesCount = ptr(int32(3))
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	replicas := int32(3)
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&neo4jv1beta1.Neo4j{}).WithObjects(
		neo4j.DeepCopy(),
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "prod-primary", Namespace: "default"}, Spec: appsv1.StatefulSetSpec{Replicas: &replicas}},
	).Build()

	admin := &fakeAdmin{
		servers: []intneo4j.Server{
			{Name: "s0", Address: "prod-primary-0.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s1", Address: "prod-primary-1.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s2", Address: "prod-primary-2.default.svc.cluster.local:7687", State: "Enabled"},
		},
		dbs: []intneo4j.DatabaseTopology{{
			Name: "neo4j", Type: "standard", HasTopology: true,
			RequestedPrimaries: 1, RequestedSecondaries: 0,
			CurrentPrimaries: 1, CurrentSecondaries: 0,
		}},
	}
	r := &Reconciler{
		Client: c,
		Connect: func(context.Context, *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
			return admin, nil
		},
	}

	for i := 0; i < 3; i++ {
		if out := r.Reconcile(t.Context(), neo4j); out.Err != nil {
			t.Fatalf("pass %d: %v", i, out.Err)
		}
		if admin.dbs[0].RequestedPrimaries != 1 {
			t.Fatalf("pass %d widened the database to %d primaries", i, admin.dbs[0].RequestedPrimaries)
		}
	}
}

func TestReconcileBlocksMultiPrimaryToOne(t *testing.T) {
	neo4j := testClusterCR(1)
	scheme := runtime.NewScheme()
	_ = neo4jv1beta1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	replicas := int32(3)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-primary", Namespace: "default"},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&neo4jv1beta1.Neo4j{}).WithObjects(neo4j.DeepCopy(), sts).Build()

	admin := &fakeAdmin{
		servers: []intneo4j.Server{
			{Name: "s0", Address: "prod-primary-0.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s1", Address: "prod-primary-1.default.svc.cluster.local:7687", State: "Enabled"},
			{Name: "s2", Address: "prod-primary-2.default.svc.cluster.local:7687", State: "Enabled"},
		},
		dbs: []intneo4j.DatabaseTopology{{
			Name: "neo4j", Type: "standard", HasTopology: true,
			RequestedPrimaries: 3, RequestedSecondaries: 0,
			CurrentPrimaries: 3, CurrentSecondaries: 0,
		}},
	}
	r := &Reconciler{
		Client: c,
		Connect: func(context.Context, *neo4jv1beta1.Neo4j) (intneo4j.Admin, error) {
			return admin, nil
		},
	}

	out := r.Reconcile(t.Context(), neo4j)
	if out.Err != nil {
		t.Fatalf("expected soft requeue, got %v", out.Err)
	}
	if out.Result.RequeueAfter == 0 {
		t.Fatal("expected requeue")
	}
	if admin.dbs[0].RequestedPrimaries != 3 {
		t.Fatal("must not ALTER topology to 1 primary")
	}
}
