# Property Sharding with Neo4j Kubernetes Operator

Property Sharding is an advanced Neo4j feature that separates graph structure (nodes and relationships) from properties, enabling better scalability for property-heavy workloads by distributing properties across multiple databases.

## Overview

Property Sharding decouples data into:

- **Graph Shard**: Single database containing nodes and relationships WITHOUT properties
- **Property Shards**: Multiple databases containing properties distributed via hash function
- **Virtual Database**: Logical database presenting unified view of graph + property shards

## Prerequisites

### System Requirements

**Neo4j Version Requirements:**

- **Minimum**: Neo4j 2025.12-enterprise (introduced in 2025.12)
- **Note**: Property sharding is an enterprise-only feature and requires valid licensing
- **Not available on Aura**

**Cluster Infrastructure Requirements:**

- **Minimum Servers**: 2 servers minimum (3+ recommended for HA graph shard primaries)
- **Recommended Servers**: 3-7 servers (odd numbers provide better consensus characteristics)
- **Maximum Recommended**: 20+ servers for very large deployments

**Resource Requirements per Server:**

| Component | Minimum (Basic) | Recommended (Production) | Notes |
|-----------|----------------|-------------------------|-------|
| **Memory** | 4GB total | 8GB+ total | 4GB minimum, 8GB+ recommended |
| **CPU** | 1 core | 2+ cores | Cross-shard queries are CPU intensive |
| **Storage** | 10GB | 100GB+ | Depends on data volume and shard count |
| **Network** | 1Gbps | 10Gbps+ | Low latency critical for transaction log sync |

**Additional Requirements:**

- **Authentication**: Admin secret required (property sharding requires authenticated cluster access)
- **Storage Class**: Persistent storage class must be specified (e.g., `standard`, `fast-ssd`)
- **Kubernetes Version**: 1.24+ for full operator compatibility
- **Network Policy**: Allow inter-pod communication on discovery and bolt ports
- **Cypher Version**: Must use Cypher 25 for sharded database operations

**Performance Considerations:**

- **Memory Overhead**: 20-30% additional memory required for shard coordination
- **CPU Overhead**: 20-30% additional CPU required for cross-shard operations
- **Storage Growth**: Linear growth with number of property shards
- **Network Traffic**: 2-3x increase in inter-server communication

**Capacity Planning Guidelines:**

*Development/Testing:*
```yaml
topology:
  servers: 3
resources:
  requests:
    memory: 4Gi    # Absolute minimum for dev/test
    cpu: 2000m     # 2 cores for cross-shard queries
  limits:
    memory: 8Gi    # Recommended for production
    cpu: 2000m
```

*Production:*
```yaml
topology:
  servers: 3      # or 5+ for larger datasets
resources:
  requests:
    memory: 4Gi    # Minimum for dev/test
    cpu: 2000m     # Cross-shard performance
  limits:
    memory: 8Gi    # Recommended for production
    cpu: 4000m     # Handle peak loads
```

*High-Performance Production:*
```yaml
topology:
  servers: 7      # Better distribution
resources:
  requests:
    memory: 8Gi    # Production recommendation
    cpu: 4000m     # Maximum throughput
  limits:
    memory: 20Gi   # Peak load handling
    cpu: 6000m     # Burst capability
```

## Quick Start

> **✅ Tested Configuration**: The examples below have been tested with Neo4j 2025.12-enterprise on Kubernetes clusters with the specified resource requirements. Property sharding cluster creation typically completes in 2-3 minutes with proper resources.

### 0. Prerequisites

Before creating a property sharding cluster, create the required admin secret:

```bash
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j \
  --from-literal=password=your-secure-password
```

### 1. Create Property Sharding Enabled Cluster

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: property-sharding-cluster
  namespace: default
