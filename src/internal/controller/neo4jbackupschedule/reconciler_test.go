package neo4jbackupschedule

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
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
			Full:     neo4jv1beta1.FullCadence{Schedule: "* * * * *"},
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
	// The chain's full has already Succeeded, so the differential is allowed to attach.
	full := succeededFull("sch-existing-f", "sch-existing")
	r, c := newReconciler(t, enterpriseNeo4j(), full, scheduleCR(func(s *neo4jv1beta1.Neo4jBackupSchedule) {
		s.Spec.Incremental = &neo4jv1beta1.IncrementalCadence{Schedule: "* * * * *"}
		// A full already anchored a chain and just ran, so only the incremental is due now.
		fullTime := metav1.NewTime(time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC))
		s.Spec.Full.Schedule = "0 0 1 1 *" // yearly — not due now
		s.Status.CurrentChain = "sch-existing"
		s.Status.LastFullTime = &fullTime
	}))
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// One incremental in addition to the pre-seeded full fixture.
	all := listBackups(t, c)
	var inc *neo4jv1beta1.Neo4jBackup
	for i := range all {
		if all[i].Spec.Type == neo4jv1beta1.BackupTypeIncremental {
			inc = &all[i]
		}
	}
	if inc == nil {
		t.Fatalf("no incremental emitted; got %+v", all)
	}
	if inc.Labels[LabelChain] != "sch-existing" {
		t.Errorf("chain label = %q, want the current chain sch-existing", inc.Labels[LabelChain])
	}
}

func TestReconcileIncrementalHeldUntilFullSucceeds(t *testing.T) {
	// The chain's full exists but is still Running — a differential now would race it and fail, so
	// the schedule must hold (emit nothing) until the full Succeeds.
	full := succeededFull("sch-existing-f", "sch-existing")
	full.Status.Phase = neo4jv1beta1.RunPhaseRunning
	r, c := newReconciler(t, enterpriseNeo4j(), full, scheduleCR(func(s *neo4jv1beta1.Neo4jBackupSchedule) {
		s.Spec.Incremental = &neo4jv1beta1.IncrementalCadence{Schedule: "* * * * *"}
		fullTime := metav1.NewTime(time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC))
		s.Spec.Full.Schedule = "0 0 1 1 *"
		s.Status.CurrentChain = "sch-existing"
		s.Status.LastFullTime = &fullTime
	}))
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, b := range listBackups(t, c) {
		if b.Spec.Type == neo4jv1beta1.BackupTypeIncremental {
			t.Fatalf("incremental emitted while chain full still Running: %s", b.Name)
		}
	}
}

