# Neo4jBackup

This document provides a reference for the `Neo4jBackup` Custom Resource Definition (CRD). This resource is used for creating and managing backups of Neo4j databases running under either `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone`.

For a comprehensive guide on using backups, see the [Backup and Restore Guide](../user_guide/guides/backup_restore.md).

## API Version

- **Group**: `neo4j.neo4j.com`
- **Version**: `v1alpha1`
- **Kind**: `Neo4jBackup`

## How it works

The operator creates a Kubernetes Job that runs `neo4j-admin database backup` inside a container using the **same Neo4j enterprise image** as the target cluster. No separate backup image is needed or configured.

Key implementation details:

- The operator automatically sets `server.backup.listen_address=0.0.0.0:6362` in `neo4j.conf` on the target StatefulSet.
- The `--from` flag is automatically populated with the FQDNs of all server pods at port `6362`.
- For cloud storage, `--to-path` uses native cloud URIs: `s3://`, `gs://`, `azb://`.
- For PVC storage, `--to-path` uses the local path within the mounted PVC.
- RBAC: Only a `neo4j-backup-sa` ServiceAccount is created. No Role or RoleBinding is created because the backup Job requires no Kubernetes API access.
- Cloud retention: The operator logs a notice to configure bucket lifecycle rules on the cloud provider side. PVC retention uses `find` + `rm` in a cleanup Job.

## Spec

