/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package resources_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/resources"
)

// TestBuildNetworkPolicyForEnterprise_DisabledByDefault — the operator must
// emit ZERO NetworkPolicies when spec.networkPolicy is unset or
// enabled=false. CNI-enforced policies can break existing workloads if
// dropped onto a running cluster without warning, so the field is opt-in.
func TestBuildNetworkPolicyForEnterprise_DisabledByDefault(t *testing.T) {
	t.Run("nil spec → nil policy", func(t *testing.T) {
		cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
		}
		assert.Nil(t, resources.BuildNetworkPolicyForEnterprise(cluster))
	})
	t.Run("enabled=false → nil policy", func(t *testing.T) {
		cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				NetworkPolicy: &neo4jv1beta1.NetworkPolicySpec{Enabled: false},
			},
		}
		assert.Nil(t, resources.BuildNetworkPolicyForEnterprise(cluster))
	})
}

// TestBuildNetworkPolicyForEnterprise_RestrictsBackupPort is the core
// contract test for issue #128 gap #2: port 6362 must NOT be reachable
// from arbitrary pods. The ingress rule for 6362 must only list backup-pod
// selectors as `from:` sources.
func TestBuildNetworkPolicyForEnterprise_RestrictsBackupPort(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "neo4j-prod"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			NetworkPolicy: &neo4jv1beta1.NetworkPolicySpec{Enabled: true},
		},
	}

	np := resources.BuildNetworkPolicyForEnterprise(cluster)
	require.NotNil(t, np)
	assert.Equal(t, "prod-server-netpol", np.Name)
	assert.Equal(t, "neo4j-prod", np.Namespace)
	require.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, np.Spec.PolicyTypes)

	// Pod selector — only the cluster's server pods are protected.
	assert.Equal(t, "prod", np.Spec.PodSelector.MatchLabels["neo4j.com/cluster"])
	assert.Equal(t, "database", np.Spec.PodSelector.MatchLabels["app.kubernetes.io/component"])

	// Find the rule that covers port 6362.
	var backupRule *networkingv1.NetworkPolicyIngressRule
	for i := range np.Spec.Ingress {
		for _, p := range np.Spec.Ingress[i].Ports {
			if p.Port != nil && p.Port.IntValue() == 6362 {
				backupRule = &np.Spec.Ingress[i]
				break
			}
		}
		if backupRule != nil {
			break
		}
	}
	require.NotNil(t, backupRule, "an ingress rule must explicitly cover port 6362")

	// The rule MUST have a non-empty From — an empty From in a NetworkPolicy
	// ingress rule means "from any source", which would defeat the gap-#2 fix.
	require.NotEmpty(t, backupRule.From,
		"backup-port rule must specify From peers; an empty From means anyone can connect")

	// Verify all three backup-pod selectors are present (one-shot Job,
	// CronJob children, centralized backup STS). Each pin protects against
	// a regression that drops one of the three Pod shapes.
	wantBackupSelectors := []map[string]string{
		{
			"app.kubernetes.io/managed-by": "neo4j-operator",
			"app.kubernetes.io/component":  "backup",
		},
		{
			"app.kubernetes.io/managed-by": "neo4j-operator",
			"app.kubernetes.io/component":  "backup-cron",
		},
		{
			"neo4j.com/cluster":   "prod",
			"neo4j.com/component": "backup",
		},
	}
	for _, want := range wantBackupSelectors {
		found := false
		for _, peer := range backupRule.From {
			if peer.PodSelector != nil && labelsEqual(peer.PodSelector.MatchLabels, want) {
				found = true
				break
			}
		}
		assert.True(t, found, "backup-port rule must include selector %v; got %+v", want, backupRule.From)
	}
}

// TestBuildNetworkPolicyForEnterprise_PublicPortsOpen — HTTP/HTTPS/Bolt
// AND the Prometheus metrics port (2004) must be reachable from any pod.
// HTTP/Bolt are client-facing; 2004 is the scrape endpoint and scrape
// solutions vary too widely to encode in a label selector. Regression
// guard: a future change that adds a From restriction to any of these
// ports would silently break application pods OR Prometheus scrape.
func TestBuildNetworkPolicyForEnterprise_PublicPortsOpen(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			NetworkPolicy: &neo4jv1beta1.NetworkPolicySpec{Enabled: true},
		},
	}
	np := resources.BuildNetworkPolicyForEnterprise(cluster)
	require.NotNil(t, np)

	// 2004 is included to prevent the silent break described in the
	// metrics-audit pass — without it, networkPolicy.enabled=true +
	// monitoring.enabled=true would isolate the Pod from Prometheus.
	for _, port := range []int{7474, 7473, 7687, 2004} {
		rule := findRuleCoveringPort(t, np.Spec.Ingress, port)
		assert.Empty(t, rule.From,
			"public/scrape port %d must have an empty From (allow from any pod); got %+v",
			port, rule.From)
	}
}