func TestReconcileIncrementalSkippedWithoutChain(t *testing.T) {
	r, c := newReconciler(t, enterpriseNeo4j(), scheduleCR(func(s *neo4jv1beta1.Neo4jBackupSchedule) {
		s.Spec.Full.Schedule = "0 0 1 1 *" // yearly — no full yet, so no chain
		s.Spec.Incremental = &neo4jv1beta1.IncrementalCadence{Schedule: "* * * * *"}
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

// --- retention pruning (full.retention, BDR-014 §10) ---

func pvcDest() neo4jv1beta1.BackupDestination {
	return neo4jv1beta1.BackupDestination{Type: neo4jv1beta1.BackupDestinationPVC, PVC: &neo4jv1beta1.BackupPVC{ClaimName: "backups"}}
}

func s3Dest() neo4jv1beta1.BackupDestination {
	return neo4jv1beta1.BackupDestination{Type: neo4jv1beta1.BackupDestinationS3, URL: "s3://b/prod/"}
}

// chainBackup builds a Succeeded Neo4jBackup stamped with a schedule's chain labels. paths become
// recorded artifacts (empty paths are skipped, as pvcArtifacts does).
// succeededFull is a chain's anchoring full backup in the Succeeded phase, named <chain>-f so the
// reconciler's chainFullReady Get resolves it.
func succeededFull(name, chain string) *neo4jv1beta1.Neo4jBackup {
	b := chainBackup(name, chain, testNow, pvcDest())
	return &b
}

func chainBackup(name, chain string, created time.Time, dest neo4jv1beta1.BackupDestination, paths ...string) neo4jv1beta1.Neo4jBackup {
	arts := make([]neo4jv1beta1.BackupArtifact, 0, len(paths))
	for _, p := range paths {
		arts = append(arts, neo4jv1beta1.BackupArtifact{Database: "neo4j", Path: p})
	}
	return neo4jv1beta1.Neo4jBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "ns",
			CreationTimestamp: metav1.NewTime(created),
			Labels:            map[string]string{LabelSchedule: "sch", LabelChain: chain, LabelBackupType: "Full"},
		},
		Spec: neo4jv1beta1.Neo4jBackupSpec{
			Neo4jRef:    neo4jv1beta1.Neo4jRef{Name: "g"},
			Databases:   []string{"neo4j"},
			Destination: dest,
			Type:        neo4jv1beta1.BackupTypeFull,
		},
		Status: neo4jv1beta1.Neo4jBackupStatus{Phase: neo4jv1beta1.RunPhaseSucceeded, Chain: chain, Artifacts: arts},
	}
}

func hasEvent(r *ScheduleReconciler, reason string) bool {
	fr := r.Recorder.(*record.FakeRecorder)
	for {
		select {
		case e := <-fr.Events:
			if strings.Contains(e, reason) {
				return true
			}
		default:
			return false
		}
	}
}

func TestExpiredChainsKeepLast(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	items := []neo4jv1beta1.Neo4jBackup{
		chainBackup("c1-f", "c1", base, pvcDest(), "a.backup"),
		chainBackup("c2-f", "c2", base.Add(time.Minute), pvcDest(), "b.backup"),
		chainBackup("c3-f", "c3", base.Add(2*time.Minute), pvcDest(), "c.backup"),
	}
	keep := int32(2)
	// keepLast=2 with c3 active keeps c3 (current) + c2; c1 expires.
	got := expiredChains(groupChains(items), &neo4jv1beta1.BackupRetention{KeepLast: &keep}, "c3", base)
	if len(got) != 1 || got[0] != "c1" {
		t.Fatalf("expired = %v, want [c1]", got)
	}
}

func TestExpiredChainsKeepDays(t *testing.T) {
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	items := []neo4jv1beta1.Neo4jBackup{
		chainBackup("old-f", "old", now.Add(-72*time.Hour), pvcDest(), "a.backup"),
		chainBackup("mid-f", "mid", now.Add(-36*time.Hour), pvcDest(), "b.backup"),
		chainBackup("new-f", "new", now.Add(-1*time.Hour), pvcDest(), "c.backup"),
	}
	keep := int32(2) // keep chains younger than 2 days
	got := expiredChains(groupChains(items), &neo4jv1beta1.BackupRetention{KeepDays: &keep}, "new", now)
	if len(got) != 1 || got[0] != "old" {
		t.Fatalf("expired = %v, want [old] (mid is 1.5d; new is current)", got)
	}
}

func TestExpiredChainsNeverPrunesCurrent(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	items := []neo4jv1beta1.Neo4jBackup{
		chainBackup("c1-f", "c1", base, pvcDest(), "a.backup"),
		chainBackup("c2-f", "c2", base.Add(time.Minute), pvcDest(), "b.backup"),
	}
	keep := int32(1)
	// c1 is the oldest but is the active chain — it must survive even under keepLast=1.
	got := expiredChains(groupChains(items), &neo4jv1beta1.BackupRetention{KeepLast: &keep}, "c1", base)
	for _, id := range got {
		if id == "c1" {
			t.Fatalf("active chain c1 must never be pruned; got %v", got)
		}
	}
	if len(got) != 1 || got[0] != "c2" {
		t.Fatalf("expired = %v, want [c2]", got)
	}
}

func TestPvcArtifactsAndObjectStoreDetection(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	p := chainBackup("p1", "c", base, pvcDest(), "x.backup", "y.backup")
	claim, files, os := pvcArtifacts([]*neo4jv1beta1.Neo4jBackup{&p})
	if os || claim != "backups" || len(files) != 2 {
		t.Fatalf("pvcArtifacts = (%q, %v, %v); want (backups, [x y], false)", claim, files, os)
	}
	s := chainBackup("s1", "c", base, s3Dest())
	if _, _, os := pvcArtifacts([]*neo4jv1beta1.Neo4jBackup{&s}); !os {
		t.Error("object-store backup must report objectStore=true")
	}
}

func TestReconcilePrunesExpiredPVCChainViaJob(t *testing.T) {
	keep := int32(1)
	old := chainBackup("old-f", "old", creation.Add(-time.Hour), pvcDest(), "neo4j-old.backup")
	sched := scheduleCR(func(s *neo4jv1beta1.Neo4jBackupSchedule) {
		s.Spec.Full.Retention = &neo4jv1beta1.BackupRetention{KeepLast: &keep}
	})
	r, c := newReconciler(t, enterpriseNeo4j(), sched, &old)

	res, err := r.Reconcile(context.Background(), req())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// The prune Job for the expired chain is created (owned by the schedule)…
	var job batchv1.Job
	if err := c.Get(context.Background(), types.NamespacedName{Name: "prune-old", Namespace: "ns"}, &job); err != nil {
		t.Fatalf("expected prune Job prune-old: %v", err)
	}
	// …and the records survive until it completes, so no file is ever orphaned.
	if err := c.Get(context.Background(), types.NamespacedName{Name: "old-f", Namespace: "ns"}, &neo4jv1beta1.Neo4jBackup{}); err != nil {
		t.Fatalf("old backup must not be deleted before the prune Job completes: %v", err)
	}
	if res.RequeueAfter <= 0 || res.RequeueAfter > 15*time.Second {
		t.Errorf("requeue = %v, want a short poll (<=15s) while the prune Job runs", res.RequeueAfter)
	}
}

func TestReconcilePruneObjectStoreUnsupported(t *testing.T) {
	keep := int32(1)
	old := chainBackup("old-f", "old", creation.Add(-time.Hour), s3Dest())
	sched := scheduleCR(func(s *neo4jv1beta1.Neo4jBackupSchedule) {
		s.Spec.BackupTemplate.Destination = s3Dest()
		s.Spec.Full.Retention = &neo4jv1beta1.BackupRetention{KeepLast: &keep}
	})
	r, c := newReconciler(t, enterpriseNeo4j(), sched, &old)

	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// Object-store chains are kept (deleting the record would orphan the bucket objects)…
	if err := c.Get(context.Background(), types.NamespacedName{Name: "old-f", Namespace: "ns"}, &neo4jv1beta1.Neo4jBackup{}); err != nil {
		t.Fatalf("object-store chain must be kept until ADR-016: %v", err)
	}
	// …and the operator says so.
	if !hasEvent(r, "SchedulePruneUnsupported") {
		t.Error("expected a SchedulePruneUnsupported event for the object-store chain")
	}
}

// --- aggregate compaction (aggregate.enabled, BDR-014 §10) ---

func chainInc(name, chain string, created time.Time, dest neo4jv1beta1.BackupDestination, paths ...string) neo4jv1beta1.Neo4jBackup {
	b := chainBackup(name, chain, created, dest, paths...)
	b.Spec.Type = neo4jv1beta1.BackupTypeIncremental
	b.Labels[LabelBackupType] = "Incremental"
	return b
}

func chainAgg(name, chain, source string, created time.Time, dest neo4jv1beta1.BackupDestination, phase neo4jv1beta1.RunPhase, paths ...string) neo4jv1beta1.Neo4jBackup {
	b := chainBackup(name, chain, created, dest, paths...)
	b.Spec.Type = neo4jv1beta1.BackupTypeAggregate
	b.Spec.Source = &neo4jv1beta1.BackupSource{BackupRef: source}
	b.Labels[LabelBackupType] = "Aggregate"
	b.Status.Phase = phase
	return b
}

func completeJob(name string) *batchv1.Job {
	controlled := true
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "ns",
			// Controlled by the schedule (UID "" matches the fixture) so Apply adopts it (NEO-002).
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "neo4j.com/v1beta1", Kind: "Neo4jBackupSchedule", Name: "sch", Controller: &controlled,
			}},
		},
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
		}},
	}
}

