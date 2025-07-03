# Neo4j Enterprise Clustering

This document describes how to configure and manage Neo4j Enterprise clusters using the Neo4j Kubernetes Operator.

## Overview

The Neo4j Kubernetes Operator supports Neo4j 5.x Enterprise clustering with multiple discovery mechanisms and advanced features like auto-scaling, read replicas, and multi-zone deployments.

## Cluster Architecture

A Neo4j Enterprise cluster consists of:

- **Primary nodes**: Core cluster members that participate in consensus and handle write operations
- **Secondary nodes**: Read replicas that handle read operations and provide high availability
- **Discovery service**: Enables cluster members to find each other
- **Routing service**: Routes client connections to appropriate cluster members

## Discovery Methods

The operator supports three discovery methods as described in the [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/clustering/setup/discovery/):

### 1. Kubernetes Discovery (Recommended)

Uses Kubernetes API to discover cluster members automatically.

```yaml
spec:
  config:
    dbms.cluster.discovery.resolver_type: k8s
    dbms.kubernetes.label_selector: app.kubernetes.io/name=my-cluster,app.kubernetes.io/instance=my-cluster
    dbms.kubernetes.discovery.service_port_name: cluster
```

### 2. DNS Discovery

Uses DNS A records to discover cluster members.

```yaml
spec:
  config:
    dbms.cluster.discovery.resolver_type: dns
    dbms.cluster.endpoints: my-cluster.example.com:6000
```

### 3. List Discovery

Uses a static list of cluster member addresses.

```yaml
spec:
  config:
    dbms.cluster.discovery.resolver_type: list
    dbms.cluster.endpoints: server1.example.com:6000,server2.example.com:6000,server3.example.com:6000
```

## Basic Cluster Configuration

### Simple 3-Node Cluster

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: simple-cluster
spec:
  edition: enterprise
  image:
    repo: neo4j
    tag: 5.26-enterprise
  topology:
    primaries: 3
    secondaries: 0
  storage:
    className: standard
    size: 10Gi
  config:
    # Neo4j 5.x clustering configuration
    dbms.cluster.discovery.resolver_type: k8s
    dbms.kubernetes.label_selector: app.kubernetes.io/name=simple-cluster,app.kubernetes.io/instance=simple-cluster
    dbms.kubernetes.discovery.service_port_name: cluster
    dbms.cluster.minimum_core_cluster_size_at_formation: 3
    dbms.cluster.minimum_core_cluster_size_at_runtime: 3
    server.cluster.listen_address: 0.0.0.0:5000
    server.discovery.listen_address: 0.0.0.0:6000
    server.routing.listen_address: 0.0.0.0:7688
```

### Using kubectl Commands

```bash
# Check cluster status
kubectl get neo4jenterprisecluster my-cluster -o yaml

# View cluster details
kubectl describe neo4jenterprisecluster my-cluster

# Check pod status
kubectl get pods -l app.kubernetes.io/instance=my-cluster
```

## Advanced Configuration

### Auto-Scaling

Enable auto-scaling for both primary and secondary nodes:

```yaml
spec:
  autoScaling:
    enabled: true
    primaries:
      enabled: true
      minReplicas: 3
      maxReplicas: 7
      allowQuorumBreak: false
      quorumProtection:
        enabled: true
        minHealthyPrimaries: 2
      metrics:
        - type: cpu
          target: "70%"
          weight: "1.0"
        - type: memory
          target: "80%"
          weight: "1.0"
    secondaries:
      enabled: true
      minReplicas: 2
      maxReplicas: 10
      zoneAware:
        enabled: true
        minReplicasPerZone: 1
        maxZoneSkew: 2
```

### Multi-Zone Deployment

Configure topology spread and anti-affinity for high availability:

```yaml
spec:
  topology:
    primaries: 3
    secondaries: 2
    placement:
      antiAffinity:
        enabled: true
        topologyKey: topology.kubernetes.io/zone
        type: preferredDuringSchedulingIgnoredDuringExecution
      topologySpread:
        enabled: true
        topologyKey: topology.kubernetes.io/zone
        maxSkew: 1
        whenUnsatisfiable: DoNotSchedule
