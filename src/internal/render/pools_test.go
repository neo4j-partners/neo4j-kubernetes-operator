package render

import (
	"testing"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestActivePoolsStandalone(t *testing.T) {
	neo4j := &neo4jv1.Neo4j{
		Spec: neo4jv1.Neo4jSpec{
			Topology: neo4jv1.TopologySpec{Mode: neo4jv1.TopologyModeStandalone},
		},
	}
	pools := ActivePools(neo4j)
	if len(pools) != 1 || pools[0] != PoolServer {
		t.Fatalf("pools = %#v", pools)
	}
}

func TestActivePoolsCluster(t *testing.T) {
	neo4j := &neo4jv1.Neo4j{
		Spec: neo4jv1.Neo4jSpec{
			Topology: neo4jv1.TopologySpec{
				Mode: neo4jv1.TopologyModeCluster,
				Primaries: &neo4jv1.PrimariesSpec{
					Members: 3,
				},
				Secondaries: &neo4jv1.SecondariesSpec{
					Analytics: &neo4jv1.SecondaryPoolSpec{Members: 1},
					Read:      &neo4jv1.SecondaryPoolSpec{Members: 2},
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
	neo4j := &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod"},
		Spec: neo4jv1.Neo4jSpec{
			Topology: neo4jv1.TopologySpec{
				Mode: neo4jv1.TopologyModeCluster,
				Primaries: &neo4jv1.PrimariesSpec{
					Members: 3,
				},
				Secondaries: &neo4jv1.SecondariesSpec{
					Analytics: &neo4jv1.SecondaryPoolSpec{Members: 1},
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

// cpuReq returns the pool's CPU request as a string (map indexing avoids the pointer-receiver
// Cpu() method on a non-addressable value).
func cpuReq(ctx Context) string {
	q := ctx.PoolResources().Requests[corev1.ResourceCPU]
	return q.String()
}

func TestPoolResourcesOverrideAndFallback(t *testing.T) {
	global := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
	}
	analytics := &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8")},
	}
	neo4j := &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod"},
		Spec: neo4jv1.Neo4jSpec{
			Resources: global,
			Topology: neo4jv1.TopologySpec{
				Mode:      neo4jv1.TopologyModeCluster,
				Primaries: &neo4jv1.PrimariesSpec{Members: 3},
				Secondaries: &neo4jv1.SecondariesSpec{
					Analytics: &neo4jv1.SecondaryPoolSpec{Members: 1, Resources: analytics},
					Read:      &neo4jv1.SecondaryPoolSpec{Members: 2},
				},
			},
		},
	}

	// Analytics has an override: it wins, replacing the global block.
	if got := cpuReq(ContextForPool(neo4j, PoolAnalytics)); got != "8" {
		t.Fatalf("analytics cpu = %q, want 8 (pool override)", got)
	}
	// Primaries and read have no override: they inherit the global block.
	if got := cpuReq(ContextForPool(neo4j, PoolPrimary)); got != "1" {
		t.Fatalf("primary cpu = %q, want 1 (global fallback)", got)
	}
	if got := cpuReq(ContextForPool(neo4j, PoolRead)); got != "1" {
		t.Fatalf("read cpu = %q, want 1 (global fallback)", got)
	}
}

func TestConfigMapNamePerPool(t *testing.T) {
	neo4j := &neo4jv1.Neo4j{ObjectMeta: metav1.ObjectMeta{Name: "prod"}}
	if got := StandaloneContext(neo4j).ConfigMapName(); got != "prod-config" {
		t.Fatalf("standalone config = %q", got)
	}
	if got := ContextForPool(neo4j, PoolPrimary).ConfigMapName(); got != "prod-primary-config" {
		t.Fatalf("primary config = %q", got)
	}
}
