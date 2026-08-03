package workload

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

// NetworkPolicyEnabled is true when spec.security.networkPolicy.enabled.
func NetworkPolicyEnabled(neo4j *neo4jv1beta1.Neo4j) bool {
	return neo4j.Spec.Security != nil &&
		neo4j.Spec.Security.NetworkPolicy != nil &&
		neo4j.Spec.Security.NetworkPolicy.Enabled
}

// NetworkPolicyName is the owned NetworkPolicy name.
func NetworkPolicyName(ctx render.Context) string {
	return ctx.Neo4j.Name + "-network-policy"
}

// NetworkPolicy builds an opt-in policy for Neo4j pods (all pools).
// Client/connector ports accept traffic from anywhere (LB / NodePort / in-cluster).
// Cluster internal ports accept traffic only from pods in the same namespace.
func NetworkPolicy(ctx render.Context) *networkingv1.NetworkPolicy {
	rules := []networkingv1.NetworkPolicyIngressRule{{
		Ports: clientNetworkPorts(ctx),
	}}
	if render.IsClusterMode(ctx.Neo4j) {
		rules = append(rules, networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{{
				PodSelector: &metav1.LabelSelector{},
			}},
			Ports: clusterNetworkPorts(),
		})
	}
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      NetworkPolicyName(ctx),
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("networkpolicy"),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: ctx.ClusterMemberSelectorLabels()},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress:     rules,
		},
	}
}

func tcpPort(p int32) networkingv1.NetworkPolicyPort {
	proto := corev1.ProtocolTCP
	n := intstr.FromInt32(p)
	return networkingv1.NetworkPolicyPort{Protocol: &proto, Port: &n}
}

func clientNetworkPorts(ctx render.Context) []networkingv1.NetworkPolicyPort {
	var ports []networkingv1.NetworkPolicyPort
	if ctx.BoltEnabled() {
		ports = append(ports, tcpPort(ctx.BoltPort()))
	}
	if ctx.HTTPEnabled() {
		ports = append(ports, tcpPort(ctx.HTTPPort()))
	}
	if ctx.HTTPSEnabled() {
		ports = append(ports, tcpPort(ctx.HTTPSPort()))
	}
	if ctx.BackupListenerEnabled() {
		ports = append(ports, tcpPort(ctx.BackupPort()))
	}
	if ctx.MetricsListenerEnabled() {
		ports = append(ports, tcpPort(ctx.MetricsPort()))
	}
	return ports
}

func clusterNetworkPorts() []networkingv1.NetworkPolicyPort {
	return []networkingv1.NetworkPolicyPort{
		tcpPort(5000), tcpPort(6000), tcpPort(7000), tcpPort(7688),
	}
}