```

### TLS Configuration

Enable TLS encryption for cluster communication:

```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: neo4j-cluster-issuer
      kind: ClusterIssuer
    duration: 8760h  # 1 year
    renewBefore: 720h  # 30 days
```

## Port Configuration

The operator uses the following default ports:

- **Bolt**: 7687 (client connections)
- **HTTP**: 7474 (HTTP API)
- **HTTPS**: 7473 (HTTPS API)
- **Cluster**: 5000 (cluster communication)
- **Discovery**: 6000 (discovery service)
- **Routing**: 7688 (routing service)
- **Raft**: 7000 (consensus protocol)

You can customize these ports in the cluster configuration:

```yaml
spec:
  config:
    server.cluster.listen_address: 0.0.0.0:5000
    server.discovery.listen_address: 0.0.0.0:6000
    server.routing.listen_address: 0.0.0.0:7688
```

## Health Monitoring

The operator provides comprehensive health monitoring:

```bash
# Check cluster health
kubectl get neo4jenterprisecluster my-cluster -o jsonpath='{.status.phase}'

# Get cluster status
kubectl describe neo4jenterprisecluster my-cluster

# View cluster logs
kubectl logs -l app.kubernetes.io/instance=my-cluster
```

## Scaling Operations

### Scale Up/Down

```bash
# Scale primaries by editing the resource
kubectl patch neo4jenterprisecluster my-cluster --type='merge' -p='{"spec":{"topology":{"primaries":5}}}'

# Scale secondaries
kubectl patch neo4jenterprisecluster my-cluster --type='merge' -p='{"spec":{"topology":{"secondaries":4}}}'

# Or edit the resource directly
kubectl edit neo4jenterprisecluster my-cluster
```

### Rolling Upgrades

```yaml
spec:
  upgradeStrategy:
    strategy: RollingUpgrade
    preUpgradeHealthCheck: true
    postUpgradeHealthCheck: true
    maxUnavailableDuringUpgrade: 1
    upgradeTimeout: 30m
    healthCheckTimeout: 5m
    stabilizationTimeout: 3m
    autoPauseOnFailure: true
```

## Troubleshooting

### Common Issues

1. **Cluster Formation Fails**
   - Check that all required ports are open
   - Verify DNS resolution for headless service
   - Ensure proper RBAC permissions for Kubernetes discovery

2. **Discovery Issues**
   - Verify label selectors match pod labels
   - Check service port names match configuration
   - Ensure network policies allow cluster communication

3. **Quorum Loss**
   - Check primary node health
   - Verify minimum cluster size configuration
   - Review auto-scaling settings

### Debug Commands

```bash
# Check cluster member status
kubectl exec -it my-cluster-primary-0 -- cypher-shell -u neo4j -p password "CALL dbms.cluster.overview()"

# Check discovery service
kubectl exec -it my-cluster-primary-0 -- cypher-shell -u neo4j -p password "CALL dbms.cluster.discovery()"

# View cluster logs
kubectl logs my-cluster-primary-0 -c neo4j
```

## Best Practices

1. **Always use odd numbers of primaries** for proper quorum consensus
2. **Enable auto-scaling** for production workloads
3. **Use multi-zone deployment** for high availability
4. **Configure proper resource limits** based on workload
5. **Enable TLS** for secure cluster communication
6. **Monitor cluster health** regularly
7. **Use rolling upgrades** for zero-downtime updates

## Migration from Neo4j 4.x

When migrating from Neo4j 4.x clustering to 5.x:

1. Update configuration from `causal_clustering.*` to `dbms.cluster.*`
2. Replace `causal_clustering_discovery_members` with Kubernetes discovery
3. Update port configurations for new routing service
4. Test cluster formation in staging environment first

## References

- [Neo4j Operations Manual - Clustering](https://neo4j.com/docs/operations-manual/current/clustering/)
- [Neo4j Operations Manual - Discovery](https://neo4j.com/docs/operations-manual/current/clustering/setup/discovery/)
- [Kubernetes StatefulSets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