// TestBuildNetworkPolicyForEnterprise_PeerPortsRestrictedToCluster —
// cluster ports (6000/7000/7688) must only accept traffic from pods in
// the same cluster (matched by `neo4j.com/cluster: <name>`). Without this
// constraint, any pod could initiate RAFT/discovery handshakes, which is
// both noisy and a potential attack vector.
func TestBuildNetworkPolicyForEnterprise_PeerPortsRestrictedToCluster(t *testing.T) {
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-a", Namespace: "ns"},
		Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
			NetworkPolicy: &neo4jv1beta1.NetworkPolicySpec{Enabled: true},
		},
	}
	np := resources.BuildNetworkPolicyForEnterprise(cluster)
	require.NotNil(t, np)

	for _, port := range []int{6000, 7000, 7688} {
		rule := findRuleCoveringPort(t, np.Spec.Ingress, port)
		require.NotEmpty(t, rule.From, "peer port %d must restrict From", port)
		// Every From peer must select the cluster — no other selectors
		// should slip in (this would let other tenants' pods in).
		for _, peer := range rule.From {
			require.NotNil(t, peer.PodSelector, "peer port %d From must be a podSelector", port)
			assert.Equal(t, "tenant-a", peer.PodSelector.MatchLabels["neo4j.com/cluster"],
				"peer port %d From must select neo4j.com/cluster=tenant-a", port)
		}
	}
}

// TestBuildNetworkPolicyForStandalone covers the standalone shape:
// minimal podSelector (just `app: <name>`), public ports open, backup
// port restricted to operator-managed backup pods.
func TestBuildNetworkPolicyForStandalone(t *testing.T) {
	t.Run("disabled → nil", func(t *testing.T) {
		s := &neo4jv1beta1.Neo4jEnterpriseStandalone{
			ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns"},
		}
		assert.Nil(t, resources.BuildNetworkPolicyForStandalone(s))
	})

	t.Run("enabled → policy with correct shape", func(t *testing.T) {
		s := &neo4jv1beta1.Neo4jEnterpriseStandalone{
			ObjectMeta: metav1.ObjectMeta{Name: "dev-single", Namespace: "neo4j-dev"},
			Spec: neo4jv1beta1.Neo4jEnterpriseStandaloneSpec{
				NetworkPolicy: &neo4jv1beta1.NetworkPolicySpec{Enabled: true},
			},
		}
		np := resources.BuildNetworkPolicyForStandalone(s)
		require.NotNil(t, np)
		assert.Equal(t, "dev-single-standalone-netpol", np.Name)
		// The standalone controller uses `app: <name>` (not the
		// neo4j.com/* labels the cluster uses), so the podSelector must
		// match that minimal scheme — otherwise the policy targets
		// zero pods and silently does nothing.
		assert.Equal(t, "dev-single", np.Spec.PodSelector.MatchLabels["app"])

		// Public ports + Prometheus scrape (2004) open; backup restricted.
		for _, port := range []int{7474, 7473, 7687, 2004} {
			rule := findRuleCoveringPort(t, np.Spec.Ingress, port)
			assert.Empty(t, rule.From, "standalone public/scrape port %d must allow from any pod", port)
		}
		backupRule := findRuleCoveringPort(t, np.Spec.Ingress, 6362)
		require.NotEmpty(t, backupRule.From,
			"standalone backup port 6362 must restrict From — otherwise gap #2 is not closed for standalone")
	})
}

// findRuleCoveringPort returns the (first) ingress rule whose Ports list
// includes the given int port. Fails the test if no such rule exists.
func findRuleCoveringPort(t *testing.T, rules []networkingv1.NetworkPolicyIngressRule, port int) networkingv1.NetworkPolicyIngressRule {
	t.Helper()
	for _, rule := range rules {
		for _, p := range rule.Ports {
			if p.Port != nil && p.Port.IntValue() == port {
				return rule
			}
		}
	}
	t.Fatalf("no ingress rule covers port %d (rules=%+v)", port, rules)
	return networkingv1.NetworkPolicyIngressRule{}
}

// labelsEqual is a small equality check that doesn't bring in
// k8s.io/apimachinery/pkg/labels.
func labelsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
