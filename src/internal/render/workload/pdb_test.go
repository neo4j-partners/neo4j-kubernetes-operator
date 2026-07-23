package workload

import (
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestPodDisruptionBudgetClusterDefaults(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode:      neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{Members: 3},
			},
			PodDisruptionBudget: &neo4jv1beta1.PodDisruptionBudgetSpec{Enabled: true},
		},
	}
	if !PDBEnabled(neo4j) {
		t.Fatal("expected enabled")
	}
	pdb := PodDisruptionBudget(render.ContextForPool(neo4j, render.PoolPrimary))
	if pdb.Name != "prod-pdb" {
		t.Fatalf("name = %q", pdb.Name)
	}
	if pdb.Spec.MinAvailable == nil || pdb.Spec.MinAvailable.IntValue() != 2 {
		t.Fatalf("minAvailable = %#v", pdb.Spec.MinAvailable)
	}
	sel := pdb.Spec.Selector.MatchLabels
	if sel[render.LabelInstance] != "prod" || sel[render.LabelName] != render.AppNameValue {
		t.Fatalf("selector = %#v", sel)
	}
	if _, ok := sel[render.LabelPool]; ok {
		t.Fatal("PDB must select all pools, not a single pool")
	}
}

func TestPodDisruptionBudgetMinAvailableOverride(t *testing.T) {
	min := intstr.FromInt32(3)
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode:      neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{Members: 5},
			},
			PodDisruptionBudget: &neo4jv1beta1.PodDisruptionBudgetSpec{
				Enabled:      true,
				MinAvailable: &min,
			},
		},
	}
	pdb := PodDisruptionBudget(render.ContextForPool(neo4j, render.PoolPrimary))
	if pdb.Spec.MinAvailable.IntValue() != 3 {
		t.Fatalf("minAvailable = %#v", pdb.Spec.MinAvailable)
	}
}

func TestPodDisruptionBudgetStandaloneDefault(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology:            neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			PodDisruptionBudget: &neo4jv1beta1.PodDisruptionBudgetSpec{Enabled: true},
		},
	}
	pdb := PodDisruptionBudget(render.StandaloneContext(neo4j))
	if pdb.Spec.MinAvailable.IntValue() != 1 {
		t.Fatalf("minAvailable = %#v", pdb.Spec.MinAvailable)
	}
}