The `Neo4jBackupSpec` defines the desired state of a Neo4j backup configuration.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `target` | [`BackupTarget`](#backuptarget) | ✅ | What to back up |
| `storage` | [`StorageLocation`](#storagelocation) | ✅ | Where to store the backup |
| `schedule` | `string` | ❌ | Cron expression for automated backups (e.g., `"0 2 * * *"`) |
| `cloud` | [`*CloudBlock`](#cloudblock) | ❌ | Top-level cloud provider configuration (used for workload identity) |
| `retention` | [`*RetentionPolicy`](#retentionpolicy) | ❌ | Backup retention policy |
| `options` | [`*BackupOptions`](#backupoptions) | ❌ | Backup-specific options |
| `suspend` | `bool` | ❌ | Suspend the backup schedule without deleting the resource |

## Type Definitions

### BackupTarget

Defines what to back up. The `kind` field controls how `name` and `clusterRef` are interpreted.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `kind` | `string` | ✅ | Type of resource to back up: `"Cluster"` or `"Database"` |
| `name` | `string` | ✅ | When `kind=Cluster`: name of the `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone`. When `kind=Database`: name of the Neo4j database (e.g., `"neo4j"`, `"mydb"`) |
| `clusterRef` | `string` | ✅ when `kind=Database` | Name of the `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` that owns the database. Unused when `kind=Cluster`. |
| `namespace` | `string` | ❌ | Namespace of the target resource (defaults to the backup namespace) |

> **Important**: In earlier releases, when `kind=Database` the `name` field was incorrectly used for cluster lookup. This has been corrected: `name` is always the database name and `clusterRef` is the cluster name. Both are required when `kind=Database`.

**Examples:**

```yaml
# Back up an entire cluster (all databases)
target:
  kind: Cluster
  name: production-cluster

# Back up a single database
target:
  kind: Database
  name: mydb
  clusterRef: production-cluster
  namespace: neo4j
```

### StorageLocation

Defines where to store backups.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | `string` | ✅ | Storage type: `"s3"`, `"gcs"`, `"azure"`, `"pvc"` |
| `bucket` | `string` | ❌ | Bucket or container name (required for cloud storage types) |
| `path` | `string` | ❌ | Path within the bucket or PVC |
| `pvc` | [`*PVCSpec`](#pvcspec) | ❌ | PVC configuration (required when `type=pvc`) |
| `cloud` | [`*CloudBlock`](#cloudblock) | ❌ | Cloud provider configuration including optional credentials secret |

### CloudBlock

Cloud provider configuration. This type appears both on `StorageLocation` (for per-storage credentials) and as a top-level `spec.cloud` field (for workload identity setup).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | `string` | ❌ | Cloud provider: `"aws"`, `"gcp"`, `"azure"` |
| `credentialsSecretRef` | `string` | ❌ | Name of a Kubernetes Secret containing cloud provider credentials as environment variables. When absent, ambient workload identity (IRSA / GKE WI / Azure WI) is used instead. |
| `identity` | [`*CloudIdentity`](#cloudidentity) | ❌ | Cloud identity configuration (for workload identity ServiceAccount annotations) |

**Secret key requirements by provider** (when `credentialsSecretRef` is set):

| Provider | Required secret keys | Notes |
|----------|---------------------|-------|
| AWS | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_DEFAULT_REGION` | Standard AWS SDK env vars |
| GCS | `GOOGLE_APPLICATION_CREDENTIALS_JSON` | Full service-account key JSON as a string value — **not** a filename path |
| Azure | `AZURE_STORAGE_ACCOUNT`, `AZURE_STORAGE_KEY` | Storage account credentials |

**Example — creating cloud credential secrets:**

```bash
# AWS
kubectl create secret generic aws-backup-credentials \
  --from-literal=AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE \
  --from-literal=AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  --from-literal=AWS_DEFAULT_REGION=us-east-1

# GCS — pass the JSON content directly as a string value
kubectl create secret generic gcs-backup-credentials \
  --from-literal=GOOGLE_APPLICATION_CREDENTIALS_JSON="$(cat service-account.json)"

# Azure
kubectl create secret generic azure-backup-credentials \
  --from-literal=AZURE_STORAGE_ACCOUNT=myaccount \
  --from-literal=AZURE_STORAGE_KEY=base64key==
```

### CloudIdentity

Cloud identity configuration for workload identity scenarios (no static credentials).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | `string` | ✅ | Identity provider: `"aws"`, `"gcp"`, `"azure"` |
| `serviceAccount` | `string` | ❌ | Name of an existing ServiceAccount to use. When absent and `autoCreate.enabled=true`, the operator creates `neo4j-backup-sa`. |
| `autoCreate` | [`*AutoCreateSpec`](#autocreatespec) | ❌ | Auto-create ServiceAccount with workload-identity annotations |

### AutoCreateSpec

Controls automatic ServiceAccount creation with workload-identity annotations.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | `bool` | ❌ | Enable auto-creation of the `neo4j-backup-sa` ServiceAccount (default: `true`) |
| `annotations` | `map[string]string` | ❌ | Annotations applied to the `neo4j-backup-sa` ServiceAccount on every reconcile. Use this to attach workload-identity annotations. |

The annotations in `autoCreate.annotations` are applied to the `neo4j-backup-sa` ServiceAccount on **every reconcile**, so they stay in sync with the desired state.

**Workload identity annotation examples:**

```yaml
# AWS IRSA
autoCreate:
  enabled: true
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/neo4j-backup-role

# GKE Workload Identity
autoCreate:
  enabled: true
  annotations:
    iam.gke.io/gcp-service-account: neo4j-backup@my-project.iam.gserviceaccount.com

# Azure Workload Identity
autoCreate:
  enabled: true
  annotations:
    azure.workload.identity/client-id: 00000000-0000-0000-0000-000000000000
```

### PVCSpec

PVC configuration for local storage.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `storageClassName` | `string` | ❌ | Storage class name for dynamic provisioning |
| `name` | `string` | ❌ | Name of an existing PVC to use |
| `size` | `string` | ❌ | Size for a new PVC (e.g., `"100Gi"`) |

### RetentionPolicy

Backup retention configuration.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `maxAge` | `string` | ❌ | Maximum age of backups to retain (e.g., `"30d"`, `"4w"`) |
| `maxCount` | `int32` | ❌ | Maximum number of backups to retain |
| `deletePolicy` | `string` | ❌ | Action for expired backups: `"Delete"` (default) or `"Archive"` |

> **Cloud storage retention**: For cloud storage targets the operator logs a notice to configure bucket lifecycle rules on the cloud provider side. Automated deletion of cloud objects is not performed by the operator.

### BackupOptions

Fine-grained backup execution options.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `compress` | `bool` | ❌ | Compress the backup (default: `true`) |
| `backupType` | `string` | ❌ | Backup type: `"FULL"`, `"DIFF"`, `"AUTO"` (default) |
| `preferDiffAsParent` | `bool` | ❌ | Use the latest differential backup as the parent when creating a new differential backup (default: `false`). Maps to `--prefer-diff-as-parent`. **Requires CalVer 2025.04+** — an error is returned at runtime if the target version does not support this flag. |
| `tempPath` | `string` | ❌ | Local directory path for temporary files during backup. **Strongly recommended for cloud storage** to avoid filling Neo4j's working directory during streaming. Maps to `--temp-path`. Example: `"/tmp/neo4j-backup-temp"` |
| `pageCache` | `string` | ❌ | Page cache size hint (e.g., `"4G"`). Must match pattern `^[0-9]+[KMG]?$` |
| `encryption` | [`*EncryptionSpec`](#encryptionspec) | ❌ | Backup encryption configuration |
| `verify` | `bool` | ❌ | Verify backup integrity after creation |
| `parallelDownload` | `bool` | ❌ | Enable parallel download for remote backups |
| `remoteAddressResolution` | `bool` | ❌ | Resolve remote addresses during backup |
| `skipRecovery` | `bool` | ❌ | Skip the recovery step after backup |
| `additionalArgs` | `[]string` | ❌ | Additional arguments passed verbatim to `neo4j-admin database backup` |

> **`preferDiffAsParent` version requirement**: This flag was introduced in Neo4j CalVer 2025.04. Using it against Neo4j 5.26.x or CalVer 2025.01–2025.03 will cause the backup Job to fail with an unsupported argument error. The operator validates this at runtime and returns an error before creating the Job.

### EncryptionSpec

Backup encryption configuration.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | `bool` | ❌ | Enable backup encryption |
| `keySecret` | `string` | ❌ | Name of a Kubernetes Secret containing the encryption key |
| `algorithm` | `string` | ❌ | Encryption algorithm: `"AES256"` (default) or `"ChaCha20"` |

## Status

The `Neo4jBackupStatus` represents the observed state of the backup.

| Field | Type | Description |
|-------|------|-------------|
| `conditions` | `[]metav1.Condition` | Current backup conditions |
| `phase` | `string` | Current backup phase |
| `message` | `string` | Human-readable message about the current state |
| `lastRunTime` | `*metav1.Time` | When the last backup Job started |
| `lastSuccessTime` | `*metav1.Time` | When the last successful backup completed |
| `nextRunTime` | `*metav1.Time` | When the next scheduled backup will run |
| `stats` | [`*BackupStats`](#backupstats) | Statistics from the most recent backup run |
| `history` | [`[]BackupRun`](#backuprun) | History of recent backup runs |

### BackupStats

| Field | Type | Description |
|-------|------|-------------|
| `size` | `string` | Total backup size (e.g., `"2.5GB"`) |
| `duration` | `string` | Backup operation duration (e.g., `"5m30s"`) |
| `throughput` | `string` | Backup throughput rate (e.g., `"8.3MB/s"`) |
| `fileCount` | `int32` | Number of files in the backup |

### BackupRun

Represents a single backup Job execution.

| Field | Type | Description |
|-------|------|-------------|
| `startTime` | `metav1.Time` | When the backup run started |
| `completionTime` | `*metav1.Time` | When the run completed (`nil` if still running) |
| `status` | `string` | Run status: `"Running"`, `"Succeeded"`, `"Failed"` |
| `error` | `string` | Error message if the backup failed |
| `stats` | [`*BackupStats`](#backupstats) | Backup statistics for this run |

## Examples

### Scheduled S3 Backup (Cluster) with IRSA

Uses AWS IRSA workload identity — no static credentials needed.

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: daily-cluster-backup
  namespace: neo4j
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
  cloud:
    provider: aws
    identity:
      provider: aws
      autoCreate:
        enabled: true
        annotations:
          eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/neo4j-backup-role
  schedule: "0 2 * * *"   # Daily at 2 AM UTC
  retention:
    maxAge: "30d"
    maxCount: 30
  options:
    compress: true
    backupType: FULL
    tempPath: /tmp/neo4j-backup-temp
    encryption:
      enabled: true
      keySecret: backup-encryption-key
```

### Scheduled S3 Backup with Static Credentials

Uses an explicit Kubernetes Secret for AWS credentials instead of IRSA.

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: daily-cluster-backup-static-creds
  namespace: neo4j
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
      credentialsSecretRef: aws-backup-credentials   # Secret with AWS_ACCESS_KEY_ID etc.
  schedule: "0 2 * * *"
  retention:
    maxAge: "30d"
    maxCount: 30
  options:
    compress: true
    backupType: FULL
    tempPath: /tmp/neo4j-backup-temp
```

### Single-Database Backup to S3

Backs up only one database. Both `name` (database) and `clusterRef` (cluster) are required.

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: mydb-daily-backup
  namespace: neo4j
spec:
  target:
    kind: Database
    name: mydb            # The Neo4j database name
    clusterRef: production-cluster   # The cluster that hosts the database
    namespace: neo4j
  storage:
    type: s3
    bucket: neo4j-backups
    path: mydb/daily/
    cloud:
      provider: aws
      credentialsSecretRef: aws-backup-credentials
  schedule: "0 3 * * *"
  options:
    compress: true
    backupType: AUTO
    tempPath: /tmp/neo4j-backup-temp
```

### Differential Backup with preferDiffAsParent (CalVer 2025.04+)

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: hourly-diff-backup
  namespace: neo4j
spec:
  target:
    kind: Cluster
    name: production-cluster-2025
  storage:
    type: s3
    bucket: neo4j-backups
    path: hourly-diff/
    cloud:
      provider: aws
      credentialsSecretRef: aws-backup-credentials
  schedule: "0 * * * *"   # Every hour
  options:
    backupType: DIFF
    preferDiffAsParent: true   # Requires Neo4j CalVer 2025.04+
    tempPath: /tmp/neo4j-backup-temp
    compress: true
```

### On-Demand PVC Backup

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: manual-pvc-backup
  namespace: neo4j
spec:
  target:
    kind: Database
    name: mydb
    clusterRef: staging-cluster
    namespace: neo4j
  storage:
    type: pvc
    pvc:
      name: backup-storage
    path: backups/manual/
  options:
    compress: true
    verify: true
    backupType: DIFF
```

### GCS Backup with GKE Workload Identity

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: weekly-gcs-backup
  namespace: neo4j
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
  cloud:
    provider: gcp
    identity:
      provider: gcp
      autoCreate:
        enabled: true
        annotations:
          iam.gke.io/gcp-service-account: neo4j-backup@my-project.iam.gserviceaccount.com
  schedule: "0 3 * * 0"   # Weekly on Sunday at 3 AM
  retention:
    maxCount: 12
    deletePolicy: Archive
  options:
    backupType: AUTO
    pageCache: "8G"
    tempPath: /tmp/neo4j-backup-temp
```

### GCS Backup with Static Service Account Credentials

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: weekly-gcs-backup-static
  namespace: neo4j
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
      credentialsSecretRef: gcs-backup-credentials   # Secret with GOOGLE_APPLICATION_CREDENTIALS_JSON
  schedule: "0 3 * * 0"
  retention:
    maxCount: 12
  options:
    backupType: AUTO
    pageCache: "8G"
    tempPath: /tmp/neo4j-backup-temp
```

### Azure Backup with Azure Workload Identity

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: daily-azure-backup
  namespace: neo4j
spec:
  target:
    kind: Cluster
    name: enterprise-cluster
  storage:
    type: azure
    bucket: neo4j-backups         # Azure storage container name
    path: daily/
    cloud:
      provider: azure
  cloud:
    provider: azure
    identity:
      provider: azure
      autoCreate:
        enabled: true
        annotations:
          azure.workload.identity/client-id: 00000000-0000-0000-0000-000000000000
  schedule: "0 1 * * *"
  retention:
    maxAge: "14d"
  options:
    compress: true
    backupType: FULL
    tempPath: /tmp/neo4j-backup-temp
```

### Azure Backup with Static Credentials

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: daily-azure-backup-static
  namespace: neo4j
spec:
  target:
    kind: Cluster
    name: enterprise-cluster
  storage:
    type: azure
    bucket: neo4j-backups
    path: daily/
    cloud:
      provider: azure
      credentialsSecretRef: azure-backup-credentials   # Secret with AZURE_STORAGE_ACCOUNT and AZURE_STORAGE_KEY
  schedule: "0 1 * * *"
  retention:
    maxAge: "14d"
  options:
    compress: true
    backupType: FULL
    tempPath: /tmp/neo4j-backup-temp
```

## Monitoring

```bash
# List all backup resources
kubectl get neo4jbackup -n neo4j

# View backup status and last run time
kubectl get neo4jbackup daily-cluster-backup -o wide

# Describe a backup for detailed status and events
kubectl describe neo4jbackup daily-cluster-backup

# Watch backup status changes
kubectl get neo4jbackup daily-cluster-backup -w

# Check logs from the most recent backup Job
kubectl logs -n neo4j -l neo4j.com/backup=daily-cluster-backup --tail=100

# Check backup phase
kubectl get neo4jbackup daily-cluster-backup -o jsonpath='{.status.phase}'

# Check last success time
kubectl get neo4jbackup daily-cluster-backup -o jsonpath='{.status.lastSuccessTime}'
```

For more information on backup operations, see the [Backup and Restore Guide](../user_guide/guides/backup_restore.md).
