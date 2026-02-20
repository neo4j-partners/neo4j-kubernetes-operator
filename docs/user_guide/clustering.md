# Neo4j Enterprise Clustering

This document describes how to configure and manage Neo4j Enterprise clusters using the Neo4j Kubernetes Operator.

## Overview

The Neo4j Kubernetes Operator supports Neo4j 5.26.x (last semver LTS) and 2025.x.x+ (CalVer) Enterprise clustering with multiple discovery mechanisms and advanced features like read replicas and multi-zone deployments.

## Cluster Architecture

A Neo4j Enterprise cluster consists of:

- **Servers**: Neo4j server instances that self-organize into primary and secondary roles automatically
- **Discovery service**: Enables cluster members to find each other
- **Routing service**: Routes client connections to appropriate cluster members

**Server Self-Organization**: In the server-based architecture, you deploy a number of servers and Neo4j automatically assigns primary and secondary roles based on database requirements and cluster state.

## Discovery Methods

The operator automatically uses **LIST discovery** with static pod FQDNs — the recommended approach for Kubernetes deployments. See the [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/clustering/setup/discovery/) for background.

### How Cluster Discovery Works

The operator uses the **LIST resolver** with pre-computed pod FQDNs from the StatefulSet headless service. Each pod's DNS name is known upfront (`{cluster}-server-{n}.{cluster}-headless.{ns}.svc.cluster.local`), so the operator injects a fixed peer list at startup — no Kubernetes API calls required for discovery.

> **Note**: Do not confuse this with the Neo4j `K8S` resolver type, which queries the Kubernetes API directly. This operator always uses the `LIST` resolver.

**Discovery ports**:
- Port **6000** (`tcp-tx`): V2 cluster communication — used by this operator for discovery endpoints
- Port **5000** (`tcp-discovery`): V1 discovery — deprecated, **never used by this operator**

### Kubernetes Services Created

The operator automatically creates:
- `{cluster-name}-headless` — StatefulSet headless service (pod FQDNs)
- `{cluster-name}-internals` — cluster-internal routing
- `{cluster-name}-client` — external Bolt/HTTP access

### Discovery Configuration (Injected Automatically)

The operator injects version-specific discovery settings into every pod's startup script. **Do not set these in `spec.config`** — the operator manages them.

**Neo4j 5.26.x (SemVer)**:
```properties
dbms.cluster.discovery.resolver_type=LIST
dbms.cluster.discovery.version=V2_ONLY
dbms.cluster.discovery.v2.endpoints=<cluster>-server-0.<cluster>-headless.<ns>.svc.cluster.local:6000,...
internal.dbms.cluster.discovery.system_bootstrapping_strategy=me   # server-0 only
```

**Neo4j 2025.x+ / 2026.x+ (CalVer)**:
```properties
dbms.cluster.discovery.resolver_type=LIST
dbms.cluster.endpoints=<cluster>-server-0.<cluster>-headless.<ns>.svc.cluster.local:6000,...
# No dbms.cluster.discovery.version — V2 is the only supported protocol
```

