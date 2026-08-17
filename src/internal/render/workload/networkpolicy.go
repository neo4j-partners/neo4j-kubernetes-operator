package workload

import (
	"fmt"
	"net"

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

// ValidateNetworkPolicy rejects enabled policies without real ingress peers (NEO-010).
// Empty From on client ports would allow every pod in the cluster — that is no longer accepted.
func ValidateNetworkPolicy(neo4j *neo4jv1beta1.Neo4j) error {
	if !NetworkPolicyEnabled(neo4j) {
		return nil
	}
	np := neo4j.Spec.Security.NetworkPolicy
	if len(np.IngressFrom) == 0 {
		return fmt.Errorf("spec.security.networkPolicy.ingressFrom is required when networkPolicy.enabled is true (NEO-010)")
	}
	if err := validatePeers("spec.security.networkPolicy.ingressFrom", np.IngressFrom); err != nil {
		return err
	}
	if err := validatePeers("spec.security.networkPolicy.backupFrom", np.BackupFrom); err != nil {
		return err
	}
	if err := validatePeers("spec.security.networkPolicy.metricsFrom", np.MetricsFrom); err != nil {
		return err
	}
	return nil
}

func validatePeers(path string, peers []networkingv1.NetworkPolicyPeer) error {
	for i, p := range peers {
		if p.PodSelector == nil && p.NamespaceSelector == nil && p.IPBlock == nil {
			return fmt.Errorf("%s[%d]: set podSelector, namespaceSelector, and/or ipBlock", path, i)
		}
		if p.IPBlock == nil {
			continue
		}
		if isUnrestrictedCIDR(p.IPBlock.CIDR) {
			return fmt.Errorf("%s[%d].ipBlock.cidr %q allows all sources; use a narrower range (NEO-010)", path, i, p.IPBlock.CIDR)
		}
	}
	return nil
}

func isUnrestrictedCIDR(cidr string) bool {
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	ones, bits := n.Mask.Size()
	return ones == 0 && (bits == 32 || bits == 128)
}

// NetworkPolicy builds an opt-in policy for Neo4j pods (all pools).
// Client / backup / metrics ports use explicit From peers (NEO-010).
// Cluster internal ports accept traffic only from pods in the same namespace.
func NetworkPolicy(ctx render.Context) *networkingv1.NetworkPolicy {
	np := ctx.Neo4j.Spec.Security.NetworkPolicy
	clientFrom := copyPeers(np.IngressFrom)

	var rules []networkingv1.NetworkPolicyIngressRule
	if ports := connectorPorts(ctx); len(ports) > 0 {
		rules = append(rules, networkingv1.NetworkPolicyIngressRule{
			From:  clientFrom,
			Ports: ports,
		})
	}
	if ctx.BackupListenerEnabled() {
		from := clientFrom
		if len(np.BackupFrom) > 0 {
			from = copyPeers(np.BackupFrom)
		}
		rules = append(rules, networkingv1.NetworkPolicyIngressRule{
			From:  from,
			Ports: []networkingv1.NetworkPolicyPort{tcpPort(ctx.BackupPort())},
		})
	}
	if ctx.MetricsListenerEnabled() {
		from := clientFrom
		if len(np.MetricsFrom) > 0 {
			from = copyPeers(np.MetricsFrom)
		}
		rules = append(rules, networkingv1.NetworkPolicyIngressRule{
			From:  from,
			Ports: []networkingv1.NetworkPolicyPort{tcpPort(ctx.MetricsPort())},
		})
	}
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

func copyPeers(in []networkingv1.NetworkPolicyPeer) []networkingv1.NetworkPolicyPeer {
	out := make([]networkingv1.NetworkPolicyPeer, len(in))
	for i := range in {
		in[i].DeepCopyInto(&out[i])
	}
	return out
}

func tcpPort(p int32) networkingv1.NetworkPolicyPort {
	proto := corev1.ProtocolTCP
	n := intstr.FromInt32(p)
	return networkingv1.NetworkPolicyPort{Protocol: &proto, Port: &n}
}

func connectorPorts(ctx render.Context) []networkingv1.NetworkPolicyPort {
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
	return ports
}

func clusterNetworkPorts() []networkingv1.NetworkPolicyPort {
	return []networkingv1.NetworkPolicyPort{
		tcpPort(5000), tcpPort(6000), tcpPort(7000), tcpPort(7688),
	}
}
