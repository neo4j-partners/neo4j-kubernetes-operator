# Neo4j Cluster Fault Tolerance Guide

## Overview

This guide explains the fault tolerance characteristics of different Neo4j cluster topologies and helps you choose the appropriate configuration for your requirements.

## Understanding Neo4j Cluster Consensus

Neo4j Enterprise clusters use a consensus protocol (Raft) among primary nodes to maintain consistency and availability. The number of primary nodes directly impacts fault tolerance behavior.

### Quorum Requirements

- **Quorum Definition**: More than half of the primary nodes must be available for the cluster to remain operational
- **Calculation**: Quorum = (Number of Primaries / 2) + 1

**Note**: With the server-based architecture, cluster size is defined by `Neo4jEnterpriseCluster.spec.topology.servers`. The primary/secondary counts below refer to `Neo4jDatabase.spec.topology` (database-level roles) and require a cluster with enough servers.

## Primary Node Topology Options

### 1. Single Node (1 Primary, 0 Secondaries)
```yaml
spec:
  topology:
    primaries: 1
    secondaries: 0
```

**Characteristics:**
- ✅ **Simplicity**: Easiest to manage and deploy
- ✅ **Resource Efficiency**: Minimal resource requirements
- ❌ **No Fault Tolerance**: Any failure results in downtime
- ❌ **No High Availability**: Not suitable for production

**Use Cases:**
- Development environments
- Testing and prototyping
- Non-critical applications

---

### 2. Two Primary Nodes ⚠️ (Limited Fault Tolerance)
```yaml
spec:
  topology:
    primaries: 2
    secondaries: 0  # or more for read scaling
```

**Characteristics:**
- ⚠️ **Split-Brain Risk**: In network partition, neither node can form quorum
- ❌ **Single Point of Failure**: If one node fails, cluster becomes read-only
- ⚠️ **Manual Intervention Required**: Recovery often requires operator intervention

**Fault Tolerance Matrix:**
| Scenario | Available Nodes | Quorum (2/2 + 1 = 2) | Cluster State |
|----------|-----------------|----------------------|---------------|
| Normal | 2 | ✅ Met | Fully Operational |
| 1 Node Down | 1 | ❌ Not Met | Read-Only Mode |
| Network Split | 1+1 | ❌ Not Met | Both Partitions Read-Only |

**When to Consider:**
- Development environments requiring some clustering features
- Cost-constrained environments with manual failover capability
- Temporary configurations during cluster expansion

---

### 3. Three Primary Nodes ✅ (Recommended Minimum)
```yaml
spec:
  topology:
    primaries: 3
    secondaries: 0  # or more for read scaling
```

**Characteristics:**
- ✅ **True Fault Tolerance**: Can survive one node failure
- ✅ **Automatic Recovery**: Cluster remains operational with 2/3 nodes
- ✅ **Split-Brain Protection**: Minority partition becomes read-only

**Fault Tolerance Matrix:**
| Scenario | Available Nodes | Quorum (3/2 + 1 = 2) | Cluster State |
|----------|-----------------|----------------------|---------------|
| Normal | 3 | ✅ Met | Fully Operational |
| 1 Node Down | 2 | ✅ Met | Fully Operational |
| 2 Nodes Down | 1 | ❌ Not Met | Read-Only Mode |
| Network Split (2+1) | 2 vs 1 | ✅/❌ Met/Not Met | Majority Operational |

**Best For:**
- Production environments
- Applications requiring high availability
- Balanced performance and fault tolerance

---

### 4. Four Primary Nodes ⚠️ (Redundant Resources)
```yaml
spec:
  topology:
    primaries: 4
    secondaries: 0  # or more for read scaling
```

**Characteristics:**
- ⚠️ **Same Fault Tolerance as 3**: Can still only survive one failure
- ❌ **Resource Inefficiency**: Extra node provides no additional fault tolerance
- ⚠️ **Increased Consensus Overhead**: More nodes participating in consensus

**Fault Tolerance Matrix:**
| Scenario | Available Nodes | Quorum (4/2 + 1 = 3) | Cluster State |
|----------|-----------------|----------------------|---------------|
| Normal | 4 | ✅ Met | Fully Operational |
| 1 Node Down | 3 | ✅ Met | Fully Operational |
| 2 Nodes Down | 2 | ❌ Not Met | Read-Only Mode |

**Consider Instead:** 4 servers (allows 3 servers to host database primaries + 1 for read scaling)

---

