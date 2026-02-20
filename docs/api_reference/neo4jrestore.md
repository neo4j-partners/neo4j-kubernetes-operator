# Neo4jRestore API Reference

This document provides a comprehensive reference for the `Neo4jRestore` Custom Resource Definition (CRD). This resource is used to restore Neo4j Enterprise databases from backups, including support for point-in-time recovery (PITR) and both cluster and standalone deployments.

For practical examples and usage guidance, see the [Backup and Restore Guide](../user_guide/guides/backup_restore.md).

## API Version

- **Group**: `neo4j.neo4j.com`
- **Version**: `v1alpha1`
- **Kind**: `Neo4jRestore`

## How it works

When a `Neo4jRestore` resource is created, the operator:

1. Resolves the `targetCluster` — it accepts both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone`. The controller detects the type automatically.
2. Creates a restore Kubernetes Job that runs `neo4j-admin database restore` inside a container using the **same Neo4j enterprise image** as the target.
3. If `stopCluster: true`, the operator scales down the cluster StatefulSet before starting the restore Job. When this flag is set, the operator mounts the actual server data PVC (`data-{cluster}-server-0`) directly into the restore Job container for offline restore access.
4. After the restore Job succeeds, the operator automatically issues one of the following Cypher commands via the Bolt client:
   - `CREATE DATABASE <dbname>` — when the database did not exist before
   - `START DATABASE <dbname>` — when the database existed but was stopped for the restore
5. Status fields and conditions are updated throughout the lifecycle.

## Neo4jRestore Spec

### Top-Level Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `targetCluster` | `string` | ✅ | Name of the `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` to restore into. The controller detects the type automatically. |
| `source` | [`RestoreSource`](#restoresource) | ✅ | Source of the backup data to restore |
| `databaseName` | `string` | ✅ | Name of the Neo4j database to restore |
| `options` | [`RestoreOptionsSpec`](#restoreoptionsspec) | ❌ | Additional restore configuration options |
| `force` | `bool` | ❌ | Pass `--overwrite-destination` to allow restoring over an existing database (default: `false`) |
| `stopCluster` | `bool` | ❌ | Scale down the target cluster before restore for an offline/cold restore (default: `false`). When `true`, mounts `data-{cluster}-server-0` PVC into the restore Job. |
| `timeout` | `string` | ❌ | Timeout for the restore Job (e.g., `"2h"`, `"30m"`) |

**Target compatibility**: `targetCluster` can reference either:
- `Neo4jEnterpriseCluster` — for HA cluster restore operations
- `Neo4jEnterpriseStandalone` — for single-node restore operations

### RestoreSource

Defines the source of the backup to restore from.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | `string` | ✅ | Type of restore source. Valid values: `"backup"`, `"storage"`, `"pitr"`, `"s3"`, `"gcs"`, `"azure"` |
| `backupRef` | `string` | ❌ | Name of a `Neo4jBackup` resource to restore from (used when `type="backup"`) |
| `storage` | [`StorageLocation`](#storagelocation) | ❌ | Direct storage location (used when `type="storage"`, `"s3"`, `"gcs"`, or `"azure"`) |
| `backupPath` | `string` | ❌ | Specific backup path within the storage location |
| `pointInTime` | `*metav1.Time` | ❌ | Recovery point in RFC3339 format; maps to `--restore-until` |
| `pitr` | [`PITRConfig`](#pitrconfig) | ❌ | Full PITR configuration (used when `type="pitr"`) |

**Valid `type` values:**

| Value | Description |
|-------|-------------|
| `"backup"` | Restore from a `Neo4jBackup` CR referenced by `backupRef` |
| `"storage"` | Restore from an explicit storage location in `storage` |
| `"pitr"` | Point-in-time recovery using transaction log replay |
| `"s3"` | Shorthand for `storage` with AWS S3 backend |
| `"gcs"` | Shorthand for `storage` with Google Cloud Storage backend |
| `"azure"` | Shorthand for `storage` with Azure Blob Storage backend |

**Examples:**

```yaml
# Restore from a Neo4jBackup resource
source:
  type: backup
  backupRef: daily-production-backup

# Restore from an explicit S3 path
source:
  type: storage
  storage:
    type: s3
    bucket: neo4j-backups
    path: production/
    cloud:
      provider: aws
      credentialsSecretRef: aws-restore-credentials
  backupPath: /backups/production/backup-20250120-020000

