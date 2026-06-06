/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package resources

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// netpolTCP is a package-local pointer to corev1.ProtocolTCP for use as
// NetworkPolicyPort.Protocol (which is *corev1.Protocol).
var netpolTCP = func() *corev1.Protocol { p := corev1.ProtocolTCP; return &p }()

// Standalone-pod label key: the standalone controller labels its Pods with
// only `app: <standalone-name>` (see neo4jenterprisestandalone_controller.go
// line ~1000). NetworkPolicy podSelector / from.podSelector blocks below
// must match that scheme exactly.
const standalonePodAppLabel = "app"

// BuildNetworkPolicyForEnterprise returns the ingress NetworkPolicy that
// hardens cluster server pods, or nil if spec.networkPolicy.enabled is unset
// or false.
//
// Closes Neo4j ops-manual checklist gap #2 (issue #128): without a
// NetworkPolicy ANY pod with network reachability to the {cluster}-client
// or {cluster}-internals Service can hit port 6362 and run
// `neo4j-admin database backup`, copying the entire dataset.
//
// Policy shape (a single NetworkPolicy with three ingress rules):
//
//  1. Public client ports (HTTP/HTTPS/Bolt) — `from: nil` allows any pod
//     to reach 7474/7473/7687. These are designed to be exposed to
//     application workloads in the namespace.
//
//  2. Intra-cluster ports (V2 discovery, RAFT, routing) — restricted to
//     other pods that carry `neo4j.com/cluster: <cluster-name>`. Only
//     peer servers in the same cluster legitimately need 6000 / 7000 /
//     7688.
//
//  3. Backup port (6362) — restricted to operator-managed backup pods.
//     Three matched selectors cover all backup pod shapes:
//     - app.kubernetes.io/component=backup       (one-shot Neo4jBackup Job)
//     - app.kubernetes.io/component=backup-cron  (CronJob children)
//     - neo4j.com/component=backup               (centralized backup STS,
//     built by buildCentralizedBackupStatefulSet)
//
// NetworkPolicy enforcement depends on the cluster's CNI plugin —
// Calico/Cilium/Antrea/Weave enforce, flannel does not. Enabling this on a
// non-enforcing CNI is a safe no-op; the resource is created but has no
// effect on traffic. See docs/user_guide/security.md.
func BuildNetworkPolicyForEnterprise(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) *networkingv1.NetworkPolicy {
	if cluster.Spec.NetworkPolicy == nil || !cluster.Spec.NetworkPolicy.Enabled {
		return nil
	}

	tcp := netpolTCP
	httpPort := intstr.FromInt(HTTPPort)
	httpsPort := intstr.FromInt(HTTPSPort)
	boltPort := intstr.FromInt(BoltPort)
	discoveryPort := intstr.FromInt(DiscoveryPort)
	raftPort := intstr.FromInt(RaftPort)
	routingPort := intstr.FromInt(RoutingPort)
	transactionPort := intstr.FromInt(TransactionPort)
	backupPort := intstr.FromInt(BackupPort)
	metricsPort := intstr.FromInt(MetricsPort)

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-server-netpol", cluster.Name),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "neo4j",
				"app.kubernetes.io/instance":   cluster.Name,
				"app.kubernetes.io/component":  "network-policy",
				"app.kubernetes.io/managed-by": "neo4j-operator",
				"neo4j.com/cluster":            cluster.Name,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			// Target: cluster server pods only. Labels here mirror the
			// `getLabelsForEnterpriseServer` output — every server pod
			// carries `neo4j.com/cluster: <name>` plus
			// `app.kubernetes.io/component: database`. We intentionally
			// avoid `app.kubernetes.io/instance` because user customizations
			// elsewhere have historically diverged on that key.
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"neo4j.com/cluster":           cluster.Name,
					"app.kubernetes.io/component": "database",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				// Rule 1: public client + scrape ports — open to any pod
				// in any namespace. From: omitted ⇒ all sources.
				//
				// Port 2004 (Prometheus metrics) is here because
				// scrape mechanisms vary widely (Prometheus operator,
				// kube-prometheus-stack, vendor-specific scrapers,
				// OpenTelemetry collectors) and the policy can't
				// reasonably encode all their Pod-label conventions.
				// "Any pod" matches the Service-level access model —
				// the cluster's ClusterIP Service already exposes 2004
				// inside the namespace, so the NetworkPolicy doesn't
				// add or remove a security boundary here; it just has
				// to not BREAK scrape. The Neo4j docs warn that
				// "you should never expose the Prometheus endpoint
				// directly to the Internet"; that's a Service / Ingress
				// boundary (which we don't expose externally by
				// default) and is documented in security.md.
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: tcp, Port: &httpPort},
						{Protocol: tcp, Port: &httpsPort},
						{Protocol: tcp, Port: &boltPort},
						{Protocol: tcp, Port: &metricsPort},
					},
				},
				// Rule 2: intra-cluster ports — peer servers only. Same
				// label that the cluster headless service uses for
				// pod-to-pod discovery.
				//
				// Port set must mirror the cluster server pod's
				// ContainerPort declarations — search
				// internal/resources/cluster.go for `Ports:
				// []corev1.ContainerPort{` on the Neo4j server
				// container (line numbers shift, the structure
				// doesn't). Missing one here doesn't break the
				// cluster on a
				// non-enforcing CNI but DOES break it on
				// Calico/Cilium/Antrea — pod-to-pod traffic on the
				// missing port silently fails after the policy lands.
				//
				// - 6000: V2 discovery + tcp-tx
				// - 7000: RAFT consensus
				// - 7688: routing service
				// - 7689: transaction streaming / catchup protocol
				//   (store-copy and log shipping between cluster
				//   members). Declared on both the headless Service
				//   and the Pod ContainerPort list; future cluster
				//   modes (read-replicas, store-copy bootstrap) need
				//   this open between peers even if the steady-state
				//   workload doesn't constantly use it.
				{
					From: []networkingv1.NetworkPolicyPeer{
						{PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"neo4j.com/cluster": cluster.Name},
						}},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: tcp, Port: &discoveryPort},
						{Protocol: tcp, Port: &raftPort},
						{Protocol: tcp, Port: &routingPort},
						{Protocol: tcp, Port: &transactionPort},
					},
				},
				// Rule 3: backup port — operator-managed backup pods only.
				// The OR semantics across multiple From peers means a Pod
				// matching ANY of these selectors can connect on 6362.
				{
					From: backupPodPeers(cluster.Name),
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: tcp, Port: &backupPort},
					},
				},
			},
		},
	}
}

