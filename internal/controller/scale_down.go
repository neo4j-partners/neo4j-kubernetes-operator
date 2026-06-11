/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	neo4jclient "github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
)

// parseServerOrdinal extracts the StatefulSet pod ordinal N from a Neo4j server
// address or name of the form "<cluster>-server-<N>[...]". Server identity in
// SHOW SERVERS is by address; the operator's pods are "<cluster>-server-<N>",
// so the ordinal tells us which server a row corresponds to (rule 47).
func parseServerOrdinal(s, clusterName string) (int, bool) {
	prefix := clusterName + "-server-"
	idx := strings.Index(s, prefix)
	if idx < 0 {
		return 0, false
	}
	rest := s[idx+len(prefix):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0, false
	}
	return n, true
}

// ---------------------------------------------------------------------------
// #173 PR2: automated scale-down drain (cordon -> deallocate -> wait -> drop).
// ---------------------------------------------------------------------------

// ScaleDownDrainingServersAnnotation holds the comma-separated Neo4j serverIds
// the operator is draining for an in-progress scale-down. Its presence gates
// the StatefulSet apply (scaleDownDrainInProgress) to HOLD replicas until the
// drain finishes. Tracking by serverId — NOT pod ordinal — is essential: once a
// server enters Deallocating its SHOW SERVERS address becomes NULL (and name is
// just the UUID), so the ordinal can no longer be derived. The id set is
// captured once at detection (while addresses are present), then driven until
// every id is Dropped/gone. Also makes the drain resumable after an operator
// restart.
const ScaleDownDrainingServersAnnotation = "neo4j.com/scale-down-draining-servers"

func scaleDownDrainInProgress(owner client.Object) bool {
	if owner == nil {
		return false
	}
	return strings.TrimSpace(owner.GetAnnotations()[ScaleDownDrainingServersAnnotation]) != ""
}

func drainServerIDsFromAnnotation(owner client.Object) []string {
	if owner == nil {
		return nil
	}
	var ids []string
	for _, p := range strings.Split(owner.GetAnnotations()[ScaleDownDrainingServersAnnotation], ",") {
		if p = strings.TrimSpace(p); p != "" {
			ids = append(ids, p)
		}
	}
	return ids
}

// scaleDownConditionIs reports whether the ServersPendingDrain condition is
// already True with the given reason — used to emit the Blocked Warning event
// only on transition, not every reconcile (the condition was fetched fresh at
// the start of this reconcile, so it reflects the persisted state).
func scaleDownConditionIs(cluster *neo4jv1beta1.Neo4jEnterpriseCluster, reason string) bool {
	c := findCondition(cluster.Status.Conditions, ConditionTypeServersPendingDrain)
	return c != nil && c.Status == metav1.ConditionTrue && c.Reason == reason
}

func serverIdentifier(s neo4jclient.ServerInfo) string {
	if s.ID != "" {
		return s.ID
	}
	if s.Name != "" {
		return s.Name
	}
	return s.Address
}

// initialRemovedServerIDs returns the serverIds to drain for a scale-down,
// computed from servers whose pod ordinal is >= desiredServers (matched on the
// still-present address/name). Dropped servers excluded. Used at detection time
// while addresses are intact; thereafter the set is tracked by id.
func initialRemovedServerIDs(servers []neo4jclient.ServerInfo, clusterName string, desiredServers int) []string {
	var ids []string
	for _, s := range servers {
		if strings.EqualFold(s.State, "dropped") {
			continue
		}
		ord, ok := parseServerOrdinal(s.Address, clusterName)
		if !ok {
			ord, ok = parseServerOrdinal(s.Name, clusterName)
		}
		if ok && ord >= desiredServers {
			ids = append(ids, serverIdentifier(s))
		}
	}
	return ids
}

type scaleDownPhase int

const (
	scaleDownNone scaleDownPhase = iota
	scaleDownCordon
	scaleDownDeallocate
	scaleDownWaitDeallocating
	scaleDownDrop
)