### 5. Five Primary Nodes ✅ (High Fault Tolerance)
```yaml
spec:
  topology:
    primaries: 5
    secondaries: 0  # or more for read scaling
```

**Characteristics:**
- ✅ **Enhanced Fault Tolerance**: Can survive two simultaneous failures
- ✅ **Complex Partition Tolerance**: Better handling of network partitions
- ⚠️ **Increased Complexity**: More nodes to manage and monitor

**Fault Tolerance Matrix:**
| Scenario | Available Nodes | Quorum (5/2 + 1 = 3) | Cluster State |
|----------|-----------------|----------------------|---------------|
| Normal | 5 | ✅ Met | Fully Operational |
| 1 Node Down | 4 | ✅ Met | Fully Operational |
| 2 Nodes Down | 3 | ✅ Met | Fully Operational |
| 3 Nodes Down | 2 | ❌ Not Met | Read-Only Mode |

**Best For:**
- Mission-critical applications
- Environments with higher failure rates
- Multi-zone deployments with zone failures

---

### 6. Six Primary Nodes ⚠️ (Diminishing Returns)
```yaml
spec:
  topology:
    primaries: 6
    secondaries: 0  # or more for read scaling
```

**Characteristics:**
- ⚠️ **Same Fault Tolerance as 5**: Can still only survive two failures
- ❌ **Significant Resource Overhead**: Extra node with no fault tolerance benefit
- ❌ **Increased Consensus Latency**: More nodes slow down consensus

**Consider Instead:** 5 primaries + 1 secondary for better resource utilization

---

### 7. Seven Primary Nodes ✅ (Maximum Recommended)
```yaml
spec:
  topology:
    primaries: 7
    secondaries: 0  # or more for read scaling
```

**Characteristics:**
- ✅ **Maximum Fault Tolerance**: Can survive three simultaneous failures
- ⚠️ **High Resource Requirements**: Significant infrastructure investment
- ⚠️ **Performance Impact**: Consensus overhead becomes noticeable

**Fault Tolerance Matrix:**
| Scenario | Available Nodes | Quorum (7/2 + 1 = 4) | Cluster State |
|----------|-----------------|----------------------|---------------|
| Normal | 7 | ✅ Met | Fully Operational |
| 1 Node Down | 6 | ✅ Met | Fully Operational |
| 2 Nodes Down | 5 | ✅ Met | Fully Operational |
| 3 Nodes Down | 4 | ✅ Met | Fully Operational |
| 4 Nodes Down | 3 | ❌ Not Met | Read-Only Mode |

**Best For:**
- Extremely critical applications
- Large-scale deployments across multiple regions
- Environments with very high availability requirements (99.99%+)

## Recommendations by Environment

The snippets below show the recommended `Neo4jDatabase.spec.topology` for each environment (database-level role distribution). The containing `Neo4jEnterpriseCluster` must have at least `servers: <primaries + secondaries>` configured.

### Development Environment
```yaml
# Neo4jEnterpriseCluster
spec:
  topology:
    servers: 1

# Neo4jDatabase (standalone dev — no cluster needed)
spec:
  topology:
    primaries: 1
    secondaries: 0
```
- **Rationale**: Simplicity and resource efficiency
- **Trade-off**: No fault tolerance for development speed

### Staging/Testing Environment
```yaml
# Neo4jEnterpriseCluster
spec:
  topology:
    servers: 4

# Neo4jDatabase
spec:
  topology:
    primaries: 3
    secondaries: 1
```
- **Rationale**: Tests production-like behavior with fault tolerance
- **Trade-off**: Moderate resource usage for realistic testing

### Production Environment (Standard)
```yaml
# Neo4jEnterpriseCluster
spec:
  topology:
    servers: 5

# Neo4jDatabase
spec:
  topology:
    primaries: 3
    secondaries: 2
```
- **Rationale**: Good balance of fault tolerance and performance
- **Trade-off**: One node failure tolerance with read scaling

### Production Environment (High Availability)
```yaml
# Neo4jEnterpriseCluster
spec:
  topology:
    servers: 8

# Neo4jDatabase
spec:
  topology:
    primaries: 5
    secondaries: 3
```
- **Rationale**: Two node failure tolerance with significant read capacity
- **Trade-off**: Higher resource requirements for enhanced availability

### Mission-Critical Environment
```yaml
# Neo4jEnterpriseCluster
spec:
  topology:
    servers: 12

# Neo4jDatabase
spec:
  topology:
    primaries: 7
    secondaries: 5
```
- **Rationale**: Maximum fault tolerance with extensive read scaling
- **Trade-off**: Significant resource investment for maximum availability

