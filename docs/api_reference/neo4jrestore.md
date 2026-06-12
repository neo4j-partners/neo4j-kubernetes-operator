# Neo4jRestore API Reference

This document provides a comprehensive reference for the `Neo4jRestore` Custom Resource Definition (CRD). This resource is used to restore Neo4j Enterprise databases from backups, including support for point-in-time recovery (PITR) and both cluster and standalone deployments.

For practical examples and usage guidance, see the [Backup and Restore Guide](../user_guide/guides/backup_restore.md).

## API Version

- **Group**: `neo4j.neo4j.com`
- **Version**: `v1beta1`
- **Kind**: `Neo4jRestore`

## How it works

The operator picks the restore method based on the target kind referenced by `clusterRef`. The Neo4j docs flag `neo4j-admin database restore` as **unsafe on clusters**, so the two paths diverge:

**`Neo4jEnterpriseCluster` target** — Cypher over Bolt, no Job:

1. Resolves `source.backupRef` (or `source.storage`) into the exact `.backup` **file** URI of the latest successful run, e.g. `s3://bucket/path/<cr-name>/<dbname>-<timestamp>.backup`. Neo4j's `CloudSeedProvider` seeds a single database from one file (a directory URI fails with `Can't open seed file`); when that file is a differential, Neo4j resolves and applies the full + differential chain from the same directory automatically.
   - **Mixed-cadence chains (`spec.chainFromBackup`)**: "latest successful run" is scoped to the **referenced CR**, not the shared chain directory. Reference the **differential** CR to restore the latest state; referencing the parent **full** CR seeds from its latest full snapshot — the newer diffs are *not* applied. Restoring via a chain-parent CR emits a `RestoreFromChainParent` Warning event naming the differential children, so a restore intending "latest" that references the full CR isn't a silent surprise. To pin an arbitrary run, use `source.type: storage` with `backupPath` set to the exact `.backup` file.
   - For **PVC-backed** backups, the operator spawns a `backup-seed-proxy-<restore-name>` Deployment + Service serving the backup PVC read-only over HTTP, plus a **NetworkPolicy restricting proxy ingress to the target cluster's server pods** (effective on enforcing CNIs). The seedURI becomes an `http://` URL at the exact `.backup` filename (`URLConnectionSeedProvider`). The whole proxy stack is **torn down automatically** as soon as the restore reaches `Completed` or `Failed` (and on CR deletion) — it does not linger for the lifetime of the CR.
2. Projects cloud credentials onto cluster pods via `spec.extraEnvFrom` (cluster CR) — required so the JVM's AWS/GCP/Azure SDK can authenticate. The operator emits an actionable error if the Secret isn't projected; set the cluster annotation `neo4j.com/auto-inherit-seed-creds=true` to auto-patch (triggers a rolling restart, which the restore waits out in `Pending`). With **workload identity** (no `credentialsSecretRef`), the SDK default chain is used — the IAM binding must then be on the **server pods'** ServiceAccount, since the seed fetch runs inside the Neo4j JVM, not in a Job.
3. Opens a Bolt session and runs `SHOW DATABASES` to detect whether the target database already exists.
4. Existing database → `CALL dbms.[cluster.]recreateDatabase($db, {seedURI: $uri})`. **Requires `spec.force: true` or `spec.options.replaceExisting: true`** — recreating wipes and replaces the live contents, so the operator refuses without the explicit opt-in (clear `Failed` message instead of a silent overwrite). Preserves user/role privileges, atomically swaps the database on every server, no `DROP` needed.
5. New database → `CREATE DATABASE $db OPTIONS { seedURI: '<file>' } WAIT` (no opt-in needed; nothing is overwritten).
6. `CREATE … WAIT` blocks until online, but `dbms.recreateDatabase` is **asynchronous** — it returns once the recreate is scheduled. The operator therefore polls `SHOW DATABASE` until every allocation reports `online` before marking the restore `Completed`. The poll deadline is **`spec.timeout`** (default **5m** when unset) — raise it for multi-GB stores seeded from object storage; on expiry the restore goes `Failed` with the database's last `statusMessage`.