spec:
  image:
    repo: neo4j
    tag: 2025.12-enterprise  # Property sharding requires 2025.12+

  # Authentication required for property sharding
  auth:
    adminSecret: neo4j-admin-secret

  topology:
    servers: 3  # 3+ recommended for HA graph shard primaries

  storage:
    size: 10Gi
    className: standard  # Storage class must be specified

  resources:
    requests:
      memory: 4Gi    # Minimum 4GB for dev/test environments
      cpu: 2000m     # 2+ cores required for cross-shard queries
    limits:
      memory: 8Gi    # Recommended 8GB for production
      cpu: 4000m     # Higher CPU for shard coordination overhead (20-30% increase)

  # Enable property sharding
  propertySharding:
    enabled: true
    config:
      # Required configuration (applied automatically if not specified)
      internal.dbms.sharded_property_database.enabled: "true"
      db.query.default_language: "CYPHER_25"
      internal.dbms.sharded_property_database.allow_external_shard_access: "false"

      # Performance tuning (optional)
      db.tx_log.rotation.retention_policy: "7 days"
      internal.dbms.sharded_property_database.property_pull_interval: "10ms"
```

### 2. Create Sharded Database

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jShardedDatabase
metadata:
  name: products-sharded-db
  namespace: default
spec:
  # Reference to property sharding enabled cluster
  clusterRef: property-sharding-cluster

  # Virtual database name (what users connect to)
  name: products

  # Cypher language version: "5" or "25". Property sharding requires Cypher 25,
  # which is only available on Neo4j 2025.x or later.
  defaultCypherLanguage: "25"

  # Property sharding configuration
  propertySharding:
    propertyShards: 4        # Number of property shards

    # Graph shard topology (stores nodes/relationships)
    graphShard:
      primaries: 3
      secondaries: 2

    # Property shard topology (stores properties)
    propertyShardTopology:
      replicas: 2

  # Database creation options
  wait: true          # Wait for creation to complete
  ifNotExists: true   # Don't fail if database exists
```

### Seeding from Backups (Optional)

Use `seedURI` for a single backup location that contains shard-suffixed artifacts
(for example, `products-g000` and `products-p000`). Use `seedURIs` for per-shard
URIs when seeding from dumps or multiple locations.

```yaml
spec:
  seedURI: "s3://backups/products/"
  seedConfig:
    restoreUntil: "2025-06-01T10:30:00Z"
  seedCredentials:
    secretRef: "seed-credentials"
```

## Configuration Reference

### PropertyShardingSpec (Neo4jEnterpriseCluster)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | boolean | Yes | Enable property sharding support on cluster |
| `config` | map[string]string | No | Advanced property sharding configuration |

#### Required Configuration Settings

When `propertySharding.enabled` is `true`, these settings are automatically applied:

```yaml
config:
  internal.dbms.sharded_property_database.enabled: "true"
  db.query.default_language: "CYPHER_25"
  internal.dbms.sharded_property_database.allow_external_shard_access: "false"
```

#### Optional Performance Tuning

```yaml
config:
  # Transaction log retention (critical for shard sync)
  db.tx_log.rotation.retention_policy: "7 days"

  # Property pull interval
  internal.dbms.sharded_property_database.property_pull_interval: "10ms"

  # Memory configuration
  server.memory.heap.max_size: "8G"
  server.memory.pagecache.size: "4G"
```

### PropertyShardingConfiguration (Neo4jShardedDatabase)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `propertyShards` | int32 | Yes | Number of property shards (1-1000) |
| `graphShard` | DatabaseTopology | Yes | Topology for graph shard |
| `propertyShardTopology` | PropertyShardTopology | Yes | Replica topology for property shards |

#### DatabaseTopology

| Field | Type | Description |
|-------|------|-------------|
| `primaries` | int32 | Number of primary replicas |
| `secondaries` | int32 | Number of secondary replicas |

#### PropertyShardTopology

| Field | Type | Description |
|-------|------|-------------|
| `replicas` | int32 | Number of replicas per property shard |

## Best Practices

### Cluster Sizing

**Development/Testing**:

