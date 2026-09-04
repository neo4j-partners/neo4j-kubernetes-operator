# Backup and restore

Three resources, each referencing a Neo4j in the same namespace by `spec.neo4jRef.name`:

| Resource | Shape | What it does |
|----------|-------|--------------|
| `Neo4jBackup` | One-shot, immutable record | Runs one `neo4j-admin database backup` and records where the artifacts landed |
| `Neo4jBackupSchedule` | Cron and chain owner | Emits `Neo4jBackup` objects on a schedule, owns the chain, and prunes old ones |
| `Neo4jRestore` | One-shot, immutable record | Seeds one or more databases from a backup into a running Neo4j |

A backup and a restore are *records*, not commands: the spec is immutable, and re-applying a
`Succeeded` object is a no-op. To take another backup, create another object.

## Prerequisites

**Backups need the backup listener.** The operator runs `neo4j-admin` from a Job that dials the
target's backup port through a derived admin Service, so the target must enable it:

```yaml
spec:
  features:
    backup:
      enabled: true
  connectivity:
    listeners:
      backup: 6362
```

**Restores need an admin Bolt path.** The operator seeds a database over its admin Bolt session
(`CREATE DATABASE … OPTIONS {seedURI…}`), which requires either verified TLS or an explicit opt-in
to plaintext — the same [NEO-004](../05-reference/errors.md) rule the rest of the operator uses:

```yaml
spec:
  trust:
    enabled: false
    insecureAdminConnection: true   # dev/kind; prefer trust.certificates.bolt in production
```

