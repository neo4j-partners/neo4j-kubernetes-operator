# Neo4jRestore API Reference

This document provides a comprehensive reference for the `Neo4jRestore` Custom Resource Definition (CRD). This resource is used to restore Neo4j Enterprise clusters from backups, including support for point-in-time recovery (PITR) and advanced restore operations.

For practical examples and usage guidance, see the [Backup and Restore Guide](../user_guide/guides/backup_restore.md).

## API Version

- **Group**: `neo4j.neo4j.com`
- **Version**: `v1alpha1`
- **Kind**: `Neo4jRestore`

## Neo4jRestore Spec

The `Neo4jRestore` spec defines the configuration for restore operations.

### Top-Level Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `targetCluster` | `string` | ✅ | Name of the cluster to restore to |
| `source` | [`RestoreSource`](#restoresource) | ✅ | Source of the backup to restore |
| `databaseName` | `string` | ✅ | Name of the database to restore |
| `options` | [`RestoreOptionsSpec`](#restoreoptionsspec) | ❌ | Additional restore configuration options |
| `force` | `boolean` | ❌ | Force restore with --overwrite-destination (default: false) |
| `stopCluster` | `boolean` | ❌ | Stop cluster before restore (default: false) |
| `timeout` | `string` | ❌ | Timeout for restore operation (e.g., "2h", "30m") |

### RestoreSource

Defines the source of the backup to restore from.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | `string` | ✅ | Type of restore source. Valid values: "backup", "storage", "pitr" |
| `backupRef` | `string` | ❌ | Reference to Neo4jBackup resource (when type="backup") |
| `storage` | [`StorageLocation`](#storagelocation) | ❌ | Direct storage location (when type="storage") |
| `backupPath` | `string` | ❌ | Specific backup path within storage |
| `pointInTime` | `string` | ❌ | Point in time for restore using --restore-until (RFC3339 format) |
| `pitr` | [`PITRConfig`](#pitrconfig) | ❌ | Point-in-time recovery configuration (when type="pitr") |

**Examples:**
```yaml
# Restore from backup reference
source:
  type: backup
  backupRef: daily-backup

# Restore from storage location
source:
  type: storage
  storage:
    type: s3
    bucket: backup-bucket
    path: backups/cluster
  backupPath: /backup/cluster/backup-20250104-120000

# Point-in-time recovery
source:
  type: pitr
  pointInTime: "2025-01-04T12:30:00Z"
  pitr:
    baseBackup:
      type: backup
      backupRef: base-backup
    logStorage:
      type: s3
      bucket: transaction-logs
      path: production/logs
```

### PITRConfig

Point-in-time recovery configuration for advanced restore scenarios.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `logStorage` | [`StorageLocation`](#storagelocation) | ❌ | Transaction log storage location |
| `logRetention` | `string` | ❌ | Transaction log retention period (default: "7d") |
| `recoveryPointObjective` | `string` | ❌ | Recovery point objective (default: "1m") |
| `baseBackup` | [`BaseBackupSource`](#basebackupsource) | ❌ | Base backup to restore from before applying logs |
| `validateLogIntegrity` | `boolean` | ❌ | Validate transaction log integrity (default: true) |
| `compression` | [`CompressionConfig`](#compressionconfig) | ❌ | Compression settings for transaction logs |
| `encryption` | [`EncryptionConfig`](#encryptionconfig) | ❌ | Encryption settings for transaction logs |

### BaseBackupSource

Base backup configuration for PITR (avoids circular references with PITR).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | `string` | ✅ | Type of backup source. Valid values: "backup", "storage" |
| `backupRef` | `string` | ❌ | Reference to Neo4jBackup resource (when type="backup") |
| `storage` | [`StorageLocation`](#storagelocation) | ❌ | Direct storage location (when type="storage") |
| `backupPath` | `string` | ❌ | Specific backup path within storage |

### CompressionConfig

Compression settings for transaction logs in PITR.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | `boolean` | ❌ | Enable compression (default: true) |
| `algorithm` | `string` | ❌ | Compression algorithm. Valid values: "gzip", "lz4", "zstd" (default: "gzip") |
| `level` | `int32` | ❌ | Compression level (algorithm-specific) |

### EncryptionConfig

Encryption settings for transaction logs in PITR.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | `boolean` | ❌ | Enable encryption (default: false) |
| `algorithm` | `string` | ❌ | Encryption algorithm. Valid values: "AES256", "ChaCha20Poly1305" (default: "AES256") |
| `keySecret` | `string` | ❌ | Secret containing encryption key |
| `keySecretKey` | `string` | ❌ | Key within the secret (default: "key") |

### RestoreOptionsSpec

Additional restore configuration options.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `replaceExisting` | `boolean` | ❌ | Replace existing database (default: false) |
| `verifyBackup` | `boolean` | ❌ | Verify backup before restore (default: false) |
| `additionalArgs` | `[]string` | ❌ | Additional arguments passed to neo4j-admin restore command |
| `preRestore` | [`RestoreHooks`](#restorehooks) | ❌ | Pre-restore hooks |
| `postRestore` | [`RestoreHooks`](#restorehooks) | ❌ | Post-restore hooks |

### RestoreHooks

Hooks to run before or after restore operations.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `job` | [`RestoreHookJob`](#restorehookjob) | ❌ | Kubernetes job to run as hook |
| `cypherStatements` | `[]string` | ❌ | Cypher statements to execute |

### RestoreHookJob

Kubernetes job configuration for restore hooks.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `template` | [`JobTemplateSpec`](#jobtemplatespec) | ✅ | Job template specification |
| `timeout` | `string` | ❌ | Timeout for the hook job |

### JobTemplateSpec

Job template for hook execution.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `container` | [`ContainerSpec`](#containerspec) | ✅ | Container specification |
| `backoffLimit` | `*int32` | ❌ | Job backoff limit |

### ContainerSpec

Container specification for hook jobs.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `image` | `string` | ✅ | Container image |
| `command` | `[]string` | ❌ | Command to execute |
| `args` | `[]string` | ❌ | Arguments to pass to command |
| `env` | `[]EnvVar` | ❌ | Environment variables |

### StorageLocation

Storage backend configuration (shared with Neo4jBackup).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | `string` | ✅ | Storage backend type. Valid values: "s3", "gcs", "azure", "pvc" |
| `bucket` | `string` | ❌ | Bucket/container name (required for cloud storage) |
| `path` | `string` | ❌ | Path within the storage location |
| `cloud` | [`CloudBlock`](#cloudblock) | ❌ | Cloud provider configuration |
| `pvc` | [`PVCSpec`](#pvcspec) | ❌ | PVC configuration (when type is "pvc") |

## Neo4jRestore Status

The `Neo4jRestore` status provides information about restore operations and their current state.

### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `conditions` | `[]metav1.Condition` | Current conditions of the restore resource |
| `phase` | `string` | Current phase of the restore operation |
| `message` | `string` | Human-readable message about the current state |
| `startTime` | `*metav1.Time` | Start time of the restore operation |
| `completionTime` | `*metav1.Time` | Completion time of the restore operation |
| `stats` | [`RestoreStats`](#restorestats) | Restore operation statistics |
| `backupInfo` | [`RestoreBackupInfo`](#restorebackupinfo) | Information about the backup that was restored |
| `observedGeneration` | `int64` | Generation of the most recently observed Neo4jRestore |

### RestoreStats

Statistics and metrics from restore operations.

| Field | Type | Description |
|-------|------|-------------|
| `duration` | `string` | Duration of the restore operation |
| `dataSize` | `string` | Size of data restored |
| `throughput` | `string` | Throughput of the restore operation |
| `fileCount` | `int32` | Number of files restored |
| `errorCount` | `int32` | Errors encountered during restore |

### RestoreBackupInfo

Information about the backup that was restored.

| Field | Type | Description |
|-------|------|-------------|
| `backupPath` | `string` | Source backup path |
| `backupCreatedAt` | `*metav1.Time` | Original creation time of the backup |
| `originalDatabase` | `string` | Original database name in the backup |
| `neo4jVersion` | `string` | Neo4j version of the backup |
| `backupSize` | `string` | Backup size |

### Restore Phases

| Phase | Description |
|-------|-------------|
| `Pending` | Restore is queued but not yet started |
| `Running` | Restore operation is in progress |
| `Completed` | Restore completed successfully |
| `Failed` | Restore operation failed |

### Condition Types

| Type | Description |
|------|-------------|
| `Ready` | Indicates whether the restore resource is ready for operation |
| `JobCreated` | Indicates whether the restore job was created successfully |
| `ClusterStopped` | Indicates whether the target cluster was stopped for restore |
| `BackupVerified` | Indicates whether the backup was verified before restore |

## Examples

### Complete Example: PITR Restore Configuration

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: pitr-restore-production
  namespace: neo4j
  labels:
    environment: production
    restore-type: pitr
spec:
  targetCluster: recovery-cluster
  databaseName: production-db
  source:
    type: pitr
    pointInTime: "2025-01-04T12:30:00Z"
    pitr:
      baseBackup:
        type: backup
        backupRef: daily-backup
      logStorage:
        type: s3
        bucket: transaction-logs
        path: production/logs
        cloud:
          provider: aws
          region: us-east-1
      logRetention: "7d"
      recoveryPointObjective: "5m"
      validateLogIntegrity: true
      compression:
        enabled: true
        algorithm: gzip
        level: 6
      encryption:
        enabled: true
        keySecret: log-encryption-key
        algorithm: AES256
  options:
    verifyBackup: true
    replaceExisting: true
    preRestore:
      cypherStatements:
        - "CALL dbms.backup.prepare()"
        - "CALL db.checkpoint()"
    postRestore:
      cypherStatements:
        - "CALL db.awaitIndexes()"
        - "CALL dbms.security.clearAuthCache()"
      job:
        template:
          container:
            image: my-registry/data-validator:latest
            command: ["/bin/sh"]
            args: ["-c", "/scripts/validate-restore.sh"]
            env:
              - name: NEO4J_URI
                value: "neo4j://recovery-cluster:7687"
              - name: DATABASE_NAME
                value: production-db
        timeout: "15m"
  force: true
  stopCluster: true
  timeout: "2h"
```

### Simple Restore Example

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: simple-restore
spec:
  targetCluster: my-cluster
  databaseName: neo4j
  source:
    type: backup
    backupRef: daily-backup
  options:
    verifyBackup: true
    replaceExisting: true
  force: false
  stopCluster: true
```

### Restore from Storage Example

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: storage-restore
spec:
  targetCluster: disaster-recovery
  databaseName: critical-app
  source:
    type: storage
    storage:
      type: gcs
      bucket: backup-storage
      path: production/backups
      cloud:
        provider: gcp
        region: us-central1
    backupPath: /backup/production/backup-20250104-120000
  options:
    verifyBackup: true
    replaceExisting: true
  force: true
  stopCluster: true
  timeout: "1h"
```

### Monitoring Example

```bash
# Check restore status
kubectl get neo4jrestore pitr-restore-production -o wide

# View detailed status
kubectl describe neo4jrestore pitr-restore-production

# Check restore progress
kubectl get neo4jrestore pitr-restore-production -w

# Monitor restore job logs
kubectl logs job/pitr-restore-production-restore

# Check restore statistics
kubectl get neo4jrestore pitr-restore-production -o jsonpath='{.status.stats}'
```

## Version-Specific Features

### Neo4j 5.26.x
- Uses `neo4j-admin database restore` command syntax
- **Correct parameters**:
  - `--from-path` (not `--from`)
  - `--overwrite-destination` (not `--force`)
  - `--restore-until` for PITR
- Automatic database state management (stop/start)

### Neo4j 2025.x
- Same restore command structure as 5.26.x
- Enhanced metadata restoration options
- Additional validation features

### Point-in-Time Recovery (PITR)
The operator supports PITR using the `--restore-until` parameter. Specify the target timestamp in RFC3339 format:
```yaml
source:
  type: backup
  backupRef: full-backup
  pointInTime: "2025-01-20T14:30:00Z"
```

## Version Requirements

- **Neo4j Version**: 5.26.0+ (semver) or 2025.01.0+ (calver)
- **Kubernetes**: 1.19+
- **Neo4j Operator**: Latest version with restore support

## Related Resources

- [`Neo4jBackup`](neo4jbackup.md) - Backup operations
- [`Neo4jEnterpriseCluster`](neo4jenterprisecluster.md) - Target cluster resource
- [Backup and Restore Guide](../user_guide/guides/backup_restore.md) - Usage examples and best practices
