# Topology-Aware Placement for Neo4j Enterprise Clusters

## Overview

The Neo4j Enterprise Operator supports **topology-aware placement**, a powerful feature that automatically distributes Neo4j database primaries and secondaries across availability zones (AZs) or other topology domains to ensure high availability and fault tolerance.

## 🎯 Problem Statement

Without topology awareness, your Neo4j cluster might end up with:

```
❌ BAD: Single Point of Failure
AZ-A: [Primary-1, Primary-2]  ← If AZ-A fails, lose quorum!
AZ-B: [Secondary-1, Secondary-2]
AZ-C: [Primary-3, Secondary-3]
```

If AZ-A experiences an outage, you lose 2 out of 3 primaries → **Loss of quorum** → **Downtime**

## ✅ Solution: Topology-Aware Distribution

With topology awareness enabled:

```
✅ GOOD: Fault Tolerant Distribution
AZ-A: [Primary-1, Secondary-1]
AZ-B: [Primary-2, Secondary-2]
AZ-C: [Primary-3, Secondary-3]
```

If any single AZ fails, you maintain quorum (2 out of 3 primaries) → **No downtime**

## 🏗️ How It Works

The operator uses two Kubernetes mechanisms to achieve topology-aware placement:

1. **Topology Spread Constraints** - Ensures even distribution across zones
2. **Pod Anti-Affinity** - Prevents pods from co-locating in the same zone

## 📋 Configuration Options

### Basic Configuration

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: my-cluster
spec:
  topology:
    primaries: 3
    secondaries: 3

    # Enable automatic distribution
    enforceDistribution: true

    # Optional: specify availability zones
    availabilityZones:
      - "us-west-2a"
      - "us-west-2b"
      - "us-west-2c"

    # Placement configuration
    placement:
      topologySpread:
        enabled: true
        topologyKey: "topology.kubernetes.io/zone"
        maxSkew: 1

      antiAffinity:
        enabled: true
        type: "required"
```

### Advanced Configuration

```yaml
spec:
  topology:
    primaries: 3
    secondaries: 3
    enforceDistribution: true

    placement:
      # Topology spread constraints
      topologySpread:
        enabled: true
        topologyKey: "topology.kubernetes.io/zone"
        maxSkew: 1                    # Max difference between zones
        whenUnsatisfiable: "DoNotSchedule"  # Hard constraint
        minDomains: 3                 # Minimum required zones

      # Pod anti-affinity
      antiAffinity:
        enabled: true
        topologyKey: "topology.kubernetes.io/zone"
        type: "required"              # Hard anti-affinity

      # Additional node selection
      nodeSelector:
        node-type: "database"
        instance-type: "m5.xlarge"

      # Hard vs soft constraints
      requiredDuringScheduling: true
```

## 🔧 Configuration Parameters

### TopologyConfiguration

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `primaries` | int32 | Number of primary (core) servers | Required |
| `secondaries` | int32 | Number of secondary (read replica) servers | 0 |
| `availabilityZones` | []string | Expected availability zones | Auto-discovered |
| `enforceDistribution` | bool | Enforce strict distribution | false |
| `placement` | PlacementConfig | Advanced placement settings | nil |

### PlacementConfig

| Field | Type | Description |
|-------|------|-------------|
| `topologySpread` | TopologySpreadConfig | Topology spread constraints |
| `antiAffinity` | PodAntiAffinityConfig | Pod anti-affinity rules |
| `nodeSelector` | map[string]string | Node selection constraints |
| `requiredDuringScheduling` | bool | Hard vs soft constraints |

### TopologySpreadConfig

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `enabled` | bool | Enable topology spread constraints | false |
| `topologyKey` | string | Topology domain key | "topology.kubernetes.io/zone" |
| `maxSkew` | int32 | Max replica difference between zones | 1 |
| `whenUnsatisfiable` | string | What to do if unsatisfiable | "DoNotSchedule" |
| `minDomains` | *int32 | Minimum required domains | nil |

### PodAntiAffinityConfig

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `enabled` | bool | Enable pod anti-affinity | false |
| `topologyKey` | string | Anti-affinity topology domain | "topology.kubernetes.io/zone" |
| `type` | string | "required" or "preferred" | "preferred" |

## 🏷️ Topology Keys

Common topology keys you can use:

| Topology Key | Description | Use Case |
|--------------|-------------|----------|
| `topology.kubernetes.io/zone` | Availability Zone | Multi-AZ distribution |
| `topology.kubernetes.io/region` | Region | Multi-region distribution |
| `kubernetes.io/hostname` | Individual nodes | Node-level anti-affinity |
| `node.kubernetes.io/instance-type` | Instance type | Hardware diversity |
| `topology.kubernetes.io/rack` | Data center rack | Rack awareness |

## 📝 Usage Examples

### Example 1: Strict Zone Distribution

Perfect for production workloads requiring maximum availability:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-ha-cluster
spec:
  topology:
    primaries: 3
    secondaries: 3
    enforceDistribution: true  # Hard requirement

    placement:
      topologySpread:
        enabled: true
        maxSkew: 1
        whenUnsatisfiable: "DoNotSchedule"  # Fail if can't distribute
        minDomains: 3

      antiAffinity:
        enabled: true
        type: "required"  # Hard anti-affinity

      requiredDuringScheduling: true

  # ... rest of configuration
```

