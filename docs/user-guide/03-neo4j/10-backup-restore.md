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
      secretName: aws-backup-creds   # per-backup static keys; omit to use the instance's
                                      # spec.security.cloudIdentity (see Credentials for object stores)
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

#### How chains are laid out (you never name them)

To keep two chains from co-mingling in one destination — which would let an increment attach to the
wrong full, or an aggregate trip over another chain's files — each chain is written into its own
sub-directory/prefix. **You never type a chain id**; the operator generates one, because the same
string must be a valid object-store key, a label value, *and* a Kubernetes object name at once:

- **Scheduled** backups use the schedule's generated chain (`<schedule>-<time>`).
- **Manual** backups get a **daily chain per target**: `<neo4jRef>-<UTCdate>` (e.g.
  `my-neo4j-20260910`). The day's first `Full` anchors it under `<destination>/<neo4jRef>-<date>/`,
  and any `Incremental` you take **the same UTC day** lands in the same place automatically and
  extends it. A new day starts a new chain — so the natural rhythm is **one full per day, then
  incrementals**. (Take the full *before* the day's incrementals; an incremental on a day with no
  full yet fails with the "no existing backup found" message above.)

So to run several independent chains by hand, you don't label anything — you just let each day be its
chain, or point each chain at a distinct `destination.url`. Restore and aggregate always resolve the
exact folder from the backup's recorded `status.artifacts[].uri`, so the layout is transparent to
you. (A wildcard-database PVC backup is the one exception that stays flat — it has no per-database
seed path to record.)

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

The `destination` can also be an **object store** — the aggregate runs in the bucket directly
(`neo4j-admin backup aggregate --from-path=s3://…`, no volume), authenticating with
`destination.credentials` or the target's `spec.security.cloudIdentity` workload identity:

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackup
metadata: { name: prod-monday-agg }
spec:
  neo4jRef: { name: prod }
  databases: ["neo4j"]
  type: Aggregate
  destination:
    type: s3                       # or gcs / azure
    url: s3://my-bucket/neo4j/prod/
    credentials: { secretName: s3-creds }   # omit to use workload identity
  source:
    backupRef: prod-monday-tip
```

Object-store aggregate records the **folder url** (not a recovered filename) on the resulting
`Neo4jBackup`, so a restore that references it seeds that folder — which now recovers to the
aggregated full. All requested databases must live under the same url (mixing stores or urls is
rejected). Requires S3 ≥ 5.19, GCS ≥ 5.21, or Azure ≥ 5.24 (MinIO via `AWS_ENDPOINT_URL_S3`).

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

> Scheduled backups are isolated per chain in their own sub-directory/prefix (on PVC and object
> stores alike) so an aggregation of one chain can never make a later increment of another chain
> mis-parent onto it. Ad-hoc backups are isolated the same way, by their daily chain (see
> [How chains are laid out](#how-chains-are-laid-out-you-never-name-them)).

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
```

Object-store reads use the **target's** `spec.security.cloudIdentity` — a restore has no credentials
field of its own, because the seed is pulled by the server pods, not a Job (see Credentials for
object stores). `file:` and `server:` URLs are credential-free and need no `type`. Exactly one of
`backupRef` / `url` is allowed.

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

Object-store access (`s3`, `gcs`, `azure`) has a **write** side (the backup Job) and a **read** side
(the server pods that seed a restore). Both draw on the instance's identity, declared once on the
`Neo4j` as `spec.security.cloudIdentity` ([ADR-016](../../design/decision-records/architecture/016-cloud-identity.md)):

```yaml
kind: Neo4j
spec:
  security:
    cloudIdentity:                          # exactly one of the two below
      # Keyless — recommended. The operator stamps these annotations onto the operand and
      # <neo4j>-backup ServiceAccounts; the cloud platform binds them to an IAM role.
      workloadIdentity:
        provider: aws                       # aws | gcp | azure
        annotations:
          eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/neo4j-backups
      # Portable / MinIO-testable alternative — a Secret projected as env into both sides.
      # staticKeySecret: cloud-creds
```