type scaleDownStep struct {
	phase     scaleDownPhase
	serverIDs []string
}

// planScaleDownStep decides the next SINGLE drain action from the live states
// of the still-active drain-target servers (one action per call —
// requeue-driven): cordon any not yet cordoned, then deallocate the cordoned
// ones, then wait while any are deallocating, then drop the deallocated ones.
// Pure + unit-tested.
func planScaleDownStep(active []neo4jclient.ServerInfo) scaleDownStep {
	if len(active) == 0 {
		return scaleDownStep{phase: scaleDownNone}
	}
	var toCordon, toDeallocate, waiting, toDrop []string
	for _, s := range active {
		id := serverIdentifier(s)
		switch strings.ToLower(s.State) {
		case "cordoned":
			toDeallocate = append(toDeallocate, id)
		case "deallocating", "deallocated":
			// A draining server is droppable once it hosts ONLY `system` (all
			// user databases relocated). It often never leaves the
			// "Deallocating" state — a system-primary server's `system` copy is
			// only released by DROP itself — so we must NOT wait for the
			// "Deallocated" state; gate on hosting instead. DROP enforces the
			// system voting-member floor and refuses if the result is below it.
			if hostsOnlySystem(s) {
				toDrop = append(toDrop, id)
			} else {
				waiting = append(waiting, id)
			}
		default: // enabled / free / unknown → not yet cordoned
			toCordon = append(toCordon, id)
		}
	}
	switch {
	case len(toCordon) > 0:
		return scaleDownStep{phase: scaleDownCordon, serverIDs: toCordon}
	case len(toDeallocate) > 0:
		return scaleDownStep{phase: scaleDownDeallocate, serverIDs: toDeallocate}
	case len(toDrop) > 0:
		return scaleDownStep{phase: scaleDownDrop, serverIDs: toDrop}
	case len(waiting) > 0:
		return scaleDownStep{phase: scaleDownWaitDeallocating, serverIDs: waiting}
	default:
		return scaleDownStep{phase: scaleDownNone}
	}
}

// hostsOnlySystem reports whether a server hosts no user databases — only the
// `system` database (or nothing). Such a draining server is safe to DROP.
func hostsOnlySystem(s neo4jclient.ServerInfo) bool {
	for _, h := range s.Hosting {
		if !strings.EqualFold(h, "system") {
			return false
		}
	}
	return true
}

