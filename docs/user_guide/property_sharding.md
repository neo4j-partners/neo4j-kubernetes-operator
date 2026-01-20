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
- **Minimum**: Neo4j 2025.10-enterprise (feature GA as of 2025.12)
- **Recommended**: Neo4j 2025.12+ (latest GA fixes and tooling)
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
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: property-sharding-cluster
  namespace: default
spec:
  image:
    repo: neo4j
    tag: 2025.12-enterprise  # Requires 2025.10+ for property sharding (GA in 2025.12)

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
apiVersion: neo4j.neo4j.com/v1alpha1
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
Error: property sharding requires Neo4j 2025.10+
```
Solution: Upgrade to Neo4j 2025.10 or later.

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

Note: The operator does not orchestrate `backupConfig` for `Neo4jShardedDatabase`. Use explicit `Neo4jBackup` resources to back up each shard.

Property sharding backup coordinates across all shards:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
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

## Performance Considerations and Optimization

### Query Performance Patterns

**Fast Queries (Graph Shard Only):**
```cypher
// Structure queries - very fast (single shard)
MATCH (n)-[r]->(m) RETURN count(r)

// Index-based lookups on graph properties - fast
MATCH (p:Product) WHERE p.id = "12345" RETURN p.name, p.category
```

**Moderate Queries (Mixed Access):**
```cypher
// Graph structure + property shard access - moderate speed
MATCH (p:Product)
WHERE p.category = "electronics"    // Graph shard
RETURN p.name, p.description        // Mixed: graph + property shard
```

**Slower Queries (Heavy Property Access):**
```cypher
// Full property shard scans - slower
MATCH (p:Product)
WHERE p.description CONTAINS "high-performance"  // Property shard scan
RETURN p.name, p.specifications                  // Property shard data
```

### Performance Optimization Strategies

**1. Automatic Property Distribution:**

Property distribution is automatic for sharded databases. Focus on shard count, replica topology, and resource sizing.

**2. Resource Scaling Recommendations:**

*Development (Basic Testing):*
- **Memory**: 6-8GB per server (basic functionality)
- **CPU**: 1-2 cores per server (acceptable for development)
- **Network**: Standard Kubernetes networking (1Gbps)
- **Servers**: 2 minimum (3+ recommended for HA)

*Production (Recommended):*
- **Memory**: 4-8GB per server (4GB minimum, 8GB recommended)
- **CPU**: 2-4 cores per server (cross-shard query performance)
- **Network**: High-speed networking (10Gbps+, low latency)
- **Servers**: 3-7 servers (better shard distribution)

*High-Performance (Enterprise):*
- **Memory**: 16-20GB+ per server (maximum throughput)
- **CPU**: 4-6+ cores per server (concurrent cross-shard queries)
- **Network**: Ultra-low latency networking (<1ms)
- **Servers**: 7+ servers (optimal shard placement)

**3. Monitoring Key Metrics:**

*Resource Utilization:*
```bash
# Monitor memory usage per server
kubectl top pods -l app.kubernetes.io/name=your-cluster --containers

# Check CPU utilization during peak queries
kubectl top pods -l app.kubernetes.io/name=your-cluster --containers
```

*Query Performance:*
```cypher
// Monitor cross-shard query latencies
CALL dbms.queryJmx("org.neo4j:instance=kernel#0,name=Transactions")
YIELD attributes
RETURN attributes.NumberOfOpenTransactions;

// Check transaction log positions
SHOW DATABASES
  WHERE name STARTS WITH "your-db-"
  RETURN name, currentStatus, requestedStatus;
```

*Network and I/O:*
```bash
# Monitor network traffic between pods
kubectl exec your-cluster-server-0 -- ss -tuln

# Check storage I/O patterns
kubectl exec your-cluster-server-0 -- iostat -x 1
```

### Storage Scaling Considerations

**Graph Shard Growth:**
- Grows with number of nodes and relationships
- Index storage for graph properties
- Transaction logs for consensus

**Property Shard Growth:**
- Grows with property volume per shard
- Distributed based on hash function
- Each shard has independent transaction logs

**Total Storage Planning:**
```
Total Storage = Graph_Shard + (Property_Shards × Avg_Property_Size) + Overhead

Example:
- Graph: 10GB (structure + graph properties)
- Property Shards: 4 × 25GB = 100GB (distributed properties)
- Overhead: 20GB (transaction logs, indexes, system)
- Total: ~130GB per server (with replication)
```

**Network Performance Requirements:**

*Transaction Log Synchronization:*
- **Latency**: <10ms between servers (critical)
- **Bandwidth**: 100MB/s+ sustained (busy clusters)
- **Consistency**: All shards must stay within transaction log window

*Cross-Shard Query Traffic:*
- **Latency**: <5ms for responsive queries
- **Bandwidth**: Proportional to result set size
- **Concurrent Queries**: Multiple cross-shard queries increase traffic

### Performance Troubleshooting

**Slow Query Performance:**
1. Check property distribution strategy
2. Monitor cross-shard query patterns
3. Verify adequate CPU allocation
4. Check network latency between servers

**High Memory Usage:**
1. Monitor heap usage during peak loads
2. Check transaction log retention settings
3. Verify property shard distribution balance
4. Consider increasing memory limits

**Network Saturation:**
1. Monitor inter-pod network traffic
2. Check for transaction log lag
3. Verify network bandwidth capacity
4. Consider network policy optimizations

## Migration from Standard Databases

Property sharding cannot be enabled on existing databases. Migration approaches:

1. **Create new sharded database and migrate data**
2. **Export/import with property distribution**
3. **Application-level migration with dual-write**

See [migration guide](migration.md) for detailed procedures.

## Limitations

- **No in-operator resharding**: Resharding requires offline `neo4j-admin database copy` and recreation
- **Neo4j version**: Requires 2025.10+ enterprise
- **Cypher version**: Must use Cypher 25
- **No online resharding**: Plan shard count carefully
- **Increased complexity**: More monitoring and operational overhead

## Related Documentation

- [Clustering Guide](clustering.md)
- [Performance Tuning](performance.md)
- [Backup and Restore](guides/backup_restore.md)
- [Troubleshooting](guides/troubleshooting.md)