# Shorthand S3 restore
source:
  type: s3
  storage:
    type: s3
    bucket: neo4j-backups
    path: production/
    cloud:
      provider: aws
      credentialsSecretRef: aws-restore-credentials
  backupPath: /backups/production/backup-20250120-020000

# Point-in-time recovery
source:
  type: pitr
  pointInTime: "2025-01-04T12:30:00Z"
  pitr:
    baseBackup:
      type: backup
      backupRef: daily-backup
    logStorage:
      type: s3
      bucket: neo4j-transaction-logs
      path: production/logs/
      cloud:
        provider: aws
    logRetention: "7d"
    validateLogIntegrity: true
```

### PITRConfig

Point-in-time recovery configuration for advanced restore scenarios.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `baseBackup` | [`BaseBackupSource`](#basebackupsource) | ❌ | Base backup to restore before applying transaction logs |
| `logStorage` | [`StorageLocation`](#storagelocation) | ❌ | Storage location for transaction logs |
| `logRetention` | `string` | ❌ | Transaction log retention period (default: `"7d"`) |
| `recoveryPointObjective` | `string` | ❌ | Recovery point objective (default: `"1m"`) |
| `validateLogIntegrity` | `bool` | ❌ | Validate transaction log integrity before restore (default: `true`) |
| `compression` | [`CompressionConfig`](#compressionconfig) | ❌ | Compression settings for transaction logs |
| `encryption` | [`EncryptionConfig`](#encryptionconfig) | ❌ | Encryption settings for transaction logs |

### BaseBackupSource

Base backup configuration for PITR (avoids circular references).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | `string` | ✅ | Source type: `"backup"` or `"storage"` |
| `backupRef` | `string` | ❌ | Name of the `Neo4jBackup` resource (when `type="backup"`) |
| `storage` | [`StorageLocation`](#storagelocation) | ❌ | Direct storage location (when `type="storage"`) |
| `backupPath` | `string` | ❌ | Specific backup path within the storage location |

### CompressionConfig

Compression settings for transaction logs in PITR.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | `bool` | ❌ | Enable compression (default: `true`) |
| `algorithm` | `string` | ❌ | Compression algorithm: `"gzip"` (default), `"lz4"`, `"zstd"` |
| `level` | `int32` | ❌ | Compression level (algorithm-specific) |

### EncryptionConfig

Encryption settings for transaction logs in PITR.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | `bool` | ❌ | Enable encryption (default: `false`) |
| `algorithm` | `string` | ❌ | Encryption algorithm: `"AES256"` (default), `"ChaCha20Poly1305"` |
| `keySecret` | `string` | ❌ | Name of the Kubernetes Secret containing the encryption key |
| `keySecretKey` | `string` | ❌ | Key within the secret that holds the value (default: `"key"`) |

### RestoreOptionsSpec

Additional restore execution options.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `replaceExisting` | `bool` | ❌ | Replace an existing database (default: `false`) |
| `verifyBackup` | `bool` | ❌ | Verify the backup before attempting restore (default: `false`) |
| `additionalArgs` | `[]string` | ❌ | Additional arguments passed verbatim to `neo4j-admin database restore` |
| `preRestore` | [`RestoreHooks`](#restorehooks) | ❌ | Hooks executed before the restore Job starts |
| `postRestore` | [`RestoreHooks`](#restorehooks) | ❌ | Hooks executed after the restore Job completes successfully |

### RestoreHooks

Hooks to run before or after the restore Job.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `job` | [`RestoreHookJob`](#restorehookjob) | ❌ | Kubernetes Job to run as a hook |
| `cypherStatements` | `[]string` | ❌ | Cypher statements to execute against the target Neo4j instance |

### RestoreHookJob

Kubernetes Job configuration for restore hooks.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `template` | [`JobTemplateSpec`](#jobtemplatespec) | ✅ | Job template specification |
| `timeout` | `string` | ❌ | Timeout for the hook Job (e.g., `"30m"`) |

### JobTemplateSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `container` | [`ContainerSpec`](#containerspec) | ✅ | Container specification |
| `backoffLimit` | `*int32` | ❌ | Job backoff limit |

### ContainerSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `image` | `string` | ✅ | Container image |
| `command` | `[]string` | ❌ | Entrypoint command |
| `args` | `[]string` | ❌ | Arguments to pass to the command |
| `env` | `[]EnvVar` | ❌ | Environment variables |

### StorageLocation

Storage backend configuration (shared with `Neo4jBackup`).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | `string` | ✅ | Storage backend: `"s3"`, `"gcs"`, `"azure"`, `"pvc"` |
| `bucket` | `string` | ❌ | Bucket or container name (required for cloud types) |
| `path` | `string` | ❌ | Path within the storage location |
| `cloud` | [`CloudBlock`](#cloudblock) | ❌ | Cloud provider configuration including optional credentials secret |
| `pvc` | [`PVCSpec`](#pvcspec) | ❌ | PVC configuration (when `type="pvc"`) |

### PVCSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | `string` | ❌ | Name of an existing PVC to use |
| `storageClassName` | `string` | ❌ | Storage class name |
| `size` | `string` | ❌ | Size for a new PVC (e.g., `"100Gi"`) |

### CloudBlock

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | `string` | ❌ | Cloud provider: `"aws"`, `"gcp"`, `"azure"` |
| `credentialsSecretRef` | `string` | ❌ | Name of a Kubernetes Secret containing cloud credentials as environment variables. When absent, ambient workload identity is used. |
| `identity` | [`*CloudIdentity`](#cloudidentity) | ❌ | Cloud identity configuration |

### CloudIdentity

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | `string` | ✅ | Identity provider: `"aws"`, `"gcp"`, `"azure"` |
| `serviceAccount` | `string` | ❌ | Existing ServiceAccount to use |
| `autoCreate` | [`*AutoCreateSpec`](#autocreatespec) | ❌ | Auto-create ServiceAccount with workload-identity annotations |

### AutoCreateSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | `bool` | ❌ | Enable auto-creation of the ServiceAccount (default: `true`) |
| `annotations` | `map[string]string` | ❌ | Annotations applied to the auto-created ServiceAccount |

## Neo4jRestore Status

### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `conditions` | `[]metav1.Condition` | Current conditions of the restore resource |
| `phase` | `string` | Current phase of the restore operation |
| `message` | `string` | Human-readable message about the current state |
| `startTime` | `*metav1.Time` | When the restore operation started |
| `completionTime` | `*metav1.Time` | When the restore operation completed |
| `stats` | [`RestoreStats`](#restorestats) | Restore operation statistics |
| `backupInfo` | [`RestoreBackupInfo`](#restorebackupinfo) | Information about the backup that was restored |
| `observedGeneration` | `int64` | Generation of the most recently observed `Neo4jRestore` spec |

### RestoreStats

| Field | Type | Description |
|-------|------|-------------|
| `duration` | `string` | Duration of the restore operation |
| `dataSize` | `string` | Amount of data restored |
| `throughput` | `string` | Restore throughput |
| `fileCount` | `int32` | Number of files restored |
| `errorCount` | `int32` | Errors encountered during restore |

### RestoreBackupInfo

| Field | Type | Description |
|-------|------|-------------|
| `backupPath` | `string` | Source backup path |
| `backupCreatedAt` | `*metav1.Time` | Original creation time of the backup |
| `originalDatabase` | `string` | Original database name in the backup |
| `neo4jVersion` | `string` | Neo4j version of the backup |
| `backupSize` | `string` | Size of the backup |

### Restore Phases

| Phase | Description |
|-------|-------------|
| `Pending` | Restore is queued but not yet started |
| `Running` | Restore Job is in progress |
| `Completed` | Restore completed successfully; database has been created or started |
| `Failed` | Restore Job or post-restore database bring-up failed |

### Condition Types

| Type | Description |
|------|-------------|
| `Ready` | Whether the restore resource is in a terminal successful state |
| `JobCreated` | Whether the restore Job was created successfully |
| `ClusterStopped` | Whether the target cluster was scaled down for offline restore |
| `BackupVerified` | Whether the backup was verified before restore |

## Post-Restore Database Bring-Up

After the restore Job completes successfully, the operator automatically issues a Cypher command to make the database available:

- **New database** (did not exist before): `CREATE DATABASE <dbname>`
- **Existing database** (was stopped for restore): `START DATABASE <dbname>`

This means the restore workflow is fully automated — you do not need to manually start the database after restore completes. The `status.phase` transitions to `Completed` only after the database bring-up command succeeds.

## `stopCluster` and Offline Restore

When `spec.stopCluster: true`:

1. The operator scales the target StatefulSet down to 0 replicas.
2. The restore Job is created with the actual server data PVC (`data-{cluster}-server-0`) mounted into the container, enabling direct offline file-level restore.
3. After the restore Job succeeds, the StatefulSet is scaled back up.
4. The operator then issues `CREATE DATABASE` or `START DATABASE` as described above.

Use `stopCluster: true` when:
- The database is too large for an online restore
- You need to restore at the storage level rather than via `neo4j-admin`
- The cluster is in an inconsistent state that prevents online operations

## Examples

### Simple Restore from a Neo4jBackup Reference

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: simple-backup-restore
  namespace: neo4j
spec:
  targetCluster: test-cluster   # Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone
  databaseName: testdb
  source:
    type: backup
    backupRef: daily-test-backup   # References a Neo4jBackup resource
  options:
    verifyBackup: true
    replaceExisting: true
  force: false
  stopCluster: false
  timeout: "1h"
```

