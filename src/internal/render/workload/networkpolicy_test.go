package workload

import (
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNetworkPolicyStandaloneClientPorts(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Security: &neo4jv1beta1.SecuritySpec{
				NetworkPolicy: &neo4jv1beta1.NetworkPolicySpec{Enabled: true},
			},
		},
	}
	if !NetworkPolicyEnabled(neo4j) {
		t.Fatal("expected enabled")
	}
	np := NetworkPolicy(render.StandaloneContext(neo4j))
	if np.Name != "dev-network-policy" {
		t.Fatalf("name = %q", np.Name)
	}
	if len(np.Spec.Ingress) != 1 {
		t.Fatalf("standalone ingress rules = %d", len(np.Spec.Ingress))
	}
	ports := map[int32]bool{}
	for _, p := range np.Spec.Ingress[0].Ports {
		ports[int32(p.Port.IntValue())] = true
	}
	if !ports[7687] || !ports[7474] {
		t.Fatalf("client ports = %#v", ports)
	}
	if np.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Fatalf("policyTypes = %#v", np.Spec.PolicyTypes)
	}
}

func TestNetworkPolicyClusterAddsInternalPorts(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode:      neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{Members: 3},
			},
			Security: &neo4jv1beta1.SecuritySpec{
				NetworkPolicy: &neo4jv1beta1.NetworkPolicySpec{Enabled: true},
			},
		},
	}
	np := NetworkPolicy(render.ContextForPool(neo4j, render.PoolPrimary))
	if len(np.Spec.Ingress) != 2 {
		t.Fatalf("cluster ingress rules = %d", len(np.Spec.Ingress))
	}
	if len(np.Spec.Ingress[1].From) != 1 || np.Spec.Ingress[1].From[0].PodSelector == nil {
		t.Fatalf("cluster from = %#v", np.Spec.Ingress[1].From)
	}
	ports := map[int32]bool{}
	for _, p := range np.Spec.Ingress[1].Ports {
		ports[int32(p.Port.IntValue())] = true
	}
	for _, want := range []int32{5000, 6000, 7000, 7688} {
		if !ports[want] {
			t.Fatalf("missing cluster port %d in %#v", want, ports)
		}
	}
}
