/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

// Unit tests for the #174 requeue-driven rolling-upgrade state machine. The
// Rolling decision layer is pure over one observation (same pattern as
// planScaleDownStep), so the walk ordering, the Neo4j rejoin gate and the
// terminal transition are verified without a live cluster. The live partition
// walk, 5.26→CalVer discovery-env switch and multi-cluster non-starvation are
// exercised on Kind.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func TestPlanUpgradeRollStep(t *testing.T) {
	base := upgradeRollObservation{
		Replicas:           3,
		Generation:         2,
		ObservedGeneration: 2,
		ReadyReplicas:      3,
	}

	t.Run("waits while STS controller has not observed the latest spec", func(t *testing.T) {
		o := base
		o.Partition = 3
		o.ObservedGeneration = 1
		assert.Equal(t, rollWait, planUpgradeRollStep(o))
	})

	t.Run("full freeze with everything ready lowers immediately", func(t *testing.T) {
		o := base
		o.Partition = 3 // == replicas: nothing rolled yet, no Neo4j gate
		assert.Equal(t, rollLower, planUpgradeRollStep(o))
	})

	t.Run("waits while the rolled pod is not on the update revision", func(t *testing.T) {
		o := base
		o.Partition = 2
		o.UpdatedReplicas = 0 // expected >= replicas - partition = 1
		assert.Equal(t, rollWait, planUpgradeRollStep(o))
	})

	t.Run("waits while any pod is unready", func(t *testing.T) {
		o := base
		o.Partition = 2
		o.UpdatedReplicas = 1
		o.ReadyReplicas = 2
		assert.Equal(t, rollWait, planUpgradeRollStep(o))
	})

	t.Run("waits on the SHOW SERVERS gate even when Kubernetes is settled", func(t *testing.T) {
		// The key Neo4j-awareness invariant: K8s readiness (HTTP 7474) is NOT
		// enough — the server must be Enabled+Available again before the next
		// pod restarts.
		o := base
		o.Partition = 2
		o.UpdatedReplicas = 1
		o.LastRolledAvailable = false
		assert.Equal(t, rollWait, planUpgradeRollStep(o))
	})

	t.Run("lowers once the rolled server rejoined", func(t *testing.T) {
		o := base
		o.Partition = 2
		o.UpdatedReplicas = 1
		o.LastRolledAvailable = true
		assert.Equal(t, rollLower, planUpgradeRollStep(o))
	})

	t.Run("done when partition 0 pod (system-DB seed, rolled last) rejoined", func(t *testing.T) {
		o := base
		o.Partition = 0
		o.UpdatedReplicas = 3
		o.LastRolledAvailable = true
		assert.Equal(t, rollDone, planUpgradeRollStep(o))
	})
}

func TestUpgradeStateMachineActive(t *testing.T) {
	c := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	assert.False(t, upgradeStateMachineActive(c), "nil UpgradeStatus")

	for _, phase := range []string{upgradePhaseStaging, upgradePhaseRolling, upgradePhaseStabilizing, upgradePhaseVerifying, upgradePhaseLegacyInProgress} {
		c.Status.UpgradeStatus = &neo4jv1beta1.UpgradeStatus{Phase: phase}
		assert.True(t, upgradeStateMachineActive(c), phase)
	}
	for _, phase := range []string{"", upgradePhaseCompleted, upgradePhaseFailed, upgradePhasePaused, "Pending"} {
		c.Status.UpgradeStatus = &neo4jv1beta1.UpgradeStatus{Phase: phase}
		assert.False(t, upgradeStateMachineActive(c), phase)
	}
}

func TestUpgradeStepDeadlineExceeded(t *testing.T) {
	now := time.Now()
	assert.False(t, upgradeStepDeadlineExceeded(nil, time.Minute, now))
	assert.False(t, upgradeStepDeadlineExceeded(&neo4jv1beta1.UpgradeStatus{}, time.Minute, now))

	recent := metav1.NewTime(now.Add(-30 * time.Second))
	assert.False(t, upgradeStepDeadlineExceeded(&neo4jv1beta1.UpgradeStatus{StepStartTime: &recent}, time.Minute, now))

	old := metav1.NewTime(now.Add(-2 * time.Minute))
	assert.True(t, upgradeStepDeadlineExceeded(&neo4jv1beta1.UpgradeStatus{StepStartTime: &old}, time.Minute, now))
}