- 2 servers minimum (3+ recommended for HA graph shard primaries)
- 4GB heap per server (absolute minimum for property sharding)
- 8GB+ total RAM per server (recommended for stable production operation)
- Property shards: 2-4

**Production**:

- 3+ servers recommended (HA graph shard primaries)
- 4-8GB+ heap per server (4GB minimum, 8GB for production)
- 20GB+ total RAM per server (accounting for system overhead)
- 2+ CPU cores per server (cross-shard query requirements)
- Property shards: 4-16 (start conservatively)

### Property Distribution Strategy

Property distribution across shards is automatic; the operator does not provide per-property controls.

### Shard Count Recommendations

| Dataset Size | Property Shards | Reasoning |
|--------------|-----------------|-----------|
| < 1M nodes | 2-4 | Minimal overhead |
| 1M-10M nodes | 4-8 | Balanced distribution |
| 10M-100M nodes | 8-16 | Better parallelization |
| 100M+ nodes | 16-32 | Maximum distribution |

**Note**: Resharding requires an offline `neo4j-admin database copy` and recreating the database; the operator does not automate resharding.

## Monitoring and Observability

### Cluster Status

Check if property sharding is ready:

```bash
kubectl get neo4jenterpriseclusters property-sharding-cluster -o yaml
```

Look for:
```yaml
status:
  phase: Ready
  propertyShardingReady: true
```

### Sharded Database Status

Check sharded database health:

```bash
kubectl get neo4jshardeddatabases products-sharded-db -o yaml
```

Key status fields:
```yaml
status:
  phase: Ready
  shardingReady: true
  graphShard:
    name: products-g000
    ready: true
    state: online
  propertyShards:
  - name: products-p000
    ready: true
    state: online
  virtualDatabase:
    name: products
    ready: true
```

### Database Operations

Connect to the virtual database:

```bash
kubectl port-forward svc/property-sharding-cluster-client 7687:7687
```

Query the virtual database (automatically routes to appropriate shards):

```cypher
// Connect to virtual database
:use products

// Standard queries work transparently
MATCH (n:Product) RETURN n.name, n.description LIMIT 10;

// Create data (properties distributed automatically)
CREATE (p:Product {
  id: "12345",           // Stays in graph shard
  name: "Widget",        // Stays in graph shard
  description: "...",    // Goes to property shard
  metadata: "{...}"      // Goes to property shard
});
```

Check individual shards:

```cypher
// Graph shard (nodes/relationships only)
:use system
SHOW DATABASES
  WHERE name STARTS WITH "products-g"

// Property shards (properties only)
SHOW DATABASES
  WHERE name STARTS WITH "products-p"
```

## Troubleshooting

### Common Issues

**1. Version Mismatch**
```
Error: property sharding requires Neo4j 2025.12+
```
Solution: Upgrade to Neo4j 2025.12 or later.

**2. Insufficient Memory**
```
Error: property sharding requires minimum 4GB memory for basic operation, got XXXMB (recommended: 8GB+)
```
**Solution**: Increase memory allocation to at least 4GB, recommended 8GB+ for production:
```yaml
resources:
  requests:
    memory: 4Gi   # Minimum requirement
  limits:
    memory: 8Gi   # Recommended for production
```

**3. Insufficient CPU Resources**
```
Error: property sharding requires minimum 1 CPU core, got 500m (recommended: 2+ cores)
```
**Solution**: Increase CPU allocation for cross-shard query performance:
```yaml
resources:
  requests:
    cpu: 2000m    # Recommended minimum for cross-shard queries
  limits:
    cpu: 4000m    # Allow burst capacity
```

**4. Invalid Server Count**
```
Error: spec.topology.servers in body should be greater than or equal to 2
```
**Solution**: Set at least 2 servers, and consider 3+ for HA graph shard primaries:
```yaml
  topology:
    servers: 3  # 3+ recommended for HA graph shard primaries
  # Consider 7 servers for larger datasets
```

