# Neo4j Property Sharding Examples

This directory contains examples demonstrating how to configure Neo4j Property Sharding with the Kubernetes Operator.

## Prerequisites

- Neo4j Kubernetes Operator deployed
- Neo4j Enterprise 2025.10+ images (GA as of 2025.12; examples use 2025.12)
- Kubernetes cluster with at least 1 node (3+ recommended for HA graph shard primaries)
- Storage class supporting persistent volumes
- Admin secret for Neo4j authentication

## Examples

### 1. Basic Property Sharding (`basic-property-sharding.yaml`)

**Use Case**: Getting started with property sharding

**Features**:
- Minimal 3-server cluster (recommended for HA primaries)
- Basic sharded database with 2 property shards
- Authentication and storage class configuration
- Suitable for learning and small datasets

**Deploy**:
```bash
kubectl apply -f basic-property-sharding.yaml
```

**Verify**:
```bash
# Check cluster is ready with property sharding
kubectl get neo4jenterpriseclusters basic-sharding-cluster -o yaml | grep propertyShardingReady

# Check sharded database status
kubectl get neo4jshardeddatabases basic-sharded-db
```

### 2. Development Setup (`development-property-sharding.yaml`)

**Use Case**: Development and testing environments

**Features**:
- Resource-optimized for development
- Separate namespace for isolation
- Minimal shard configuration
- Fast deployment and teardown

**Deploy**:
```bash
kubectl apply -f development-property-sharding.yaml
```

**Access**:
```bash
# Port forward to access database
kubectl port-forward -n neo4j-dev svc/dev-sharding-cluster-client 7687:7687

# Connect with cypher-shell
cypher-shell -a bolt://localhost:7687 -u neo4j -p development123
```

### 3. Production Configuration (`advanced-property-sharding.yaml`)

**Use Case**: Production deployments with high availability

**Features**:
- 3+ server cluster with high availability
- 8 property shards with replica topology
- TLS encryption
- Performance tuning
- Backup integration
- Resource optimization

**Deploy**:
```bash
# Make sure to replace placeholder secrets with real credentials
kubectl apply -f advanced-property-sharding.yaml
```

### 4. Backup Integration (`property-sharding-with-backup.yaml`)

**Use Case**: Demonstrating backup strategies for sharded databases

**Features**:
- Coordinated multi-shard backups using `Neo4jBackup`

Note: `backupConfig` on `Neo4jShardedDatabase` is not orchestrated yet; use explicit backup resources.
- S3 storage integration
- Consistency guarantees across shards
- Retention policies

**Deploy**:
```bash
# Update backup credentials before deploying
kubectl apply -f property-sharding-with-backup.yaml
```

## Configuration Guide

### Choosing Property Shard Count

| Dataset Size | Recommended Shards | Reasoning |
|--------------|-------------------|-----------|
| < 1M nodes | 2-4 | Minimal overhead |
| 1M-10M nodes | 4-8 | Balanced distribution |
| 10M-100M nodes | 8-16 | Better parallelization |
| 100M+ nodes | 16-32 | Maximum distribution |

### Property Distribution Strategy

Property distribution across shards is automatic; there are no per-property controls in the operator.

### Resource Sizing

**Development**:
```yaml
resources:
  requests:
    memory: 4Gi    # Property sharding requires 4GB+ memory (minimum)
    cpu: 2000m     # 2+ cores required for cross-shard queries
  limits:
    memory: 8Gi    # Recommended for stable property sharding performance
    cpu: 2000m     # Consistent performance for development
```

**Production (High-Performance)**:
```yaml
resources:
  requests:
    memory: 16Gi   # High-performance allocation for large datasets (minimum 4GB supported)
    cpu: 4000m     # 4+ cores for optimal cross-shard performance
  limits:
    memory: 20Gi   # Maximum performance for enterprise workloads
    cpu: 6000m     # Additional CPU for high-throughput shard coordination
```

## Verification Commands

### Check Cluster Status
```bash
# Verify property sharding is enabled and ready
kubectl get neo4jenterpriseclusters -o custom-columns=\
NAME:.metadata.name,READY:.status.propertyShardingReady,PHASE:.status.phase
```

### Check Sharded Database Status
```bash
# List all sharded databases
kubectl get neo4jshardeddatabases

# Get detailed status
kubectl describe neo4jshardeddatabase <name>
```

### Connect and Query
```bash
# Port forward to cluster
kubectl port-forward svc/<cluster-name>-client 7687:7687

# Connect with cypher-shell
cypher-shell -a bolt://localhost:7687 -u neo4j -p <password>

# Use virtual database
:use <database-name>

# Query normally (properties retrieved from appropriate shards)
MATCH (n) RETURN n LIMIT 10;
```

### Check Individual Shards
```cypher
// Connect to system database
:use system

// List all databases (including shards)
SHOW DATABASES;

// Check virtual databases
SHOW DATABASES;

// Verify shard topology
SHOW DATABASES WHERE name STARTS WITH '<db-name>-'
```

## Troubleshooting

### Common Issues

**Cluster not ready for property sharding**:
```bash
kubectl describe neo4jenterprisecluster <name>
# Check events and status.message for details
```

**Sharded database creation failed**:
```bash
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager
# Look for sharded database controller errors
```

**Performance issues**:
- Check property distribution strategy
- Monitor cross-shard query patterns
- Verify resource allocation
- Review property shard count

### Debugging Commands
```bash
# Check operator logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager

# Check cluster formation
kubectl exec <cluster>-server-0 -- \
  cypher-shell -u neo4j -p <password> "SHOW SERVERS"

# Check shard status
kubectl exec <cluster>-server-0 -- \
  cypher-shell -u neo4j -p <password> "SHOW DATABASES"

# Monitor resource usage
kubectl top pods -l app.kubernetes.io/name=<cluster>
```

## Migration from Standard Databases

Property sharding cannot be enabled on existing databases. For migration:

1. **Create new sharded database**
2. **Export data from existing database**
3. **Import data with property distribution**
4. **Update application connection strings**
5. **Decommission old database**

## Performance Testing

Sample queries to test property sharding performance:

```cypher
// Test graph structure queries (should be fast)
MATCH (n)-[r]->(m) RETURN count(r);

// Test property access (may be slower for sharded properties)
MATCH (n) WHERE n.description CONTAINS "keyword" RETURN n.name;

// Test mixed queries
MATCH (n:Product)
WHERE n.category = "electronics"  // graph shard
RETURN n.name, n.description      // mixed: graph + property shard
LIMIT 100;
```

## Best Practices

1. **Plan shard count carefully** - cannot be changed later
2. **Distribute properties strategically** - keep frequently accessed properties in graph shard
3. **Monitor performance** - watch for cross-shard query patterns
4. **Use consistent backups** - ensure point-in-time consistency
5. **Test thoroughly** - validate performance with representative workloads
6. **Resource planning** - property sharding requires more memory and CPU
7. **Network optimization** - ensure low-latency connectivity between servers

## Related Documentation

- [Property Sharding User Guide](../../docs/user_guide/property_sharding.md)
- [Property Sharding API Reference](../../docs/api_reference/neo4jshardeddatabase.md)
- [Performance Tuning](../../docs/user_guide/performance.md)
- [Backup and Restore](../../docs/user_guide/guides/backup_restore.md)