// TestHandleRollingUpgrade_LegacyInProgressResumesAsStaging pins the
// operator-restart resume path: a persisted "InProgress" marker from an older
// operator version transitions to Staging instead of wedging or re-running a
// blocking loop.
func TestHandleRollingUpgrade_LegacyInProgressResumesAsStaging(t *testing.T) {
	scheme := makeUpgradeScheme()
	cluster := clusterForUpgrade("legacy", "default", "5.26.0-enterprise", "5.26.1-enterprise", 3)
	cluster.Status.UpgradeStatus = &neo4jv1beta1.UpgradeStatus{Phase: upgradePhaseLegacyInProgress}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
	r := &Neo4jEnterpriseClusterReconciler{Client: fc, Recorder: record.NewFakeRecorder(10)}

	res, err := r.handleRollingUpgrade(context.Background(), cluster)
	require.NoError(t, err)
	assert.Equal(t, upgradeTransitionRequeue, res.RequeueAfter)

	latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "legacy", Namespace: "default"}, latest))
	assert.Equal(t, upgradePhaseStaging, latest.Status.UpgradeStatus.Phase)
	assert.NotNil(t, latest.Status.UpgradeStatus.StepStartTime)
}

// TestStepUpgradeRolling_LowersPartitionFromFullFreeze drives one Rolling step
// against a settled StatefulSet at full freeze: the partition must drop by one
// and be persisted both on the StatefulSet and in status.currentPartition.
// (partition == replicas means nothing rolled yet, so no Neo4j round-trip.)
func TestStepUpgradeRolling_LowersPartitionFromFullFreeze(t *testing.T) {
	scheme := makeUpgradeScheme()
	const replicas int32 = 3
	cluster := clusterForUpgrade("roll", "default", "5.26.0-enterprise", "5.26.1-enterprise", replicas)
	now := metav1.Now()
	cluster.Status.UpgradeStatus = &neo4jv1beta1.UpgradeStatus{
		Phase:            upgradePhaseRolling,
		TargetVersion:    "5.26.1-enterprise",
		StartTime:        &now,
		StepStartTime:    &now,
		CurrentPartition: ptr.To(replicas),
		Progress:         &neo4jv1beta1.UpgradeProgress{Total: replicas, Pending: replicas},
	}

	sts := serverSTSForUpgrade("roll", "default", "neo4j:5.26.1-enterprise", replicas)
	sts.Generation = 2
	sts.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To(replicas)
	sts.Status = appsv1.StatefulSetStatus{
		ObservedGeneration: 2,
		ReadyReplicas:      replicas,
		UpdatedReplicas:    0,
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, sts).WithStatusSubresource(cluster).Build()
	r := &Neo4jEnterpriseClusterReconciler{Client: fc, Recorder: record.NewFakeRecorder(10)}

	res, err := r.stepUpgradeRolling(context.Background(), cluster)
	require.NoError(t, err)
	assert.Equal(t, upgradeStepRequeue, res.RequeueAfter)

	gotSTS := &appsv1.StatefulSet{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "roll-server", Namespace: "default"}, gotSTS))
	require.NotNil(t, gotSTS.Spec.UpdateStrategy.RollingUpdate.Partition)
	assert.Equal(t, replicas-1, *gotSTS.Spec.UpdateStrategy.RollingUpdate.Partition,
		"partition must drop by exactly one (highest ordinal rolls first)")

	latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "roll", Namespace: "default"}, latest))
	require.NotNil(t, latest.Status.UpgradeStatus.CurrentPartition)
	assert.Equal(t, replicas-1, *latest.Status.UpgradeStatus.CurrentPartition)
	assert.Equal(t, upgradePhaseRolling, latest.Status.UpgradeStatus.Phase)
}

// TestHandleRollingUpgrade_TargetChangedMidFlightRestages pins the mid-flight
// retarget behavior: editing the image tag again sends the machine back to
// Staging with the new target from ANY active phase (Rolling, Stabilizing,
// Verifying) rather than finishing — or failing verification — against a
// stale one. The check lives in the dispatcher (handleRollingUpgrade).
func TestHandleRollingUpgrade_TargetChangedMidFlightRestages(t *testing.T) {
	scheme := makeUpgradeScheme()
	for _, phase := range []string{upgradePhaseRolling, upgradePhaseStabilizing, upgradePhaseVerifying} {
		t.Run(phase, func(t *testing.T) {
			cluster := clusterForUpgrade("retarget", "default", "5.26.0-enterprise", "5.26.2-enterprise", 3)
			now := metav1.Now()
			cluster.Status.UpgradeStatus = &neo4jv1beta1.UpgradeStatus{
				Phase:         phase,
				TargetVersion: "5.26.1-enterprise", // walk started against .1, spec now wants .2
				StartTime:     &now,
				StepStartTime: &now,
			}

			fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
			r := &Neo4jEnterpriseClusterReconciler{Client: fc, Recorder: record.NewFakeRecorder(10)}

			res, err := r.handleRollingUpgrade(context.Background(), cluster)
			require.NoError(t, err)
			assert.Equal(t, upgradeTransitionRequeue, res.RequeueAfter)

			latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
			require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "retarget", Namespace: "default"}, latest))
			assert.Equal(t, upgradePhaseStaging, latest.Status.UpgradeStatus.Phase)
			assert.Equal(t, "5.26.2-enterprise", latest.Status.UpgradeStatus.TargetVersion)
		})
	}
}

