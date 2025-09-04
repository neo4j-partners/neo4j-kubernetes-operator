# Neo4jShardedDatabase API Reference

The `Neo4jShardedDatabase` Custom Resource Definition (CRD) manages sharded databases with property sharding for horizontal scaling of large datasets in Neo4j 2025.06+ clusters.

## Overview

- **API Version**: `neo4j.com/v1alpha1`
- **Kind**: `Neo4jShardedDatabase`
- **Supported Neo4j Versions**: 2025.06.0+ (requires property sharding support)
- **Prerequisites**: Neo4jEnterpriseCluster with `propertySharding.enabled: true`

This document provides detailed API specifications for both Neo4jShardedDatabase and the property sharding configuration in Neo4jEnterpriseCluster.

## Neo4jEnterpriseCluster.propertySharding

Property sharding configuration for Neo4j Enterprise clusters.

### PropertyShardingSpec

```yaml
propertySharding:
  enabled: boolean                    # Required: Enable property sharding support
  config: map[string]string          # Optional: Advanced configuration
```

#### Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | `boolean` | Yes | - | Enables property sharding support on the cluster |
| `config` | `map[string]string` | No | See below | Advanced property sharding configuration |

#### Default Configuration

When `enabled: true`, these settings are automatically applied:

```yaml
config:
  internal.dbms.sharded_property_database.enabled: "true"
  db.query.default_language: "CYPHER_25"
  internal.dbms.cluster.experimental_protocol_version.dbms_enabled: "true"
  internal.dbms.sharded_property_database.allow_external_shard_access: "false"
```

#### Configuration Options

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `internal.dbms.sharded_property_database.enabled` | string | "true" | Enable property sharding database feature |
| `db.query.default_language` | string | "CYPHER_25" | Default Cypher language version |
| `internal.dbms.cluster.experimental_protocol_version.dbms_enabled` | string | "true" | Enable experimental protocol features |
| `internal.dbms.sharded_property_database.allow_external_shard_access` | string | "false" | Allow external access to individual shards |
| `db.tx_log.rotation.retention_policy` | string | "7 days" | Transaction log retention policy |
| `internal.dbms.sharded_property_database.property_pull_interval` | string | "10ms" | Property synchronization interval |

### Status Fields

```yaml
status:
  propertyShardingReady: boolean      # Indicates if property sharding is ready
```

| Field | Type | Description |
|-------|------|-------------|
| `propertyShardingReady` | `*bool` | Indicates whether property sharding is configured and operational |

#### Prerequisites for propertyShardingReady=true

1. Cluster phase is "Ready"
2. Neo4j version is 2025.06+
3. Minimum 3 servers configured
4. Minimum 4GB memory per server
5. All required configuration applied

---

## Neo4jShardedDatabase

Manages property-sharded databases on Neo4j Enterprise clusters.

### Neo4jShardedDatabaseSpec

```yaml
apiVersion: neo4j.com/v1alpha1
kind: Neo4jShardedDatabase
metadata:
  name: string
spec:
  clusterRef: string                             # Required
  name: string                                   # Required
  defaultCypherLanguage: string                  # Required: "25"
  propertySharding: PropertyShardingConfiguration  # Required
  wait: boolean                                  # Optional: true
  ifNotExists: boolean                          # Optional: true
  initialGraphData: InitialDataSpec             # Optional
  seedConfig: SeedConfiguration                 # Optional
  seedCredentials: SeedCredentials              # Optional
  backupConfig: ShardedDatabaseBackupConfig     # Optional
```

#### Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `clusterRef` | `string` | Yes | - | Reference to Neo4j cluster hosting this sharded database |
| `name` | `string` | Yes | - | Name of the sharded database to create |
| `defaultCypherLanguage` | `string` | Yes | - | Must be "25" for property sharding |
| `propertySharding` | `PropertyShardingConfiguration` | Yes | - | Property sharding configuration |
| `wait` | `boolean` | No | true | Wait for database creation to complete |
| `ifNotExists` | `boolean` | No | true | Create database only if it doesn't exist |
| `initialGraphData` | `InitialDataSpec` | No | - | Initial data for graph shard |
| `seedConfig` | `SeedConfiguration` | No | - | Seed configuration for initialization |
| `seedCredentials` | `SeedCredentials` | No | - | Credentials for seed URI access |
| `backupConfig` | `ShardedDatabaseBackupConfig` | No | - | Backup configuration |

