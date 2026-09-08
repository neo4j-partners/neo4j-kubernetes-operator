package upgrade

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

var t0 = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// converged is a pool that has finished: on the target image, every pod updated and ready.
func converged(n int32) PoolState {
	return PoolState{Desired: n, Updated: n, Ready: n, OnTarget: true, Settled: true}
}

func TestObserve(t *testing.T) {
	cases := []struct {
		name         string
		running      string
		pools        []PoolState
		wantNil      bool
		wantPhase    neo4jv1beta1.UpgradePhase
		wantUpgraded int32
		wantPending  int32
	}{
		{
			name: "first install reports no upgrade", running: "",
			pools: []PoolState{converged(3)}, wantNil: true,
		},
		{
			name: "steady state on the target version", running: "2026.07.0",
			pools: []PoolState{converged(3)}, wantNil: true,
		},
		{
			name: "everything converged onto the new image", running: "2026.05.0",
			pools: []PoolState{converged(3), converged(2)}, wantNil: true,
		},
		{
			name: "pods still moving", running: "2026.05.0",
			pools:     []PoolState{{Desired: 3, Updated: 1, Ready: 2, Rolling: true, OnTarget: true, Settled: true}},
			wantPhase: neo4jv1beta1.UpgradePhaseRolling, wantUpgraded: 1, wantPending: 2,
		},
		{
			name: "a held pool counts as pending however ready it is", running: "2026.05.0",
			pools: []PoolState{
				converged(2), // secondaries done
				{Desired: 3, Updated: 3, Ready: 3, OnTarget: false, Settled: true}, // primaries held back
			},
			wantPhase: neo4jv1beta1.UpgradePhaseRolling, wantUpgraded: 2, wantPending: 3,
		},
		{
			name: "on the new image but a member has not come back", running: "2026.05.0",
			pools:     []PoolState{{Desired: 3, Updated: 3, Ready: 2, OnTarget: true, Settled: true}},
			wantPhase: neo4jv1beta1.UpgradePhaseRolling, wantUpgraded: 2, wantPending: 1,
		},
		{
			// The window the Settled flag closes: the template has changed but the StatefulSet
			// controller has not caught up, so its revisions still describe the old spec and every
			// replica still counts as updated and ready. Without this the operator would call an
			// upgrade finished before a single pod had restarted.
			name: "template changed but the StatefulSet has not caught up", running: "2026.05.0",
			pools:     []PoolState{{Desired: 3, Updated: 3, Ready: 3, OnTarget: true, Settled: false}},
			wantPhase: neo4jv1beta1.UpgradePhaseVerifying, wantUpgraded: 3, wantPending: 0,
		},
		{
			name: "updated and ready, waiting on the cluster", running: "2026.05.0",
			pools:     []PoolState{{Desired: 3, Updated: 3, Ready: 3, Rolling: true, OnTarget: true, Settled: true}},
			wantPhase: neo4jv1beta1.UpgradePhaseVerifying, wantUpgraded: 3, wantPending: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := cr(tc.running, "2026.07.0")
			got := Observe(n, tc.pools, t0)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected no upgrade in flight, got phase %q", got.Phase)
				}
				return
			}
			if got == nil {
				t.Fatal("expected an upgrade in flight, got nil")
			}
			if got.Phase != tc.wantPhase {
				t.Errorf("phase = %q, want %q", got.Phase, tc.wantPhase)
			}
			if got.TargetVersion != "2026.07.0" || got.PreviousVersion != tc.running {
				t.Errorf("versions = %s -> %s, want %s -> 2026.07.0", got.PreviousVersion, got.TargetVersion, tc.running)
			}
			if got.Progress == nil {
				t.Fatal("progress must always be reported while an upgrade is in flight")
			}
			if got.Progress.Upgraded != tc.wantUpgraded || got.Progress.Pending != tc.wantPending {
				t.Errorf("progress = %d upgraded / %d pending, want %d / %d",
					got.Progress.Upgraded, got.Progress.Pending, tc.wantUpgraded, tc.wantPending)
			}
		})
	}
}

// The clock measures the current step, so progress restarts it and a stalled step does not.
func TestObserveStepStartTime(t *testing.T) {
	n := cr("2026.05.0", "2026.07.0")
	rolling := []PoolState{{Desired: 3, Updated: 1, Ready: 2, Rolling: true, OnTarget: true, Settled: true}}

	first := Observe(n, rolling, t0)
	if first.StepStartTime == nil {
		t.Fatal("a new step must stamp stepStartTime")
	}

	// Same step a minute later: the clock keeps running.
	n.Status.Upgrade = first
	same := Observe(n, rolling, t0.Add(time.Minute))
	if !same.StepStartTime.Time.Equal(first.StepStartTime.Time) {
		t.Errorf("stepStartTime moved while the step was unchanged: %s -> %s", first.StepStartTime, same.StepStartTime)
	}

	// A member finishes: forward progress restarts the clock, because a slow upgrade is not a stuck one.
	n.Status.Upgrade = same
	moved := Observe(n, []PoolState{{Desired: 3, Updated: 2, Ready: 2, Rolling: true, OnTarget: true, Settled: true}}, t0.Add(2*time.Minute))
	if moved.StepStartTime.Time.Equal(first.StepStartTime.Time) {
		t.Error("stepStartTime must restart when a member is upgraded")
	}

	// Going backwards is not progress, so it must not restart the clock.
	n.Status.Upgrade = moved
	back := Observe(n, []PoolState{{Desired: 3, Updated: 1, Ready: 1, Rolling: true, OnTarget: true, Settled: true}}, t0.Add(3*time.Minute))
	if !back.StepStartTime.Time.Equal(moved.StepStartTime.Time) {
		t.Error("a member count that fell back must not restart the clock — that is churn, not progress")
	}
}

