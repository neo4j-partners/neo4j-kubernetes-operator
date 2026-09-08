/*
Copyright Neo4j.

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

package upgrade

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

// PoolState is one pool's rollout as the status writer observes it. The writer already reads every
// pool's StatefulSet, so this rides those reads rather than adding API calls.
type PoolState struct {
	// Desired is the member count the spec asks of this pool.
	Desired int32
	// Updated is the pods already on the StatefulSet's update revision.
	Updated int32
	// Ready is the pods passing their readiness probe.
	Ready int32
	// Rolling reports currentRevision != updateRevision — Kubernetes is still moving this pool.
	Rolling bool
	// OnTarget reports whether the StatefulSet's pod template already carries the target image. A
	// pool held back for ordering has not been given it yet, so its pods count as pending however
	// ready they are.
	OnTarget bool
}

// Observe derives status.upgrade from the pool StatefulSets. It needs no Bolt connection: what it
// reports is the movement of pods onto a new image, not the state of Neo4j.
//
// A nil result means there is nothing in flight — the caller clears status.upgrade. The finished
// record of an upgrade is status.version plus status.lastUpgradeTime, not a Completed block left
// lying around, which is the same choice formation makes when it removes conditions that no longer
// apply.
func Observe(n *neo4jv1beta1.Neo4j, pools []PoolState, now time.Time) *neo4jv1beta1.UpgradeStatus {
	target, running := n.Spec.Version, n.Status.Version
	if running == "" || running == target || len(pools) == 0 {
		return nil // first install, or nothing to do
	}

	var total, upgraded, ready int32
	converged := true
	for _, p := range pools {
		total += p.Desired
		ready += p.Ready
		if p.OnTarget {
			upgraded += p.Updated
		} else {
			converged = false // this pool has not even been given the new template yet
		}
		if p.Rolling {
			converged = false
		}
	}
	if upgraded < total || ready < total {
		converged = false
	}
	if converged {
		return nil // the caller advances status.version and stamps lastUpgradeTime
	}

	out := &neo4jv1beta1.UpgradeStatus{
		TargetVersion:   target,
		PreviousVersion: running,
		Progress:        &neo4jv1beta1.UpgradeProgress{Total: total, Upgraded: upgraded, Pending: total - upgraded},
	}

	switch {
	case upgraded < total:
		out.Phase = neo4jv1beta1.UpgradePhaseRolling
	case ready < total:
		out.Phase = neo4jv1beta1.UpgradePhaseStabilizing
	default:
		// Every member is on the new image and ready. Whether the cluster re-formed is the caller's
		// to judge — it holds the ClusterFormed condition formation wrote in this same pass.
		out.Phase = neo4jv1beta1.UpgradePhaseVerifying
	}

	// stepStartTime measures the current step, so it survives only while the step does. Progress of
	// any kind restarts the clock: a slow upgrade is not a stuck one.
	out.StepStartTime = stepStart(n.Status.Upgrade, out, now)

	if budget := MemberBudget(n); now.Sub(out.StepStartTime.Time) > budget {
		out.Phase = neo4jv1beta1.UpgradePhaseFailed
		out.LastError = fmt.Sprintf(
			"no progress for %s while upgrading %s to %s (%d of %d members upgraded, %d ready). "+
				"That is longer than a member may take to stop and start with this probe configuration, "+
				"so the roll is stuck rather than slow; check the pods that have not come back",
			budget.Round(time.Second), running, target, upgraded, total, ready)
	}
	return out
}

// stepStart keeps the previous timestamp while the step is unchanged, and takes now whenever the
// phase or the upgraded count moves.
func stepStart(prev, next *neo4jv1beta1.UpgradeStatus, now time.Time) *metav1.Time {
	if prev == nil || prev.StepStartTime == nil ||
		prev.Phase != next.Phase ||
		prev.TargetVersion != next.TargetVersion ||
		progressOf(prev) != progressOf(next) {
		t := metav1.NewTime(now)
		return &t
	}
	return prev.StepStartTime.DeepCopy()
}

func progressOf(s *neo4jv1beta1.UpgradeStatus) int32 {
	if s == nil || s.Progress == nil {
		return -1
	}
	return s.Progress.Upgraded
}

// MemberBudget is how long one member may take to stop and start again, derived from the probe
// configuration this CR actually has rather than a constant: an override replaces a probe whole, so
// a resource with a tight startup probe has a correspondingly tight budget, and one that raised it
// for a large store gets the longer window it asked for (ADR-017).
//
// ponytail: the defaults are duplicated from render/workload/probes.go and
// render/workload/scheduling.go. Export them from render if a third caller appears.
func MemberBudget(n *neo4jv1beta1.Neo4j) time.Duration {
	failureThreshold, periodSeconds := int64(1000), int64(5)
	if n.Spec.Probes != nil && n.Spec.Probes.Startup != nil {
		if v := n.Spec.Probes.Startup.FailureThreshold; v > 0 {
			failureThreshold = int64(v)
		}
		if v := n.Spec.Probes.Startup.PeriodSeconds; v > 0 {
			periodSeconds = int64(v)
		}
	}
	grace := int64(3600)
	if n.Spec.Scheduling != nil && n.Spec.Scheduling.TerminationGracePeriodSeconds != nil {
		grace = *n.Spec.Scheduling.TerminationGracePeriodSeconds
	}
	return time.Duration(failureThreshold*periodSeconds+grace) * time.Second
}