### PropertyShardingConfiguration

```yaml
propertySharding:
  propertyShards: int32                    # Required: 1-64
  hashFunction: string                     # Optional: "murmur3"|"sha256"
  includedProperties: []string             # Optional
  excludedProperties: []string             # Optional
  graphShard: DatabaseTopology             # Required
  propertyShardTopology: DatabaseTopology  # Required
  config: map[string]string               # Optional
```

#### Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `propertyShards` | `int32` | Yes | - | Number of property shards (1-64) |
| `hashFunction` | `string` | No | "murmur3" | Hash function: "murmur3" or "sha256" |
| `includedProperties` | `[]string` | No | [] | Only these properties are sharded |
| `excludedProperties` | `[]string` | No | [] | These properties stay in graph shard |
| `graphShard` | `DatabaseTopology` | Yes | - | Topology for graph shard database |
| `propertyShardTopology` | `DatabaseTopology` | Yes | - | Topology for property shard databases |
| `config` | `map[string]string` | No | {} | Advanced configuration options |

#### Hash Functions

| Value | Description | Performance | Use Case |
|-------|-------------|-------------|----------|
| `murmur3` | Fast, good distribution | High | General purpose (recommended) |
| `sha256` | Cryptographically secure | Lower | Security-sensitive environments |

#### Property Filtering

**includedProperties**: If specified, only these properties are distributed to property shards. All others remain in graph shard.

**excludedProperties**: These properties always stay in graph shard. Takes precedence over `includedProperties`.

**Best Practices**:
- Include: Large text, JSON, blob data, metadata
- Exclude: IDs, frequently accessed properties, indexed fields

### DatabaseTopology

```yaml
topology:
  primaries: int32     # Required: Number of primary replicas
  secondaries: int32   # Required: Number of secondary replicas
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `primaries` | `int32` | Yes | Number of primary replicas |
| `secondaries` | `int32` | Yes | Number of secondary replicas |

#### Topology Guidelines

**Graph Shard** (stores nodes/relationships):
- Recommended: 3+ primaries for high availability
- Uses Raft consensus for consistency

**Property Shards** (store properties):
- Recommended: 2+ primaries for fault tolerance
- Uses replica-based replication

### ShardedDatabaseBackupConfig

```yaml
backupConfig:
  enabled: boolean           # Optional: true
  schedule: string          # Optional: Cron expression
  storage: StorageLocation  # Optional
  retention: string         # Optional: "7d"
  consistencyMode: string   # Optional: "strict"|"eventual"
  timeout: string          # Optional: "30m"
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | `boolean` | true | Enable coordinated backups |
| `schedule` | `string` | - | Cron schedule for backups |
| `storage` | `StorageLocation` | - | Backup storage configuration |
| `retention` | `string` | "7d" | Backup retention policy |
| `consistencyMode` | `string` | "strict" | "strict" or "eventual" consistency |
| `timeout` | `string` | "30m" | Maximum backup operation timeout |

#### Consistency Modes

| Mode | Description | Performance | Use Case |
|------|-------------|-------------|----------|
| `strict` | All shards backed up simultaneously | Lower | Consistent point-in-time backups |
| `eventual` | Shards backed up sequentially | Higher | Faster backups, less consistency |

### Neo4jShardedDatabaseStatus

```yaml
status:
  conditions: []metav1.Condition         # Standard conditions
  phase: string                         # Current phase
  message: string                       # Status message
  observedGeneration: int64             # Observed generation
  shardingReady: boolean               # All shards operational
  creationTime: metav1.Time            # Creation timestamp
  graphShard: ShardStatus              # Graph shard status
  propertyShards: []ShardStatus        # Property shard statuses
  virtualDatabase: VirtualDatabaseStatus # Virtual database status
  backupStatus: ShardedBackupStatus    # Backup status
  totalSize: string                    # Total size across shards
  lastBackupTime: metav1.Time          # Last backup time
```

#### Phase Values

