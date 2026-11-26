package render

import neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"

// IsClusterMode reports whether the CR targets clustered Neo4j (BDR-002).
func IsClusterMode(neo4j *neo4jv1.Neo4j) bool {
	return neo4j.Spec.Topology.Mode == neo4jv1.TopologyModeCluster
}

// ActivePools returns workload pools that should be reconciled for the current topology.
func ActivePools(neo4j *neo4jv1.Neo4j) []PoolID {
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

// secondaryPools are the fixed V1 secondary pools, in teardown order.
var secondaryPools = []PoolID{PoolAnalytics, PoolRead}

// RemovedPools returns the secondary pools the current spec no longer declares (their
// topology.secondaries key was deleted, or the whole block was). A pool listed here may still have
// a running StatefulSet and Enabled servers from an earlier generation: ActivePools stops managing
// a pool the instant its key leaves the spec, so formation uses this to find pools to drain, drop,
// and delete instead of orphaning them. Empty outside cluster mode.
func RemovedPools(neo4j *neo4jv1.Neo4j) []PoolID {
	if !IsClusterMode(neo4j) {
		return nil
	}
	active := map[PoolID]bool{}
	for _, p := range ActivePools(neo4j) {
		active[p] = true
	}
	var out []PoolID
	for _, p := range secondaryPools {
		if !active[p] {
			out = append(out, p)
		}
	}
	return out
}

// ContextForPool builds a render context for the given pool.
func ContextForPool(neo4j *neo4jv1.Neo4j, pool PoolID) Context {
	return NewContext(neo4j, pool)
}

// ClientServiceContext returns the pool context used for the north-south client Service.
func ClientServiceContext(neo4j *neo4jv1.Neo4j) Context {
	if IsClusterMode(neo4j) {
		return NewContext(neo4j, PoolPrimary)
	}
	return StandaloneContext(neo4j)
}
