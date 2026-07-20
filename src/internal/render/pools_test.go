package render

import (
	"testing"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestActivePoolsStandalone(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
		},
	}
	pools := ActivePools(neo4j)
	if len(pools) != 1 || pools[0] != PoolServer {
		t.Fatalf("pools = %#v", pools)
	}
}

func TestActivePoolsCluster(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode: neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{
					Members: 3,
				},
				Secondaries: &neo4jv1beta1.SecondariesSpec{
					Analytics: &neo4jv1beta1.SecondaryPoolSpec{Members: 1},
					Read:      &neo4jv1beta1.SecondaryPoolSpec{Members: 2},
				},
			},
		},
	}
	pools := ActivePools(neo4j)
	want := []PoolID{PoolPrimary, PoolAnalytics, PoolRead}
	if len(pools) != len(want) {
		t.Fatalf("pools = %#v, want %#v", pools, want)
	}
	for i := range want {
		if pools[i] != want[i] {
			t.Fatalf("pools[%d] = %q, want %q", i, pools[i], want[i])
		}
	}
}

func TestPoolReplicasCluster(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{
				Mode: neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{
					Members: 3,
				},
				Secondaries: &neo4jv1beta1.SecondariesSpec{
					Analytics: &neo4jv1beta1.SecondaryPoolSpec{Members: 1},
				},
			},
		},
	}
	if got := ContextForPool(neo4j, PoolPrimary).PoolReplicas(); got != 3 {
		t.Fatalf("primary replicas = %d", got)
	}
	if got := ContextForPool(neo4j, PoolAnalytics).PoolReplicas(); got != 1 {
		t.Fatalf("analytics replicas = %d", got)
	}
}

func TestConfigMapNamePerPool(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{ObjectMeta: metav1.ObjectMeta{Name: "prod"}}
	if got := StandaloneContext(neo4j).ConfigMapName(); got != "prod-config" {
		t.Fatalf("standalone config = %q", got)
	}
	if got := ContextForPool(neo4j, PoolPrimary).ConfigMapName(); got != "prod-primary-config" {
		t.Fatalf("primary config = %q", got)
	}
}