**5. Cluster Not Configured for Property Sharding**
```
Error: referenced cluster does not have property sharding enabled
```
Solution: Ensure `propertySharding.enabled: true` on the referenced cluster and that it is Ready.

### Debugging Commands

```bash
# Check cluster configuration
kubectl describe neo4jenterprisecluster property-sharding-cluster

# Check sharded database events
kubectl describe neo4jshardeddatabase products-sharded-db

# View operator logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager

# Check individual database status
kubectl exec property-sharding-cluster-server-0 -- \
  cypher-shell -u neo4j -p password "SHOW DATABASES"

# Check virtual database
kubectl exec property-sharding-cluster-server-0 -- \
  cypher-shell -u neo4j -p password "SHOW DATABASES"
```

## Backup and Recovery

Note: There is no `backupConfig` on `Neo4jShardedDatabase`. Use an explicit `Neo4jBackup`
resource with `spec.target.kind: ShardedDatabase` and `spec.target.name` set to the
**`Neo4jShardedDatabase` resource name** (e.g. `products-sharded-db`) — the operator
resolves the logical database name (`spec.name`, e.g. `products`) from that resource.
A single backup captures every shard consistently in one `neo4j-admin database backup`
invocation via a `{logical-name}*` glob — you do **not** list the individual shard
databases (`products-g000`, `products-p000`, …).

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: sharded-backup
  namespace: default
spec:
  target:
    kind: ShardedDatabase          # backs up all shards in one invocation
    name: products-sharded-db      # the Neo4jShardedDatabase RESOURCE name
    clusterRef: property-sharding-cluster

  storage:
    type: pvc                      # one of: pvc | s3 | gcs | azure
    pvc:
      name: sharded-backup-storage
      storageClassName: standard
      size: 50Gi

  schedule: "0 2 * * *"            # optional cron; omit for one-shot

  retention:
    maxCount: 10

  options:
    backupType: AUTO               # FULL | DIFF | AUTO
    validate: true                 # optional per-shard recoverability check
```

The per-shard `.backup` artifacts produced by the run are recorded in
`status.history[].shardArtifacts[]` (one entry per shard, e.g. `products-g000`,
`products-p000`). When `options.validate: true`, per-shard recoverability is
surfaced under `status.history[].validation`.

To restore a sharded database from a backup, seed a new `Neo4jShardedDatabase`
via `spec.seedBackupRef` (referencing this `Neo4jBackup` CR), or perform a
destructive in-place restore with `spec.replaceExisting: true` + `spec.force: true`
(see the field reference above).

## Performance and Sizing

Queries hitting only the graph shard (structure + graph properties) are fastest; cross-shard queries touching property shards add latency proportional to result-set size. For sizing, see the dedicated [Resource Sizing guide](guides/resource_sizing.md). Sharded clusters benefit from low-latency networking (<10ms inter-pod for transaction-log sync, <5ms for query responsiveness) — co-locate pods in the same zone where possible.

**Storage planning**: total ≈ graph-shard size + (property-shards × avg property data per shard) + ~20% transaction-log + index overhead. Each property shard has its own transaction log; size accordingly.

## Migration from Standard Databases

Property sharding cannot be enabled on existing databases. Migration approaches:

1. **Create new sharded database and migrate data**
2. **Export/import with property distribution**
3. **Application-level migration with dual-write**

See [migration guide](migration_guide.md) for detailed procedures.

## Limitations

- **No in-operator resharding**: Resharding requires offline `neo4j-admin database copy` and recreation
- **Neo4j version**: Requires 2025.12+ enterprise
- **Cypher version**: Must use Cypher 25
- **No online resharding**: Plan shard count carefully
- **Increased complexity**: More monitoring and operational overhead

## Related Documentation

- [Clustering Guide](clustering.md)
- [Performance Tuning](performance.md)
- [Backup and Restore](guides/backup_restore.md)
- [Troubleshooting](guides/troubleshooting.md)