- **Workload identity is instance-level, not per-backup** — cloud IAM trust binds to a fixed
  ServiceAccount, so it can't vary between `Neo4jBackup` objects. `provider: azure` also adds the
  `azure.workload.identity/use` pod label and takes the client id from
  `annotations["azure.workload.identity/client-id"]`.
- **Per-backup static keys** — set `Neo4jBackup.destination.credentials.secretName` to a Secret whose
  keys (`AWS_ACCESS_KEY_ID`, `GOOGLE_APPLICATION_CREDENTIALS`, …) are projected verbatim into that
  Job. This overrides the instance identity for the write side of a single backup.
- **Restore reads have no credentials field** — they always use the target's `cloudIdentity`.

### How workload identity works (the 60-second version)

Workload identity means **no keys stored anywhere**. Think of it as a passport and a visa:

1. **Passport** — Kubernetes gives each pod a short-lived, signed token that says *"I am
   ServiceAccount `<namespace>:<name>`."* The cloud webhook only injects it when the pod carries the
   right marker (for Azure, the `azure.workload.identity/use` label — the operator adds it for you).
2. **Visa** — in the cloud, you tell the identity provider *once*: "trust passports whose name
   (**subject**) is exactly `system:serviceaccount:<namespace>:<sa-name>`, and let them become this
   cloud identity." (AWS: IRSA trust policy; GCP: IAM binding; Azure: a *federated credential*.)
3. **Keycard** — you grant that cloud identity real permission on the bucket (e.g. `Storage Blob
   Data Contributor`).

At runtime the pod shows its passport, the cloud checks the visa (**the subject must match to the
character**), hands back a real cloud token, and the token opens the bucket. Nothing to rotate.

**What the operator wires for you** (from `spec.security.cloudIdentity`): a dedicated
`<neo4j>-backup` ServiceAccount for backup Jobs and the operand ServiceAccount for the server pods,
both stamped with your `workloadIdentity.annotations`, plus the Azure pod label. **What you do once
in the cloud**: create the identity, grant it bucket access, and add the visa (trust) for each SA
subject.

### Setup checklist (do this once per instance)

Two pods touch the bucket, so there are **two subjects to trust** — this is the #1 gotcha:

| Pod | ServiceAccount (subject) | Needed for |
|-----|--------------------------|-----------|
| Backup Job | `<namespace>:<neo4j>-backup` | backup **writes** |
| Neo4j server | `<namespace>:<neo4j>` | restore **reads** (seed-from-URI) |

1. Create the cloud identity and grant it read/write on the bucket.
2. Add the trust/visa for **both** subjects above (a restore that only federates the backup SA fails
   with "not a valid location" — the server pod's passport matches no visa).
3. Put `spec.security.cloudIdentity.workloadIdentity` on the `Neo4j` with the provider and its
   annotation (e.g. the cloud identity's client id / role ARN). The operator does the rest.

> Common gotchas: **subject typos** (must match exactly, including the `-backup` suffix), and running
> an **older operator** that doesn't stamp the server-pod label yet (restore reads then get no token).
> Verify with `kubectl get pod <pod> -o jsonpath='{.metadata.labels}'` (Azure: expect
> `azure.workload.identity/use=true`).

See [`examples/standalone/25-cloud-identity.yaml`](../../../examples/standalone/25-cloud-identity.yaml)
and the [Azure Workload Identity backup + restore runbook](../../../examples/standalone/25-cloud-identity-azure-backup.md)
for a full, copy-pasteable walkthrough.

## What is not covered

- **`system` / whole-cluster disaster recovery** is out of scope for automated `Neo4jRestore` in this
  release: it needs cluster-wide downtime and per-member `unbind-system-db`. It is a documented
  manual runbook.
- **Per-increment retention** — by design; see [Scheduling](#scheduling-backups).
- **Dynamic provisioning of a PVC backup destination** — provide an existing claim for now.

## Next

[Storage](03-storage.md#auxiliary-volumes) · [Security](05-security.md) ·
[API reference](../05-reference/api.md) · [Error reference](../05-reference/errors.md)
