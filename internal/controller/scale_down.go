/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

import (
	"context"
	"fmt"
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

// serversPendingDrain returns the identifiers of servers still registered with
// the cluster whose pod ordinal is >= desiredServers — i.e. servers slated for
// removal by a scale-down that have NOT yet been deallocated and dropped. They
// linger in SHOW SERVERS (often Enabled but Unavailable once their pod is gone)
// and their databases may be left under-replicated. Servers already in a
// terminal removal state (Deallocated/Dropped) are excluded — they're being
// handled. Pure function so the detection is unit-testable without a cluster.
func serversPendingDrain(servers []neo4jclient.ServerInfo, clusterName string, desiredServers int) []string {
	var pending []string
	for _, s := range servers {
		ord, ok := parseServerOrdinal(s.Address, clusterName)
		if !ok {
			ord, ok = parseServerOrdinal(s.Name, clusterName)
		}
		if !ok || ord < desiredServers {
			continue
		}
		switch strings.ToLower(s.State) {
		case "dropped", "deallocated":
			continue
		}
		// Report the serverId — it's always populated and is what
		// DEALLOCATE/DROP SERVER accept. The `name` column is often empty
		// (defaults to the id) and the bolt `address` (…:7687) is NOT a valid
		// server-management argument. Fall back only if id is somehow missing.
		id := s.ID
		if id == "" {
			id = s.Name
		}
		if id == "" {
			id = s.Address
		}
		pending = append(pending, id)
	}
	return pending
}

// reconcileScaleDownDrainStatus surfaces, as a status condition + a one-shot
// Warning event, any servers left registered beyond spec.topology.servers after
// a scale-down. The operator does not yet auto-deallocate/drop removed servers
// (#173), so this makes the resulting under-replication VISIBLE instead of it
// silently passing the Ready check (which compares against the new, smaller
// server count). Non-fatal: connection / query / status-write failures are
// swallowed — this is observability, never a reconcile gate.
func (r *Neo4jEnterpriseClusterReconciler) reconcileScaleDownDrainStatus(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) {
	logger := log.FromContext(ctx)
	desired := int(cluster.Spec.Topology.Servers)
	if desired <= 0 {
		return
	}

	nc, err := r.createNeo4jClient(ctx, cluster)
	if err != nil {
		logger.V(1).Info("Skipping scale-down drain status: could not create Neo4j client", "error", err)
		return
	}
	defer nc.Close()

	servers, err := nc.ListServers(ctx)
	if err != nil {
		logger.V(1).Info("Skipping scale-down drain status: SHOW SERVERS failed", "error", err)
		return
	}

	pending := serversPendingDrain(servers, cluster.Name, desired)

	status := metav1.ConditionFalse
	reason := ConditionReasonNoServersPendingDrain
	message := "No servers pending drain"
	if len(pending) > 0 {
		status = metav1.ConditionTrue
		reason = ConditionReasonServersPendingDrain
		message = fmt.Sprintf("%d server(s) registered beyond spec.topology.servers=%d and not yet deallocated/dropped: %s. Databases may be under-replicated on these servers — the operator does not yet auto-drain removed servers (#173). Deallocate them (DEALLOCATE DATABASES FROM SERVER) and DROP SERVER manually, or scale back up.",
			len(pending), desired, strings.Join(pending, ", "))
	}

	// Emit the Warning only on transition INTO the pending state to avoid
	// per-reconcile spam.
	prev := findCondition(cluster.Status.Conditions, ConditionTypeServersPendingDrain)
	wasPending := prev != nil && prev.Status == metav1.ConditionTrue

	// Persist via refetch + RetryOnConflict (never write a stale in-memory
	// object — cf. #207).
	writeErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		if getErr := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); getErr != nil {
			return getErr
		}
		SetNamedCondition(&latest.Status.Conditions, ConditionTypeServersPendingDrain,
			latest.Generation, status, reason, message)
		return r.Status().Update(ctx, latest)
	})
	if writeErr != nil {
		logger.Error(writeErr, "Failed to update ServersPendingDrain condition (non-fatal)")
		return
	}

	if len(pending) > 0 && !wasPending {
		r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonScaleDownPendingDrain, message)
	}
}

