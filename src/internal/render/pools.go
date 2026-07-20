package render

import neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"

// IsClusterMode reports whether the CR targets clustered Neo4j (BDR-002).
func IsClusterMode(neo4j *neo4jv1beta1.Neo4j) bool {
	return neo4j.Spec.Topology.Mode == neo4jv1beta1.TopologyModeCluster
}

// ActivePools returns workload pools that should be reconciled for the current topology.
func ActivePools(neo4j *neo4jv1beta1.Neo4j) []PoolID {
	if !IsClusterMode(neo4j) {
		return []PoolID{PoolServer}
	}

	pools := []PoolID{PoolPrimary}
	if neo4j.Spec.Topology.Secondaries != nil {
		if a := neo4j.Spec.Topology.Secondaries.Analytics; a != nil && a.Members > 0 {
			pools = append(pools, PoolAnalytics)
		}
		if r := neo4j.Spec.Topology.Secondaries.Read; r != nil && r.Members > 0 {
			pools = append(pools, PoolRead)
		}
	}
	return pools
}

// ContextForPool builds a render context for the given pool.
func ContextForPool(neo4j *neo4jv1beta1.Neo4j, pool PoolID) Context {
	return NewContext(neo4j, pool)
}

// ClientServiceContext returns the pool context used for the north-south client Service.
func ClientServiceContext(neo4j *neo4jv1beta1.Neo4j) Context {
	if IsClusterMode(neo4j) {
		return NewContext(neo4j, PoolPrimary)
	}
	return StandaloneContext(neo4j)
}
