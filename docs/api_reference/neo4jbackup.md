# Neo4jBackup

This document provides a reference for the `Neo4jBackup` Custom Resource Definition (CRD). This resource is used for creating and managing backups of Neo4j databases running under either `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone`.

For a comprehensive guide on using backups, see the [Backup and Restore Guide](../user_guide/guides/backup_restore.md).

## API Version

- **Group**: `neo4j.neo4j.com`
- **Version**: `v1beta1`
- **Kind**: `Neo4jBackup`

## How it works

The operator creates a Kubernetes Job that runs `neo4j-admin database backup` inside a container using the **same Neo4j enterprise image** as the target cluster. No separate backup image is needed or configured.

Key implementation details:

- The operator automatically sets `server.backup.listen_address=0.0.0.0:6362` in `neo4j.conf` on the target StatefulSet.
- The `--from` flag is automatically populated with the FQDNs of all server pods at port `6362`.
- For cloud storage, `--to-path` uses native cloud URIs: `s3://`, `gs://`, `azb://`.
- For PVC storage, `--to-path` uses the local path within the mounted PVC.
- RBAC: Only a `neo4j-backup-sa` ServiceAccount is created. No Role or RoleBinding is created because the backup Job requires no Kubernetes API access.
- Retention: cloud storage (S3/GCS/Azure) is pruned by **your bucket's lifecycle rules**, not the operator. For PVC storage, the operator runs a cleanup Job **only when the Neo4jBackup CR is deleted** — see [RetentionPolicy](#retentionpolicy).
- `target.kind: Cluster` backs up **every database** on the instance in one `neo4j-admin database backup "*"` invocation — one `.backup` artifact per database lands in the chain directory. Restore is per-database: create one `Neo4jRestore` per database you want back (an all-databases restore mode is tracked in [#222](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues/222)).

## Spec

The `Neo4jBackupSpec` defines the desired state of a Neo4j backup configuration.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `target` | [`BackupTarget`](#backuptarget) | ✅ | What to back up |
| `storage` | [`StorageLocation`](#storagelocation) | ✅ | Where to store the backup |
| `schedule` | `string` | ❌ | Cron expression for automated backups (e.g., `"0 2 * * *"`). Plain 5-field UTC syntax only — `TZ=`/`CRON_TZ=` prefixes are **rejected** (Kubernetes refuses timezone-embedded CronJob schedules). Scheduled backup names are limited to **40 characters** (the generated `<name>-backup-cron` CronJob must fit Kubernetes' 52-char CronJob-name limit). Removing `schedule` from an existing CR **deletes the CronJob** (the CR becomes a one-shot backup). |
| `cloud` | [`*CloudBlock`](#cloudblock) | ❌ | Top-level cloud provider configuration (used for workload identity) |
| `retention` | [`*RetentionPolicy`](#retentionpolicy) | ❌ | Backup retention policy |
| `options` | [`*BackupOptions`](#backupoptions) | ❌ | Backup-specific options |
| `suspend` | `bool` | ❌ | Suspend backups without deleting the resource. For scheduled backups, the operator propagates this to `CronJob.spec.suspend`, so Kubernetes stops firing scheduled Jobs; setting it back to `false` resumes the schedule. `suspend: true` also pauses one-shot backups that haven't run yet. |
| `chainFromBackup` | `string` | ❌ | Names another `Neo4jBackup` CR in the **same namespace** whose `<base>/<cr-name>/` directory this CR should write into instead of its own. Used to compose mixed-cadence workflows (e.g. a daily `FULL` CR plus an hourly `DIFF` CR that chains off the daily's artifacts). Must be a DNS-1123 name (validator-enforced). See [Chaining mixed-cadence backups](#chaining-mixed-cadence-backups). |

## Type Definitions

### BackupTarget

Defines what to back up. The `kind` field controls how `name` and `clusterRef` are interpreted.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `kind` | `string` | ✅ | Type of resource to back up: `"Cluster"` (every database on the instance — `neo4j-admin` is invoked with the `"*"` glob, producing one artifact per database), `"Database"` (a single database), or `"ShardedDatabase"` |
| `name` | `string` | ✅ | When `kind=Cluster`: name of the `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone`. When `kind=Database`: name of the Neo4j database (e.g., `"neo4j"`, `"mydb"`). When `kind=ShardedDatabase`: the logical sharded-database name (e.g. `"products"`); the operator backs up all shards (`products-g000`, `products-p000`, …) in one `neo4j-admin` invocation via a glob. |
| `clusterRef` | `string` | ✅ when `kind=Database` or `kind=ShardedDatabase` | Name of the `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` that owns the database. Unused when `kind=Cluster`. |
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
| `bucket` | `string` | ❌ | Bucket or container name (required for cloud storage types). Restricted to `A-Z a-z 0-9 . _ / -` — the validator rejects other characters (the value is interpolated into the Job's shell command). |
| `path` | `string` | ❌ | Path within the bucket or PVC. Same `A-Z a-z 0-9 . _ / -` charset restriction as `bucket`. Defaults to `backups` for cloud storage when empty. |
| `pvc` | [`*PVCSpec`](#pvcspec) | ❌ | PVC configuration (required when `type=pvc`) |
| `cloud` | [`*CloudBlock`](#cloudblock) | ❌ | Cloud provider configuration including optional credentials secret |

### CloudBlock

Cloud provider configuration. This type appears both on `StorageLocation` (for per-storage credentials) and as a top-level `spec.cloud` field (for workload identity setup).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | `string` | ❌ | Cloud provider: `"aws"`, `"gcp"`, `"azure"` |
| `credentialsSecretRef` | `string` | ❌ | Name of a Kubernetes Secret containing cloud provider credentials as environment variables. When absent, ambient workload identity (IRSA / GKE WI / Azure WI) is used instead. |
| `identity` | [`*CloudIdentity`](#cloudidentity) | ❌ | Cloud identity configuration (for workload identity ServiceAccount annotations) |
| `endpointURL` | `string` | ❌ | Override the S3 API endpoint. Use for S3-compatible stores such as **MinIO**, Ceph RGW, or Cloudflare R2 (e.g. `"http://minio.minio.svc:9000"`). Only applies when `provider: aws`. |
| `forcePathStyle` | `bool` | ❌ | Force S3 path-style addressing (`endpoint/bucket/key` instead of `bucket.endpoint/key`). **Required for MinIO** and most self-hosted S3-compatible stores. Only effective when `endpointURL` is set. |

**Secret key requirements by provider** (when `credentialsSecretRef` is set):

| Provider | Required secret keys | Notes |
|----------|---------------------|-------|
| AWS | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` | Standard AWS SDK env vars |
| MinIO / S3-compatible | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` | Same keys as AWS; set `endpointURL` and `forcePathStyle: true` on `CloudBlock` |
| GCS | `GOOGLE_APPLICATION_CREDENTIALS_JSON` | Full service-account key JSON as a string value — **not** a filename path |
| Azure | `AZURE_STORAGE_ACCOUNT`, `AZURE_STORAGE_KEY` | Storage account credentials |

**Example — creating cloud credential secrets:**

```bash
# AWS
kubectl create secret generic aws-backup-credentials \
  --from-literal=AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE \
  --from-literal=AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  --from-literal=AWS_REGION=us-east-1

# MinIO (uses the same keys; region value is arbitrary — MinIO ignores it)
kubectl create secret generic minio-backup-credentials \
  --from-literal=AWS_ACCESS_KEY_ID=minioadmin \
  --from-literal=AWS_SECRET_ACCESS_KEY=minioadmin \
  --from-literal=AWS_REGION=us-east-1

# GCS — pass the JSON content directly as a string value
kubectl create secret generic gcs-backup-credentials \
  --from-literal=GOOGLE_APPLICATION_CREDENTIALS_JSON="$(cat service-account.json)"

# Azure
kubectl create secret generic azure-backup-credentials \
  --from-literal=AZURE_STORAGE_ACCOUNT=myaccount \
  --from-literal=AZURE_STORAGE_KEY=base64key==
```

**MinIO / S3-compatible example:**

```yaml
storage:
  type: s3
  bucket: neo4j-backups
  path: cluster/full
  cloud:
    provider: aws
    credentialsSecretRef: minio-backup-credentials
    endpointURL: http://minio.minio.svc:9000   # in-cluster MinIO service
    forcePathStyle: true                        # required for MinIO
```

> **How it works**: `endpointURL` is injected as `AWS_ENDPOINT_URL_S3` (AWS SDK v2 standard). `forcePathStyle: true` injects `-Daws.s3.forcePathStyle=true` via `JAVA_TOOL_OPTIONS`, which the neo4j-admin JVM process reads at startup.

### CloudIdentity

Cloud identity configuration for workload identity scenarios (no static credentials).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | `string` | ✅ | Identity provider: `"aws"`, `"gcp"`, `"azure"` |
| `serviceAccount` | `string` | ❌ | **RESERVED — currently a no-op.** Backup Jobs always run as the operator-managed `neo4j-backup-sa` (restore Jobs: `neo4j-restore-sa`); this field is accepted for backward compatibility but not read. Bind your cloud IAM role via `autoCreate.annotations` instead. |
| `autoCreate` | [`*AutoCreateSpec`](#autocreatespec) | ❌ | Workload-identity annotations for the operator-managed ServiceAccount |

### AutoCreateSpec

Carries workload-identity annotations for the operator-managed ServiceAccount.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | `bool` | ❌ | **RESERVED — currently a no-op.** The operator always ensures the backup/restore ServiceAccount exists; only `annotations` is honored. |
| `annotations` | `map[string]string` | ❌ | Annotations applied to the `neo4j-backup-sa` ServiceAccount on every reconcile. Use this to attach workload-identity annotations. |

The annotations in `autoCreate.annotations` are applied to the `neo4j-backup-sa` ServiceAccount on **every reconcile**, so they stay in sync with the desired state.

**Workload identity annotation examples:**

```yaml
# AWS IRSA
autoCreate:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/neo4j-backup-role

# GKE Workload Identity
autoCreate:
  annotations:
    iam.gke.io/gcp-service-account: neo4j-backup@my-project.iam.gserviceaccount.com

# Azure Workload Identity
autoCreate:
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
| `maxAge` | `string` | ❌ | Maximum age of artifacts to retain. Accepted units: `d` (days), `h` (hours), `m` (minutes), `s` (seconds) — e.g. `"30d"`, `"168h"`, `"90m"`. `"4w"` is **rejected** by the validator. |
| `maxCount` | `int32` | ❌ | Maximum number of `.backup` artifacts to retain |
| `deletePolicy` | `string` | ❌ | `"Delete"` (default). `"Archive"` is **RESERVED — currently a no-op** (no archival logic exists; accepted for backward compatibility). |

**How retention actually works** (read this before relying on it):

- **Cloud storage (S3/GCS/Azure)**: the operator never deletes cloud objects. Configure **bucket lifecycle rules** on the provider side; `retention` on a cloud-backed CR is advisory only.
- **PVC storage**: retention is enforced by a cleanup Job that runs **only when the Neo4jBackup CR is deleted** — it is *not* a continuous pruning loop. The Job prunes `*.backup` **files** inside this CR's chain directory (`/backup/<chain-root>/`) only, **oldest-first by file mtime**, and **always keeps the newest artifact** even if it has aged out. Other CRs' chain directories on a shared PVC are never touched.
- **DIFF-orphaning caveat**: artifact filenames don't encode FULL vs DIFF, so pruning can orphan differential artifacts whose parent FULL ages out (making them unrestorable). The operator emits a `BackupRetentionCaveat` Warning event when retention is configured on a CR that can produce DIFFs. Prefer `options.backupType: FULL` on CRs that use retention, or prune chains with `neo4j-admin backup aggregate`.

### BackupOptions

Fine-grained backup execution options.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `resources` | [`*corev1.ResourceRequirements`](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/) | ❌ | CPU/memory requests + limits on the backup Job's container. When unset, the operator applies a Burstable default (request 100m CPU / 512Mi memory, limit 1 CPU / 2Gi memory) sized for small databases and CI. Tune upward for large production databases. |
| `compress` | `*bool` | ❌ | Compress the backup (default: `true`). Pointer type so an explicit `false` survives updates (a plain bool would silently snap back to the default). |
| `backupType` | `string` | ❌ | Backup type: `"FULL"`, `"DIFF"`, `"AUTO"` (default) |
| `preferDiffAsParent` | `bool` | ❌ | Use the latest differential backup as the parent when creating a new differential backup (default: `false`). Maps to `--prefer-diff-as-parent`. **Requires CalVer 2025.04+** — an error is returned at runtime if the target version does not support this flag. |
| `tempPath` | `string` | ❌ | Local directory path for temporary files during backup. When `tempStorage` is configured, this is set automatically. Only set manually if you are mounting your own volume. Maps to `--temp-path`. Must be an **absolute** path restricted to `A-Z a-z 0-9 . _ / -` (validator-enforced). |
| `tempStorage` | [`*TempStorageSpec`](#tempstoragespec) | ❌ | Provisions a PVC for temporary staging files during cloud backups. The operator mounts this PVC and passes `--temp-path` automatically. Recommended for large databases to avoid filling ephemeral disk. |
| `pageCache` | `string` | ❌ | Page cache size hint (e.g., `"4G"`). Must match pattern `^[0-9]+[KMG]?$` |
| `verify` | `bool` | ❌ | **RESERVED — currently a no-op.** Accepted for backward compatibility but not read by the operator. For real artifact verification use `validate` (below). |
| `validate` | `*bool` | ❌ | When `true`, runs `neo4j-admin backup validate` against the artifacts **after** the backup succeeds, recording per-shard recoverability into `status.history[].validation`. Appended with `\|\| true` so validate failures don't fail the Job (the backup already succeeded). Pointer type preserves an explicit `true` or `false` across updates; nil (default) skips validate. |
| `parallelDownload` | `bool` | ❌ | Enable parallel download for remote backups |
| `remoteAddressResolution` | `*bool` | ❌ | Resolve remote addresses via the cluster discovery service (useful in multi-homed environments). Pointer type: when unset and `target.kind=ShardedDatabase` on Neo4j 2025.09+, the operator defaults this to `true` to match the canonical upstream sharded-backup invocation; otherwise unset. Set explicitly (`true` or `false`) to override in either direction. |
| `skipRecovery` | `bool` | ❌ | Skip the recovery step after backup |
| `includeMetadata` | `string` | ❌ | Controls which metadata is included in the backup. Values: `"all"` (default), `"none"`, `"users"`, `"roles"`. Requires Neo4j 5.26+. |
| `parallelRecovery` | `bool` | ❌ | Enable multi-threaded transaction application during backup |
| `keepFailed` | `bool` | ❌ | Preserve failed backup artifacts for debugging instead of deleting them |
| `additionalArgs` | `[]string` | ❌ | Additional arguments passed verbatim to `neo4j-admin database backup` |

> **`preferDiffAsParent` version requirement**: This flag was introduced in Neo4j CalVer 2025.04. Using it against Neo4j 5.26.x or CalVer 2025.01–2025.03 will cause the backup Job to fail with an unsupported argument error. The operator validates this at runtime and returns an error before creating the Job.

### TempStorageSpec

Provisions temporary staging storage for cloud backup/restore. The operator creates a PVC, mounts it at `/tmp/neo4j-staging` in the Job pod, and passes `--temp-path=/tmp/neo4j-staging` to `neo4j-admin`. The PVC is owned by the Job and garbage-collected when the Job's TTL expires.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `size` | `string` | ✅ | Size of the temporary PVC (e.g., `"50Gi"`). Should be at least as large as the expected backup artifact. Must match pattern `^[0-9]+(Ki\|Mi\|Gi\|Ti)?$` |
| `storageClassName` | `string` | ❌ | StorageClass for the temporary PVC. If empty, uses the cluster default. |

## Status

The `Neo4jBackupStatus` represents the observed state of the backup.

| Field | Type | Description |
|-------|------|-------------|
| `conditions` | `[]metav1.Condition` | Current backup conditions. The operator maintains a single `Ready` condition derived from `phase` (`Completed` → `True` with reason `BackupSucceeded`; `Failed`/`Suspended`/`Invalid` → `False`; everything else → `Unknown`). |
| `phase` | `string` | Current backup phase — see [Backup Phases](#backup-phases) |
| `message` | `string` | Human-readable message about the current state |
| `observedGeneration` | `int64` | The `.metadata.generation` most recently observed by the controller |
| `lastRunTime` | `*metav1.Time` | When the last backup Job started |
| `lastSuccessTime` | `*metav1.Time` | When a **one-shot** backup completed (set on the `Completed` phase transition). **Never set for scheduled backups** — they don't pass through `Completed`; read `status.history[]` instead. |
| `nextRunTime` | `*metav1.Time` | When the next scheduled backup will run |
| `stats` | [`*BackupStats`](#backupstats) | Statistics from the most recent backup run |
| `history` | [`[]BackupRun`](#backuprun) | History of recent backup runs. For scheduled backups this is the **authoritative record** of run outcomes. |

### Backup Phases

| Phase | Meaning |
|-------|---------|
| `Pending` | A transient precondition isn't met yet — e.g. the `chainFromBackup` parent CR doesn't exist yet, another Job in the same chain is still running, or the sharded-backup preflight couldn't connect to the cluster. The controller requeues and retries. |
| `Waiting` | The target cluster/standalone CR doesn't exist yet (common with `kubectl apply -f dir/` ordering) or isn't `Ready`. Transient — the controller requeues. |
| `Scheduled` | A CronJob has been created for `spec.schedule`. **This is the steady state for scheduled backups** — they never transition to `Completed`; per-run outcomes accumulate in `status.history[]`. |
| `Suspended` | `spec.suspend: true` — the CronJob is suspended and one-shot runs are paused. |
| `Running` | A one-shot backup Job is executing. |
| `Completed` | The one-shot backup Job succeeded. Terminal — re-running requires deleting and recreating the CR. |
| `Failed` | The one-shot backup Job failed terminally (Job `Failed` condition, after retries) or a non-transient error occurred (e.g. chain target/storage mismatch). Terminal for one-shot backups. |
| `Invalid` | The spec failed validation (clear aggregated message in `status.message`). Recoverable — fixing the spec re-triggers reconcile. |

> **Don't `kubectl wait --for=condition=Ready` on a scheduled backup** — it stays in `Scheduled` (Ready=`Unknown`) forever. Poll `status.history[*].status` for `Succeeded` instead.

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
| `runID` | `string` | Unique identifier for this run, populated from the backing Job's **`metadata.name`** (not its opaque UID) — e.g. `<backup>-backup` for one-shots, `<backup>-backup-cron-<unix-seconds>` for CronJob children. Used by the operator to dedupe history entries across reconciles and by users to correlate a history entry with the actual Job (`kubectl logs job/<runID>`). |
| `startTime` | `metav1.Time` | When the backup run started |
| `completionTime` | `*metav1.Time` | When the run completed (`nil` if still running) |
| `status` | `string` | Run status: `"Running"`, `"Succeeded"`, `"Failed"` |
| `error` | `string` | Error message if the backup failed |
| `stats` | [`*BackupStats`](#backupstats) | Backup statistics for this run |
| `backupsPath` | `string` | The shared per-CR directory under `spec.storage.path` where this run wrote its `.backup` artifact. **Same value for every run of one CR** — all runs accumulate in this directory so `neo4j-admin` can chain differential backups off the prior full. Value is the Neo4jBackup CR name (or the `chainFromBackup` chain-root name when set). Use the `runID` field (Job name) for per-run identity. |
| `artifactFilename` | `string` | Filename of the `.backup` artifact produced by this standard-DB run (e.g. `"neo4j-2026-06-08T01-18-06.backup"`). Populated by parsing the Job's Pod log after completion; empty when logs couldn't be fetched or the pattern didn't match. Used by the cluster PVC-restore path to build a per-restore seed URL. |
| `shardArtifacts` | [`[]ShardArtifact`](#shardartifact) | Per-shard `.backup` files produced by a sharded backup run (`target.kind=ShardedDatabase`). Empty for non-sharded runs. |
| `validation` | [`*BackupValidationResult`](#backupvalidationresult) | Per-shard outcome of the optional `neo4j-admin backup validate` step (only when `options.validate=true` and the operator could parse the output). |

### ShardArtifact

Identifies one `.backup` file produced by a sharded backup.

| Field | Type | Description |
|-------|------|-------------|
| `shardName` | `string` | Per-shard database name (e.g. `"products-g000"`, `"products-p000"`). Derived by stripping the timestamp suffix from the neo4j-admin output filename. |
| `filename` | `string` | On-disk filename as written by neo4j-admin (e.g. `"products-g000-2025-06-11T21-04-42.backup"`). |
| `size` | `int64` | Artifact size in bytes as reported by `ls -la`. Zero if not parseable. |

### BackupValidationResult

Captures the output of `neo4j-admin backup validate` run after a backup, surfacing per-shard recoverability.

| Field | Type | Description |
|-------|------|-------------|
| `overallStatus` | `string` | `"OK"` if every shard's chain is recoverable to the same transaction or higher; `"Degraded"` if any shard is ahead/behind beyond the lenient consistency window; `"Unknown"` if validate failed or its output couldn't be parsed. |
| `perShard` | [`[]ShardValidationStatus`](#shardvalidationstatus) | The validate command's status report for each shard. |
| `rawOutput` | `string` | Truncated stdout of `neo4j-admin backup validate`, kept for debugging when the parser couldn't classify a per-shard line. Capped at 2 KiB. |

### ShardValidationStatus

One row of validate output.

| Field | Type | Description |
|-------|------|-------------|
| `shardName` | `string` | Per-shard database name. |
| `status` | `string` | One of `"OK"`, `"Ahead"`, `"Behind"`, `"Unknown"`. |
| `message` | `string` | Human-readable detail for this shard's status. |

## Chaining mixed-cadence backups

`spec.chainFromBackup` lets a higher-frequency differential backup share artifact storage with a lower-frequency full backup, so `neo4j-admin database backup --type=DIFF` can auto-detect and chain off the prior full/diff in the shared directory.

A typical setup is a daily `FULL` backup CR plus an hourly `DIFF` backup CR that points `chainFromBackup` at the daily CR. Both write into `<base>/<daily-cr-name>/`.

**Constraints** (enforced by the validator):

- Must reference a CR in the **same namespace**.
- Cannot self-reference (`chainFromBackup: <this-cr-name>` is rejected).
- Target (cluster + database) must match the referenced CR.
- Storage backend (type + bucket/path) must match.

**Runtime safety**: the operator labels every backup Job with `app.kubernetes.io/part-of: <chain-root-name>` and refuses to submit a new Job while another Job sharing the same `part-of` label is still active — preventing the daily FULL and hourly DIFF from racing against the same artifact directory.

When empty (default), the CR writes to its own per-name directory.

```yaml
# Daily FULL backup (the chain root)
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: daily-full
  namespace: neo4j
spec:
  target:
    kind: Cluster
    name: production-cluster
  storage:
    type: s3
    bucket: neo4j-backups
    path: prod/
    cloud:
      provider: aws
  schedule: "0 2 * * *"
  options:
    backupType: FULL
---
# Hourly DIFF backup chaining off the daily full
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: hourly-diff
  namespace: neo4j
spec:
  target:
    kind: Cluster
    name: production-cluster
  storage:
    type: s3
    bucket: neo4j-backups
    path: prod/
    cloud:
      provider: aws
  schedule: "0 * * * *"
  chainFromBackup: daily-full   # share daily-full's artifact directory
  options:
    backupType: DIFF
```

## Examples

### Scheduled S3 Backup (Cluster) with IRSA

Uses AWS IRSA workload identity — no static credentials needed.

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
        annotations:
          eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/neo4j-backup-role
  schedule: "0 2 * * *"   # Daily at 2 AM UTC
  retention:
    maxAge: "30d"
    maxCount: 30
  options:
    compress: true
    backupType: FULL
    tempStorage:
      size: "50Gi"
```

### Scheduled S3 Backup with Static Credentials

Uses an explicit Kubernetes Secret for AWS credentials instead of IRSA.

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
    tempStorage:
      size: "50Gi"
```

### Single-Database Backup to S3

Backs up only one database. Both `name` (database) and `clusterRef` (cluster) are required.

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
    tempStorage:
      size: "50Gi"
```

### Differential Backup with preferDiffAsParent (CalVer 2025.04+)

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
    tempStorage:
      size: "50Gi"
    compress: true
```

### On-Demand PVC Backup

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
    validate: true   # runs `neo4j-admin backup validate` after the backup
    backupType: DIFF
```

### GCS Backup with GKE Workload Identity

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
        annotations:
          iam.gke.io/gcp-service-account: neo4j-backup@my-project.iam.gserviceaccount.com
  schedule: "0 3 * * 0"   # Weekly on Sunday at 3 AM
  retention:
    maxCount: 12
  options:
    backupType: AUTO
    pageCache: "8G"
    tempStorage:
      size: "50Gi"
```

### GCS Backup with Static Service Account Credentials

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
    tempStorage:
      size: "50Gi"
```

### Azure Backup with Azure Workload Identity

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
        annotations:
          azure.workload.identity/client-id: 00000000-0000-0000-0000-000000000000
  schedule: "0 1 * * *"
  retention:
    maxAge: "14d"
  options:
    compress: true
    backupType: FULL
    tempStorage:
      size: "50Gi"
```

### Azure Backup with Static Credentials

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
    tempStorage:
      size: "50Gi"
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

# Check backup phase (scheduled backups stay in "Scheduled"; only one-shot
# backups reach "Completed")
kubectl get neo4jbackup daily-cluster-backup -o jsonpath='{.status.phase}'

# Check run outcomes — for scheduled backups, status.history is the record
# (lastSuccessTime is only set for one-shot backups)
kubectl get neo4jbackup daily-cluster-backup -o jsonpath='{.status.history[*].status}'
```

For more information on backup operations, see the [Backup and Restore Guide](../user_guide/guides/backup_restore.md).