### Example 2: Flexible Distribution

Good for development or resource-constrained environments:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-flexible-cluster
spec:
  topology:
    primaries: 3
    secondaries: 2
    enforceDistribution: false  # Allow flexibility

    placement:
      topologySpread:
        enabled: true
        maxSkew: 2  # Allow more skew
        whenUnsatisfiable: "ScheduleAnyway"  # Best effort

      antiAffinity:
        enabled: true
        type: "preferred"  # Soft anti-affinity

      requiredDuringScheduling: false

  # ... rest of configuration
```

### Example 3: Multi-Region Distribution

For global deployments:

```yaml
spec:
  topology:
    primaries: 3
    secondaries: 6

    availabilityZones:
      - "us-west-2a"
      - "us-west-2b"
      - "us-west-2c"
      - "eu-west-1a"
      - "eu-west-1b"
      - "eu-west-1c"

    placement:
      topologySpread:
        enabled: true
        topologyKey: "topology.kubernetes.io/region"
        maxSkew: 1

      antiAffinity:
        enabled: true
        topologyKey: "topology.kubernetes.io/zone"
```

### Example 4: Multi-Cluster with Topology Awareness

**The Ultimate Resilience Setup** - Combining topology awareness with multi-cluster deployments:

```yaml
# This example shows how topology awareness enhances multi-cluster deployments
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-resilient-global
  namespace: neo4j
spec:
  # 🌐 Multi-cluster configuration
  multiCluster:
    enabled: true
    clusterName: "global-primary"
    region: "us-east-1"
    role: "primary"

    # Define cross-cluster topology
    topology:
      clusters:
      - name: "global-primary"
        region: "us-east-1"
        primaries: 3
        secondaries: 3
      - name: "global-dr"
        region: "us-west-2"
        primaries: 3
        secondaries: 2

  # ⚡ Topology-aware configuration WITHIN each cluster
  topology:
    primaries: 3
    secondaries: 3
    enforceDistribution: true

    # Zone distribution within the region
    availabilityZones:
      - "us-east-1a"
      - "us-east-1b"
      - "us-east-1c"

    placement:
      # Ensure even distribution across zones
      topologySpread:
        enabled: true
        topologyKey: "topology.kubernetes.io/zone"
        maxSkew: 1
        whenUnsatisfiable: "DoNotSchedule"
        minDomains: 3

      # Prevent co-location within zones
      antiAffinity:
        enabled: true
        type: "required"
        topologyKey: "topology.kubernetes.io/zone"

      # Additional host-level anti-affinity
      additionalAntiAffinity:
      - topologyKey: "kubernetes.io/hostname"
        type: "preferred"
        weight: 100

  # Storage and other configurations...
  storage:
    className: "gp3"
    size: "200Gi"
```

**Why This Configuration is Powerful:**

1. **Zone-Level Protection**: Each cluster is distributed across 3 zones
2. **Region-Level Protection**: Multiple clusters across different regions
3. **Automatic Failover**: Both within zones and across regions
4. **Quorum Maintenance**: Odd number of primaries in each cluster
5. **Resource Isolation**: Anti-affinity prevents single points of failure

**Failure Tolerance Matrix:**

| Scenario | Impact | Recovery |
|----------|---------|----------|
| Single pod failure | None - automatic leader election | < 30 seconds |
| Single zone failure | None - continues on other zones | < 30 seconds |
| Two zones fail | None - third zone maintains quorum | < 30 seconds |
| Entire region fails | Brief interruption - failover to DR | 1-3 minutes |
| Network partition | Graceful degradation | Automatic when resolved |

## 🔍 Monitoring and Validation

### Check Pod Distribution

```bash
# View pod placement across zones
kubectl get pods -o wide -l app.kubernetes.io/name=neo4j

