package neo4jbackupschedule

import (
	"context"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

var (
	testNow  = time.Date(2026, 9, 2, 12, 0, 30, 0, time.UTC)
	creation = time.Date(2026, 9, 2, 11, 59, 0, 0, time.UTC)
)

func scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := neo4jv1beta1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func enterpriseNeo4j() *neo4jv1beta1.Neo4j {
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "g", Namespace: "ns"},
		Spec:       neo4jv1beta1.Neo4jSpec{Edition: neo4jv1beta1.EditionEnterprise},
	}
}

func scheduleCR(mut func(*neo4jv1beta1.Neo4jBackupSchedule)) *neo4jv1beta1.Neo4jBackupSchedule {
	s := &neo4jv1beta1.Neo4jBackupSchedule{
		ObjectMeta: metav1.ObjectMeta{Name: "sch", Namespace: "ns", CreationTimestamp: metav1.NewTime(creation)},
		Spec: neo4jv1beta1.Neo4jBackupScheduleSpec{
			Neo4jRef: neo4jv1beta1.Neo4jRef{Name: "g"},
			Full:     neo4jv1beta1.BackupCadence{Schedule: "* * * * *"},
			BackupTemplate: neo4jv1beta1.BackupTemplate{
				Databases: []string{"neo4j"},
				Destination: neo4jv1beta1.BackupDestination{
					Type: neo4jv1beta1.BackupDestinationPVC,
					PVC:  &neo4jv1beta1.BackupPVC{ClaimName: "backups"},
				},
			},
		},
	}
	if mut != nil {
		mut(s)
	}
	return s
}

func newReconciler(t *testing.T, objs ...client.Object) (*ScheduleReconciler, client.Client) {
	t.Helper()
	s := scheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).
		WithStatusSubresource(&neo4jv1beta1.Neo4jBackupSchedule{}).Build()
	return &ScheduleReconciler{
		Client: c, Scheme: s, Recorder: record.NewFakeRecorder(16),
		Now: func() time.Time { return testNow },
	}, c
}

func req() ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: "sch", Namespace: "ns"}}
}

func listBackups(t *testing.T, c client.Client) []neo4jv1beta1.Neo4jBackup {
	t.Helper()
	var l neo4jv1beta1.Neo4jBackupList
	if err := c.List(context.Background(), &l, client.InNamespace("ns")); err != nil {
		t.Fatalf("list backups: %v", err)
	}
	return l.Items
}

func getSchedule(t *testing.T, c client.Client) *neo4jv1beta1.Neo4jBackupSchedule {
	t.Helper()
	var s neo4jv1beta1.Neo4jBackupSchedule
	if err := c.Get(context.Background(), req().NamespacedName, &s); err != nil {
		t.Fatalf("get schedule: %v", err)
	}
	return &s
}

func TestDue(t *testing.T) {
	sched, err := cron.ParseStandard("* * * * *")
	if err != nil {
		t.Fatal(err)
	}
	// Owed: the 12:00 tick after the 11:59 base has passed by 12:00:30; collapses to the most
	// recent tick (12:00, not a burst of every missed minute).
	if fire, at := due(sched, creation, testNow); !fire || !at.Equal(time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("due = %v,%v; want fire at 12:00:00", fire, at)
	}
	// Not owed: base is now, next tick is in the future.
	if fire, _ := due(sched, testNow, testNow); fire {
		t.Error("must not fire when the next tick is in the future")
	}
}

func TestReconcileEmitsFullAndTracksChain(t *testing.T) {
	r, c := newReconciler(t, enterpriseNeo4j(), scheduleCR(nil))
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	backups := listBackups(t, c)
	if len(backups) != 1 {
		t.Fatalf("emitted %d backups, want 1", len(backups))
	}
	b := backups[0]
	if b.Spec.Type != neo4jv1beta1.BackupTypeFull {
		t.Errorf("type = %q, want Full", b.Spec.Type)
	}
	if b.Name != "sch-20260902-1200-f" {
		t.Errorf("name = %q, want sch-20260902-1200-f", b.Name)
	}
	wantChain := "sch-20260902-1200"
	if b.Labels[LabelChain] != wantChain || b.Labels[LabelBackupType] != "Full" || b.Labels[LabelSchedule] != "sch" {
		t.Errorf("labels = %v; want chain=%s type=Full schedule=sch", b.Labels, wantChain)
	}
	if len(b.OwnerReferences) != 1 || b.OwnerReferences[0].Name != "sch" {
		t.Errorf("owner refs = %v; want controlled by sch", b.OwnerReferences)
	}
	got := getSchedule(t, c)
	if got.Status.CurrentChain != wantChain || got.Status.LastFullTime == nil || got.Status.LastBackup != b.Name {
		t.Errorf("status = %+v; want currentChain=%s, lastFullTime set, lastBackup=%s", got.Status, wantChain, b.Name)
	}
}