// ---------------------------------------------------------------------------
// #173 PR2: automated scale-down drain (cordon -> deallocate -> wait -> drop).
// ---------------------------------------------------------------------------

// ScaleDownDrainingAnnotation is set on the cluster CR while the operator is
// draining servers removed by a scale-down. While present, the StatefulSet
// apply holds spec.replicas at the current count so the to-be-removed pods stay
// running (and reachable for data hand-off) until their databases have been
// deallocated and the servers dropped — only then is lowering replicas safe.
// Mirrors RestoreInProgressAnnotation (#117).
const ScaleDownDrainingAnnotation = "neo4j.com/scale-down-draining"

func scaleDownDrainInProgress(owner client.Object) bool {
	if owner == nil {
		return false
	}
	_, draining := owner.GetAnnotations()[ScaleDownDrainingAnnotation]
	return draining
}

// removedServers returns the registered (non-Dropped) servers whose pod ordinal
// is >= desiredServers — the servers a scale-down intends to remove, in ANY
// drain state (Enabled/Cordoned/Deallocating/Deallocated). Dropped servers are
// gone and excluded. (Distinct from serversPendingDrain, which is the PR1
// visibility signal and excludes Deallocated.)
func removedServers(servers []neo4jclient.ServerInfo, clusterName string, desiredServers int) []neo4jclient.ServerInfo {
	var out []neo4jclient.ServerInfo
	for _, s := range servers {
		ord, ok := parseServerOrdinal(s.Address, clusterName)
		if !ok {
			ord, ok = parseServerOrdinal(s.Name, clusterName)
		}
		if !ok || ord < desiredServers {
			continue
		}
		if strings.EqualFold(s.State, "dropped") {
			continue
		}
		out = append(out, s)
	}
	return out
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

func serverIdentifier(s neo4jclient.ServerInfo) string {
	if s.ID != "" {
		return s.ID
	}
	if s.Name != "" {
		return s.Name
	}
	return s.Address
}

// planScaleDownStep decides the next SINGLE drain action from the live states
// of the removed servers (one action per call — requeue-driven): cordon any not
// yet cordoned, then deallocate the cordoned ones, then wait while any are
// deallocating, then drop the deallocated ones. Pure + unit-tested.
func planScaleDownStep(removed []neo4jclient.ServerInfo) scaleDownStep {
	if len(removed) == 0 {
		return scaleDownStep{phase: scaleDownNone}
	}
	var toCordon, toDeallocate, deallocating, toDrop []string
	for _, s := range removed {
		id := serverIdentifier(s)
		switch strings.ToLower(s.State) {
		case "cordoned":
			toDeallocate = append(toDeallocate, id)
		case "deallocating":
			deallocating = append(deallocating, id)
		case "deallocated":
			toDrop = append(toDrop, id)
		default: // enabled / free / unknown → not yet cordoned
			toCordon = append(toCordon, id)
		}
	}
	switch {
	case len(toCordon) > 0:
		return scaleDownStep{phase: scaleDownCordon, serverIDs: toCordon}
	case len(toDeallocate) > 0:
		return scaleDownStep{phase: scaleDownDeallocate, serverIDs: toDeallocate}
	case len(deallocating) > 0:
		return scaleDownStep{phase: scaleDownWaitDeallocating, serverIDs: deallocating}
	case len(toDrop) > 0:
		return scaleDownStep{phase: scaleDownDrop, serverIDs: toDrop}
	default:
		return scaleDownStep{phase: scaleDownNone}
	}
}

// reconcileScaleDownDrain drives the automated drain. It must run BEFORE the
// StatefulSet apply so the ScaleDownDrainingAnnotation it sets holds replicas
// this same reconcile. Detects a pending scale-down (current STS replicas >
// spec.topology.servers), holds replicas, and executes one drain step per
// reconcile: cordon → DRYRUN-feasibility → deallocate → wait → drop. When all
// removed servers are dropped it clears the annotation, releasing the hold so a
// later reconcile lowers replicas. Non-fatal on connection/query errors — it
// keeps holding and retries (never removes pods it couldn't drain).
func (r *Neo4jEnterpriseClusterReconciler) reconcileScaleDownDrain(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster) error {
	logger := log.FromContext(ctx)
	desired := int(cluster.Spec.Topology.Servers)
	if desired <= 0 {
		return nil
	}

	sts := &appsv1.StatefulSet{}
	stsKey := types.NamespacedName{Name: cluster.Name + "-server", Namespace: cluster.Namespace}
	if err := r.Get(ctx, stsKey, sts); err != nil {
		return nil // STS not created yet — nothing to drain
	}
	current := 0
	if sts.Spec.Replicas != nil {
		current = int(*sts.Spec.Replicas)
	}
	if current <= desired {
		// No scale-down pending — ensure we are not holding replicas.
		return r.setScaleDownDraining(ctx, cluster, false)
	}

	// Scale-down pending — hold replicas while we drain.
	if err := r.setScaleDownDraining(ctx, cluster, true); err != nil {
		return err
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

	removed := removedServers(servers, cluster.Name, desired)
	if len(removed) == 0 {
		logger.Info("Scale-down drain complete; releasing replica hold", "desired", desired)
		return r.setScaleDownDraining(ctx, cluster, false)
	}

	step := planScaleDownStep(removed)
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
		// DRYRUN feasibility first — refuse and keep holding if the remaining
		// servers can't satisfy a database topology (never auto-reduce topology).
		if derr := nc.DeallocateServers(ctx, step.serverIDs, true); derr != nil {
			msg := fmt.Sprintf("Scale-down to %d server(s) is blocked: DEALLOCATE dry-run failed: %v. Reduce database topology (ALTER DATABASE ... SET TOPOLOGY) or keep the servers — the operator will not auto-reduce topology. Replicas are held until this is resolvable.", desired, derr)
			r.setScaleDownConditionPersisted(ctx, cluster, metav1.ConditionTrue, ConditionReasonScaleDownBlocked, msg)
			r.Recorder.Event(cluster, corev1.EventTypeWarning, EventReasonScaleDownBlocked, msg)
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
				logger.Error(derr, "Scale-down drain: drop failed", "server", id)
				return nil
			}
		}
		r.Recorder.Eventf(cluster, corev1.EventTypeNormal, EventReasonScaleDownDraining,
			"Scale-down: dropped %d drained server(s): %s", len(step.serverIDs), strings.Join(step.serverIDs, ", "))
	}
	return nil
}