// reconcileScaleDownDrain drives the automated drain. It runs BEFORE the
// StatefulSet apply so the annotation it sets holds replicas this same
// reconcile. Detects a pending scale-down (current STS replicas >
// spec.topology.servers), captures the removed serverIds, and executes one
// drain step per reconcile (cordon → DRYRUN-feasibility → deallocate → wait →
// drop) tracked BY ID. Only once every target is Dropped/gone does it clear the
// annotation, releasing the hold so a later reconcile lowers replicas. Non-fatal
// on connect/query errors — it keeps holding and retries (never removes pods it
// couldn't drain).
func (r *Neo4jEnterpriseClusterReconciler) reconcileScaleDownDrain(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	desired := int(cluster.Spec.Topology.Servers)
	if desired <= 0 {
		return nil
	}

	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: cluster.Name + "-server", Namespace: cluster.Namespace}, sts); err != nil {
		return nil // STS not created yet — nothing to drain
	}
	current := 0
	if sts.Spec.Replicas != nil {
		current = int(*sts.Spec.Replicas)
	}

	if current <= desired {
		// Not scaling down (or scaled back up mid-drain) — cancel any hold so
		// replicas reconcile normally, and reset a stale ServersPendingDrain
		// condition (e.g. a prior ScaleDownBlocked) so aborting clears it.
		// Servers not yet cordoned are untouched.
		return r.clearScaleDownState(ctx, cluster)
	}

	// Engage the replica hold THIS reconcile, before we touch Neo4j. The hold is
	// gated on the draining annotation; if we returned on a connect/SHOW SERVERS
	// error below WITHOUT a hold already in place, this same reconcile's
	// StatefulSet apply would lower replicas and delete pods before any drain
	// ran. So if no drain is recorded yet, seed the annotation with the
	// to-be-removed pod NAMES (derived from STS ordinals — no Neo4j needed).
	// They only need to be non-empty to hold; once SHOW SERVERS succeeds they
	// are replaced by real serverIds (and evicted from the set — they never
	// match a live server, which is keyed by id).
	if !scaleDownDrainInProgress(cluster) {
		if err := r.setDrainServers(ctx, cluster, provisionalDrainHoldNames(cluster.Name, current, desired)); err != nil {
			return err
		}
	}

	nc, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.Info("Scale-down drain: cannot connect yet, holding replicas", "error", err)
		return nil
	}
	defer nc.Close()

	servers, err := nc.ListServers(ctx)
	if err != nil {
		logger.Info("Scale-down drain: SHOW SERVERS failed, holding replicas", "error", err)
		return nil
	}

	live := map[string]neo4jclient.ServerInfo{}
	for _, s := range servers {
		live[serverIdentifier(s)] = s
	}

	// Drain-target id set = (currently removable by ordinal) ∪ (persisted ids
	// still present as live servers). The union absorbs further scale-downs
	// mid-drain; keeping a persisted id only while it's still a live server
	// covers a Deallocating server whose address went NULL (initialRemoved can't
	// derive its ordinal, but its id row persists) AND evicts both the
	// provisional pod-name hold seeded above and servers already Dropped + gone.
	idSet := map[string]bool{}
	for _, id := range initialRemovedServerIDs(servers, cluster.Name, desired) {
		idSet[id] = true
	}
	for _, id := range drainServerIDsFromAnnotation(cluster) {
		if s, ok := live[id]; ok && !strings.EqualFold(s.State, "dropped") {
			idSet[id] = true
		}
	}
	if len(idSet) == 0 {
		return r.clearScaleDownState(ctx, cluster)
	}

	var active []neo4jclient.ServerInfo
	for id := range idSet {
		s, present := live[id]
		if !present {
			continue // dropped + pod stopped → gone
		}
		if strings.EqualFold(s.State, "dropped") {
			continue // dropped; pod will stop once we release the hold
		}
		active = append(active, s)
	}

	if len(active) == 0 {
		logger.Info("Scale-down drain complete; releasing replica hold", "desired", desired)
		r.setScaleDownConditionPersisted(ctx, cluster, metav1.ConditionFalse, ConditionReasonNoServersPendingDrain, "Scale-down drain complete")
		return r.setDrainServers(ctx, cluster, nil)
	}

	// Persist/refresh the target set (also holds replicas via the annotation).
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if err := r.setDrainServers(ctx, cluster, ids); err != nil {
		return err
	}

	// Pre-flight floor: a cluster cannot be scaled below the system database's
	// minimum voting members (dbms.cluster.minimum_initial_system_primaries_count,
	// default 3). Neo4j only rejects this at DROP time — AFTER the irreversible
	// deallocate — which permanently strands the server (a Deallocating server
	// can't be re-enabled). Refuse up front, holding replicas, WITHOUT cordoning
	// or deallocating anything.
	if minPrimaries := nc.MinimumSystemPrimaries(ctx); desired < minPrimaries {
		msg := fmt.Sprintf("Refusing to scale this cluster to %d server(s): a Neo4j cluster cannot drop below %d servers — the system database requires at least %d voting members (dbms.cluster.minimum_initial_system_primaries_count). Scaling lower would strand servers (a deallocated server can't rejoin). Keep at least %d servers, or use Neo4jEnterpriseStandalone for a single node. Replicas held at %d.",
			desired, minPrimaries, minPrimaries, minPrimaries, current)
		if !scaleDownConditionIs(cluster, ConditionReasonScaleDownBlocked) {
			r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonScaleDownBlocked, msg)
		}
		r.setScaleDownConditionPersisted(ctx, cluster, metav1.ConditionTrue, ConditionReasonScaleDownBlocked, msg)
		return nil
	}

	// Fail fast: while no target has started deallocating yet, dry-run the whole
	// set up front so an infeasible scale-down (e.g. a single-primary database on
	// a removed server, or a topology the survivors can't satisfy) blocks
	// IMMEDIATELY — before we cordon anything (cordon is a visible side-effect)
	// and without a reconcile round-trip. The per-phase dry-run below still
	// guards the moment of the real deallocate.
	preDeallocate := true
	for _, s := range active {
		if st := strings.ToLower(s.State); st == "deallocating" || st == "deallocated" {
			preDeallocate = false
			break
		}
	}
	if preDeallocate {
		activeIDs := make([]string, 0, len(active))
		for _, s := range active {
			activeIDs = append(activeIDs, serverIdentifier(s))
		}
		if derr := nc.DeallocateServers(ctx, activeIDs, true); derr != nil {
			msg := fmt.Sprintf("Scale-down to %d server(s) is blocked: DEALLOCATE dry-run failed: %v. Give single-primary databases an additional primary (ALTER DATABASE ... SET TOPOLOGY) or keep the servers — the operator will not auto-reduce topology. No server has been cordoned; replicas are held until this is resolvable.", desired, derr)
			if !scaleDownConditionIs(cluster, ConditionReasonScaleDownBlocked) {
				r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonScaleDownBlocked, msg)
			}
			r.setScaleDownConditionPersisted(ctx, cluster, metav1.ConditionTrue, ConditionReasonScaleDownBlocked, msg)
			return nil
		}
	}

	step := planScaleDownStep(active)
	switch step.phase {
	case scaleDownCordon:
		for _, id := range step.serverIDs {
			if cerr := nc.CordonServer(ctx, id); cerr != nil {
				logger.Error(cerr, "Scale-down drain: cordon failed", "server", id)
				return nil
			}
		}
		r.Recorder.Eventf(cluster, corev1.EventTypeNormal, EventReasonScaleDownDraining,
			"Scale-down: cordoned %d server(s): %s", len(step.serverIDs), strings.Join(step.serverIDs, ", "))
	case scaleDownDeallocate:
		if derr := nc.DeallocateServers(ctx, step.serverIDs, true); derr != nil {
			msg := fmt.Sprintf("Scale-down to %d server(s) is blocked: DEALLOCATE dry-run failed: %v. Reduce database topology (ALTER DATABASE ... SET TOPOLOGY) or keep the servers — the operator will not auto-reduce topology. Replicas are held until this is resolvable.", desired, derr)
			if !scaleDownConditionIs(cluster, ConditionReasonScaleDownBlocked) {
				r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonScaleDownBlocked, msg)
			}
			r.setScaleDownConditionPersisted(ctx, cluster, metav1.ConditionTrue, ConditionReasonScaleDownBlocked, msg)
			return nil
		}
		if derr := nc.DeallocateServers(ctx, step.serverIDs, false); derr != nil {
			logger.Error(derr, "Scale-down drain: deallocate failed", "servers", step.serverIDs)
			return nil
		}
		r.Recorder.Eventf(cluster, corev1.EventTypeNormal, EventReasonScaleDownDraining,
			"Scale-down: deallocating databases from %d server(s): %s", len(step.serverIDs), strings.Join(step.serverIDs, ", "))
	case scaleDownWaitDeallocating:
		logger.Info("Scale-down drain: waiting for servers to finish deallocating", "servers", step.serverIDs)
	case scaleDownDrop:
		for _, id := range step.serverIDs {
			if derr := nc.DropServer(ctx, id); derr != nil {
				// A DROP failure here is typically the system-db minimum-voting-
				// members floor (defensive — the pre-flight above normally
				// catches it). Surface it as blocked rather than looping.
				msg := fmt.Sprintf("Scale-down to %d server(s) is blocked: DROP SERVER %s failed: %v. This usually means the drop would take the system database below its minimum voting members. Keep at least the system minimum number of servers. Replicas are held.", desired, id, derr)
				if !scaleDownConditionIs(cluster, ConditionReasonScaleDownBlocked) {
					r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonScaleDownBlocked, msg)
				}
				r.setScaleDownConditionPersisted(ctx, cluster, metav1.ConditionTrue, ConditionReasonScaleDownBlocked, msg)
				return nil
			}
		}
		r.Recorder.Eventf(cluster, corev1.EventTypeNormal, EventReasonScaleDownDraining,
			"Scale-down: dropped %d drained server(s): %s", len(step.serverIDs), strings.Join(step.serverIDs, ", "))
	}

	r.setScaleDownConditionPersisted(ctx, cluster, metav1.ConditionTrue, ConditionReasonServersPendingDrain,
		fmt.Sprintf("Scale-down to %d server(s) in progress: draining %d server(s)", desired, len(active)))
	return nil
}

