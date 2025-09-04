# Property Sharding with Neo4j Kubernetes Operator

Property Sharding is an advanced Neo4j feature that separates graph structure (nodes and relationships) from properties, enabling better scalability for property-heavy workloads by distributing properties across multiple databases.

## Overview

Property Sharding decouples data into:
- **Graph Shard**: Single database containing nodes and relationships WITHOUT properties
- **Property Shards**: Multiple databases containing properties distributed via hash function
- **Virtual Database**: Logical database presenting unified view of graph + property shards

## Prerequisites

### Neo4j Version Requirements
- **Minimum**: Neo4j 2025.06.0-enterprise (first version with property sharding support)
- **Recommended**: Neo4j 2025.08.0+ (includes stability improvements)

### Cluster Requirements
- **Minimum Servers**: 3 (to host graph shard with HA and property shards)
- **Recommended Servers**: 5+ (for better distribution and fault tolerance)
- **Memory**: 4GB+ per server (property sharding requires additional memory)
- **Cypher Version**: Must use Cypher 25 for sharded database operations

## Quick Start

### 1. Create Property Sharding Enabled Cluster

```yaml
apiVersion: neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: property-sharding-cluster
  namespace: default
spec:
  image:
    repo: neo4j
    tag: 2025.06-enterprise  # Required for property sharding

  topology:
    servers: 5  # Minimum 3, recommended 5+

  storage:
    size: 10Gi

  resources:
    requests:
      memory: 4Gi    # Minimum for property sharding
      cpu: 1000m
    limits:
      memory: 8Gi
      cpu: 2000m

  # Enable property sharding
  propertySharding:
    enabled: true
    config:
      # Required configuration (applied automatically if not specified)
      internal.dbms.sharded_property_database.enabled: "true"
      db.query.default_language: "CYPHER_25"
      internal.dbms.cluster.experimental_protocol_version.dbms_enabled: "true"
      internal.dbms.sharded_property_database.allow_external_shard_access: "false"

      # Performance tuning (optional)
      db.tx_log.rotation.retention_policy: "7 days"
      internal.dbms.sharded_property_database.property_pull_interval: "10ms"
```

### 2. Create Sharded Database

```yaml
apiVersion: neo4j.com/v1alpha1
kind: Neo4jShardedDatabase
metadata:
  name: products-sharded-db
  namespace: default
spec:
  # Reference to property sharding enabled cluster
  clusterRef: property-sharding-cluster

  # Virtual database name (what users connect to)
  name: products

  # Required for property sharding
  defaultCypherLanguage: "25"

  # Property sharding configuration
  propertySharding:
    propertyShards: 4        # Number of property shards
    hashFunction: murmur3    # Hash function for distribution

    # Graph shard topology (stores nodes/relationships)
    graphShard:
      primaries: 3
      secondaries: 2

    # Property shard topology (stores properties)
    propertyShardTopology:
      primaries: 2
      secondaries: 1

    # Optional: Property filtering
    includedProperties:      # Only these properties are sharded
      - description
      - metadata
      - large_text_field

    excludedProperties:      # These properties stay in graph shard
      - id                   # Keep IDs in graph shard for performance
      - name                 # Frequently accessed properties

  # Database creation options
  wait: true          # Wait for creation to complete
  ifNotExists: true   # Don't fail if database exists
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
  internal.dbms.cluster.experimental_protocol_version.dbms_enabled: "true"
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
| `propertyShards` | int32 | Yes | Number of property shards (1-64) |
| `hashFunction` | string | No | Hash function: `murmur3` (default) or `sha256` |
| `includedProperties` | []string | No | Only these properties are sharded |
| `excludedProperties` | []string | No | These properties stay in graph shard |
| `graphShard` | DatabaseTopology | Yes | Topology for graph shard |
| `propertyShardTopology` | DatabaseTopology | Yes | Topology for property shards |

#### DatabaseTopology

| Field | Type | Description |
|-------|------|-------------|
| `primaries` | int32 | Number of primary replicas |
| `secondaries` | int32 | Number of secondary replicas |

## Best Practices

### Cluster Sizing

**Development/Testing**:
- 3 servers minimum
- 4GB RAM per server
- Property shards: 2-4

**Production**:
- 5+ servers recommended
- 8GB+ RAM per server
- Property shards: 4-16 (start conservatively)

### Property Distribution Strategy

**Include in Property Shards**:
- Large text fields
- JSON/blob data
- Infrequently accessed metadata
- Analytics properties

**Keep in Graph Shard**:
- Primary keys and IDs
- Frequently accessed properties
- Small properties used in WHERE clauses
- Properties used for indexing

### Shard Count Recommendations

| Dataset Size | Property Shards | Reasoning |
|--------------|-----------------|-----------|
| < 1M nodes | 2-4 | Minimal overhead |
| 1M-10M nodes | 4-8 | Balanced distribution |
| 10M-100M nodes | 8-16 | Better parallelization |
| 100M+ nodes | 16-32 | Maximum distribution |

**Note**: Property shard count is fixed at creation and cannot be changed later.

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
Error: property sharding requires Neo4j 2025.06+
```
Solution: Upgrade to Neo4j 2025.06 or later.

**2. Insufficient Resources**
```
Error: property sharding requires minimum 4GB memory
```
Solution: Increase cluster resource limits.

**3. Too Few Servers**
```
Error: property sharding requires minimum 3 servers
```
Solution: Scale cluster to at least 3 servers.

**4. Cluster Not Ready**
```
Error: cluster does not support property sharding
```
Solution: Ensure `propertySharding.enabled: true` and cluster is Ready.

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
  cypher-shell -u neo4j -p password "SHOW SHARDED DATABASES"
```

## Backup and Recovery

Property sharding backup coordinates across all shards:

```yaml
apiVersion: neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: sharded-backup
spec:
  clusterRef: property-sharding-cluster

  # Multi-database backup
  databases:
    - name: "products-g000"  # Graph shard
      type: "full+differential"
    - name: "products-p000"  # Property shards
      type: "full"
    - name: "products-p001"
      type: "full"
    - name: "products-p002"
      type: "full"
    - name: "products-p003"
      type: "full"

  schedule: "0 2 * * *"
  consistency: "cross-database"  # Ensure consistent backup point
```

## Performance Considerations

### Query Performance
- Cross-shard queries may have higher latency
- Properties in graph shard have better query performance
- Use appropriate property distribution strategy

### Storage Growth
- Graph shard grows with nodes/relationships
- Property shards grow with property volume
- Total storage is sum of all shards plus overhead

### Network Traffic
- Increased inter-server traffic for shard coordination
- Transaction log synchronization between shards
- Consider low-latency networking

## Migration from Standard Databases

Property sharding cannot be enabled on existing databases. Migration approaches:

1. **Create new sharded database and migrate data**
2. **Export/import with property distribution**
3. **Application-level migration with dual-write**

See [migration guide](migration.md) for detailed procedures.

## Limitations

- **Fixed shard count**: Cannot change property shard count after creation
- **Neo4j version**: Requires 2025.06+ enterprise
- **Cypher version**: Must use Cypher 25
- **No dynamic resharding**: Plan shard count carefully
- **Increased complexity**: More monitoring and operational overhead

## Related Documentation

- [Clustering Guide](clustering.md)
- [Performance Tuning](performance.md)
- [Backup and Restore](guides/backup_restore.md)
- [Troubleshooting](guides/troubleshooting.md)