### Restore from S3 (Static Credentials)

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: dev-s3-restore
  namespace: development
spec:
  targetCluster: dev-standalone   # Neo4jEnterpriseStandalone
  databaseName: dev-app
  source:
    type: storage
    storage:
      type: s3
      bucket: dev-neo4j-backups
      path: snapshots/
      cloud:
        provider: aws
        credentialsSecretRef: aws-restore-credentials
    backupPath: /backups/dev-app/backup-20250120-103000
  options:
    verifyBackup: false
    replaceExisting: true
    postRestore:
      cypherStatements:
        - "CALL db.awaitIndexes(60)"
        - "CREATE (:TestNode {restored: datetime()})"
  force: true
  stopCluster: false
  timeout: "30m"
```

### Restore from GCS (Static Credentials)

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: gcs-restore
  namespace: neo4j
spec:
  targetCluster: analytics-cluster
  databaseName: analytics-db
  source:
    type: gcs
    storage:
      type: gcs
      bucket: neo4j-analytics-backups
      path: weekly/
      cloud:
        provider: gcp
        credentialsSecretRef: gcs-restore-credentials
    backupPath: /backups/analytics-db/backup-20250120-030000
  options:
    verifyBackup: true
    replaceExisting: true
  force: true
  stopCluster: true
  timeout: "2h"
```