// aggregateSchedule is a schedule with the yearly full/incremental crons (nothing emits during the
// test) and aggregate compaction enabled, pinned to an active chain so closed chains can compact.
func aggregateSchedule() *neo4jv1beta1.Neo4jBackupSchedule {
	return scheduleCR(func(s *neo4jv1beta1.Neo4jBackupSchedule) {
		s.Spec.Full.Schedule = "0 0 1 1 *"
		s.Spec.Aggregate = &neo4jv1beta1.AggregateCadence{Enabled: true}
		s.Status.CurrentChain = "cur"
	})
}

func TestReconcileCompactionEmitsAggregateForQuiescedChain(t *testing.T) {
	t0 := creation.Add(-10 * time.Minute)
	full := chainBackup("old-f", "old", t0, pvcDest(), "old/neo4j-f.backup")
	inc := chainInc("old-1-i", "old", t0.Add(time.Minute), pvcDest(), "old/neo4j-i.backup")
	r, c := newReconciler(t, enterpriseNeo4j(), aggregateSchedule(), &full, &inc)

	res, err := r.Reconcile(context.Background(), req())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var agg *neo4jv1beta1.Neo4jBackup
	for _, b := range listBackups(t, c) {
		if b.Spec.Type == neo4jv1beta1.BackupTypeAggregate {
			bb := b
			agg = &bb
		}
	}
	if agg == nil {
		t.Fatalf("no aggregate emitted for the closed chain")
	}
	if agg.Name != "old-agg" {
		t.Errorf("name = %q, want old-agg", agg.Name)
	}
	if agg.Spec.Source == nil || agg.Spec.Source.BackupRef != "old-1-i" {
		t.Errorf("source = %+v, want backupRef=old-1-i (the tip)", agg.Spec.Source)
	}
	if agg.Labels[LabelChain] != "old" {
		t.Errorf("chain label = %q, want old", agg.Labels[LabelChain])
	}
	if res.RequeueAfter <= 0 || res.RequeueAfter > 15*time.Second {
		t.Errorf("requeue = %v, want a short poll while the aggregate Job runs", res.RequeueAfter)
	}
}

