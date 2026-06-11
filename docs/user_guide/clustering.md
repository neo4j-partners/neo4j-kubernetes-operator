# Neo4j Enterprise Clustering

This document describes how to configure and manage Neo4j Enterprise clusters using the Neo4j Kubernetes Operator.

## Overview

The Neo4j Kubernetes Operator supports Neo4j 5.26 LTS (the final SemVer release) and any CalVer release (2025.x, 2026.x, and onward) for Enterprise clustering, with multiple discovery mechanisms and advanced features like read replicas and multi-zone deployments.

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

- **Automatic strict peer validation** (default): The operator emits `dbms.ssl.policy.cluster.trust_all=false`, `client_auth=REQUIRE` (mutual TLS), and `verify_hostname=true`, projecting the cert-manager Secret's `ca.crt` to `/ssl/trusted/ca.crt` as the trust anchor. This matches Neo4j's canonical production guidance. Set `spec.tls.strictPeerValidation: false` to opt out into the legacy `trust_all=true` posture (only useful if your external issuer doesn't populate `ca.crt`).
- **Parallel startup maintained**: TLS doesn't change the pod startup behavior
- **Reliable formation**: With proper configuration, TLS clusters form as reliably as non-TLS clusters

For detailed TLS configuration, see the [TLS Configuration Guide](tls_configuration.md).

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

#### Formation Requirements

All primaries must be present for initial cluster formation:

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
apiVersion: neo4j.neo4j.com/v1beta1
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
  # LIST discovery with static pod FQDNs is configured automatically.
  # The operator never uses the K8S service-list resolver; do not set
  # dbms.kubernetes.discovery.* in spec.config (the validator rejects it).
```

**Note**: The operator automatically handles all clustering configuration including:

- LIST discovery setup with pod FQDNs on port 6000 (`tcp-tx`)
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

## Default Database Topology

When a Neo4j cluster starts for the first time, it automatically creates a default database called `neo4j`. This database is created with Neo4j's built-in defaults: **1 primary and 0 secondaries**, regardless of how many servers are in your cluster.

This means that on a 3-server cluster, the default `neo4j` database will only be hosted on a single server — which may be surprising if you expect it to span the entire cluster.

### Configuring Default Topology at Bootstrap

You can control the default topology for all newly created databases (including the auto-created `neo4j` database) using `initial.*` settings in `spec.config`:

```yaml
spec:
  config:
    initial.dbms.default_primaries_count: "3"
    initial.dbms.default_secondaries_count: "1"
```

> **Important**: These are bootstrap-only settings. They only take effect when the cluster is created for the first time. Adding or changing them on an existing cluster has no effect.

### Changing Database Topology After Bootstrap

To change the topology of an existing database, use the `ALTER DATABASE` Cypher command:

```bash
kubectl exec <cluster>-server-0 -c neo4j -- cypher-shell -u neo4j -p <password> \
  "ALTER DATABASE neo4j SET TOPOLOGY 3 PRIMARIES 1 SECONDARY"
```

Alternatively, you can manage database topology declaratively using the `Neo4jDatabase` CRD. See the [Neo4jDatabase API Reference](../api_reference/neo4jdatabase.md) for details.

### Cannot Skip Default Database Creation

Neo4j does not provide a way to prevent the default `neo4j` database from being created. It is always created at cluster bootstrap. The only post-bootstrap options are:

- **Change its topology** using `ALTER DATABASE`
- **Rename the default** using the `dbms.setDefaultDatabase()` procedure (do not use the deprecated `dbms.default_database` config setting):
  ```bash
  kubectl exec <cluster>-server-0 -c neo4j -- cypher-shell -u neo4j -p <password> \
    "CALL dbms.setDefaultDatabase('mydb');"
  ```
  This must be executed as a database operation after bootstrap, not via `spec.config`.
- **Manage it declaratively** via a `Neo4jDatabase` CRD (the operator will warn that you are shadowing the default)

## Advanced Configuration

### Server Role Constraints

By default every server can host databases in either primary or secondary mode (`NONE`). You can optionally constrain server roles to influence how Neo4j allocates database instances — for example, dedicating some servers to secondary (read-replica) duty.

**Cluster-wide constraint** — `spec.topology.serverModeConstraint` applies to all servers. Valid values: `NONE` (default), `PRIMARY`, `SECONDARY`.

```yaml
spec:
  topology:
    servers: 3
    serverModeConstraint: PRIMARY  # all servers only host primaries
```

**Per-server constraints** — `spec.topology.serverRoles[]` overrides `serverModeConstraint` for the named servers. Each entry has a `serverIndex` (0-based) and a `modeConstraint` (`NONE`, `PRIMARY`, or `SECONDARY`):

```yaml
spec:
  topology:
    servers: 3
    serverModeConstraint: NONE
    serverRoles:
      - serverIndex: 0
        modeConstraint: PRIMARY    # server-0 only hosts primaries
      - serverIndex: 1
        modeConstraint: SECONDARY  # server-1 only hosts secondaries
      - serverIndex: 2
        modeConstraint: NONE       # server-2 can host either mode
```

**Validation rules** (enforced by the controller, not a webhook):

- `servers` must be between 2 and 100 (the 100 cap is an operator safety rail, not a Neo4j limit; realistic deployments rarely exceed ~10).
- Each `serverRoles[].serverIndex` must be in the range `[0, servers-1]`.
- `serverRoles[].serverIndex` values must be unique (no duplicates).
- You cannot constrain **all** servers to `SECONDARY` — at least one server must be able to host primaries.

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

**Scale-up** just raises the replica count; new servers join and you rebalance databases onto them (`REALLOCATE DATABASES`, or set per-database `TOPOLOGY`).

#### Scaling down (automated, safe-by-default)

When you **lower** `spec.topology.servers`, the operator drains the removed servers **before** their pods are stopped — it never just deletes pods out from under live data. For each removed server (highest ordinals first) it runs, one step per reconcile:

```
cordon → DRYRUN DEALLOCATE (feasibility) → DEALLOCATE DATABASES FROM SERVER → wait until it hosts only `system` → DROP SERVER → lower replicas
```

Replicas are **held** at the current count (the removed pods stay running and reachable for data hand-off) until every removed server is dropped; only then are the pods stopped. Progress shows as a `ServersPendingDrain` condition and `ScaleDownDraining` events. Removed-ordinal PVCs are reclaimed (`PVCRetentionPolicy.WhenScaled=Delete`).

**The system-database floor.** A cluster cannot be scaled below `spec.topology.minSystemPrimaries` (default **min(3, servers)** — i.e. 3 for any cluster of 3+, 2 for a 2-server cluster). This is the `system` database's voting-member quorum; Neo4j refuses to drop below it. To run 1–2 nodes, use `Neo4jEnterpriseStandalone`.

**When a scale-down is refused.** The operator sets `ServersPendingDrain` to a `ScaleDownBlocked` reason (with the exact cause) and **holds replicas — nothing is cordoned or removed** — when:

- The target is **below the system-DB floor** (above) — keep ≥ the floor.
- A **single-primary database** (e.g. the default `neo4j` DB, which has 1 primary by default) lives on a server being removed. Neo4j refuses to move a sole primary (it would mean write-unavailability), and *no* reallocation primitive can relocate it. Give that database an additional primary first so it becomes relocatable:
  ```cypher
  ALTER DATABASE neo4j SET TOPOLOGY 2 PRIMARIES;   -- then the scale-down proceeds
  ```
- A database's `TOPOLOGY` can no longer be satisfied by the survivors (e.g. a 3-primary DB on a would-be 2-server cluster) — reduce its topology or keep the servers.

The operator deliberately **never auto-changes a database's `TOPOLOGY`** (that alters your durability guarantees) — it only relocates what Neo4j allows and refuses the rest with actionable guidance.

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
   - The parallel startup strategy and topology-aligned minimum primaries configuration eliminate most formation issues
   - Check pod logs: `kubectl logs {cluster-name}-server-0` for any startup errors
   - Verify all pods can resolve DNS: `kubectl exec {cluster-name}-server-0 -- nslookup {cluster-name}-headless`

2. **Discovery (LIST resolver via headless service)**
   - The operator uses LIST discovery against the StatefulSet's `-headless` service (the legacy `-discovery` ClusterIP service exists for backward compatibility but is NOT used by the V2 discovery path).
   - Verify the headless service exists: `kubectl get service {cluster-name}-headless`
   - Check endpoints include all pods: `kubectl describe endpoints {cluster-name}-headless`
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
4. **Enable TLS** for secure cluster communication
5. **Monitor cluster health** regularly
6. **Use rolling upgrades** for zero-downtime updates
7. **Trust automatic discovery** - the operator handles all Kubernetes discovery configuration

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