### Restore from Azure Blob Storage

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: enterprise-azure-restore
  namespace: neo4j
  labels:
    compliance: required
    environment: production
spec:
  targetCluster: enterprise-cluster
  databaseName: customer-data
  source:
    type: azure
    storage:
      type: azure
      bucket: enterprise-backups   # Azure container name
      path: production/
      cloud:
        provider: azure
        credentialsSecretRef: azure-restore-credentials
    backupPath: /backups/customer-data/backup-20250120-020000.backup
  options:
    verifyBackup: true
    replaceExisting: true
    preRestore:
      cypherStatements:
        - "CALL db.checkpoint()"
    postRestore:
      cypherStatements:
        - "CALL db.awaitIndexes()"
        - "CALL dbms.security.clearAuthCache()"
      job:
        template:
          container:
            image: enterprise-compliance-checker:latest
            command: ["/scripts/compliance-check.sh"]
            env:
              - name: NEO4J_URI
                value: "neo4j://enterprise-cluster-client:7687"
              - name: COMPLIANCE_LEVEL
                value: "GDPR,HIPAA"
        timeout: "60m"
    additionalArgs:
      - "--check-consistency"
      - "--verbose"
  force: true
  stopCluster: true
  timeout: "6h"
```

### Point-in-Time Recovery (PITR)

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: production-pitr-restore
  namespace: neo4j
spec:
  targetCluster: recovery-cluster
  databaseName: production-db
  source:
    type: pitr
    pointInTime: "2025-01-04T12:30:00Z"
    pitr:
      baseBackup:
        type: backup
        backupRef: daily-production-backup
      logStorage:
        type: s3
        bucket: neo4j-transaction-logs
        path: production/logs/
        cloud:
          provider: aws
          credentialsSecretRef: aws-restore-credentials
      logRetention: "7d"
      recoveryPointObjective: "5m"
      validateLogIntegrity: true
      compression:
        enabled: true
        algorithm: lz4
        level: 3
      encryption:
        enabled: true
        keySecret: transaction-log-encryption
        algorithm: AES256
  options:
    verifyBackup: true
    replaceExisting: true
    preRestore:
      cypherStatements:
        - "CALL db.checkpoint()"
    postRestore:
      cypherStatements:
        - "CALL db.awaitIndexes(600)"
        - "CALL dbms.security.clearAuthCache()"
  force: true
  stopCluster: true
  timeout: "4h"
```

