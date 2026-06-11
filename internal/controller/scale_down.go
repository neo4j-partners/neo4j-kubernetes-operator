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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