// provisionalDrainHoldNames returns the StatefulSet pod names of the servers a
// scale-down will remove (ordinals [desired, current)). Used ONLY to seed the
// draining annotation so the replica hold engages before the operator can reach
// Neo4j; once SHOW SERVERS succeeds these are replaced by real serverIds.
func provisionalDrainHoldNames(clusterName string, current, desired int) []string {
	var names []string
	for ord := desired; ord < current; ord++ {
		names = append(names, fmt.Sprintf("%s-server-%d", clusterName, ord))
	}
	return names
}

// clearScaleDownState releases the replica hold (clears the draining annotation)
// and, when the ServersPendingDrain condition is currently True (e.g. a prior
// ScaleDownBlocked or in-progress), resets it to False — so aborting a
// scale-down (scaling back up, or a no-op) never leaves a stale Blocked/InProgress
// status. Both halves are no-ops when already clear, so it is safe to call every
// reconcile.
func (r *Neo4jEnterpriseClusterReconciler) clearScaleDownState(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	if c := findCondition(cluster.Status.Conditions, ConditionTypeServersPendingDrain); c != nil && c.Status == metav1.ConditionTrue {
		r.setScaleDownConditionPersisted(ctx, cluster, metav1.ConditionFalse, ConditionReasonNoServersPendingDrain, "Scale-down not in progress")
	}
	return r.setDrainServers(ctx, cluster, nil)
}