func TestReconcileSuspendEmitsNothing(t *testing.T) {
	r, c := newReconciler(t, enterpriseNeo4j(), scheduleCR(func(s *neo4jv1beta1.Neo4jBackupSchedule) {
		s.Spec.Suspend = true
	}))
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n := len(listBackups(t, c)); n != 0 {
		t.Errorf("emitted %d backups while suspended, want 0", n)
	}
	got := getSchedule(t, c)
	if !got.Status.Suspended {
		t.Error("status.Suspended must be true")
	}
	if cond := got.Status.Conditions[0]; cond.Reason != "ScheduleSuspended" {
		t.Errorf("reason = %q, want ScheduleSuspended", cond.Reason)
	}
}

func TestReconcileIncrementalAttachesToCurrentChain(t *testing.T) {
	r, c := newReconciler(t, enterpriseNeo4j(), scheduleCR(func(s *neo4jv1beta1.Neo4jBackupSchedule) {
		s.Spec.Incremental = &neo4jv1beta1.BackupCadence{Schedule: "* * * * *"}
		// A full already anchored a chain and just ran, so only the incremental is due now.
		full := metav1.NewTime(time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC))
		s.Spec.Full.Schedule = "0 0 1 1 *" // yearly — not due now
		s.Status.CurrentChain = "sch-existing"
		s.Status.LastFullTime = &full
	}))
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	backups := listBackups(t, c)
	if len(backups) != 1 {
		t.Fatalf("emitted %d backups, want 1 (incremental only)", len(backups))
	}
	b := backups[0]
	if b.Spec.Type != neo4jv1beta1.BackupTypeIncremental {
		t.Errorf("type = %q, want Incremental", b.Spec.Type)
	}
	if b.Labels[LabelChain] != "sch-existing" {
		t.Errorf("chain label = %q, want the current chain sch-existing", b.Labels[LabelChain])
	}
}

func TestReconcileIncrementalSkippedWithoutChain(t *testing.T) {
	r, c := newReconciler(t, enterpriseNeo4j(), scheduleCR(func(s *neo4jv1beta1.Neo4jBackupSchedule) {
		s.Spec.Full.Schedule = "0 0 1 1 *" // yearly — no full yet, so no chain
		s.Spec.Incremental = &neo4jv1beta1.BackupCadence{Schedule: "* * * * *"}
	}))
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n := len(listBackups(t, c)); n != 0 {
		t.Errorf("emitted %d backups, want 0 (incremental must wait for a chain)", n)
	}
}

func TestReconcileInvalidCronFails(t *testing.T) {
	r, c := newReconciler(t, enterpriseNeo4j(), scheduleCR(func(s *neo4jv1beta1.Neo4jBackupSchedule) {
		s.Spec.Full.Schedule = "not a cron"
	}))
	res, err := r.Reconcile(context.Background(), req())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Error("invalid cron is terminal until spec is fixed; must not requeue")
	}
	if cond := getSchedule(t, c).Status.Conditions[0]; cond.Reason != "ScheduleInvalidCron" {
		t.Errorf("reason = %q, want ScheduleInvalidCron", cond.Reason)
	}
}

func TestReconcileNotDueRequeues(t *testing.T) {
	r, c := newReconciler(t, enterpriseNeo4j(), scheduleCR(func(s *neo4jv1beta1.Neo4jBackupSchedule) {
		s.Spec.Full.Schedule = "0 0 1 1 *" // yearly, far off
	}))
	res, err := r.Reconcile(context.Background(), req())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n := len(listBackups(t, c)); n != 0 {
		t.Errorf("emitted %d backups, want 0", n)
	}
	if res.RequeueAfter <= 0 {
		t.Error("expected a positive requeue toward the next tick")
	}
	if cond := getSchedule(t, c).Status.Conditions[0]; cond.Reason != "ScheduleActive" {
		t.Errorf("reason = %q, want ScheduleActive", cond.Reason)
	}
}

func TestReconcileIdempotentWithinTick(t *testing.T) {
	r, c := newReconciler(t, enterpriseNeo4j(), scheduleCR(nil))
	for i := 0; i < 3; i++ {
		if _, err := r.Reconcile(context.Background(), req()); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if n := len(listBackups(t, c)); n != 1 {
		t.Errorf("emitted %d backups across 3 reconciles at the same instant, want 1", n)
	}
}