Ref: [5.26.x discovery docs](https://neo4j.com/docs/operations-manual/5/clustering/setup/discovery/) · [2025.x+ discovery docs](https://neo4j.com/docs/operations-manual/current/clustering/setup/discovery/)

## Cluster Formation

The operator uses a **ME/OTHER bootstrap strategy** with `Parallel` pod management for fast, split-brain-free cluster formation.

### Key Configuration

- **Bootstrap strategy**: server-0 uses `me` (preferred bootstrapper); all other servers use `other` (join when ready)
- **Minimum primaries**: Set to `TOTAL_SERVERS` on initial formation — all servers must mutually discover each other before RAFT elects a leader, preventing premature solo bootstrap
- **On restart** (data already exists): minimum primaries check is skipped so servers rejoin immediately without blocking StatefulSet rolling updates
- **Pod Management**: `Parallel` — all pods start simultaneously

### How It Works

1. **All server pods start in parallel** — Single StatefulSet with `Parallel` pod management
2. **Servers discover each other** — Via static pod FQDNs in the LIST endpoint list (port 6000)
3. **RAFT coordination** — server-0's `me` hint makes it the preferred bootstrapper; others wait with `other` hint
4. **All N servers must see each other** — `dbms.cluster.minimum_initial_system_primaries_count=N` prevents any single node from forming a solo cluster (split-brain)
5. **Cluster forms once quorum reached** — RAFT elects server-0 as bootstrap leader; others join
6. **Servers self-organize** — Neo4j automatically assigns primary and secondary roles per database

### Benefits

- **Split-brain prevention** — All servers must be mutually visible before formation completes
- **Reliable formation** — Deterministic peer addresses (one FQDN per pod) unlike K8S ClusterIP which returns a single VIP
- **Fast restarts** — Minimum primaries check skipped on pod restarts so rolling updates aren't blocked

### TLS-Enabled Clusters

TLS-enabled clusters use the same parallel formation approach with additional optimizations:

- **Automatic trust configuration**: The operator sets `dbms.ssl.policy.cluster.trust_all=true` for intra-cluster communication
- **Parallel startup maintained**: TLS doesn't change the pod startup behavior
- **Reliable formation**: With proper configuration, TLS clusters form as reliably as non-TLS clusters

For detailed TLS configuration, see the [TLS Configuration Guide](configuration/tls.md).

### Cluster Formation Strategy

The operator uses a unified clustering approach for all deployments:

#### Unified Cluster Formation
- All deployments use Neo4j's clustering infrastructure (even single-node)
- Automatic handling of discovery configuration based on Neo4j version
- Coordinated startup ensures data consistency

#### Parallel Pod Management
- Uses **parallel pod startup** for faster cluster formation
- All pods start simultaneously and coordinate during bootstrap
- Prevents split-brain scenarios through proper coordination

### Cluster Formation Strategy

The operator uses a unified approach where all primaries must be present for initial cluster formation:

| Cluster Size | Formation Requirement | Rationale |
|--------------|----------------------|-----------|
| 2 servers | 2 servers required | Minimum cluster size |
| 3 servers | 3 servers required | Odd number for optimal fault tolerance |
| 4+ servers | All servers required | Ensures consistent initial state |

This approach ensures that clusters form with a complete and consistent initial membership.

### Cluster Formation Process

1. **Resource Creation**: The operator creates all Kubernetes resources (StatefulSets, Services, RBAC)
2. **Parallel Pod Startup**: All pods start simultaneously (not sequentially)
3. **Discovery Phase**: Pods discover each other via Kubernetes service discovery
4. **Coordination Phase**: All pods wait for complete membership before forming cluster
5. **Service Ready**: Cluster accepts connections after successful formation

### Important Considerations

- **Complete Membership**: All configured server nodes must be available for initial cluster formation
- **Startup Time**: Cluster formation typically completes within 2-3 minutes
- **Pod Readiness**: Pods are marked ready only after successful cluster formation
- **Scaling**: After initial formation, clusters can be scaled following Neo4j's online scaling procedures

## Basic Cluster Configuration

### Simple 3-Node Cluster

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: simple-cluster
spec:
  image:
    repo: neo4j
    tag: 5.26-enterprise
  topology:
    servers: 3  # 3 servers will self-organize into appropriate roles
  storage:
    className: standard
    size: 10Gi
  # Kubernetes discovery is automatically configured by the operator
  # No manual discovery configuration needed!
```

**Note**: The operator automatically handles all clustering configuration including:
- Kubernetes discovery setup
- RBAC resource creation
- Service creation for cluster communication
- Neo4j configuration for optimal clustering

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


### Multi-Zone Deployment

Configure topology spread and anti-affinity for high availability:

```yaml
spec:
  topology:
    servers: 5  # 5 servers will self-organize into appropriate roles
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

> **📖 For comprehensive topology placement options**, including zone distribution strategies, anti-affinity configurations, and troubleshooting tips, see the [Topology Placement Guide](./topology_placement.md).

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

| Port | Name | Purpose |
|------|------|---------|
| 7687 | bolt | Client Bolt connections |
| 7474 | http | HTTP API |
| 7473 | https | HTTPS API |
| **6000** | **tcp-tx** | **V2 cluster traffic + discovery endpoints (always used)** |
| 5000 | tcp-discovery | V1 discovery — **deprecated, not used by this operator** |
| 7688 | routing | Routing service |
| 7000 | raft | RAFT consensus |

```yaml
spec:
  config:
    server.cluster.listen_address: 0.0.0.0:6000   # V2 cluster + discovery
    server.routing.listen_address: 0.0.0.0:7688
    server.cluster.raft.listen_address: 0.0.0.0:7000
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
# Scale servers by editing the resource
kubectl patch neo4jenterprisecluster my-cluster --type='merge' -p='{"spec":{"topology":{"servers":7}}}'

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

1. **Cluster Formation Issues (Rare with Current Configuration)**
   - The parallel startup with MIN_PRIMARIES=1 eliminates most formation issues
   - Check pod logs: `kubectl logs <pod-name>` for any startup errors
   - Verify all pods can resolve DNS: `kubectl exec <pod> -- nslookup <cluster>-discovery`

2. **Discovery Service**
   - Verify discovery service exists: `kubectl get service {cluster-name}-discovery`
   - Check endpoints include all pods: `kubectl describe endpoints {cluster-name}-discovery`
   - Verify the headless service exists: `kubectl get service {cluster-name}-headless`
   - Check that pod FQDNs resolve: `kubectl exec {cluster-name}-server-0 -- nslookup {cluster-name}-server-0.{cluster-name}-headless.{ns}.svc.cluster.local`
   - Inspect the startup script for correct LIST endpoints: `kubectl get configmap {cluster-name}-config -o yaml | grep -A2 resolver_type`
   - Verify pod has correct ServiceAccount: `kubectl get pod {cluster-name}-server-0 -o jsonpath='{.spec.serviceAccountName}'`

3. **Quorum Loss**
   - Check primary node health
   - Verify minimum cluster size configuration

### Debug Commands

```bash
# Check cluster member status
kubectl exec -it my-cluster-server-0 -- cypher-shell -u neo4j -p password "SHOW SERVERS"

# Check database allocation
kubectl exec -it my-cluster-server-0 -- cypher-shell -u neo4j -p password "SHOW DATABASES"

# View cluster logs
kubectl logs my-cluster-server-0 -c neo4j
```

## Best Practices

1. **Use odd numbers of servers (3, 5, 7)** for optimal fault tolerance. Even numbers are allowed but may have less optimal quorum behavior.
2. **Configure proper resource limits** based on workload
3. **Use multi-zone deployment** for high availability
4. **Configure proper resource limits** based on workload
5. **Enable TLS** for secure cluster communication
6. **Monitor cluster health** regularly
7. **Use rolling upgrades** for zero-downtime updates
8. **Trust automatic discovery** - the operator handles all Kubernetes discovery configuration

## Migration from Neo4j 4.x

When migrating from Neo4j 4.x clustering to 5.x:

1. **Remove all manual discovery configuration** - the operator handles discovery automatically
2. **Update from `causal_clustering.*` to `dbms.cluster.*`** - but discovery settings are managed by operator
3. **Update port configurations** for new routing service if customized
4. **Remove static endpoint lists** - no longer needed with Kubernetes discovery
5. **Test cluster formation** in staging environment first

**Important**: The operator automatically handles all cluster discovery. Remove any manual discovery configuration from your cluster specifications.

## References

- [Neo4j Operations Manual - Clustering](https://neo4j.com/docs/operations-manual/current/clustering/)
- [Neo4j Operations Manual - Discovery](https://neo4j.com/docs/operations-manual/current/clustering/setup/discovery/)
- [Kubernetes StatefulSets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