// setDrainServers sets (or clears, when ids is empty) the draining-servers
// annotation on the cluster CR — both in memory (so THIS reconcile's
// StatefulSet apply holds replicas) and persisted (refetch + RetryOnConflict).
func (r *Neo4jEnterpriseClusterReconciler) setDrainServers(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster, ids []string) error {
	val := strings.Join(ids, ",")
	if cluster.GetAnnotations()[ScaleDownDrainingServersAnnotation] == val {
		return nil
	}
	if cluster.Annotations == nil {
		cluster.Annotations = map[string]string{}
	}
	if val == "" {
		delete(cluster.Annotations, ScaleDownDrainingServersAnnotation)
	} else {
		cluster.Annotations[ScaleDownDrainingServersAnnotation] = val
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
			return err
		}
		if latest.Annotations == nil {
			latest.Annotations = map[string]string{}
		}
		if val == "" {
			delete(latest.Annotations, ScaleDownDrainingServersAnnotation)
		} else {
			latest.Annotations[ScaleDownDrainingServersAnnotation] = val
		}
		return r.Update(ctx, latest)
	})
}

// setScaleDownConditionPersisted writes the ServersPendingDrain condition
// (refetch + RetryOnConflict — never a stale write).
func (r *Neo4jEnterpriseClusterReconciler) setScaleDownConditionPersisted(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster, status metav1.ConditionStatus, reason, message string) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		if getErr := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); getErr != nil {
			return getErr
		}
		SetNamedCondition(&latest.Status.Conditions, ConditionTypeServersPendingDrain,
			latest.Generation, status, reason, message)
		return r.Status().Update(ctx, latest)
	})
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to set scale-down condition (non-fatal)")
	}
}