func TestReconcileCompactionSkipsLoneFull(t *testing.T) {
	// A chain with no incremental has nothing to collapse — no aggregate should be emitted.
	full := chainBackup("old-f", "old", creation.Add(-10*time.Minute), pvcDest(), "old/neo4j-f.backup")
	r, c := newReconciler(t, enterpriseNeo4j(), aggregateSchedule(), &full)
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, b := range listBackups(t, c) {
		if b.Spec.Type == neo4jv1beta1.BackupTypeAggregate {
			t.Fatalf("aggregate emitted for a lone full: %s", b.Name)
		}
	}
}

func TestReconcileCompactionHeldUntilChainQuiesces(t *testing.T) {
	// A still-running link means the chain hasn't quiesced; aggregating now would race it.
	t0 := creation.Add(-10 * time.Minute)
	full := chainBackup("old-f", "old", t0, pvcDest(), "old/neo4j-f.backup")
	inc := chainInc("old-1-i", "old", t0.Add(time.Minute), pvcDest(), "old/neo4j-i.backup")
	inc.Status.Phase = neo4jv1beta1.RunPhaseRunning
	r, c := newReconciler(t, enterpriseNeo4j(), aggregateSchedule(), &full, &inc)
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, b := range listBackups(t, c) {
		if b.Spec.Type == neo4jv1beta1.BackupTypeAggregate {
			t.Fatalf("aggregate emitted before the chain quiesced: %s", b.Name)
		}
	}
}

func TestReconcileCompactionPrunesLinksAfterAggregateSucceeds(t *testing.T) {
	t0 := creation.Add(-10 * time.Minute)
	full := chainBackup("old-f", "old", t0, pvcDest(), "old/neo4j-f.backup")
	inc := chainInc("old-1-i", "old", t0.Add(time.Minute), pvcDest(), "old/neo4j-i.backup")
	agg := chainAgg("old-agg", "old", "old-1-i", t0.Add(2*time.Minute), pvcDest(), neo4jv1beta1.RunPhaseSucceeded, "old/neo4j-agg.backup")
	r, c := newReconciler(t, enterpriseNeo4j(), aggregateSchedule(), &full, &inc, &agg)

	res, err := r.Reconcile(context.Background(), req())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// A compaction prune Job (distinct from expiry's prune-<chain>) targets the link files…
	var job batchv1.Job
	if err := c.Get(context.Background(), types.NamespacedName{Name: "compact-old", Namespace: "ns"}, &job); err != nil {
		t.Fatalf("expected compaction Job compact-old: %v", err)
	}
	// …the recovered full is never in the delete set.
	script := job.Spec.Template.Spec.Containers[0].Command[2]
	if strings.Contains(script, "neo4j-agg.backup") {
		t.Errorf("compaction must not delete the recovered full; script = %q", script)
	}
	if !strings.Contains(script, "neo4j-f.backup") || !strings.Contains(script, "neo4j-i.backup") {
		t.Errorf("compaction must delete the original links; script = %q", script)
	}
	// Records survive until the Job completes (files-before-records), and the aggregate is kept.
	for _, name := range []string{"old-f", "old-1-i", "old-agg"} {
		if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "ns"}, &neo4jv1beta1.Neo4jBackup{}); err != nil {
			t.Fatalf("%s must survive until the compaction Job completes: %v", name, err)
		}
	}
	if res.RequeueAfter <= 0 || res.RequeueAfter > 15*time.Second {
		t.Errorf("requeue = %v, want a short poll while the compaction Job runs", res.RequeueAfter)
	}
}

