package workload

import (
	"strings"
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func sameNamespaceClients() []networkingv1.NetworkPolicyPeer {
	return []networkingv1.NetworkPolicyPeer{{
		PodSelector: &metav1.LabelSelector{},
	}}
}

func TestNetworkPolicyStandaloneClientPorts(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Security: &neo4jv1beta1.SecuritySpec{
				NetworkPolicy: &neo4jv1beta1.NetworkPolicySpec{
					Enabled:     true,
					IngressFrom: sameNamespaceClients(),
				},
			},
		},
	}
	if !NetworkPolicyEnabled(neo4j) {
		t.Fatal("expected enabled")
	}
	if err := ValidateNetworkPolicy(neo4j); err != nil {
		t.Fatal(err)
	}
	np := NetworkPolicy(render.StandaloneContext(neo4j))
	if np.Name != "dev-network-policy" {
		t.Fatalf("name = %q", np.Name)
	}
	if len(np.Spec.Ingress) != 1 {
		t.Fatalf("standalone ingress rules = %d", len(np.Spec.Ingress))
	}
	if len(np.Spec.Ingress[0].From) == 0 {
		t.Fatal("client From must not be empty (NEO-010)")
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
				NetworkPolicy: &neo4jv1beta1.NetworkPolicySpec{
					Enabled:     true,
					IngressFrom: sameNamespaceClients(),
				},
			},
		},
	}
	np := NetworkPolicy(render.ContextForPool(neo4j, render.PoolPrimary))
	if len(np.Spec.Ingress) != 2 {
		t.Fatalf("cluster ingress rules = %d", len(np.Spec.Ingress))
	}
	if len(np.Spec.Ingress[0].From) == 0 {
		t.Fatal("client From must not be empty")
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

func TestValidateNetworkPolicyRequiresIngressFrom(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Security: &neo4jv1beta1.SecuritySpec{
				NetworkPolicy: &neo4jv1beta1.NetworkPolicySpec{Enabled: true},
			},
		},
	}
	err := ValidateNetworkPolicy(neo4j)
	if err == nil || !strings.Contains(err.Error(), "ingressFrom") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNetworkPolicyRejectsAllowAllCIDR(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Security: &neo4jv1beta1.SecuritySpec{
				NetworkPolicy: &neo4jv1beta1.NetworkPolicySpec{
					Enabled: true,
					IngressFrom: []networkingv1.NetworkPolicyPeer{{
						IPBlock: &networkingv1.IPBlock{CIDR: "0.0.0.0/0"},
					}},
				},
			},
		},
	}
	err := ValidateNetworkPolicy(neo4j)
	if err == nil || !strings.Contains(err.Error(), "0.0.0.0/0") {
		t.Fatalf("got %v", err)
	}
}

func TestNetworkPolicySplitsBackupFrom(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Features: &neo4jv1beta1.FeaturesSpec{
				Backup: &neo4jv1beta1.BackupFeatureSpec{Enabled: true},
			},
			Connectivity: &neo4jv1beta1.ConnectivitySpec{
				Listeners: &neo4jv1beta1.ConnectivityListenersSpec{
					Backup: ptrInt32(6362),
				},
			},
			Security: &neo4jv1beta1.SecuritySpec{
				NetworkPolicy: &neo4jv1beta1.NetworkPolicySpec{
					Enabled: true,
					IngressFrom: []networkingv1.NetworkPolicyPeer{{
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "clients"},
						},
					}},
					BackupFrom: []networkingv1.NetworkPolicyPeer{{
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "backup"},
						},
					}},
				},
			},
		},
	}
	np := NetworkPolicy(render.StandaloneContext(neo4j))
	if len(np.Spec.Ingress) != 2 {
		t.Fatalf("rules = %d", len(np.Spec.Ingress))
	}
	backup := np.Spec.Ingress[1]
	if backup.From[0].PodSelector.MatchLabels["app"] != "backup" {
		t.Fatalf("backup from = %#v", backup.From)
	}
	if int32(backup.Ports[0].Port.IntValue()) != 6362 {
		t.Fatalf("backup port = %v", backup.Ports[0].Port)
	}
}

func ptrInt32(v int32) *int32 { return &v }