### Offline Restore via stopCluster

When `stopCluster: true`, the operator mounts the server data PVC (`data-{cluster}-server-0`) directly into the restore Job for a cold/offline restore.

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: offline-restore
  namespace: neo4j
spec:
  targetCluster: production-cluster
  databaseName: large-graph
  source:
    type: storage
    storage:
      type: s3
      bucket: neo4j-backups
      path: large-graph/
      cloud:
        provider: aws
        credentialsSecretRef: aws-restore-credentials
    backupPath: /backups/large-graph/backup-20250120-020000
  force: true
  stopCluster: true   # Scales cluster to 0; mounts data-production-cluster-server-0 PVC
  timeout: "8h"
```

### Standalone Restore

`targetCluster` can reference a `Neo4jEnterpriseStandalone` resource. The controller detects the type automatically.

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: standalone-restore
  namespace: development
spec:
  targetCluster: dev-standalone   # Neo4jEnterpriseStandalone
  databaseName: app-db
  source:
    type: backup
    backupRef: dev-daily-backup
  options:
    replaceExisting: true
    verifyBackup: false
  force: true
  stopCluster: false   # Standalone does not require scaling down
  timeout: "30m"
```

### Cross-Cloud Disaster Recovery

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: cross-cloud-dr-restore
  namespace: disaster-recovery
spec:
  targetCluster: dr-cluster
  databaseName: critical-app
  source:
    type: gcs
    storage:
      type: gcs
      bucket: primary-site-backups
      path: disaster-recovery/
      cloud:
        provider: gcp
        credentialsSecretRef: gcs-dr-credentials
    backupPath: /backups/critical-app/latest.backup
  options:
    verifyBackup: true
    replaceExisting: true
    preRestore:
      cypherStatements:
        - "CALL db.checkpoint()"
    postRestore:
      cypherStatements:
        - "CALL db.awaitIndexes()"
        - "CALL dbms.security.clearAuthCache()"
      job:
        template:
          container:
            image: dr-validation-tool:latest
            command: ["/scripts/dr-validation.sh"]
            env:
              - name: NEO4J_URI
                value: "neo4j://dr-cluster-client:7687"
        timeout: "45m"
  force: true
  stopCluster: true
  timeout: "3h"
```

## Monitoring

```bash
# List all restore resources
kubectl get neo4jrestore -n neo4j

# Watch restore status
kubectl get neo4jrestore production-pitr-restore -w

# View detailed status and events
kubectl describe neo4jrestore production-pitr-restore

# Check restore phase
kubectl get neo4jrestore production-pitr-restore -o jsonpath='{.status.phase}'

# Check restore statistics
kubectl get neo4jrestore production-pitr-restore -o jsonpath='{.status.stats}'

# Monitor restore Job logs
kubectl logs -n neo4j job/production-pitr-restore-restore --follow

# Check completion time
kubectl get neo4jrestore production-pitr-restore -o jsonpath='{.status.completionTime}'
```

## Version-Specific Notes

### Neo4j 5.26.x

- Restore command: `neo4j-admin database restore`
- Key flags: `--from-path` (source), `--overwrite-destination` (not `--force`)
- PITR flag: `--restore-until` in RFC3339 format
- Automatic database state management via `STOP DATABASE` / `START DATABASE`

### Neo4j 2025.x (CalVer)

- Same restore command structure as 5.26.x
- Enhanced metadata restoration
- Additional `--restore-until` precision for PITR scenarios

## Version Requirements

- **Neo4j Version**: 5.26.0+ (semver) or 2025.01.0+ (CalVer)
- **Kubernetes**: 1.19+
- **Operator**: Latest version with restore support

## Related Resources

- [`Neo4jBackup`](neo4jbackup.md) — Backup operations
- [`Neo4jEnterpriseCluster`](neo4jenterprisecluster.md) — Target cluster resource
- [Backup and Restore Guide](../user_guide/guides/backup_restore.md) — Usage examples and best practices