// TestStepUpgradeRolling_PersistedPartitionBeatsStaleLiveRead pins the
// partition-resolution preference: the machine's persisted currentPartition is
// authoritative over the live StatefulSet value, which can be a stale informer
// read (e.g. partition=0 left by a previous completed upgrade observed just
// after Staging re-froze it). Trusting the stale 0 would fast-forward the walk.
func TestStepUpgradeRolling_PersistedPartitionBeatsStaleLiveRead(t *testing.T) {
	scheme := makeUpgradeScheme()
	const replicas int32 = 3
	cluster := clusterForUpgrade("stale", "default", "5.26.0-enterprise", "5.26.1-enterprise", replicas)
	now := metav1.Now()
	cluster.Status.UpgradeStatus = &neo4jv1beta1.UpgradeStatus{
		Phase:            upgradePhaseRolling,
		TargetVersion:    "5.26.1-enterprise",
		StartTime:        &now,
		StepStartTime:    &now,
		CurrentPartition: ptr.To(replicas), // machine just froze at full
		Progress:         &neo4jv1beta1.UpgradeProgress{Total: replicas, Pending: replicas},
	}

	sts := serverSTSForUpgrade("stale", "default", "neo4j:5.26.1-enterprise", replicas)
	sts.Generation = 2
	// Stale read: partition still 0 from a PREVIOUS completed upgrade, status
	// settled — with live-first resolution this looked like a finished walk.
	sts.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To(int32(0))
	sts.Status = appsv1.StatefulSetStatus{
		ObservedGeneration: 2,
		ReadyReplicas:      replicas,
		UpdatedReplicas:    0, // nothing rolled to the new revision yet
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, sts).WithStatusSubresource(cluster).Build()
	r := &Neo4jEnterpriseClusterReconciler{Client: fc, Recorder: record.NewFakeRecorder(10)}

	res, err := r.stepUpgradeRolling(context.Background(), cluster)
	require.NoError(t, err)
	assert.Equal(t, upgradeStepRequeue, res.RequeueAfter)

	latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "stale", Namespace: "default"}, latest))
	// Persisted partition (replicas) wins: with UpdatedReplicas=0 and a full
	// freeze the planner lowers to replicas-1 — it must NOT jump to
	// Stabilizing as a stale live partition=0 would suggest.
	assert.Equal(t, upgradePhaseRolling, latest.Status.UpgradeStatus.Phase)
	require.NotNil(t, latest.Status.UpgradeStatus.CurrentPartition)
	assert.Equal(t, replicas-1, *latest.Status.UpgradeStatus.CurrentPartition)
}

// TestFailUpgrade_RoutesPausedAndFailed pins the failure routing: autoPause →
// Paused (no requeue, never auto-resumed), default → Failed with the error
// surfaced and requeue for the normal path to take over.
func TestFailUpgrade_RoutesPausedAndFailed(t *testing.T) {
	scheme := makeUpgradeScheme()

	t.Run("autoPauseOnFailure pauses", func(t *testing.T) {
		cluster := clusterForUpgrade("pause", "default", "5.26.0-enterprise", "5.26.1-enterprise", 3)
		cluster.Spec.UpgradeStrategy = &neo4jv1beta1.UpgradeStrategySpec{AutoPauseOnFailure: true}
		now := metav1.Now()
		cluster.Status.UpgradeStatus = &neo4jv1beta1.UpgradeStatus{Phase: upgradePhaseRolling, StartTime: &now}

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
		r := &Neo4jEnterpriseClusterReconciler{Client: fc, Recorder: record.NewFakeRecorder(10)}

		res, err := r.failUpgrade(context.Background(), cluster, assert.AnError)
		require.NoError(t, err)
		assert.Zero(t, res.RequeueAfter, "paused upgrades are not auto-requeued")

		latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "pause", Namespace: "default"}, latest))
		assert.Equal(t, upgradePhasePaused, latest.Status.UpgradeStatus.Phase)
		assert.Contains(t, latest.Status.UpgradeStatus.LastError, assert.AnError.Error())
		assert.Equal(t, "Paused", latest.Status.Phase)
	})

	t.Run("default fails with requeue", func(t *testing.T) {
		cluster := clusterForUpgrade("fail", "default", "5.26.0-enterprise", "5.26.1-enterprise", 3)
		now := metav1.Now()
		cluster.Status.UpgradeStatus = &neo4jv1beta1.UpgradeStatus{Phase: upgradePhaseRolling, StartTime: &now}

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
		r := &Neo4jEnterpriseClusterReconciler{Client: fc, Recorder: record.NewFakeRecorder(10)}

		_, err := r.failUpgrade(context.Background(), cluster, assert.AnError)
		assert.ErrorIs(t, err, assert.AnError)

		latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "fail", Namespace: "default"}, latest))
		assert.Equal(t, upgradePhaseFailed, latest.Status.UpgradeStatus.Phase)
		assert.Equal(t, "Failed", latest.Status.Phase)
	})
}