// BuildNetworkPolicyForStandalone returns the ingress NetworkPolicy for a
// Neo4jEnterpriseStandalone, or nil if disabled. Standalone is single-pod
// so there are no peer ports — just public client ports and the
// backup-restricted 6362.
func BuildNetworkPolicyForStandalone(standalone *neo4jv1beta1.Neo4jEnterpriseStandalone) *networkingv1.NetworkPolicy {
	if standalone.Spec.NetworkPolicy == nil || !standalone.Spec.NetworkPolicy.Enabled {
		return nil
	}

	tcp := netpolTCP
	httpPort := intstr.FromInt(HTTPPort)
	httpsPort := intstr.FromInt(HTTPSPort)
	boltPort := intstr.FromInt(BoltPort)
	backupPort := intstr.FromInt(BackupPort)
	metricsPort := intstr.FromInt(MetricsPort)

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-standalone-netpol", standalone.Name),
			Namespace: standalone.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "neo4j",
				"app.kubernetes.io/instance":   standalone.Name,
				"app.kubernetes.io/component":  "network-policy",
				"app.kubernetes.io/managed-by": "neo4j-operator",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			// The standalone controller labels its pods with only
			// `app: <standalone-name>` — no neo4j.com/* labels — so the
			// podSelector here matches that minimal scheme.
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					standalonePodAppLabel: standalone.Name,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				// Rule 1: public client + scrape ports — open to any pod.
				// Port 2004 is the Prometheus metrics endpoint; see the
				// rationale on the cluster builder above.
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: tcp, Port: &httpPort},
						{Protocol: tcp, Port: &httpsPort},
						{Protocol: tcp, Port: &boltPort},
						{Protocol: tcp, Port: &metricsPort},
					},
				},
				// Rule 2: backup port — operator-managed backup pods only.
				// The selectors are deliberately identical to the cluster
				// path (one-shot Job, CronJob children, centralized STS) —
				// the standalone backup workflow uses the same Neo4jBackup
				// CR shape, so the same Pod labels apply.
				{
					From: backupPodPeers(standalone.Name),
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: tcp, Port: &backupPort},
					},
				},
			},
		},
	}
}

// backupPodPeers returns the NetworkPolicy peer list that matches all
// operator-managed backup pod shapes for a given Neo4j workload name
// (cluster or standalone). Multiple peers OR together.
//
// Pod label sources (verify before editing):
//   - one-shot Job: `backupLabels(backup, "backup")` in
//     internal/controller/neo4jbackup_controller.go → produces
//     `app.kubernetes.io/component=backup` +
//     `app.kubernetes.io/managed-by=neo4j-operator`
//   - CronJob child Job: same labels with component=backup-cron
//   - centralized backup StatefulSet (cluster only):
//     `buildCentralizedBackupStatefulSet` in cluster.go → produces
//     `neo4j.com/component=backup` + `neo4j.com/cluster=<name>`
//
// `workloadName` is the Neo4jBackup CR's spec.target.name — for centralized
// backup STS pods this matches the cluster name (so the
// neo4j.com/cluster=<name> selector is targeted), and for Neo4jBackup Jobs
// the `app.kubernetes.io/managed-by` selector covers both cluster and
// standalone targets uniformly.
func backupPodPeers(workloadName string) []networkingv1.NetworkPolicyPeer {
	return []networkingv1.NetworkPolicyPeer{
		// One-shot Neo4jBackup Job pods.
		{PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app.kubernetes.io/managed-by": "neo4j-operator",
				"app.kubernetes.io/component":  "backup",
			},
		}},
		// CronJob-spawned scheduled backup Job pods.
		{PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app.kubernetes.io/managed-by": "neo4j-operator",
				"app.kubernetes.io/component":  "backup-cron",
			},
		}},
		// Centralized backup StatefulSet pods (cluster only — standalone
		// has none, but matching with the cluster name in a standalone
		// policy is harmless because nothing will carry that label).
		{PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"neo4j.com/cluster":   workloadName,
				"neo4j.com/component": "backup",
			},
		}},
	}
}