| Phase | Description |
|-------|-------------|
| `Initializing` | Creating and configuring shards |
| `Ready` | All shards operational |
| `Failed` | Error in shard creation or operation |
| `Mixed` | Some shards operational, others not |

#### Conditions

Standard Kubernetes conditions with these types:

| Type | Description |
|------|-------------|
| `Ready` | Sharded database is ready for use |
| `GraphShardReady` | Graph shard is operational |
| `PropertyShardsReady` | All property shards are operational |
| `VirtualDatabaseReady` | Virtual database is accessible |

### ShardStatus

```yaml
shardStatus:
  name: string                  # Shard database name
  type: string                 # "graph" or "property"
  state: string                # Database state
  size: string                 # Database size
  servers: []string            # Hosting servers
  ready: boolean              # Operational status
  lastError: string           # Last error message
  propertyShardIndex: int32   # Property shard index (property shards only)
  propertyCount: int64        # Property count (property shards only)
```

#### Shard Types

| Type | Description | Naming Pattern |
|------|-------------|----------------|
| `graph` | Graph structure (nodes/relationships) | `{database}-g000` |
| `property` | Properties distributed by hash | `{database}-p{000-999}` |

#### Shard States

| State | Description |
|-------|-------------|
| `online` | Shard is operational |
| `offline` | Shard is not available |
| `quarantined` | Shard temporarily excluded due to lag |

### VirtualDatabaseStatus

```yaml
virtualDatabase:
  name: string                    # Virtual database name
  ready: boolean                  # Ready for queries
  endpoint: string               # Connection endpoint
  metrics: VirtualDatabaseMetrics # Performance metrics
```

#### VirtualDatabaseMetrics

```yaml
metrics:
  totalNodes: int64                          # Total nodes across shards
  totalRelationships: int64                  # Total relationships
  totalProperties: int64                     # Total properties
  queryMetrics: QueryPerformanceMetrics      # Query performance
```

#### QueryPerformanceMetrics

```yaml
queryMetrics:
  averageQueryTime: string                   # Average execution time
  crossShardQueriesPerSecond: string         # Cross-shard query rate
  propertyCacheHitRatio: string             # Property cache efficiency
```

## Validation Rules

### Neo4jEnterpriseCluster Validation

- Neo4j version must be 2025.06+ when property sharding enabled
- Minimum 3 servers required for property sharding
- Minimum 4GB memory per server recommended
- Required configuration automatically applied

### Neo4jShardedDatabase Validation

- `clusterRef` must reference existing Neo4jEnterpriseCluster with property sharding enabled
- `defaultCypherLanguage` must be "25"
- `propertyShards` must be 1-64
- `hashFunction` must be "murmur3" or "sha256"
- Target cluster must be in "Ready" phase with `propertyShardingReady: true`
- `graphShard.primaries` should be >= 3 for high availability
- Cannot have duplicate property indices
- `excludedProperties` takes precedence over `includedProperties`

## Error Conditions

### Common Validation Errors

| Error | Cause | Resolution |
|-------|-------|------------|
| `property sharding requires Neo4j 2025.06+` | Old Neo4j version | Upgrade to 2025.06+ |
| `property sharding requires minimum 3 servers` | Insufficient servers | Increase server count |
| `property sharding requires minimum 4GB memory` | Insufficient memory | Increase memory limits |
| `defaultCypherLanguage must be '25'` | Wrong Cypher version | Set to "25" |
| `cluster does not support property sharding` | Cluster not configured | Enable property sharding on cluster |
| `propertyShards must be at least 1` | Invalid shard count | Set to valid range (1-64) |

### Runtime Errors

| Error | Cause | Resolution |
|-------|-------|------------|
| `failed to create graph shard` | Cluster capacity issues | Check cluster resources |
| `property shard creation failed` | Network or storage issues | Check cluster connectivity |
| `virtual database creation failed` | Neo4j configuration issues | Verify cluster configuration |
| `shard synchronization failed` | Transaction log issues | Check retention policies |

## Examples

See [examples directory](../../examples/property_sharding/) for complete configuration examples.

## Related APIs

- [Neo4jEnterpriseCluster API](cluster_api.md)
- [Neo4jBackup API](backup_api.md)
- [Neo4jRestore API](restore_api.md)
- [Storage APIs](storage_api.md)