## Multi-Zone Considerations

When deploying across availability zones, consider:

### Zone Distribution
- **Odd Primary Count**: Ensures majority can be maintained if one zone fails
- **Zone Spread**: Distribute primaries across zones (e.g., 2-2-1 for 5 primaries across 3 zones)

### Example: 3-Zone Deployment
```yaml
# Neo4jEnterpriseCluster — 8 servers spread across 3 zones
spec:
  topology:
    servers: 8
    placement:
      topologySpread:
        enabled: true
        topologyKey: "topology.kubernetes.io/zone"
        maxSkew: 1

# Neo4jDatabase — 5 primaries + 3 secondaries hosted on those servers
spec:
  topology:
    primaries: 5
    secondaries: 3
```

## Monitoring and Alerting

### Key Metrics to Monitor
- **Cluster Health**: All nodes responsive
- **Consensus Latency**: Time for write operations to commit
- **Node Availability**: Individual node health status
- **Quorum Status**: Whether cluster has sufficient nodes for writes

### Alert Conditions
```yaml
# Example alert rules
- alert: Neo4jClusterQuorumLost
  expr: neo4j_cluster_available_primaries < (neo4j_cluster_total_primaries / 2) + 1

- alert: Neo4jClusterLowFaultTolerance
  expr: neo4j_cluster_total_primaries == 2

- alert: Neo4jNodeDown
  expr: up{job="neo4j"} == 0
```

## Operator Warnings

The Neo4j Kubernetes Operator will emit warnings for suboptimal configurations:

### Even Number of Primaries
```
Warning: Even number of primary nodes (4) reduces fault tolerance.
In a split-brain scenario, the cluster may become unavailable.
Consider using an odd number (3, 5, or 7) for optimal fault tolerance.
```

### Two Primary Nodes
```
Warning: 2 primary nodes provide limited fault tolerance.
If one node fails, the remaining node cannot form quorum.
Consider using 3 primary nodes for production deployments.
```

### Excessive Primary Nodes
```
Warning: More than 7 primary nodes (9) may impact cluster performance
due to increased consensus overhead.
Consider using read replicas instead for scaling read capacity.
```

## Migration Strategies

### Scaling from 2 to 3 Primaries
1. **Add Third Primary**: Scale primaries from 2 to 3
2. **Wait for Sync**: Ensure new node is fully synchronized
3. **Verify Quorum**: Confirm cluster health with 3 nodes

### Converting Even to Odd Topology
1. **Add One Primary**: Increase primary count by 1
2. **Monitor Performance**: Watch for consensus impact
3. **Consider Read Replicas**: If performance is affected, use secondaries instead

## Troubleshooting

### Split-Brain Scenarios
```bash
# Check cluster status
kubectl describe neo4jenterprisecluster my-cluster

# Check individual node logs
kubectl logs my-cluster-server-0

# Verify database consistency
kubectl exec -it my-cluster-server-0 -- neo4j-admin check-consistency
```

### Recovery Procedures
1. **Identify Failed Nodes**: Check node status and logs
2. **Verify Quorum**: Ensure remaining nodes can form majority
3. **Replace Failed Nodes**: Allow operator to recreate failed pods
4. **Monitor Recovery**: Watch cluster reform and sync

## Best Practices

1. **Prefer Odd Numbers**: Use odd numbers (3, 5, 7) for primary nodes in production for optimal fault tolerance. Even numbers are allowed but generate warnings.
2. **Plan for Failures**: Size cluster to handle expected failure scenarios
3. **Monitor Continuously**: Set up comprehensive monitoring and alerting
4. **Test Failover**: Regularly test cluster behavior during node failures
5. **Consider Costs**: Balance fault tolerance requirements with resource costs
6. **Use Read Replicas**: Scale read capacity with secondaries rather than excessive primaries
7. **Document Topology**: Clearly document the rationale for your chosen topology
8. **Heed Operator Warnings**: Pay attention to validation warnings about even numbers and excessive primaries

## Conclusion

Choosing the right Neo4j cluster topology requires balancing fault tolerance, performance, and resource costs. While the operator now allows even numbers of primary nodes, odd numbers are strongly recommended for production environments to ensure optimal fault tolerance and avoid split-brain scenarios.

For most production workloads, 3 or 5 primary nodes provide the best balance of availability and resource efficiency, supplemented with read replicas as needed for performance scaling.
