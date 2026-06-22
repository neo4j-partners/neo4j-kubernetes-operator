# Neo4jShardedDatabase API Reference

The `Neo4jShardedDatabase` Custom Resource Definition (CRD) manages sharded databases with property sharding for horizontal scaling of large datasets in Neo4j 2025.12+ clusters.

## Overview

- **API Version**: `neo4j.neo4j.com/v1beta1`
- **Kind**: `Neo4jShardedDatabase`
- **Supported Neo4j Versions**: 2025.12+ (requires property sharding support)
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
  internal.dbms.sharded_property_database.allow_external_shard_access: "false"
```

#### Configuration Options

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `internal.dbms.sharded_property_database.enabled` | string | "true" | Enable property sharding database feature |
| `db.query.default_language` | string | "CYPHER_25" | Default Cypher language version |
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
2. Neo4j version is 2025.12+
3. Minimum 2 servers configured (3+ recommended for HA graph shard primaries)
4. Minimum 4GB memory per server (8GB+ recommended for production)
5. Minimum 1 CPU core per server (2+ cores recommended for cross-shard queries)
6. All required configuration applied
7. Authentication configured (admin secret required)

#### Resource Planning Guidelines

**Development Environment:**
```yaml
topology:
  servers: 3
resources:
  requests:
    memory: 4Gi    # Absolute minimum for property sharding
    cpu: 1000m     # Basic operation
  limits:
    memory: 8Gi
    cpu: 2000m
```

**Production Environment:**
```yaml
topology:
  servers: 3      # or 5+ for larger datasets
resources:
  requests:
    memory: 8Gi    # Recommended for production
    cpu: 2000m     # Cross-shard query performance
  limits:
    memory: 8Gi    # Allow headroom for peak loads
    cpu: 4000m     # Handle concurrent operations
```

**High-Performance Production:**
```yaml
topology:
  servers: 7      # Better shard distribution
resources:
  requests:
    memory: 8Gi    # Production performance
    cpu: 4000m     # Maximum throughput
  limits:
    memory: 20Gi   # Peak load handling
    cpu: 6000m     # Burst capability
```

---

## Neo4jShardedDatabase

Manages property-sharded databases on Neo4j Enterprise clusters.

### Neo4jShardedDatabaseSpec

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jShardedDatabase
metadata:
  name: string
spec:
  clusterRef: string                             # Required
  name: string                                   # Required
  defaultCypherLanguage: string                  # Required: "25"
  propertySharding: PropertyShardingConfiguration  # Required
  wait: boolean                                  # Optional: true
  ifNotExists: boolean                          # Optional: *bool, default true
  replaceExisting: boolean                      # Optional: destructive recreate
  force: boolean                                # Optional: confirms replaceExisting
  seedURI: string                               # Optional
  seedURIs: map[string]string                   # Optional
  seedBackupRef: string                         # Optional
  seedSourceDatabase: string                    # Optional
  seedConfig: SeedConfiguration                 # Optional
  seedCredentials: SeedCredentials              # Optional
  txLogEnrichment: string                       # Optional
```

#### Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `clusterRef` | `string` | Yes | - | Reference to Neo4j cluster hosting this sharded database |
| `name` | `string` | Yes | - | Name of the sharded database to create |
| `defaultCypherLanguage` | `string` | Yes | - | Must be "25" for property sharding |
| `propertySharding` | `PropertyShardingConfiguration` | Yes | - | Property sharding configuration |
| `wait` | `boolean` | No | true | Wait for database creation to complete |
| `ifNotExists` | `*boolean` | No | true | Pointer type. When unset (nil) or `true`, creation is idempotent (`CREATE DATABASE ... IF NOT EXISTS`). Set explicitly to `false` to omit the `IF NOT EXISTS` clause — required when paired with `replaceExisting: true`. Use `ifNotExistsEffective()`; never dereference directly |
| `replaceExisting` | `boolean` | No | false | **Destructive.** Drops and recreates the sharded database from the seed (typically `seedBackupRef`). Runs `DROP DATABASE {name} DESTROY DATA WAIT` before CREATE — all existing data is lost. Requires `force: true`; mutually exclusive with `ifNotExists: true`; requires a seed source |
| `force` | `boolean` | No | false | Confirms the destructive `replaceExisting` operation. The validator rejects `replaceExisting: true` without `force: true` so an accidental flip can't destroy data |
| `seedURI` | `string` | No | - | Seed URI for creating the sharded database |
| `seedURIs` | `map[string]string` | No | - | Seed URIs keyed by shard name |
| `seedBackupRef` | `string` | No | - | Names a `Neo4jBackup` CR (same namespace) whose most-recent Succeeded run seeds this database. Resolved to a concrete seed URI at reconcile time. Mutually exclusive with `seedURI` and `seedURIs`. If the referenced backup has no Succeeded run yet, the database stays in `Pending` and the reconciler requeues |
| `seedSourceDatabase` | `string` | No | - | Seed source database name for metadata lookup |
| `seedConfig` | `SeedConfiguration` | No | - | Seed configuration for initialization |
| `seedCredentials` | `SeedCredentials` | No | - | Credentials for seed URI access |
| `txLogEnrichment` | `string` | No | - | Transaction log enrichment option |

**Seed URI Notes**:

- `seedURI` is for a single backup location (expects shard-suffixed artifacts).
- `seedURIs` is for per-shard URIs (e.g., dump files or multi-location backups).
- `seedBackupRef` is mutually exclusive with both `seedURI` and `seedURIs`; the operator materialises it into a seed URI at reconcile time.
- `seedConfig.restoreUntil` maps to the `seedRestoreUntil` CREATE DATABASE option for sharded databases.

### PropertyShardingConfiguration

```yaml
propertySharding:
  propertyShards: int32                    # Required: 1-1000
  graphShard: DatabaseTopology             # Required
  propertyShardTopology: PropertyShardTopology  # Required
```

#### Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `propertyShards` | `int32` | Yes | - | Number of property shards (1-1000) |
| `graphShard` | `DatabaseTopology` | Yes | - | Topology for graph shard database |
| `propertyShardTopology` | `PropertyShardTopology` | Yes | - | Replica topology for property shard databases |

### DatabaseTopology

```yaml
topology:
  primaries: int32     # Optional: Number of primary replicas
  secondaries: int32   # Optional: Number of secondary replicas
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `primaries` | `int32` | No | Number of primary replicas (at least 1 required to host the database) |
| `secondaries` | `int32` | No | Number of secondary replicas (read-only scaling) |

### PropertyShardTopology

```yaml
propertyShardTopology:
  replicas: int32   # Optional: Number of replicas per property shard (default 1)
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `replicas` | `int32` | No | 1 | Number of replicas per property shard |

#### Topology Guidelines

**Graph Shard** (stores nodes/relationships):

- Recommended: 3+ primaries for high availability
- Uses Raft consensus for consistency

**Property Shards** (store properties):

- Recommended: 2+ replicas for fault tolerance
- Uses replica-based replication

### Backups and Restore

Sharded databases are a first-class backup/restore target. The `Neo4jBackup` CRD's `target.kind` accepts `ShardedDatabase` (alongside `Cluster` and `Database`); a single backup run captures every shard consistently using a `{name}*` glob, with per-shard artifacts tracked in `status.history[].shardArtifacts`.

Restore is performed by re-creating the sharded database from a backup rather than via a restore Job:

