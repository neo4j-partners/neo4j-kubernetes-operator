# Neo4jBackup

This document provides a reference for the `Neo4jBackup` Custom Resource Definition (CRD). This resource is used for creating and managing backups of Neo4j databases.

For a comprehensive guide on using backups, see the [Backup and Restore Guide](../user_guide/guides/backup_restore.md).

## Spec

The `Neo4jBackupSpec` defines the desired state of a Neo4j backup configuration.

| Field | Type | Description |
|---|---|---|
| `target` | [`BackupTarget`](#backuptarget) | **Required**. What to backup |
| `storage` | [`StorageLocation`](#storagelocation) | **Required**. Where to store the backup |
| `schedule` | `string` | Cron schedule for automated backups |
| `cloud` | [`*CloudBlock`](#cloudblock) | Cloud provider configuration |
| `retention` | [`*RetentionPolicy`](#retentionpolicy) | Backup retention policy |
| `options` | [`*BackupOptions`](#backupoptions) | Backup-specific options |
| `suspend` | `bool` | Suspend the backup schedule |

## Type Definitions

### BackupTarget

Defines what to backup.

| Field | Type | Description |
|---|---|---|
| `kind` | `string` | **Required**. Type of resource: `"Cluster"` or `"Database"` |
| `name` | `string` | **Required**. Name of the target resource |
| `namespace` | `string` | Namespace of the target resource (defaults to backup namespace) |

### StorageLocation

Defines where to store backups.

| Field | Type | Description |
|---|---|---|
| `type` | `string` | **Required**. Storage type: `"s3"`, `"gcs"`, `"azure"`, `"pvc"` |
| `bucket` | `string` | Bucket name (for cloud storage) |
| `path` | `string` | Path within bucket or PVC |
| `pvc` | [`*PVCSpec`](#pvcspec) | PVC configuration (for `pvc` type) |
| `cloud` | [`*CloudBlock`](#cloudblock) | Cloud provider configuration |

### CloudBlock

Cloud provider configuration.

| Field | Type | Description |
|---|---|---|
| `provider` | `string` | Cloud provider: `"aws"`, `"gcp"`, `"azure"` |
| `identity` | [`*CloudIdentity`](#cloudidentity) | Cloud identity configuration |

### CloudIdentity

Cloud identity configuration.

| Field | Type | Description |
|---|---|---|
| `provider` | `string` | **Required**. Identity provider: `"aws"`, `"gcp"`, `"azure"` |
| Additional fields depend on provider type |

### PVCSpec

PVC configuration for local storage.

| Field | Type | Description |
|---|---|---|
| `claimName` | `string` | Name of existing PVC to use |
| `size` | `string` | Size for new PVC (e.g., `"100Gi"`) |
| `storageClassName` | `string` | Storage class name |
| `accessModes` | `[]string` | Access modes |

### RetentionPolicy

Backup retention configuration.

| Field | Type | Description |
|---|---|---|
| `maxAge` | `string` | Maximum age of backups to keep (e.g., `"30d"`) |
| `maxCount` | `int32` | Maximum number of backups to keep |
| `deletePolicy` | `string` | What to do with expired backups: `"Delete"` (default) or `"Archive"` |

### BackupOptions

Backup-specific options.

| Field | Type | Description |
|---|---|---|
| `compress` | `bool` | Compress the backup (default: `true`) |
| `encryption` | [`*EncryptionSpec`](#encryptionspec) | Encryption configuration |
| `verify` | `bool` | Verify backup integrity after creation |
| `backupType` | `string` | Backup type: `"FULL"`, `"DIFF"`, `"AUTO"` (default) |
| `pageCache` | `string` | Page cache size (e.g., `"4G"`) - must match pattern `^[0-9]+[KMG]?$` |
| `additionalArgs` | `[]string` | Additional neo4j-admin backup arguments |

### EncryptionSpec

Backup encryption configuration.

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Enable encryption |
| `keySecret` | `string` | Secret containing encryption key |
| `algorithm` | `string` | Encryption algorithm: `"AES256"` (default) or `"ChaCha20"` |

## Status

The `Neo4jBackupStatus` represents the observed state of the backup.

| Field | Type | Description |
|---|---|---|
| `conditions` | `[]metav1.Condition` | Current backup conditions |
| `phase` | `string` | Current backup phase |
| `message` | `string` | Additional information about current state |
| `lastRunTime` | `*metav1.Time` | When the last backup was started |
| `lastSuccessTime` | `*metav1.Time` | When the last successful backup completed |
| `nextRunTime` | `*metav1.Time` | When the next backup is scheduled |
| `stats` | [`*BackupStats`](#backupstats) | Backup statistics |
| `history` | [`[]BackupRun`](#backuprun) | History of recent backup runs |

### BackupStats

Backup statistics.

| Field | Type | Description |
|---|---|---|
| `size` | `string` | Size of the backup |
| `duration` | `string` | Duration of the backup operation |
| `filesBackedUp` | `int64` | Number of files backed up |
| `bytesProcessed` | `int64` | Total bytes processed |

### BackupRun

Information about a backup run.

| Field | Type | Description |
|---|---|---|
| `startTime` | `metav1.Time` | When the backup started |
| `endTime` | `*metav1.Time` | When the backup ended |
| `phase` | `string` | Backup phase: `"Running"`, `"Succeeded"`, `"Failed"` |
| `message` | `string` | Additional information |
| `location` | `string` | Where the backup was stored |
| `size` | `string` | Size of the backup |

## Examples

### Scheduled S3 Backup

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: daily-cluster-backup
spec:
  target:
    kind: Cluster
    name: production-cluster
  storage:
    type: s3
    bucket: neo4j-backups
    path: daily/
    cloud:
      provider: aws
      identity:
        provider: aws
  schedule: "0 2 * * *"  # Daily at 2 AM
  retention:
    maxAge: "30d"
    maxCount: 30
  options:
    compress: true
    backupType: FULL
    encryption:
      enabled: true
      keySecret: backup-encryption-key
```

### On-Demand PVC Backup

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: manual-backup
spec:
  target:
    kind: Database
    name: mydb
    namespace: neo4j
  storage:
    type: pvc
    pvc:
      claimName: backup-storage
    path: backups/manual/
  options:
    compress: true
    verify: true
    backupType: DIFF
```

### GCS Backup with Retention

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: weekly-gcs-backup
spec:
  target:
    kind: Cluster
    name: analytics-cluster
  storage:
    type: gcs
    bucket: neo4j-analytics-backups
    path: weekly/
    cloud:
      provider: gcp
  schedule: "0 3 * * 0"  # Weekly on Sunday at 3 AM
  retention:
    maxCount: 12  # Keep 12 weeks
    deletePolicy: Archive
  options:
    backupType: AUTO
    pageCache: "8G"
```

For more information on backup operations, see the [Backup and Restore Guide](../user_guide/guides/backup_restore.md).