**`Neo4jEnterpriseStandalone` target** — Kubernetes Job:

1. Spawns a restore Job that runs `neo4j-admin database restore --from-path=$(ls <dir>/<dbname>-*.backup | tail -1) <dbname>`. The shell substitution picks the latest run in the chain by default.
2. If `stopCluster: true`, the operator scales down the StatefulSet first and mounts `data-{name}-server-0` directly into the Job container for offline access. With `stopCluster: false` the operator **refuses to start the Job while any server pod is running** (it never writes into a live data volume) — so in practice standalone restores need `stopCluster: true` unless you have already scaled the instance down yourself.
3. After the Job succeeds, automatically runs `CREATE DATABASE <dbname>` (new) or `START DATABASE <dbname>` (existed but stopped) via Bolt.
4. `spec.options.preRestore` / `postRestore` hooks run **only on this path** (never for cluster targets): pre-restore hooks run **before** the instance is stopped (so Cypher like `CALL db.checkpoint()` hits a live Bolt endpoint); post-restore hooks run **after** the restored database is registered and started.

**`Neo4jShardedDatabase` target** — rejected. Sharded restore is owned by the `Neo4jShardedDatabase` CRD; set `spec.replaceExisting: true` + `spec.force: true` on the target sharded DB and reference the backup via `spec.seedBackupRef`. The Neo4jRestore validator emits an actionable error pointing at this flow.

**No manual post-restore Cypher is required** for either path — with one exception: if the backup was taken with `includeMetadata` (users/roles), `neo4j-admin database restore` writes a `restore_metadata.cypher` script next to the restored store, and the operator does **not** run it. Re-creating the database's users/roles is a manual step — run it against the `system` database, e.g.:

```bash
kubectl exec <instance>-server-0 -c neo4j -- bash -c \
  'cypher-shell -u neo4j -p "$NEO4J_PASSWORD" -d system \
     -f /data/scripts/<dbname>/restore_metadata.cypher'
```

### Retrying a finished restore

`Completed` and `Failed` are terminal **for the current spec generation**, not forever. To re-run a restore, **edit the spec** (any change that bumps `metadata.generation` — typically `source.backupRef` or `databaseName`): the operator clears the previous attempt's one-shot state and issues a fresh restore. Changing `source.backupRef` also invalidates the pinned `status.resolvedSource`, so the new reference is re-resolved rather than silently reusing the old snapshot. Alternatively, delete and recreate the CR.

## Neo4jRestore Spec