func TestReconcileCompactionCompletesKeepingRecoveredFull(t *testing.T) {
	t0 := creation.Add(-10 * time.Minute)
	full := chainBackup("old-f", "old", t0, pvcDest(), "old/neo4j-f.backup")
	inc := chainInc("old-1-i", "old", t0.Add(time.Minute), pvcDest(), "old/neo4j-i.backup")
	agg := chainAgg("old-agg", "old", "old-1-i", t0.Add(2*time.Minute), pvcDest(), neo4jv1beta1.RunPhaseSucceeded, "old/neo4j-agg.backup")
	r, c := newReconciler(t, enterpriseNeo4j(), aggregateSchedule(), &full, &inc, &agg, completeJob("compact-old"))

	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// Links are gone…
	for _, name := range []string{"old-f", "old-1-i"} {
		err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "ns"}, &neo4jv1beta1.Neo4jBackup{})
		if err == nil {
			t.Errorf("link %s must be deleted once the compaction Job completed", name)
		}
	}
	// …the recovered full is kept.
	if err := c.Get(context.Background(), types.NamespacedName{Name: "old-agg", Namespace: "ns"}, &neo4jv1beta1.Neo4jBackup{}); err != nil {
		t.Fatalf("recovered full old-agg must be kept: %v", err)
	}
	if !hasEvent(r, "ScheduleCompacted") {
		t.Error("expected a ScheduleCompacted event")
	}
}

func TestReconcileCompactionDisabledIsNoop(t *testing.T) {
	// aggregate is nil (default) — a closed chain with an incremental is left untouched.
	t0 := creation.Add(-10 * time.Minute)
	full := chainBackup("old-f", "old", t0, pvcDest(), "old/neo4j-f.backup")
	inc := chainInc("old-1-i", "old", t0.Add(time.Minute), pvcDest(), "old/neo4j-i.backup")
	sched := scheduleCR(func(s *neo4jv1beta1.Neo4jBackupSchedule) {
		s.Spec.Full.Schedule = "0 0 1 1 *"
		s.Status.CurrentChain = "cur"
	})
	r, c := newReconciler(t, enterpriseNeo4j(), sched, &full, &inc)
	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n := len(listBackups(t, c)); n != 2 {
		t.Errorf("backups = %d, want 2 (no aggregate when compaction disabled)", n)
	}
}

func TestExpiryDropsCompactedChainWholesale(t *testing.T) {
	// A compacted chain is just its recovered full; when it expires, expiry must delete that
	// aggregate record and its file (aggregate-aware retention, for free via grouping).
	keep := int32(1)
	agg := chainAgg("old-agg", "old", "old-1-i", creation.Add(-time.Hour), pvcDest(), neo4jv1beta1.RunPhaseSucceeded, "old/neo4j-agg.backup")
	sched := scheduleCR(func(s *neo4jv1beta1.Neo4jBackupSchedule) {
		s.Spec.Full.Retention = &neo4jv1beta1.BackupRetention{KeepLast: &keep}
	})
	r, c := newReconciler(t, enterpriseNeo4j(), sched, &agg)

	if _, err := r.Reconcile(context.Background(), req()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// Expiry prunes the compacted chain via a Job over its recovered-full file.
	var job batchv1.Job
	if err := c.Get(context.Background(), types.NamespacedName{Name: "prune-old", Namespace: "ns"}, &job); err != nil {
		t.Fatalf("expected expiry Job prune-old for the compacted chain: %v", err)
	}
	script := job.Spec.Template.Spec.Containers[0].Command[2]
	if !strings.Contains(script, "neo4j-agg.backup") {
		t.Errorf("expiry must delete the recovered full of a compacted chain; script = %q", script)
	}
}