**The PVC round-trip also needs the destination mounted as the backups volume** — see
[The PVC round-trip](#the-pvc-round-trip) below.

## Taking a one-off backup

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: nightly-2026-09-05
spec:
  neo4jRef:
    name: prod
  databases: ["neo4j"]        # omit for ["*"] — all databases, including system
  type: Auto                  # Full | Incremental | Auto (default) | Aggregate
  destination:
    type: s3
    url: s3://my-bucket/neo4j/prod/
    credentials:
      secretName: aws-backup-creds   # omit to use workload identity
  options:                    # all optional neo4j-admin passthrough
    compress: true
    keepFailed: false
    verbose: false
    includeMetadata: all      # none | all | users | roles (ignored for system)
```

The reconciler creates a Job, watches it, and records the outcome on `status`:

```bash
kubectl get n4jb nightly-2026-09-05 -o wide
# NAME                 CLUSTER   TYPE   PHASE       CHAIN                  AGE
# nightly-2026-09-05   prod      Auto   Succeeded   prod-20260905-0100     40s
```

`status.artifacts[]` lists one entry per database with the exact object written (`uri`), the real
filename (`path`), and `sizeBytes`. On failure, `status.reason` carries a stable machine reason and
`status.message` the tail of the `neo4j-admin` output — so "differential without a full" or an
unwritable destination shows up verbatim instead of a generic "backoff limit exceeded".

### Backup types and chains

`type` maps to `neo4j-admin --type`, with one deliberate rename:

| Type | Meaning |
|------|---------|
| `Full` | A complete, self-contained backup — anchors a chain |
| `Incremental` | A differential on top of the current chain (Neo4j's on-disk "differential") |
| `Auto` | Full if no chain exists yet, otherwise incremental — safe to start cold |
| `Aggregate` | Not a live backup: collapses an existing chain into one recovered full |

We call it **`Incremental`, not "differential"**, because the artifacts form a *dependent chain*: a
restore needs the whole chain (full + every later increment), never "full + latest". Keep that in
mind when you prune or move artifacts by hand — dropping a middle link breaks every link after it.

An incremental with no full in the destination fails clearly rather than silently:

```
Differential backups require that a full backup of the same database exists in the folder
defined in --to-path. No existing backup found here: /destination
```

Use `type: Auto` to avoid that entirely — it self-seeds a full on the first run.

### Aggregating a chain ad hoc

`type: Aggregate` collapses a chain into a single **recovered full** so a later restore seeds one
artifact instead of replaying the whole chain (lower restore time). It points at any link of the
chain via `spec.source.backupRef` and runs `neo4j-admin backup aggregate`:

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: prod-monday-recovered
spec:
  neo4jRef: { name: prod }
  databases: ["neo4j"]
  type: Aggregate
  destination:
    type: pvc
    pvc: { claimName: prod-backups }
  source:
    backupRef: prod-monday-tip   # the chain's last link (or its full)
```

Aggregation always keeps the original chain (`--keep-old-backup=true`); the recovered full is a
first-class, restorable `Neo4jBackup` in its own right. The [schedule](#scheduling-backups) can do
this automatically at chain boundaries.

## Scheduling backups

`Neo4jBackupSchedule` owns two independent cron cadences and the chain they build. It emits ordinary
`Neo4jBackup` objects (owner-referenced by the schedule), so everything above applies to each one.

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackupSchedule
metadata:
  name: prod-backups
spec:
  neo4jRef: { name: prod }
  suspend: false
  full:
    schedule: "0 1 * * 0"      # Sunday 01:00 — anchors a new chain
    retention:
      keepLast: 4              # keep the last 4 whole chains (or keepDays: N, not both)
  incremental:
    schedule: "0 1 * * 1-6"    # Mon–Sat 01:00 — attaches to the current chain
  aggregate:
    enabled: true              # compact each closed chain into one recovered full
  backupTemplate:              # the inline Neo4jBackup spec every cadence emits
    databases: ["neo4j"]
    destination:
      type: pvc
      pvc: { claimName: prod-backups }
```

Emitted backups are named deterministically from the chain and cadence (`<schedule>-<chain>-f` for
the full, `-i` for increments, `-agg` for the aggregate). `status` tracks `currentChain`,
`lastFullTime`, `lastIncrementalTime`, and `lastBackup`:

```bash
kubectl get n4jbs prod-backups -o jsonpath='{.status.currentChain}{"\n"}'
kubectl get n4jb -l neo4j.com/chain=prod-20260905-0100   # every link of one chain
```

**Retention is whole-chain only.** `full.retention` keeps the last N chains (`keepLast`) or chains
younger than N days (`keepDays`) and prunes older ones *entirely* — files first, then the records.
There is no per-increment retention: deleting a mid-chain link would break every later link's
restore, so within-chain growth is bounded by the aggregate cadence and by starting a fresh chain,
not by dropping links.

**Aggregate compaction is boundary-triggered.** With `aggregate.enabled: true`, when a new full
closes the previous chain, the schedule waits for that closed chain to quiesce (every link
`Succeeded`), emits an `Aggregate` backup for it, and — only once the recovered full is verified and
cataloged — prunes the chain's original links. This **preserve-then-clean** order guarantees a chain
is never left without a restorable artifact. The active chain is never touched.

Set `suspend: true` to pause every cadence without deleting the schedule or its history.

> On a PVC destination, scheduled backups are isolated per chain in their own sub-directory so an
> aggregation of one chain can never make a later increment of another chain mis-parent onto it.
> Ad-hoc `Neo4jBackup` objects stay flat.

## Restoring

A `Neo4jRestore` seeds one or more **user** databases into a running Neo4j. `system` cannot be
restored this way — whole-cluster disaster recovery is a manual runbook (see
[what is not covered](#what-is-not-covered)).

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: restore-neo4j
spec:
  neo4jRef: { name: prod }
  databases: ["neo4j"]        # user databases only; "*" means all user databases
  source:
    backupRef: nightly-2026-09-05   # a Neo4jBackup in this namespace (recommended)
  overwrite: true             # required to replace a database that already exists
  forceOffline: true          # stop it first to fence writers, then restart (needs overwrite)
  restoreMetadata: false      # reapply users/roles/privileges — see below
```

**Point at a `Neo4jBackup` with `source.backupRef`** and the operator resolves the location and
walks the chain for you. Reference the chain's **latest link** to restore the most recent
point-in-time — Neo4j replays the whole full→increment chain from there. (Referencing an aggregate's
recovered full restores that collapsed point instead.)

For an external or hand-made artifact, use a raw `source.url` with a `source.type` instead:

```yaml
  source:
    type: s3
    url: s3://my-bucket/neo4j/prod/neo4j-2026-09-05T01-00-00.backup
    credentials: { secretName: aws-backup-creds }
```

`file:` and `server:` URLs are credential-free and need no `type`. Exactly one of `backupRef` /
`url` is allowed.

**`overwrite` and `forceOffline` are the safety gates.** A restore onto an existing database fails
(`reason=DatabaseExists`) unless `overwrite: true`, because recreating a database destroys the
current store. `forceOffline` additionally stops the database first to fence in-flight writes and
requires `overwrite`. `status.databases[]` reports per-database progress; the run is `Succeeded`
only when every database is back online.

### Reapplying users, roles, and privileges

A seed-from-URI restore carries **store data only** — it does not reapply the backed-up security
metadata. Set `restoreMetadata: true` to run a post-seed Job that regenerates Neo4j's
`restore_metadata.cypher` from the artifact and applies it to the system database:

```yaml
spec:
  source: { backupRef: nightly-2026-09-05 }
  databases: ["neo4j"]
  overwrite: true
  restoreMetadata: true
```

This is supported only for a **PVC-backed `source.backupRef` the target mounts as its backups
volume** (the Job needs filesystem access to the artifact); other sources are rejected. A statement
that clashes with a role or user that already exists on the target is skipped with a Warning event,
and the restore still Succeeds. When the target's Bolt listener uses TLS, the Job connects encrypted
(`neo4j+ssc`).

## The PVC round-trip

A PVC destination keeps artifacts in-cluster, and a restore can seed straight off the filesystem with
no cloud credentials — but only when the **target mounts the same claim** the backup wrote to, as its
`storage.volumes.backups`:

```yaml
# The backup writes here…
kind: Neo4jBackup
spec:
  destination: { type: pvc, pvc: { claimName: prod-backups } }
---
# …and the restore target mounts the same claim as its backups volume, so the artifact is on the
# server's filesystem at /backups and can be seeded as file:/backups/<artifact>.
kind: Neo4j
spec:
  storage:
    volumes:
      backups:
        mode: Existing
        existing:
          claimName: prod-backups
```

With that in place the operator seeds `file:/backups/<artifact>` and auto-enables Neo4j's
`FileSeedProvider`. Things to know:

- **Provide an existing `pvc.claimName`.** Dynamic provisioning of a backup destination is not yet
  wired, so the claim must already exist.
- **Shared storage must be `ReadWriteMany` *and* POSIX-compliant** for anything beyond a single-node
  standalone. A cluster seeds every member from the one claim, so it needs RWX. Azure Files
  **SMB/CIFS is not POSIX** and fails artifact creation (`Operation not permitted`) — use **Azure
  Files NFS v4.1** (or another POSIX RWX class). `ReadWriteOnce` (e.g. Azure Disk) is fine only for a
  single-pod standalone. Object-store destinations sidestep this entirely.
- **Restore points at the real artifact, not a renamed pointer.** To restore the latest
  point-in-time, reference the latest backup in the chain, not the anchoring full.

## Credentials for object stores

Object-store destinations (`s3`, `gcs`, `azure`) authenticate one of two ways:

- **Workload identity** (recommended) — omit `credentials` entirely and let the pod assume a cloud
  identity (IRSA / GKE WI / Azure WI). Full support across providers is tracked under ADR-016.
- **A Secret** — `credentials.secretName` names a Secret in the same namespace whose keys
  (`AWS_ACCESS_KEY_ID`, `GOOGLE_APPLICATION_CREDENTIALS`, …) are projected verbatim into the Job.

## What is not covered

- **`system` / whole-cluster disaster recovery** is out of scope for automated `Neo4jRestore` in this
  release: it needs cluster-wide downtime and per-member `unbind-system-db`. It is a documented
  manual runbook.
- **Per-increment retention** — by design; see [Scheduling](#scheduling-backups).
- **Dynamic provisioning of a PVC backup destination** — provide an existing claim for now.

## Next

[Storage](03-storage.md#auxiliary-volumes) · [Security](05-security.md) ·
[API reference](../05-reference/api.md) · [Error reference](../05-reference/errors.md)