### Top-Level Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `clusterRef` | `string` | ✅ | Name of the `Neo4jEnterpriseCluster` or `Neo4jEnterpriseStandalone` to restore into. The controller detects the type automatically. |
| `source` | [`RestoreSource`](#restoresource) | ✅ | Source of the backup data to restore |
| `databaseName` | `string` | ✅ | Name of the Neo4j database to restore |
| `options` | [`RestoreOptionsSpec`](#restoreoptionsspec) | ❌ | Additional restore configuration options |
| `force` | `bool` | ❌ | Confirm restoring **over an existing database** (default: `false`). Standalone targets: passes `--overwrite-destination` to `neo4j-admin`. Cluster targets: required (or `options.replaceExisting: true`) before the operator issues the destructive `dbms.recreateDatabase` against an existing database. |
| `stopCluster` | `bool` | ❌ | **Standalone targets only** (cluster targets restore via Cypher and ignore it). `true` scales the instance down before the restore Job (mounting `data-{name}-server-0` directly) and scales it back up after. With `false`, the operator **refuses** to run the Job while any server pod is running — it never writes into a live data volume. |
| `timeout` | `string` | ❌ | Go duration (e.g. `"30m"`, `"2h"`). For **cluster** targets this bounds the online-convergence wait after `dbms.recreateDatabase` is issued (default **5m** when unset) — raise it for multi-GB stores seeded from object storage. |

**Target compatibility**: `clusterRef` can reference either:

- `Neo4jEnterpriseCluster` — for HA cluster restore operations
- `Neo4jEnterpriseStandalone` — for single-node restore operations

### RestoreSource

Defines the source of the backup to restore from.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | `string` | ✅ | Type of restore source. Valid values: `"backup"`, `"storage"`, `"pitr"`. |
| `backupRef` | `string` | ❌ | Name of a `Neo4jBackup` resource to restore from (used when `type="backup"`) |
| `storage` | [`StorageLocation`](#storagelocation) | ❌ | Direct storage location (used when `type="storage"`). The cloud backend — `s3`, `gcs`, `azure`, or `pvc` — is selected on `storage.type` inside this struct, *not* on the outer `source.type`. |
| `backupPath` | `string` | ❌ | Specific backup path within the storage location |
| `pointInTime` | `*metav1.Time` | ❌ | Recovery point in RFC3339 format; maps to `--restore-until` |
| `pitr` | [`PITRConfig`](#pitrconfig) | ❌ | Full PITR configuration (used when `type="pitr"`) |

**Valid `type` values:**

| Value | Description |
|-------|-------------|
| `"backup"` | Restore from a `Neo4jBackup` CR referenced by `backupRef` |
| `"storage"` | Restore from an explicit storage location in `storage`. The cloud backend is set via `storage.type` (`s3` / `gcs` / `azure` / `pvc`). |
| `"pitr"` | Point-in-time recovery using transaction log replay |

**Examples:**

```yaml
# Restore from a Neo4jBackup resource
source:
  type: backup
  backupRef: daily-production-backup

# Restore from an explicit S3 path. backupPath is relative to storage.path and
# must point at the exact .backup artifact (layout:
# <chain-root>/<dbname>-YYYY-MM-DDThh-mm-ss.backup — the chain root is the
# Neo4jBackup CR name; the filename is recorded on
# Neo4jBackup.status.history[*].artifactFilename).
source:
  type: storage
  storage:
    type: s3
    bucket: neo4j-backups
    path: production
    cloud:
      provider: aws
      credentialsSecretRef: aws-restore-credentials
  backupPath: daily-backup/mydb-2026-06-01T02-00-00.backup

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
```

> **Note:** `source.type: pitr` (the `--restore-until` path) applies only to a `Neo4jEnterpriseStandalone` target. For cluster point-in-time recovery, create a `Neo4jDatabase` with `spec.seedConfig.restoreUntil`. The operator rejects `source.type: pitr` against a cluster target with an actionable error.

### PITRConfig

Point-in-time recovery configuration for advanced restore scenarios.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `baseBackup` | [`BaseBackupSource`](#basebackupsource) | ❌ | Base backup to restore before applying transaction logs |
| `logStorage` | [`StorageLocation`](#storagelocation) | ❌ | Storage location for transaction logs |

### BaseBackupSource

Base backup configuration for PITR (avoids circular references).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | `string` | ✅ | Source type: `"backup"` or `"storage"` |
| `backupRef` | `string` | ❌ | Name of the `Neo4jBackup` resource (when `type="backup"`) |
| `storage` | [`StorageLocation`](#storagelocation) | ❌ | Direct storage location (when `type="storage"`) |
| `backupPath` | `string` | ❌ | Specific backup path within the storage location |

### RestoreOptionsSpec

Additional restore execution options.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `replaceExisting` | `bool` | ❌ | Replace an existing database (default: `false`). Equivalent confirmation to top-level `force` for cluster restores of an existing database. |
| `verifyBackup` | `bool` | ❌ | **RESERVED — currently a no-op.** Accepted for backward compatibility but not read by the operator. Verify artifacts at backup time via `Neo4jBackup.spec.options.validate` instead. |
| `additionalArgs` | `[]string` | ❌ | Additional arguments passed verbatim to `neo4j-admin database restore` |
| `tempPath` | `string` | ❌ | Local directory for temporary files during restore. When `tempStorage` is configured this is set automatically to the mount path; only set manually if you mount your own volume by other means. |
| `tempStorage` | [`TempStorageSpec`](#tempstoragespec) | ❌ | Provisions a PVC for temporary staging files during cloud restores. Without it, cloud restores use the container's ephemeral disk (may be too small for large databases). The operator mounts the PVC and passes `--temp-path` automatically. |
| `resources` | `corev1.ResourceRequirements` | ❌ | CPU/memory requests + limits on the restore Job's container. When unset, the operator applies a Burstable default (request 100m CPU / 512Mi memory, limit 1 CPU / 2Gi memory). **Standalone restores only** — cluster targets use the Cypher path (no Job) and ignore this field. |
| `preRestore` | [`RestoreHooks`](#restorehooks) | ❌ | **Standalone (Job-path) restores only** — never invoked for cluster targets. Executed **before the instance is stopped**, so Cypher hooks (e.g. `CALL db.checkpoint()`) hit a live Bolt endpoint. |
| `postRestore` | [`RestoreHooks`](#restorehooks) | ❌ | **Standalone (Job-path) restores only.** Executed after the restore Job succeeds **and** the restored database has been registered/started (`CREATE`/`START DATABASE`), so Cypher hooks target a live database. |

### TempStorageSpec

Provisions a temporary PVC for staging files during cloud restores.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `size` | `string` | ✅ | Size of the temporary PVC (e.g., `"50Gi"`). Should be at least as large as the expected backup/restore artifact. |
| `storageClassName` | `string` | ❌ | StorageClassName for the temporary PVC. If empty, uses the cluster default. |

### RestoreHooks

Hooks to run before or after the restore Job. **Hooks run only for `Neo4jEnterpriseStandalone` targets** — the cluster Cypher restore path never invokes them.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `job` | [`RestoreHookJob`](#restorehookjob) | ❌ | Kubernetes Job to run as a hook. The hook Job runs in its own pod — commands like `cypher-shell` must be pointed at the instance's service explicitly (e.g. `cypher-shell -a neo4j://<standalone>-service:7687 …`), otherwise they dial localhost inside the hook pod. |
| `cypherStatements` | `[]string` | ❌ | Cypher statements the operator executes over Bolt against the standalone instance (pre-restore: before the instance is stopped; post-restore: after the database is registered/started) |

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
| `endpointURL` | `string` | ❌ | Overrides the S3 API endpoint URL. Use to target S3-compatible stores such as MinIO, Ceph RGW, or Cloudflare R2. |
| `forcePathStyle` | `bool` | ❌ | Forces S3 path-style addressing (bucket name in the URL path rather than as a subdomain). Typically required for S3-compatible stores. |
| `identity` | [`*CloudIdentity`](#cloudidentity) | ❌ | Cloud identity configuration |

### CloudIdentity

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | `string` | ✅ | Identity provider: `"aws"`, `"gcp"`, `"azure"` |
| `serviceAccount` | `string` | ❌ | **RESERVED — currently a no-op.** Restore Jobs always run as the operator-managed `neo4j-restore-sa`. Bind your cloud IAM role via `autoCreate.annotations`. |
| `autoCreate` | [`*AutoCreateSpec`](#autocreatespec) | ❌ | Workload-identity annotations for the operator-managed ServiceAccount |

### AutoCreateSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | `bool` | ❌ | **RESERVED — currently a no-op.** The operator always ensures the ServiceAccount exists; only `annotations` is honored. |
| `annotations` | `map[string]string` | ❌ | Annotations applied to the operator-managed ServiceAccount on every reconcile |

## Neo4jRestore Status

### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `conditions` | `[]metav1.Condition` | Current conditions of the restore resource — see [Condition Types](#condition-types) |
| `phase` | `string` | Current phase of the restore operation |
| `message` | `string` | Human-readable message about the current state |
| `startTime` | `*metav1.Time` | When the restore operation started |
| `completionTime` | `*metav1.Time` | When the restore operation completed |
| `stats` | [`RestoreStats`](#restorestats) | Restore operation statistics |
| `backupInfo` | [`RestoreBackupInfo`](#restorebackupinfo) | Information about the backup that was restored |
| `resolvedSource` | [`ResolvedRestoreSource`](#resolvedrestoresource) | The concrete backup location this restore pinned the first time it resolved `source.backupRef`. From then on the restore reads this snapshot, so deleting the `Neo4jBackup` CR mid-restore doesn't break it. Cleared automatically if `spec.source.backupRef` changes (the new reference is re-resolved). |
| `observedGeneration` | `int64` | Generation of the most recently observed `Neo4jRestore` spec |

### ResolvedRestoreSource

| Field | Type | Description |
|-------|------|-------------|
| `backupRef` | `string` | The `Neo4jBackup` name this source was resolved from (provenance) |
| `storage` | [`StorageLocation`](#storagelocation) | Concrete storage location (PVC or cloud, credentials reference folded in) of the resolved backup |
| `backupPath` | `string` | The per-CR shared chain directory of the resolved most-recent Succeeded run |
| `artifactFilename` | `string` | Exact `.backup` filename of the resolved run — required by the cluster Cypher paths (cloud seedURI + PVC proxy), which seed from a single file |
| `resolvedAt` | `*metav1.Time` | When the `backupRef` was first dereferenced |

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
| `Pending` | A transient precondition isn't met yet — the target cluster/standalone CR doesn't exist yet, the referenced backup has no Succeeded run, or (cluster restores) the seed-credentials rollout / seed proxy is still in progress. The controller requeues and retries automatically. |
| `Running` | The restore Job (standalone) is executing, or the cluster Cypher recreate has been issued and the operator is polling for online convergence. |
| `Completed` | Restore completed successfully; the database is online. Terminal for the current spec generation — see [Retrying a finished restore](#retrying-a-finished-restore). |
| `Failed` | The restore failed (validation, Job failure, seed failure, or convergence timeout). Terminal for the current spec generation — bump the spec or recreate the CR to retry. |

### Condition Types

The operator maintains a single **`Ready`** condition, derived from `phase`: `Completed` → `True`; `Failed` → `False` (reason `Failed`); `Pending`/`Running` → `Unknown`. No other condition types are set.

## Post-Restore Database Bring-Up (standalone Job path)

After the restore Job completes successfully on a **standalone** target, the operator automatically issues a Cypher command to make the database available:

- **New database** (did not exist before): `CREATE DATABASE <dbname>`
- **Existing database** (was stopped for restore): `START DATABASE <dbname>`

This means the restore workflow is fully automated — you do not need to manually start the database after restore completes. The `status.phase` transitions to `Completed` only after the database bring-up (and any post-restore hooks) succeed.

Cluster targets don't have a separate bring-up step: the Cypher restore (`CREATE DATABASE … OPTIONS { seedURI } WAIT` / `dbms.recreateDatabase`) brings the database online as part of the restore itself, and the operator marks `Completed` only after every allocation reports `online`.

> **Note on the legacy multi-server re-seed**: older operator releases ran the restore Job against `data-{cluster}-server-0` on multi-server clusters and then called `dbms.[cluster.]recreateDatabase($db, {seedingServers: [server0]})` to force every server to re-seed from server-0. On current releases, **cluster targets never take the Job path** — they restore via the seedURI Cypher path, where every server seeds from the backup artifact in parallel and no re-seed step is needed. The re-seed code remains only as a non-fatal safety net on the Job path (which is now reachable only for standalone/single-server targets, where it is skipped).

## `stopCluster` and Offline Restore (standalone targets)

`spec.stopCluster` applies only to **standalone** targets — cluster targets restore online via Cypher and ignore it.

When `spec.stopCluster: true`:

1. The operator scales the target StatefulSet down to 0 replicas (recording the original replica count so re-entries don't lose it).
2. The restore Job is created with the actual server data PVC (`data-{name}-server-0`) mounted into the container, enabling direct offline file-level restore.
3. After the restore Job succeeds, the StatefulSet is scaled back up. If the restore fails after the scale-down (hook failure, Job-create failure), the operator scales the instance back up rather than leaving it stranded at 0 replicas.
4. The operator then issues `CREATE DATABASE` or `START DATABASE` as described above.

When `spec.stopCluster: false`, the operator treats the instance as **already quiesced** and **refuses to start the Job while any server pod is running** — restoring into a live data volume is invisible data loss. Use `stopCluster: true` unless you have scaled the instance down yourself.

## Examples

### Simple Restore from a Neo4jBackup Reference

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: simple-backup-restore
  namespace: neo4j
spec:
  clusterRef: test-cluster   # Neo4jEnterpriseCluster (Cypher path — no Job)
  databaseName: testdb
  source:
    type: backup
    backupRef: daily-test-backup   # References a Neo4jBackup resource
  options:
    replaceExisting: true   # required if "testdb" already exists on the cluster
  timeout: "30m"   # online-convergence budget for the cluster restore (default 5m)
```

### Restore from S3 (Static Credentials, Standalone)

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: dev-s3-restore
  namespace: development
spec:
  clusterRef: dev-standalone   # Neo4jEnterpriseStandalone (Job path — hooks run here)
  databaseName: dev-app
  source:
    type: storage
    storage:
      type: s3
      bucket: dev-neo4j-backups
      path: snapshots
      cloud:
        provider: aws
        credentialsSecretRef: aws-restore-credentials
    # Exact artifact: <chain-root>/<dbname>-<timestamp>.backup
    backupPath: dev-backup/dev-app-2026-06-01T10-30-00.backup
  options:
    replaceExisting: true
    postRestore:   # hooks run only on standalone targets
      cypherStatements:
        - "CALL db.awaitIndexes(60)"
        - "CREATE (:TestNode {restored: datetime()})"
  force: true
  stopCluster: true   # standalone Job restores need the instance stopped
  timeout: "30m"
```

### Restore from GCS (Static Credentials, Cluster)

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: gcs-restore
  namespace: neo4j
spec:
  clusterRef: analytics-cluster   # Neo4jEnterpriseCluster (Cypher path)
  databaseName: analytics-db
  source:
    type: storage
    storage:
      type: gcs
      bucket: neo4j-analytics-backups
      path: weekly
      cloud:
        provider: gcp
        credentialsSecretRef: gcs-restore-credentials
    # Cluster targets REQUIRE the exact .backup file (CloudSeedProvider seeds
    # a single database from one file — a directory path fails):
    backupPath: weekly-backup/analytics-db-2026-06-01T03-00-00.backup
  options:
    replaceExisting: true   # required: analytics-db already exists
  force: true
  timeout: "2h"   # online-convergence budget — raise for large stores
```

> For the cluster Cypher path the credentials Secret must also be projected onto the **server pods** via the cluster's `spec.extraEnvFrom` (or annotate the cluster with `neo4j.com/auto-inherit-seed-creds: "true"` and let the operator patch it).

### Restore from Azure Blob Storage (Cluster)

Cluster targets restore via Cypher — hooks, `additionalArgs` and `stopCluster` belong to the standalone Job path and are intentionally absent here.

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: enterprise-azure-restore
  namespace: neo4j
  labels:
    compliance: required
    environment: production
spec:
  clusterRef: enterprise-cluster   # Neo4jEnterpriseCluster (Cypher path)
  databaseName: customer-data
  source:
    type: storage
    storage:
      type: azure
      bucket: enterprise-backups   # Azure container name
      path: production
      cloud:
        provider: azure
        credentialsSecretRef: azure-restore-credentials
    # Exact .backup file, required for cluster targets:
    backupPath: nightly-backup/customer-data-2026-06-01T02-00-00.backup
  options:
    replaceExisting: true   # required: customer-data already exists
  force: true
  timeout: "6h"   # large store — generous online-convergence budget
```

### Point-in-Time Recovery (PITR)

> **Note:** `source.type: pitr` applies only to a `Neo4jEnterpriseStandalone` target. For cluster point-in-time recovery, create a `Neo4jDatabase` with `spec.seedConfig.restoreUntil`. The operator rejects `source.type: pitr` against a cluster target with an actionable error.

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: production-pitr-restore
  namespace: neo4j
spec:
  clusterRef: recovery-standalone   # Neo4jEnterpriseStandalone (PITR is standalone-only)
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
        path: production/logs
        cloud:
          provider: aws
          credentialsSecretRef: aws-restore-credentials
  options:
    replaceExisting: true
    preRestore:     # hooks run only on standalone targets, BEFORE the stop
      cypherStatements:
        - "CALL db.checkpoint()"
    postRestore:    # AFTER the restored database is registered/started
      cypherStatements:
        - "CALL db.awaitIndexes(600)"
        - "CALL dbms.security.clearAuthCache()"
  force: true
  stopCluster: true
  timeout: "4h"
```

### Offline Restore via stopCluster (Standalone)

`stopCluster` applies to **standalone** targets only — cluster targets restore online via Cypher and ignore it. When `stopCluster: true`, the operator scales the standalone to 0 and mounts its data PVC (`data-{name}-server-0`) directly into the restore Job for a cold/offline restore.

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: offline-restore
  namespace: neo4j
spec:
  clusterRef: reporting-standalone   # Neo4jEnterpriseStandalone
  databaseName: large-graph
  source:
    type: storage
    storage:
      type: s3
      bucket: neo4j-backups
      path: large-graph
      cloud:
        provider: aws
        credentialsSecretRef: aws-restore-credentials
    backupPath: nightly-backup/large-graph-2026-06-01T02-00-00.backup
  force: true
  stopCluster: true   # Scales the standalone to 0; mounts data-reporting-standalone-server-0
  timeout: "8h"
```

### Standalone Restore

`clusterRef` can reference a `Neo4jEnterpriseStandalone` resource. The controller detects the type automatically.

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: standalone-restore
  namespace: development
spec:
  clusterRef: dev-standalone   # Neo4jEnterpriseStandalone
  databaseName: app-db
  source:
    type: backup
    backupRef: dev-daily-backup
  options:
    replaceExisting: true
  force: true
  stopCluster: true   # required: with false the operator refuses while pods are running
  timeout: "30m"
```

### Cross-Cloud Disaster Recovery (Cluster)

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: cross-cloud-dr-restore
  namespace: disaster-recovery
spec:
  clusterRef: dr-cluster   # Neo4jEnterpriseCluster (Cypher path — no hooks, no Job)
  databaseName: critical-app
  source:
    type: storage
    storage:
      type: gcs
      bucket: primary-site-backups
      path: disaster-recovery
      cloud:
        provider: gcp
        credentialsSecretRef: gcs-dr-credentials
    # Exact .backup file, required for cluster targets:
    backupPath: dr-backup/critical-app-2026-06-01T02-00-00.backup
  options:
    replaceExisting: true
  force: true
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

# Monitor restore Job logs (standalone targets only — cluster targets restore
# via Cypher and create no Job; follow the operator log instead)
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
