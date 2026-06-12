/*
Copyright 2025.

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

// Requeue-driven rolling-upgrade state machine (#174).
//
// The previous implementation (RollingUpgradeOrchestrator.ExecuteRollingUpgrade)
// ran the whole partition walk synchronously inside one Reconcile — blocking up
// to ~30 minutes per pod. With MaxConcurrentReconciles=1 on the cluster
// controller, one upgrading cluster starved reconciliation of every other
// cluster. This file replaces the blocking loop with one non-blocking step per
// reconcile:
//
//	Staging     — apply the FULL target-version pod template (image AND the
//	              resource-builder env for the new tag — discovery config keys
//	              differ between 5.26 and CalVer) with the StatefulSet
//	              RollingUpdate partition frozen at replicas, then reconcile
//	              the ConfigMap under the same freeze.
//	Rolling     — lower the partition one ordinal at a time (highest first,
//	              ordinal 0 / system-DB seed last), gating each step on the
//	              StatefulSet rollout AND the restarted server re-appearing as
//	              Enabled+Available in SHOW SERVERS.
//	Stabilizing — one-shot cluster health + consensus + replication gate.
//	Verifying   — per-server version verification, then Completed.
//
// State is persisted in status.upgradeStatus (phase, currentPartition,
// stepStartTime), so an operator restart resumes mid-walk instead of wedging.

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
)

// Upgrade phases persisted in status.upgradeStatus.phase.
const (
	upgradePhaseStaging     = "Staging"
	upgradePhaseRolling     = "Rolling"
	upgradePhaseStabilizing = "Stabilizing"
	upgradePhaseVerifying   = "Verifying"
	upgradePhaseCompleted   = "Completed"
	upgradePhaseFailed      = "Failed"
	upgradePhasePaused      = "Paused"
	// upgradePhaseLegacyInProgress is the marker older operator versions
	// persisted while their blocking loop ran; resumed as Staging.
	upgradePhaseLegacyInProgress = "InProgress"
)

// upgradeStepRequeue is the poll interval between state-machine steps. Short
// enough to keep per-pod latency low, long enough that an upgrading cluster
// leaves plenty of reconcile bandwidth for other clusters.
const upgradeStepRequeue = 15 * time.Second

// upgradeTransitionRequeue is used right after a phase transition, where the
// next step is expected to make progress immediately.
const upgradeTransitionRequeue = 5 * time.Second

// upgradeStateMachineActive reports whether a rolling upgrade is mid-flight
// and the state machine must keep driving it (bypassing the normal reconcile
// path, which would otherwise stomp the partition freeze and re-apply
// replicas). Terminal phases (Completed/Failed/Paused) are NOT active: the
// normal path takes over and isUpgradeRequired decides whether to re-enter.
func upgradeStateMachineActive(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) bool {
	if cluster.Status.UpgradeStatus == nil {
		return false
	}
	switch cluster.Status.UpgradeStatus.Phase {
	case upgradePhaseStaging, upgradePhaseRolling, upgradePhaseStabilizing,
		upgradePhaseVerifying, upgradePhaseLegacyInProgress:
		return true
	}
	return false
}

// upgradeRollObservation is everything the Rolling step needs to decide its
// next action, gathered by the caller so the decision itself is pure and
// unit-testable (same pattern as planScaleDownStep in scale_down.go).
type upgradeRollObservation struct {
	Replicas           int32
	Partition          int32
	Generation         int64
	ObservedGeneration int64
	UpdatedReplicas    int32
	ReadyReplicas      int32
	// LastRolledAvailable is the SHOW SERVERS Enabled+Available gate for the
	// most recently rolled pod (ordinal == Partition). Ignored while
	// Partition == Replicas (nothing rolled yet).
	LastRolledAvailable bool
}

type upgradeRollDecision int

const (
	// rollWait — the StatefulSet rollout or the Neo4j rejoin gate for the
	// current partition step is still pending; requeue and re-observe.
	rollWait upgradeRollDecision = iota
	// rollLower — current step verified; lower the partition by one to roll
	// the next ordinal.
	rollLower
	// rollDone — partition is 0 and ordinal 0 is verified; the walk is done.
	rollDone
)

// planUpgradeRollStep decides the next Rolling action from one observation.
func planUpgradeRollStep(o upgradeRollObservation) upgradeRollDecision {
	// StatefulSet controller hasn't observed the latest spec yet.
	if o.ObservedGeneration < o.Generation {
		return rollWait
	}
	// Pods at ordinals >= partition must all be on the update revision.
	if o.UpdatedReplicas < o.Replicas-o.Partition {
		return rollWait
	}
	// Every pod must be Ready before touching the next one.
	if o.ReadyReplicas < o.Replicas {
		return rollWait
	}
	// Kubernetes readiness only checks HTTP 7474; the restarted server must
	// also be Enabled+Available in SHOW SERVERS before the next pod rolls.
	if o.Partition < o.Replicas && !o.LastRolledAvailable {
		return rollWait
	}
	if o.Partition == 0 {
		return rollDone
	}
	return rollLower
}

// patchUpgradeStatus applies mutate to status.upgradeStatus on a refetched
// object with conflict retry (see #207 — bare Status().Update on the stale
// reconcile-start object silently loses writes), mirroring the result back
// onto the in-memory cluster.
func (r *Neo4jEnterpriseClusterReconciler) patchUpgradeStatus(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
	mutate func(*neo4jv1beta1.UpgradeStatus),
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
			return err
		}
		if latest.Status.UpgradeStatus == nil {
			latest.Status.UpgradeStatus = &neo4jv1beta1.UpgradeStatus{}
		}
		mutate(latest.Status.UpgradeStatus)
		cluster.Status.UpgradeStatus = latest.Status.UpgradeStatus
		return r.Status().Update(ctx, latest)
	})
}

// upgradeStepDeadlineExceeded reports whether the current step has been
// running longer than its budget.
func upgradeStepDeadlineExceeded(us *neo4jv1beta1.UpgradeStatus, budget time.Duration, now time.Time) bool {
	if us == nil || us.StepStartTime == nil {
		return false
	}
	return now.After(us.StepStartTime.Time.Add(budget))
}

// startRollingUpgrade runs the pre-upgrade gates and, when they pass,
// transitions the state machine into Staging. Called when image drift is
// detected and no upgrade is mid-flight.
func (r *Neo4jEnterpriseClusterReconciler) startRollingUpgrade(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("rolling-upgrade")

	// Mutual exclusion with the scale-down drain (#173): finish an in-progress
	// drain before starting an upgrade (Neo4j docs: don't change topology
	// mid-upgrade). Advance the drain here so it can complete — the normal
	// reconcile path is not reached from the upgrade branch.
	if scaleDownDrainInProgress(cluster) {
		logger.Info("Deferring rolling upgrade: scale-down drain in progress")
		if err := r.reconcileScaleDownDrain(ctx, cluster); err != nil {
			logger.Error(err, "Scale-down drain step failed while deferring upgrade (non-fatal)")
		}
		r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonUpgradeDeferred,
			"Rolling upgrade deferred until the in-progress scale-down drain completes")
		return ctrl.Result{RequeueAfter: upgradeStepRequeue}, nil
	}

	orch := NewRollingUpgradeOrchestrator(r.Client, cluster.Name, cluster.Namespace)

	// Version-compatibility and StatefulSet readiness gates (no Neo4j I/O).
	if err := orch.validateVersionCompatibility(cluster.Status.Version, cluster.Spec.Image.Tag); err != nil {
		return r.failUpgrade(ctx, cluster, fmt.Errorf("version compatibility check failed: %w", err))
	}
	if err := orch.validateStatefulSetsReady(ctx, cluster); err != nil {
		// Not an upgrade failure — the cluster isn't quiescent yet. Retry.
		logger.Info("Upgrade waiting for StatefulSet readiness", "reason", err.Error())
		return ctrl.Result{RequeueAfter: upgradeStepRequeue}, nil
	}

	// Pre-upgrade Neo4j health gate (one-shot), unless disabled.
	if cluster.Spec.UpgradeStrategy == nil || cluster.Spec.UpgradeStrategy.PreUpgradeHealthCheck {
		neo4jClient, err := r.createNeo4jClient(ctx, cluster)
		if err != nil {
			return r.failUpgrade(ctx, cluster, fmt.Errorf("failed to create Neo4j client: %w", err))
		}
		defer func() { _ = neo4jClient.Close() }()
		if err := neo4jClient.ValidateUpgradeSafety(ctx, cluster.Spec.Image.Tag); err != nil {
			return r.failUpgrade(ctx, cluster, fmt.Errorf("upgrade safety validation failed: %w", err))
		}
	}

	now := metav1.Now()
	servers := cluster.Spec.Topology.Servers
	previousVersion := cluster.Status.Version
	targetVersion := cluster.Spec.Image.Tag
	if err := r.patchUpgradeStatus(ctx, cluster, func(us *neo4jv1beta1.UpgradeStatus) {
		*us = neo4jv1beta1.UpgradeStatus{
			Phase:           upgradePhaseStaging,
			StartTime:       &now,
			StepStartTime:   &now,
			CurrentStep:     "Staging target pod template",
			PreviousVersion: previousVersion,
			TargetVersion:   targetVersion,
			Progress: &neo4jv1beta1.UpgradeProgress{
				Total:   servers,
				Pending: servers,
			},
		}
	}); err != nil {
		logger.Error(err, "Failed to initialize upgrade status")
		return ctrl.Result{RequeueAfter: upgradeTransitionRequeue}, nil
	}

	r.Recorder.Eventf(cluster, corev1.EventTypeNormal, EventReasonUpgradeStarted,
		"Rolling upgrade started: %s -> %s", previousVersion, targetVersion)
	logger.Info("Rolling upgrade started", "from", previousVersion, "to", targetVersion)
	return ctrl.Result{RequeueAfter: upgradeTransitionRequeue}, nil
}

// buildDesiredServerStatefulSet computes the full desired server StatefulSet —
// builder output for the current spec plus topology constraints — exactly as
// the normal reconcile path applies it. Staging MUST go through this (not an
// image-only patch): the builder's env for the target tag carries the
// version-correct discovery config (5.26 uses dbms.cluster.discovery.v2.*;
// CalVer uses dbms.cluster.endpoints), so each pod restarts into a
// self-consistent target template.
func (r *Neo4jEnterpriseClusterReconciler) buildDesiredServerStatefulSet(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
) (*appsv1.StatefulSet, error) {
	sts := resources.BuildServerStatefulSetForEnterprise(cluster)
	if r.TopologyScheduler != nil {
		placement, err := r.TopologyScheduler.CalculateTopologyPlacement(ctx, cluster)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate topology placement: %w", err)
		}
		if placement != nil {
			if err := r.TopologyScheduler.ApplyTopologyConstraints(ctx, sts, cluster, placement); err != nil {
				return nil, fmt.Errorf("failed to apply topology constraints: %w", err)
			}
		}
	}
	return sts, nil
}

// stepUpgradeStaging applies the full target template with the partition
// frozen at replicas (atomic in one StatefulSet update — no window where the
// new template can roll pods uncontrolled), then reconciles the ConfigMap
// under the same freeze, then transitions to Rolling.
func (r *Neo4jEnterpriseClusterReconciler) stepUpgradeStaging(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("rolling-upgrade")
	orch := NewRollingUpgradeOrchestrator(r.Client, cluster.Name, cluster.Namespace)

	if upgradeStepDeadlineExceeded(cluster.Status.UpgradeStatus, orch.getUpgradeTimeout(cluster), time.Now()) {
		return r.failUpgrade(ctx, cluster, fmt.Errorf("timed out staging the target pod template"))
	}

	live, err := orch.getServerStatefulSet(ctx, cluster)
	if err != nil {
		return r.failUpgrade(ctx, cluster, fmt.Errorf("failed to get server StatefulSet: %w", err))
	}
	replicas := int32(0)
	if live.Spec.Replicas != nil {
		replicas = *live.Spec.Replicas
	}
	if replicas == 0 {
		return r.failUpgrade(ctx, cluster, fmt.Errorf("server StatefulSet has zero replicas configured"))
	}

	desired, err := r.buildDesiredServerStatefulSet(ctx, cluster)
	if err != nil {
		return r.failUpgrade(ctx, cluster, err)
	}
	// Freeze every pod restart in the SAME update that lands the new template.
	if desired.Spec.UpdateStrategy.RollingUpdate == nil {
		desired.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateStatefulSetStrategy{}
	}
	partition := replicas
	desired.Spec.UpdateStrategy.RollingUpdate.Partition = &partition

	if err := r.createOrUpdateResource(ctx, desired, cluster); err != nil {
		logger.Error(err, "Failed to stage target StatefulSet template")
		return ctrl.Result{RequeueAfter: upgradeStepRequeue}, nil
	}

	// ConfigMap content can also be version-specific. Reconcile it AFTER the
	// partition freeze so its config-hash restart annotation lands gated, and
	// each pod picks up config + template in a single partition-driven restart.
	if err := r.ConfigMapManager.ReconcileConfigMap(ctx, cluster); err != nil {
		logger.Error(err, "Failed to reconcile ConfigMap during upgrade staging")
		return ctrl.Result{RequeueAfter: upgradeStepRequeue}, nil
	}

	now := metav1.Now()
	if err := r.patchUpgradeStatus(ctx, cluster, func(us *neo4jv1beta1.UpgradeStatus) {
		us.Phase = upgradePhaseRolling
		us.CurrentPartition = &partition
		us.StepStartTime = &now
		us.CurrentStep = "Rolling servers to the target version"
		us.Message = us.CurrentStep
	}); err != nil {
		logger.Error(err, "Failed to persist Rolling transition")
		return ctrl.Result{RequeueAfter: upgradeTransitionRequeue}, nil
	}
	logger.Info("Upgrade staged: target template applied, partition frozen", "partition", partition)
	return ctrl.Result{RequeueAfter: upgradeTransitionRequeue}, nil
}

// stepUpgradeRolling advances the partition walk by at most one ordinal per
// reconcile. The system-DB seed (ordinal 0) is always rolled last — the
// StatefulSet partition strategy restarts highest-ordinal first by
// construction.
func (r *Neo4jEnterpriseClusterReconciler) stepUpgradeRolling(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("rolling-upgrade")
	orch := NewRollingUpgradeOrchestrator(r.Client, cluster.Name, cluster.Namespace)
	us := cluster.Status.UpgradeStatus
	// Mid-flight target changes are handled centrally in handleRollingUpgrade
	// (re-stage from any active phase) before this step is dispatched.

	live, err := orch.getServerStatefulSet(ctx, cluster)
	if err != nil {
		return r.failUpgrade(ctx, cluster, fmt.Errorf("failed to get server StatefulSet: %w", err))
	}
	replicas := int32(0)
	if live.Spec.Replicas != nil {
		replicas = *live.Spec.Replicas
	}
	if replicas == 0 {
		return r.failUpgrade(ctx, cluster, fmt.Errorf("server StatefulSet has zero replicas configured"))
	}

	// Surface (don't act on) a topology change requested mid-upgrade; the
	// scale-down drain runs after the upgrade completes (#173 / #174 mutual
	// exclusion — upgrade wins).
	if cluster.Spec.Topology.Servers < replicas {
		r.setScaleDownConditionPersisted(ctx, cluster, metav1.ConditionTrue,
			ConditionReasonScaleDownDeferredByUpgrade,
			"Scale-down deferred until the rolling upgrade completes")
	}

	// Resolve the partition: the machine's persisted currentPartition is
	// authoritative — the live StatefulSet value can be a STALE informer read
	// (e.g. partition=0 left by a PREVIOUS completed upgrade, observed just
	// after Staging re-froze it) and trusting it could fast-forward the walk.
	// The persisted value is at worst one step BEHIND the StatefulSet (crash
	// between the STS write and the status patch), which converges safely:
	// the step's gates re-verify and the partition write is idempotent. Fall
	// back to the live value (legacy resume without the field), then to a
	// full freeze.
	partition := replicas
	if us.CurrentPartition != nil {
		partition = *us.CurrentPartition
	} else if live.Spec.UpdateStrategy.RollingUpdate != nil && live.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		partition = *live.Spec.UpdateStrategy.RollingUpdate.Partition
	}

	obs := upgradeRollObservation{
		Replicas:           replicas,
		Partition:          partition,
		Generation:         live.Generation,
		ObservedGeneration: live.Status.ObservedGeneration,
		UpdatedReplicas:    live.Status.UpdatedReplicas,
		ReadyReplicas:      live.Status.ReadyReplicas,
	}

	// Only pay for the SHOW SERVERS round-trip when the Kubernetes-level
	// rollout for the current step is already complete.
	kubernetesSettled := obs.ObservedGeneration >= obs.Generation &&
		obs.UpdatedReplicas >= obs.Replicas-obs.Partition &&
		obs.ReadyReplicas >= obs.Replicas
	if kubernetesSettled && partition < replicas {
		podName := fmt.Sprintf("%s-server-%d", cluster.Name, partition)
		available, err := r.isServerAvailable(ctx, cluster, podName)
		if err != nil {
			logger.V(1).Info("SHOW SERVERS gate not yet answerable (retrying)", "pod", podName, "error", err.Error())
		}
		obs.LastRolledAvailable = available
	}

	switch planUpgradeRollStep(obs) {
	case rollWait:
		if upgradeStepDeadlineExceeded(us, orch.getUpgradeTimeout(cluster), time.Now()) {
			return r.failUpgrade(ctx, cluster, fmt.Errorf(
				"timed out waiting for server ordinal %d to roll and rejoin the cluster", partition))
		}
		return ctrl.Result{RequeueAfter: upgradeStepRequeue}, nil

	case rollDone:
		now := metav1.Now()
		if err := r.patchUpgradeStatus(ctx, cluster, func(s *neo4jv1beta1.UpgradeStatus) {
			s.Phase = upgradePhaseStabilizing
			s.StepStartTime = &now
			s.CurrentStep = "All servers rolled; waiting for cluster stabilization"
			s.Message = s.CurrentStep
			if s.Progress != nil {
				s.Progress.Upgraded = replicas
				s.Progress.Pending = 0
				s.Progress.InProgress = 0
			}
		}); err != nil {
			logger.Error(err, "Failed to persist Stabilizing transition")
		}
		logger.Info("All servers rolled to the target version")
		return ctrl.Result{RequeueAfter: upgradeTransitionRequeue}, nil

	default: // rollLower
		newPartition := partition - 1
		if _, err := orch.updateServerStatefulSet(ctx, cluster, func(sts *appsv1.StatefulSet) {
			if sts.Spec.UpdateStrategy.RollingUpdate == nil {
				sts.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateStatefulSetStrategy{}
			}
			p := newPartition
			sts.Spec.UpdateStrategy.RollingUpdate.Partition = &p
		}); err != nil {
			logger.Error(err, "Failed to lower StatefulSet partition", "partition", newPartition)
			return ctrl.Result{RequeueAfter: upgradeStepRequeue}, nil
		}
		now := metav1.Now()
		if err := r.patchUpgradeStatus(ctx, cluster, func(s *neo4jv1beta1.UpgradeStatus) {
			s.CurrentPartition = &newPartition
			s.StepStartTime = &now
			s.CurrentStep = fmt.Sprintf("Rolling server ordinal %d", newPartition)
			s.Message = s.CurrentStep
			if s.Progress != nil {
				s.Progress.Upgraded = replicas - partition
				s.Progress.InProgress = 1
				s.Progress.Pending = partition - 1
				if s.Progress.Pending < 0 {
					s.Progress.Pending = 0
				}
			}
		}); err != nil {
			logger.Error(err, "Failed to persist partition advance")
		}
		logger.Info("Lowered upgrade partition", "ordinal", newPartition)
		return ctrl.Result{RequeueAfter: upgradeStepRequeue}, nil
	}
}

// stepUpgradeStabilizing gates on one-shot cluster health + consensus +
// replication checks. With Verifying re-checking replication ~10s later, the
// upgrade completes only after two independent healthy observations —
// equivalent rigor to the old WaitForClusterStabilization consecutive-check
// loop, without blocking the reconcile.
func (r *Neo4jEnterpriseClusterReconciler) stepUpgradeStabilizing(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("rolling-upgrade")
	orch := NewRollingUpgradeOrchestrator(r.Client, cluster.Name, cluster.Namespace)

	// Post-upgrade health gates can be explicitly disabled.
	if cluster.Spec.UpgradeStrategy != nil && !cluster.Spec.UpgradeStrategy.PostUpgradeHealthCheck {
		return r.transitionToVerifying(ctx, cluster, "Post-upgrade health check disabled; verifying versions")
	}

	healthy := false
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err == nil {
		defer func() { _ = neo4jClient.Close() }()
		ok, herr := neo4jClient.IsClusterHealthy(ctx)
		if herr == nil && ok {
			consensus, cerr := neo4jClient.GetClusterConsensusState(ctx)
			if cerr == nil && consensus {
				replicating, rerr := neo4jClient.IsClusterReplicationHealthy(ctx)
				healthy = rerr == nil && replicating
			}
		}
	}

	if !healthy {
		if upgradeStepDeadlineExceeded(cluster.Status.UpgradeStatus, orch.getStabilizationTimeout(cluster), time.Now()) {
			return r.failUpgrade(ctx, cluster, fmt.Errorf("cluster failed to stabilize after the rolling upgrade"))
		}
		logger.V(1).Info("Cluster not yet stable after upgrade; retrying")
		return ctrl.Result{RequeueAfter: upgradeStepRequeue}, nil
	}

	return r.transitionToVerifying(ctx, cluster, "Cluster stable; verifying server versions")
}

func (r *Neo4jEnterpriseClusterReconciler) transitionToVerifying(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
	step string,
) (ctrl.Result, error) {
	now := metav1.Now()
	if err := r.patchUpgradeStatus(ctx, cluster, func(s *neo4jv1beta1.UpgradeStatus) {
		s.Phase = upgradePhaseVerifying
		s.StepStartTime = &now
		s.CurrentStep = step
		s.Message = step
	}); err != nil {
		log.FromContext(ctx).Error(err, "Failed to persist Verifying transition")
	}
	return ctrl.Result{RequeueAfter: upgradeTransitionRequeue}, nil
}

// stepUpgradeVerifying confirms every server reports the target version, then
// completes the upgrade.
func (r *Neo4jEnterpriseClusterReconciler) stepUpgradeVerifying(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("rolling-upgrade")
	orch := NewRollingUpgradeOrchestrator(r.Client, cluster.Name, cluster.Namespace)

	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		if upgradeStepDeadlineExceeded(cluster.Status.UpgradeStatus, orch.getHealthCheckTimeout(cluster), time.Now()) {
			return r.failUpgrade(ctx, cluster, fmt.Errorf("failed to create Neo4j client for verification: %w", err))
		}
		return ctrl.Result{RequeueAfter: upgradeStepRequeue}, nil
	}
	defer func() { _ = neo4jClient.Close() }()

	if err := orch.verifyVersionUpgrade(ctx, cluster, neo4jClient); err != nil {
		if upgradeStepDeadlineExceeded(cluster.Status.UpgradeStatus, orch.getHealthCheckTimeout(cluster), time.Now()) {
			return r.failUpgrade(ctx, cluster, fmt.Errorf("post-upgrade version verification failed: %w", err))
		}
		logger.V(1).Info("Version verification not yet passing; retrying", "error", err.Error())
		return ctrl.Result{RequeueAfter: upgradeStepRequeue}, nil
	}

	// Completed: stamps CompletionTime, LastUpgradeTime and Status.Version on
	// a refetched object (see updateUpgradeStatus), then the cluster phase +
	// version in one write (#207 fix pattern). The Completed persist GATES the
	// Ready write: if it fails, retry the (idempotent) verification next
	// reconcile rather than reporting Ready while upgradeStatus still says
	// Verifying — that combination would keep the upgrade branch blocking
	// normal reconciliation with no record of why.
	startTime := time.Now()
	if us := cluster.Status.UpgradeStatus; us != nil && us.StartTime != nil {
		startTime = us.StartTime.Time
	}
	if err := orch.updateUpgradeStatus(ctx, cluster, upgradePhaseCompleted, "Rolling upgrade completed successfully", ""); err != nil {
		logger.Error(err, "Failed to persist Completed upgrade phase; retrying")
		return ctrl.Result{RequeueAfter: upgradeTransitionRequeue}, nil
	}
	_ = r.updateClusterStatusWithVersion(ctx, cluster, "Ready", "Rolling upgrade completed successfully", cluster.Spec.Image.Tag)

	orch.upgradeMetrics.RecordUpgrade(ctx, true, time.Since(startTime))
	r.Recorder.Event(cluster, corev1.EventTypeNormal, EventReasonUpgradeCompleted, "Rolling upgrade completed successfully")
	logger.Info("Rolling upgrade completed successfully", "version", cluster.Spec.Image.Tag)
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

// failUpgrade routes a step failure: Paused when autoPauseOnFailure is set
// (manual intervention required, never auto-resumed), Failed otherwise (the
// normal path takes over; isUpgradeRequired re-enters once the cluster is
// Ready again and image drift persists).
func (r *Neo4jEnterpriseClusterReconciler) failUpgrade(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
	cause error,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("rolling-upgrade")
	logger.Error(cause, "Rolling upgrade failed")

	startTime := time.Now()
	if us := cluster.Status.UpgradeStatus; us != nil && us.StartTime != nil {
		startTime = us.StartTime.Time
	}
	orch := NewRollingUpgradeOrchestrator(r.Client, cluster.Name, cluster.Namespace)
	// Metric recorded only after the terminal phase persists (below) — a
	// failed persist retries this whole routing and would double-count.
	recordFailureMetric := func() { orch.upgradeMetrics.RecordUpgrade(ctx, false, time.Since(startTime)) }

	if cluster.Spec.UpgradeStrategy != nil && cluster.Spec.UpgradeStrategy.AutoPauseOnFailure {
		if err := r.patchUpgradeStatus(ctx, cluster, func(s *neo4jv1beta1.UpgradeStatus) {
			s.Phase = upgradePhasePaused
			s.LastError = cause.Error()
			s.CurrentStep = "Upgrade paused due to failure - manual intervention required"
			s.Message = s.CurrentStep
		}); err != nil {
			// The terminal phase MUST land before any other side effect: if it
			// doesn't, upgradeStateMachineActive keeps treating the upgrade as
			// mid-flight and the steps keep driving after we reported a pause.
			// Requeue and re-route the same failure next reconcile.
			logger.Error(err, "Failed to persist Paused upgrade phase; retrying")
			return ctrl.Result{RequeueAfter: upgradeStepRequeue}, nil
		}
		recordFailureMetric()
		_ = r.updateClusterStatus(ctx, cluster, "Paused", "Upgrade paused due to failure - manual intervention required")
		r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonUpgradePaused,
			fmt.Sprintf("Upgrade paused: %v", cause))
		return ctrl.Result{}, nil // no auto-requeue: manual intervention required
	}

	if err := r.patchUpgradeStatus(ctx, cluster, func(s *neo4jv1beta1.UpgradeStatus) {
		s.Phase = upgradePhaseFailed
		s.LastError = cause.Error()
		s.CurrentStep = "Rolling upgrade failed"
		s.Message = s.CurrentStep
	}); err != nil {
		// Same gating as the Paused branch: without the persisted Failed phase
		// the machine stays active; retry the failure routing next reconcile.
		logger.Error(err, "Failed to persist Failed upgrade phase; retrying")
		return ctrl.Result{RequeueAfter: upgradeStepRequeue}, nil
	}
	recordFailureMetric()
	r.Recorder.Eventf(cluster, corev1.EventTypeWarning, EventReasonUpgradeFailed,
		"Rolling upgrade failed: %v", cause)
	_ = r.updateClusterStatus(ctx, cluster, "Failed", fmt.Sprintf("Rolling upgrade failed: %v", cause))
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, cause
}

// isServerAvailable is a one-shot SHOW SERVERS check: does the server whose
// bolt address contains podName report Enabled+Available?
func (r *Neo4jEnterpriseClusterReconciler) isServerAvailable(
	ctx context.Context,
	cluster *neo4jv1beta1.Neo4jEnterpriseCluster,
	podName string,
) (bool, error) {
	neo4jClient, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		return false, err
	}
	defer func() { _ = neo4jClient.Close() }()

	servers, err := neo4jClient.GetServerList(ctx)
	if err != nil {
		return false, err
	}
	for _, s := range servers {
		if strings.Contains(s.Address, podName) && s.State == "Enabled" && s.Health == "Available" {
			return true, nil
		}
	}
	return false, nil
}