// setScaleDownDraining sets or clears ScaleDownDrainingAnnotation on the cluster
// CR — both in memory (so THIS reconcile's StatefulSet apply sees it) and
// persisted (refetch + RetryOnConflict).
func (r *Neo4jEnterpriseClusterReconciler) setScaleDownDraining(ctx context.Context, cluster *neo4jv1beta1.Neo4jEnterpriseCluster, draining bool) error {
	_, has := cluster.GetAnnotations()[ScaleDownDrainingAnnotation]
	if has == draining {
		return nil
	}
	if draining {
		if cluster.Annotations == nil {
			cluster.Annotations = map[string]string{}
		}
		cluster.Annotations[ScaleDownDrainingAnnotation] = "true"
	} else {
		delete(cluster.Annotations, ScaleDownDrainingAnnotation)
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &neo4jv1beta1.Neo4jEnterpriseCluster{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
			return err
		}
		if latest.Annotations == nil {
			latest.Annotations = map[string]string{}
		}
		if draining {
			latest.Annotations[ScaleDownDrainingAnnotation] = "true"
		} else {
			delete(latest.Annotations, ScaleDownDrainingAnnotation)
		}
		return r.Update(ctx, latest)
	})
}

// setScaleDownConditionPersisted writes the ServersPendingDrain condition with a
// scale-down-specific reason (refetch + RetryOnConflict — never a stale write).
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