- Seed a fresh sharded database from a backup with `spec.seedBackupRef` (resolves the named `Neo4jBackup`'s latest Succeeded run into a seed URI).
- Recover an existing sharded database destructively with `spec.replaceExisting: true` + `spec.force: true`, which drops and recreates the database from the seed (`Neo4jRestore` rejects sharded targets and points here).

### Neo4jShardedDatabaseStatus

```yaml
status:
  conditions: []metav1.Condition         # Standard conditions
  phase: string                          # Current phase
  message: string                        # Status message
  observedGeneration: int64              # Observed generation
  shardingReady: boolean                 # All shards operational
  creationTime: metav1.Time              # Creation timestamp
  graphShard: ShardStatus                # Graph shard status
  propertyShards: []ShardStatus          # Property shard statuses
  virtualDatabase: VirtualDatabaseStatus  # Virtual database status
  totalSize: string                      # Total size across shards
  lastBackup: object                     # Reverse-lookup of most recent Succeeded backup
  lastDestructiveRestoreGeneration: int64  # Generation at which the last replaceExisting recreate completed
```

`lastDestructiveRestoreGeneration` gates the destructive `replaceExisting` path: the controller only runs the `DROP ... DESTROY DATA` + recreate cycle when `lastDestructiveRestoreGeneration < metadata.generation`, stamping it on success so subsequent reconciles fall through to a no-op. Re-trigger by mutating the spec (which bumps the generation). `lastBackup` is a non-authoritative observability shortcut populated from the owning `Neo4jBackup` history; the source of truth remains `Neo4jBackup.status.history`.

#### Phase Values

| Phase | Description |
|-------|-------------|
| `Initializing` | Creating and configuring shards |
| `Pending` | Waiting on a `seedBackupRef` whose backup has no Succeeded run yet (reconciler requeues) |
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

- Neo4j version must be 2025.12+ when property sharding enabled
- Minimum 2 servers required for property sharding (3+ recommended for HA graph shard primaries)
- Minimum 4GB memory per server (8GB+ recommended for production)
- Minimum 1 CPU core per server (2+ cores recommended for cross-shard queries)
- Authentication required (admin secret must be configured)
- Storage class must be specified
- Required configuration automatically applied

### Neo4jShardedDatabase Validation

- `clusterRef` must reference existing Neo4jEnterpriseCluster with property sharding enabled
- `defaultCypherLanguage` must be "25"
- `propertyShards` must be 1-1000
- `propertyShardTopology.replicas` must be >= 1
- `seedURI` and `seedURIs` cannot both be set
- `seedBackupRef` is mutually exclusive with `seedURI` and `seedURIs`
- `seedSourceDatabase`, `seedConfig`, and `seedCredentials` require `seedURI` or `seedURIs`
- `replaceExisting: true` requires `force: true` (destructive `DROP ... DESTROY DATA`)
- `replaceExisting: true` is mutually exclusive with `ifNotExists: true` and requires a seed source (`seedURI`, `seedURIs`, or `seedBackupRef`)
- Target cluster must be in "Ready" phase with `propertyShardingReady: true`
- `graphShard.primaries` should be >= 3 for high availability

## Error Conditions

### Common Validation Errors

| Error | Cause | Resolution |
|-------|-------|------------|
| `property sharding requires Neo4j 2025.12+` | Old Neo4j version | Upgrade to 2025.12+ |
| `spec.topology.servers in body should be greater than or equal to 2` | Invalid server count | Increase server count to 2+ (3+ recommended for HA) |
| `property sharding requires minimum 4GB memory` | Insufficient memory | Increase memory to 8GB+ (recommended) |
| `defaultCypherLanguage must be '25'` | Wrong Cypher version | Set to "25" |
| `referenced cluster does not have property sharding enabled` | Cluster not configured | Enable property sharding on cluster |
| `propertyShards must be at least 1` | Invalid shard count | Set to valid range (1-1000) |

### Runtime Errors

| Error | Cause | Resolution |
|-------|-------|------------|
| `failed to create sharded database` | Cluster capacity or configuration issues | Check cluster resources and options |
| `failed to prepare cloud credentials` | Missing or invalid seed credentials | Verify seed credentials secret and URI scheme |

## Examples

See [examples directory](https://github.com/priyolahiri/neo4j-kubernetes-operator/tree/main/examples/property_sharding) for complete configuration examples.

## Related APIs

- [Neo4jEnterpriseCluster API](neo4jenterprisecluster.md)
- [Neo4jBackup API](neo4jbackup.md)
- [Neo4jRestore API](neo4jrestore.md)
