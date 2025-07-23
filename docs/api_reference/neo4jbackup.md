# Neo4jBackup API Reference

This document provides a comprehensive reference for the `Neo4jBackup` Custom Resource Definition (CRD). This resource is used to define and manage automated and on-demand backups of your Neo4j Enterprise clusters.

For practical examples and usage guidance, see the [Backup and Restore Guide](../user_guide/guides/backup_restore.md).

## API Version

- **Group**: `neo4j.neo4j.com`
- **Version**: `v1alpha1`
- **Kind**: `Neo4jBackup`

## Neo4jBackup Spec

The `Neo4jBackup` spec defines the configuration for backup operations.

### Top-Level Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `target` | [`BackupTarget`](#backuptarget) | ✅ | Defines what to backup (cluster or specific database) |
| `storage` | [`StorageLocation`](#storagelocation) | ✅ | Storage backend configuration for backup destination |
| `schedule` | `string` | ❌ | Cron expression for scheduled backups (e.g., "0 2 * * *") |
| `cloud` | [`CloudBlock`](#cloudblock) | ❌ | Cloud provider configuration for cloud storage backends |
| `retention` | [`RetentionPolicy`](#retentionpolicy) | ❌ | Backup retention and cleanup policy |
| `options` | [`BackupOptions`](#backupoptions) | ❌ | Additional backup configuration options including backup type |
| `suspend` | `boolean` | ❌ | Suspend scheduled backups (default: false) |

### BackupTarget

Defines the scope of the backup operation.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `kind` | `string` | ✅ | Type of backup target. Valid values: "Cluster", "Database" |
| `name` | `string` | ✅ | Name of the target cluster or database |
| `namespace` | `string` | ❌ | Namespace of the target resource (defaults to backup resource namespace) |

**Examples:**
```yaml
# Backup entire cluster
target:
  kind: Cluster
  name: production-cluster

# Backup specific database
target:
  kind: Database
  name: user-data
```

### StorageLocation

Configures the storage backend for backup data.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | `string` | ✅ | Storage backend type. Valid values: "s3", "gcs", "azure", "pvc" |
| `bucket` | `string` | ❌ | Bucket/container name (required for cloud storage) |
| `path` | `string` | ❌ | Path within the storage location |
| `cloud` | [`CloudBlock`](#cloudblock) | ❌ | Cloud provider configuration |
| `pvc` | [`PVCSpec`](#pvcspec) | ❌ | PVC configuration (when type is "pvc") |

**Examples:**
```yaml
# S3 storage
storage:
  type: s3
  bucket: my-backup-bucket
  path: neo4j-backups/production
  cloud:
    provider: aws
    region: us-east-1

# GCS storage
storage:
  type: gcs
  bucket: my-gcs-bucket
  path: backups/neo4j
  cloud:
    provider: gcp
    region: us-central1

# Azure Blob Storage
storage:
  type: azure
  bucket: backup-container
  path: neo4j/production
  cloud:
    provider: azure
    region: eastus

# PVC storage
storage:
  type: pvc
  pvc:
    name: backup-storage
    size: 100Gi
    storageClass: fast-ssd
```

### CloudBlock

Cloud provider configuration for cloud storage backends.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | `string` | ✅ | Cloud provider. Valid values: "aws", "gcp", "azure" |
| `region` | `string` | ✅ | Cloud region for the storage bucket |
| `credentialsSecret` | `string` | ❌ | Secret containing cloud credentials |

### PVCSpec

PVC configuration for local storage.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | `string` | ❌ | Name of existing PVC (if not specified, operator may create one) |
| `size` | `string` | ❌ | Size of PVC to create (e.g., "100Gi") |
| `storageClass` | `string` | ❌ | Storage class for PVC creation |
| `accessModes` | `[]string` | ❌ | Access modes for PVC (default: ["ReadWriteOnce"]) |

### RetentionPolicy

Defines backup retention and cleanup policies.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `maxAge` | `string` | ❌ | Maximum age of backups to retain (e.g., "30d", "720h") |
| `maxCount` | `int32` | ❌ | Maximum number of backups to retain |
| `deletePolicy` | `string` | ❌ | Policy for expired backups. Valid values: "Delete", "Archive" (default: "Delete") |

**Examples:**
```yaml
# Keep last 7 daily backups
retention:
  maxCount: 7
  deletePolicy: Delete

# Keep backups for 30 days
retention:
  maxAge: "30d"
  deletePolicy: Delete

# Archive old backups instead of deleting
retention:
  maxAge: "90d"
  maxCount: 12
  deletePolicy: Archive
```

### BackupOptions

Additional backup configuration options for Neo4j 5.26+ and 2025.x.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `backupType` | `string` | ❌ | Type of backup. Valid values: "FULL", "DIFF", "AUTO" (default: "AUTO") |
| `compress` | `boolean` | ❌ | Enable backup compression (default: true) |
| `pageCache` | `string` | ❌ | Page cache size for backup operation (e.g., "2G") |
| `encryption` | [`EncryptionSpec`](#encryptionspec) | ❌ | Backup encryption configuration |
| `verifyBackup` | `boolean` | ❌ | Verify backup integrity after creation (default: false) |
| `additionalArgs` | `[]string` | ❌ | Additional arguments passed to neo4j-admin backup command |

**Backup Types:**
- **FULL**: Complete backup of all data
- **DIFF**: Incremental backup (requires previous backup)
- **AUTO**: Automatically choose between FULL and DIFF based on existing backups

### EncryptionSpec

Backup encryption configuration.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | `boolean` | ❌ | Enable backup encryption (default: false) |
| `keySecret` | `string` | ❌ | Secret containing encryption key |
| `algorithm` | `string` | ❌ | Encryption algorithm. Valid values: "AES256", "ChaCha20" (default: "AES256") |

**Example:**
```yaml
options:
  compress: true
  verify: true
  encryption:
    enabled: true
    keySecret: backup-encryption-key
    algorithm: AES256
  additionalArgs:
    - "--parallel-recovery"
    - "--verbose"
```

## Neo4jBackup Status

The `Neo4jBackup` status provides information about backup operations and their current state.

### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `conditions` | `[]metav1.Condition` | Current conditions of the backup resource |
| `phase` | `string` | Current phase of the backup operation |
| `message` | `string` | Human-readable message about the current state |
| `lastRunTime` | `*metav1.Time` | Time when the last backup was started |
| `lastSuccessTime` | `*metav1.Time` | Time when the last successful backup completed |
| `nextRunTime` | `*metav1.Time` | Time when the next backup is scheduled |
| `stats` | [`BackupStats`](#backupstats) | Statistics from the latest backup |
| `history` | `[]BackupRun` | History of recent backup runs |

### BackupStats

Statistics and metrics from backup operations.

| Field | Type | Description |
|-------|------|-------------|
| `size` | `string` | Size of the backup |
| `duration` | `string` | Duration of the backup operation |
| `throughput` | `string` | Backup throughput |
| `fileCount` | `int32` | Number of files in the backup |

### BackupRun

Information about individual backup executions.

| Field | Type | Description |
|-------|------|-------------|
| `startTime` | `metav1.Time` | Start time of the backup run |
| `completionTime` | `*metav1.Time` | Completion time of the backup run |
| `status` | `string` | Status of the backup run |
| `error` | `string` | Error message if the backup failed |
| `stats` | `*BackupStats` | Statistics for this backup run |

### Backup Phases

| Phase | Description |
|-------|-------------|
| `Pending` | Backup is queued but not yet started |
| `Running` | Backup operation is in progress |
| `Completed` | Backup completed successfully |
| `Failed` | Backup operation failed |
| `Scheduled` | Backup is scheduled (for cron-based backups) |
| `Suspended` | Scheduled backup is suspended |

### Condition Types

| Type | Description |
|------|-------------|
| `Ready` | Indicates whether the backup resource is ready for operation |
| `JobCreated` | Indicates whether the backup job was created successfully |
| `StorageReady` | Indicates whether the storage backend is accessible |

## Examples

### Complete Example: Production Backup Configuration

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: production-backup
  namespace: neo4j
  labels:
    environment: production
    backup-type: daily
spec:
  target:
    kind: Cluster
    name: production-cluster
  schedule: "0 2 * * *"  # Daily at 2 AM UTC
  storage:
    type: s3
    bucket: neo4j-production-backups
    path: daily-backups
    cloud:
      provider: aws
      region: us-east-1
  retention:
    maxAge: "30d"
    maxCount: 30
    deletePolicy: Delete
  options:
    compress: true
    verify: true
    encryption:
      enabled: true
      keySecret: backup-encryption-key
      algorithm: AES256
    additionalArgs:
      - "--temp-path=/tmp/backup"
      - "--parallel-recovery"
```

### Monitoring Example

```bash
# Check backup status
kubectl get neo4jbackup production-backup -o wide

# View detailed status
kubectl describe neo4jbackup production-backup

# Check backup history
kubectl get neo4jbackup production-backup -o jsonpath='{.status.history[*]}'

# Monitor backup job logs
kubectl logs job/production-backup-backup
```

## Version-Specific Features

### Neo4j 5.26.x
- Uses `neo4j-admin database backup` command syntax
- Supports `--include-metadata=all` for cluster backups
- Automatic backup from secondary servers when available
- Correct parameters: `--type`, `--compress`, `--pagecache`

### Neo4j 2025.x
- Same backup command structure as 5.26.x
- Enhanced metadata options
- Future support for `--source-database` parameter

### Automatic Secondary Backup
When the target cluster has secondary servers, the operator automatically configures backups to run from a secondary server using the `--from` parameter. This reduces load on primary servers during backup operations.

## RBAC and Permissions

The Neo4j Operator automatically manages all RBAC resources required for backup operations:

### Automatic RBAC Creation

When you create a `Neo4jBackup` resource, the operator automatically:

1. **Creates a ServiceAccount** for the backup job
2. **Creates a Role** with the following permissions:
   - `pods/exec` - Required to execute backup commands in the Neo4j pods
   - `pods/log` - Required to read logs from backup operations
3. **Creates a RoleBinding** to grant the ServiceAccount the necessary permissions

**Important**: No manual RBAC configuration is required. The operator handles all permission management automatically.

### Required Operator Permissions

The operator itself requires the following cluster-level permissions to manage backup RBAC:
- Create, update, and delete ServiceAccounts
- Create, update, and delete Roles and RoleBindings
- Grant `pods/exec` and `pods/log` permissions to backup jobs

These permissions are included in the operator's ClusterRole when installed from the official manifests.

## Version Requirements

- **Neo4j Version**: 5.26.0+ (semver) or 2025.01.0+ (calver)
- **Kubernetes**: 1.19+
- **Neo4j Operator**: Latest version with automatic RBAC support

## Related Resources

- [`Neo4jRestore`](neo4jrestore.md) - Restore operations
- [`Neo4jEnterpriseCluster`](neo4jenterprisecluster.md) - Target cluster resource
- [Backup and Restore Guide](../user_guide/guides/backup_restore.md) - Usage examples and best practices