# Check topology distribution
kubectl get pods -l app.kubernetes.io/name=neo4j \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.nodeName}{"\n"}{end}' | \
  while read pod node; do
    zone=$(kubectl get node $node -o jsonpath='{.metadata.labels.topology\.kubernetes\.io/zone}')
    echo "$pod -> $node ($zone)"
  done
```

### Verify Cluster Health

```bash
# Check cluster status
kubectl get neo4jenterprisecluster -o yaml

# View topology events
kubectl describe neo4jenterprisecluster my-cluster
```

### Neo4j Cluster Status

```cypher
// Connect to Neo4j and check cluster topology
CALL dbms.cluster.overview() YIELD id, role, addresses, database
RETURN id, role, addresses, database;
```

## 🚨 Troubleshooting

### Common Issues

#### 1. Pods Stuck in Pending

**Symptom**: Pods remain in `Pending` status

**Possible Causes**:

- Not enough availability zones
- Resource constraints in specific zones
- Hard constraints too restrictive

**Solutions**:

```bash
# Check pod events
kubectl describe pod <pod-name>

# Check node availability
kubectl get nodes -l topology.kubernetes.io/zone

# Relax constraints temporarily
spec:
  topology:
    placement:
      topologySpread:
        whenUnsatisfiable: "ScheduleAnyway"
      antiAffinity:
        type: "preferred"
```

#### 2. Uneven Distribution

**Symptom**: Pods not evenly distributed across zones

**Possible Causes**:

- `maxSkew` too high
- Soft constraints only
- Zone capacity differences

**Solutions**:

```yaml
# Tighten constraints
spec:
  topology:
    placement:
      topologySpread:
        maxSkew: 1
        whenUnsatisfiable: "DoNotSchedule"
      antiAffinity:
        type: "required"
```

#### 3. Insufficient Zones

**Error**: `cannot enforce distribution: 3 primaries require at least 3 availability zones, but only 2 are available`

**Solution**:

- Add more zones to your cluster
- Reduce number of primaries
- Disable `enforceDistribution`

### Debugging Commands

```bash
# Check topology scheduler logs
kubectl logs -l app.kubernetes.io/name=neo4j-operator -f

# View StatefulSet topology constraints
kubectl get statefulset <cluster-name>-primary -o yaml | grep -A 20 topologySpreadConstraints

# Check node labels
kubectl get nodes --show-labels | grep topology
```

## 🎯 Best Practices

### Production Deployment

1. **Always use hard constraints** for production:

   ```yaml
   enforceDistribution: true
   requiredDuringScheduling: true
   whenUnsatisfiable: "DoNotSchedule"
   type: "required"
   ```

2. **Ensure sufficient zones**: Have at least as many zones as primaries

3. **Monitor resource capacity** in each zone

4. **Use appropriate maxSkew**:
   - `maxSkew: 1` for strict even distribution
   - `maxSkew: 2` for some flexibility

### Development/Testing

1. **Use soft constraints** for flexibility:

   ```yaml
   enforceDistribution: false
   whenUnsatisfiable: "ScheduleAnyway"
   type: "preferred"
   ```

2. **Allow larger maxSkew** for resource-constrained environments

### Multi-Region Considerations

1. **Layer your constraints**:
   - Primary constraint: Region-level distribution
   - Secondary constraint: Zone-level distribution

2. **Consider network latency** between regions

3. **Plan for cross-region networking** costs

## 🔮 Advanced Features

### Custom Topology Keys

```yaml
spec:
  topology:
    placement:
      topologySpread:
        topologyKey: "custom.company.com/datacenter"
```

### Multiple Constraints

```yaml
spec:
  topology:
    placement:
      topologySpread:
        enabled: true
        # This will be expanded to support multiple constraints
```

### Integration with Pod Disruption Budgets

The operator automatically creates appropriate PodDisruptionBudgets based on your topology configuration to ensure maintenance operations don't violate availability requirements.

## 🔗 Related Documentation

- [Neo4j Clustering Guide](https://neo4j.com/docs/operations-manual/current/clustering/)
- [Kubernetes Topology Spread Constraints](https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/)
- [Kubernetes Pod Anti-Affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity)
- [Neo4j Operator Deployment Guide](./deployment-guide.md)

## 📞 Support

For issues with topology-aware placement:

1. Check the [troubleshooting section](#-troubleshooting) above
2. Review operator logs: `kubectl logs -l app.kubernetes.io/name=neo4j-operator`
3. Open an issue in the [GitHub repository](https://github.com/neo4j-labs/neo4j-kubernetes-operator)

---

**Set it and forget it!** Once configured, the Neo4j Operator automatically ensures your cluster maintains optimal distribution across your infrastructure. 🚀