// TestStartRollingUpgrade_DeferredWhileDrainInProgress pins the #173/#174
// mutual exclusion: an image upgrade requested while a scale-down drain is
// mid-flight does NOT start — the drain keeps running and the upgrade is
// deferred with an event.
func TestStartRollingUpgrade_DeferredWhileDrainInProgress(t *testing.T) {
	scheme := makeUpgradeScheme()
	cluster := clusterForUpgrade("defer", "default", "5.26.0-enterprise", "5.26.1-enterprise", 2)
	cluster.Annotations = map[string]string{ScaleDownDrainingServersAnnotation: "id-a,id-b"}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
	recorder := record.NewFakeRecorder(10)
	r := &Neo4jEnterpriseClusterReconciler{Client: fc, Recorder: recorder}

	res, err := r.startRollingUpgrade(context.Background(), cluster)
	require.NoError(t, err)
	assert.Equal(t, upgradeStepRequeue, res.RequeueAfter)

	latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "defer", Namespace: "default"}, latest))
	assert.Nil(t, latest.Status.UpgradeStatus, "upgrade must not be initialized while a drain is in progress")

	select {
	case ev := <-recorder.Events:
		assert.Contains(t, ev, EventReasonUpgradeDeferred)
	default:
		t.Fatal("expected an UpgradeDeferred event")
	}
}

// TestIsUpgradeRequired_FailedRetry pins the Failed-upgrade re-entry: after
// Staging, the StatefulSet template already carries the target image, so plain
// image drift can never re-fire. A Failed attempt toward the still-desired
// target with the version never verified must re-enter; a verified version
// (or a Paused hold) must not.
func TestIsUpgradeRequired_FailedRetry(t *testing.T) {
	scheme := makeUpgradeScheme()

	mk := func(upgradePhase, statusVersion string) *neo4jv1beta1.Neo4jEnterpriseCluster {
		c := clusterForUpgrade("retry", "default", statusVersion, "2025.01.0-enterprise", 3)
		c.Status.Phase = "Ready"
		c.Status.Version = statusVersion
		c.Status.UpgradeStatus = &neo4jv1beta1.UpgradeStatus{
			Phase:         upgradePhase,
			TargetVersion: "2025.01.0-enterprise",
		}
		return c
	}
	// Template already staged to the target image — no plain drift.
	sts := serverSTSForUpgrade("retry", "default", "neo4j:2025.01.0-enterprise", 3)

	t.Run("failed attempt with unverified version re-enters", func(t *testing.T) {
		c := mk(upgradePhaseFailed, "")
		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(c, sts.DeepCopy()).Build()
		r := &Neo4jEnterpriseClusterReconciler{Client: fc}
		assert.True(t, r.isUpgradeRequired(context.Background(), c))
	})

	t.Run("completed upgrade does not re-enter", func(t *testing.T) {
		c := mk(upgradePhaseCompleted, "2025.01.0-enterprise")
		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(c, sts.DeepCopy()).Build()
		r := &Neo4jEnterpriseClusterReconciler{Client: fc}
		assert.False(t, r.isUpgradeRequired(context.Background(), c))
	})

	t.Run("paused never auto-resumes", func(t *testing.T) {
		c := mk(upgradePhasePaused, "")
		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(c, sts.DeepCopy()).Build()
		r := &Neo4jEnterpriseClusterReconciler{Client: fc}
		assert.False(t, r.isUpgradeRequired(context.Background(), c))
	})

	t.Run("failed attempt toward an abandoned target does not re-enter", func(t *testing.T) {
		c := mk(upgradePhaseFailed, "")
		c.Status.UpgradeStatus.TargetVersion = "2025.02.0-enterprise" // spec moved on
		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(c, sts.DeepCopy()).Build()
		r := &Neo4jEnterpriseClusterReconciler{Client: fc}
		assert.False(t, r.isUpgradeRequired(context.Background(), c))
	})
}