// A pod that cannot start is never counted as upgraded, so the clock runs and the upgrade reaches
// its deadline. Counting pods created at the new revision instead would let a kubelet recreating a
// failing pod on a backoff look like progress forever.
func TestAPodThatNeverStartsIsNotProgress(t *testing.T) {
	n := cr("2026.05.0", "2026.99.0")
	stuck := []PoolState{{Desired: 1, Updated: 1, Ready: 0, OnTarget: true, Settled: true}}

	first := Observe(n, stuck, t0)
	if got := first.Progress.Upgraded; got != 0 {
		t.Fatalf("progress.upgraded = %d for a pod that never became ready, want 0", got)
	}
	n.Status.Upgrade = first
	later := Observe(n, stuck, t0.Add(time.Minute))
	if !later.StepStartTime.Time.Equal(first.StepStartTime.Time) {
		t.Fatal("the clock restarted while nothing got better")
	}
	n.Status.Upgrade = later
	expired := Observe(n, stuck, t0.Add(MemberBudget(n)+time.Minute))
	if expired.Phase != neo4jv1beta1.UpgradePhaseFailed {
		t.Errorf("phase = %q after the budget expired on a pod that never started, want Failed", expired.Phase)
	}
}

func TestObserveFailsWhenTheStepOutlastsItsBudget(t *testing.T) {
	n := cr("2026.05.0", "2026.07.0")
	rolling := []PoolState{{Desired: 3, Updated: 1, Ready: 2, Rolling: true, OnTarget: true, Settled: true}}

	first := Observe(n, rolling, t0)
	n.Status.Upgrade = first

	within := Observe(n, rolling, t0.Add(MemberBudget(n)-time.Minute))
	if within.Phase == neo4jv1beta1.UpgradePhaseFailed {
		t.Error("a step inside its budget must not be reported as failed")
	}

	over := Observe(n, rolling, t0.Add(MemberBudget(n)+time.Minute))
	if over.Phase != neo4jv1beta1.UpgradePhaseFailed {
		t.Fatalf("phase = %q, want Failed once the step outlasts its budget", over.Phase)
	}
	if over.LastError == "" {
		t.Error("a failed upgrade must say why")
	}
}

// The budget is derived from the probes this CR actually has: the repo's own example ships a
// 90-second startup budget, and an upgrade under it must not be held to the default's 83 minutes.
func TestMemberBudgetFollowsTheEffectiveProbe(t *testing.T) {
	n := cr("2026.05.0", "2026.07.0")
	def := MemberBudget(n)
	if want := (1000*5 + 3600) * time.Second; def != want {
		t.Errorf("default budget = %s, want %s", def, want)
	}

	n.Spec.Probes = &neo4jv1beta1.ProbesSpec{
		Startup: &corev1.Probe{FailureThreshold: 9, PeriodSeconds: 10},
	}
	grace := int64(30)
	n.Spec.Scheduling = &neo4jv1beta1.SchedulingSpec{TerminationGracePeriodSeconds: &grace}
	if got, want := MemberBudget(n), (9*10+30)*time.Second; got != want {
		t.Errorf("overridden budget = %s, want %s", got, want)
	}
}

func TestObserveHandlesAnEmptyPoolList(t *testing.T) {
	n := cr("2026.05.0", "2026.07.0")
	if got := Observe(n, nil, t0); got != nil {
		t.Errorf("with no StatefulSets observed there is nothing to report, got phase %q", got.Phase)
	}
}

// Progress counts must survive a round trip through the API types the CRD publishes.
func TestObserveProgressIsDeepCopyable(t *testing.T) {
	n := cr("2026.05.0", "2026.07.0")
	got := Observe(n, []PoolState{{Desired: 3, Updated: 1, Ready: 2, Rolling: true, OnTarget: true, Settled: true}}, t0)
	clone := got.DeepCopy()
	if clone.Progress.Upgraded != got.Progress.Upgraded || !clone.StepStartTime.Equal(got.StepStartTime) {
		t.Errorf("deep copy lost fields: %+v vs %+v", clone, got)
	}
	var _ metav1.Time = *clone.StepStartTime
}
