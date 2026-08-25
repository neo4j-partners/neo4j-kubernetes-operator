# ADR-015 — Backup and restore execution architecture

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-08-25 |
| **Depends on** | [BDR-014](../business/backup-restore/014-backup-restore.md) — `Neo4jBackup` / `Neo4jBackupSchedule` / `Neo4jRestore` contract · [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md) — per-pool topology restore allocates against · [ADR-007](007-formation-and-bolt.md) — formation & Bolt · [ADR-008](008-finalizers-and-deletion.md) — finalizers · [ADR-013](013-neo4j-conf-directory-fragments.md) |
| **Constraints** | Enterprise online backup · cloud SDKs out of operator hot path · FR `NEO-2-013` / `NEO-2-014` · V2 |

---

## Context

[BDR-014](../business/backup-restore/014-backup-restore.md) fixes the API. This ADR decides **how the operator executes** a backup/restore: what runs `neo4j-admin`, where cloud credentials are consumed, how the reconcilers guarantee run-once semantics, and how restore is sequenced against a live cluster.

What is fixed and must not be redefined:

- **Three controllers** map to the three CRDs (CNPG: `BackupReconciler`, `ScheduledBackupReconciler`, restore path). Core `Neo4j` reconciler stays one ([cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md) D3).
- **Backup is online** via the backup listener (port 6362, `features.backup`) — one member's store is sufficient ([BDR-014](../business/backup-restore/014-backup-restore.md)).
- **Cloud SDKs stay out of the operator hot path** — mount credentials in the workload, not the manager ([cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md) D5; cloud identity → ADR-016).

**Restore is not symmetric to backup in a cluster.** A database is not a single store: [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md) allocates it across pool StatefulSets (N primaries + analytics/read secondaries), each with its own copy. So `neo4j-admin database restore` — a **single-server, offline** tool — is the wrong primitive for the common path. The cluster-native mechanism is **online seed-from-URI**: `CREATE OR REPLACE DATABASE <db> TOPOLOGY … OPTIONS {existingData:'use', seedURI:'s3://…'}` (or `dbms.recreateDatabase(<db>, {seedURI})` for an existing database), issued **once** — the DBMS allocates the database over `ENABLED` servers per its topology and **every allocation seeds itself from the URI**. The seed is read by the **Neo4j servers**, so the cloud credential must be on the workload pods, not on a Job.

The `system` database is the exception: it holds cluster metadata and topology, is replicated on every primary, and cannot be seed-restored. Full `system` / whole-cluster DR is the documented all-servers-offline runbook (`neo4j-admin dbms unbind-system-db` → `database dump system` → `database load system --overwrite-destination`), which requires cluster-wide downtime.

**Forces / what breaks if we choose wrong:**

- Running `neo4j-admin` *inside* the operator pod drags cloud SDKs + Neo4j tooling into the manager image and its RBAC/network blast radius.
- A backup that is not idempotent re-runs on GitOps re-apply and corrupts artifacts or double-bills object storage.
- A restore that races formation ([ADR-007](007-formation-and-bolt.md)) seeds a database before servers are `ENABLED` and quorum is present → wrong/failed allocation.
- Restoring per-server (offline `neo4j-admin restore` on one pod) in a cluster leaves the other allocations stale → split store, unsafe.
- Automating `system` restore = orchestrating cluster-wide downtime + `unbind-system-db` on every member — high blast radius, easy to leave the cluster unbootable.

---

## Analysis

### Option A — One Kubernetes `Job` per run (neo4j image), credentials mounted in the Job — **chosen**

`BackupReconciler` creates a `batch/v1` Job owner-referenced by the `Neo4jBackup`. The Job runs the Neo4j image, invokes `neo4j-admin database backup` against the target's backup listener, and pushes artifacts to object storage using credentials mounted **into the Job** (Secret or projected workload-identity token). The operator only creates/watches the Job and mirrors its terminal state into `status.phase`. This is the CNPG barman-cloud pattern.

| Advantages | Disadvantages |
|------------|---------------|
| Cloud SDK + `neo4j-admin` stay out of the operator image and RBAC | Operator needs `batch/jobs` create/watch RBAC |
| Job = natural run-to-completion unit; ownerRef GC cleans it up | Job scheduling latency on top of backup time |
| Credentials scoped to the ephemeral Job pod, not the manager | Must map Job phases → CRD `status.phase` |
| Job shape also covers standalone / designated-seeder restore | Cluster restore is not a Job — uses Bolt seed-from-URI (Decision) |

### Option B — Exec into a running Neo4j pod (`pods/exec`)

Reconciler runs `neo4j-admin` via `pods/exec` in an existing member pod, streaming to object storage from that pod.

| Advantages | Disadvantages |
|------------|---------------|
| No extra pod to schedule | `pods/exec` is a broad, audited privilege on the operator SA |
| Reuses the live pod's mounts | Backup CPU/IO competes with live query traffic |
| — | Cloud creds must live on the *workload* pod permanently, widening its trust surface |
| — | No clean run-to-completion object; harder status/retry model |

### Option C — Always-on backup sidecar in every member pod

A sidecar container in each `Neo4j` pod performs scheduled backups locally.

| Advantages | Disadvantages |
|------------|---------------|
| No Job scheduling latency | Sidecar runs 24/7 for an occasional task — wasted resources |
| — | Couples backup lifecycle to workload rollout ([ADR-013](013-neo4j-conf-directory-fragments.md)) |
| — | Cloud creds pinned to every member pod permanently |
| — | Diverges from CNPG; no per-run record |

---

## Comparison

| Criterion | A — Job per run (chosen) | B — pods/exec | C — sidecar |
|-----------|--------------------------|---------------|-------------|
| Operator image / RBAC blast radius | **small** | large (`pods/exec`) | medium |
| Credential scope | ephemeral Job | permanent on workload | permanent on workload |
| Run-to-completion record | native | synthesized | none |
| Isolation from live traffic | **best** | poor | poor |
| controller-runtime fit | **high** (owned Job) | medium | low |
| V1/V2 fit | **yes** | no | no |

---

## Decision

We will use **Option A** — one Kubernetes `Job` per **backup** run — and drive **restore** through **online seed-from-URI over Bolt** ([ADR-007](007-formation-and-bolt.md)), because a clustered database is not a single restorable store (see Context).

### Controllers and objects

| Controller | Watches | Executes via | Terminal record |
|------------|---------|--------------|-----------------|
| `BackupReconciler` | `Neo4jBackup` | one `Job`, ownerRef → `Neo4jBackup` (`neo4j-admin database backup`) | mirrors Job → `status.phase`, fills `status.artifacts[]` |
| `ScheduleReconciler` | `Neo4jBackupSchedule` | emits `Neo4jBackup` objects on cron; prunes per `retention` | updates `status.lastScheduleTime` / `lastBackup` |
| `RestoreReconciler` | `Neo4jRestore` | **Bolt** `CREATE OR REPLACE DATABASE … OPTIONS {seedURI}` / `dbms.recreateDatabase` per database ([ADR-007](007-formation-and-bolt.md)); DBMS seeds all allocations | mirrors `SHOW DATABASES` → `status.phase` |

### Idempotency (GitOps re-apply safe)

```go
// sketch — not binding
func (r *BackupReconciler) Reconcile(ctx, req) (Result, error) {
    b := getNeo4jBackup(req)
    if b.Status.Phase == Succeeded || b.Status.Phase == Failed {
        return Done // immutable record — never re-run
    }
    job := findOwnedJob(b)
    if job == nil {
        job = r.buildBackupJob(b) // resolves neo4jRef, backup listener addr, destination, creds
        return r.create(job)
    }
    return r.mirrorJobStatus(b, job) // Pending|Running|Succeeded|Failed
}
```

`status.phase` is the run-once guard: a `Succeeded`/`Failed` record never spawns a second Job.

### Credentials / cloud identity (delegated)

- `spec.destination.credentials.secretName` set → project that Secret into the Job pod only.
- omitted → assume **workload identity** (IRSA / GKE WI / Azure WI) via the Job pod's ServiceAccount. The token/annotation plumbing is owned by **ADR-016**; this ADR only guarantees credentials never touch the operator pod.

### Restore sequencing (against a live cluster)

1. Admission (webhook, [ADR-001](001-crd-validation-process.md)) checks target `neo4jRef` exists, edition is `enterprise`, `source` is well-formed, and **rejects `system` in `databases`** (see below). **No Bolt from the webhook.**
2. Reconciler waits for the target to be formation-stable — every member `ENABLED`, quorum present, `ClusterFormed` ([ADR-007](007-formation-and-bolt.md)) — before touching any database. A restore before formation would allocate over an incomplete server set.
3. Per user database, over Bolt (one statement, DBMS distributes):
   - **new database** → `CREATE DATABASE <db> TOPOLOGY <p> PRIMARIES <s> SECONDARIES OPTIONS {existingData:'use', seedURI:'…'}`.
   - **existing database** → `dbms.recreateDatabase("<db>", {seedURI:"…"})` (or `CREATE OR REPLACE DATABASE`) — replaces the store on **all allocations**; the database is unavailable while it re-seeds, but the DBMS stays up.
   - `TOPOLOGY` is derived from the target's pools ([BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md)); the backup's original topology is not assumed.
4. The **Neo4j workload pods** read `seedURI` using their cloud identity (ADR-016) — the operator passes only the URI, never the object bytes. No per-run Job is created for cluster restore.
5. Poll `SHOW DATABASES` until each restored database is `online` on its allocations → `status.phase=Succeeded`; partial failure is reported per database.

**Standalone** may use the same seed-from-URI, or an offline `neo4j-admin database restore` Job (single store, no distribution concern).

**`system` / whole-cluster DR is out of scope for automated `Neo4jRestore` in the first release** — it requires cluster-wide downtime and `unbind-system-db` on every member (see Context). `ponytail:` documented manual runbook for now; the upgrade path is a future guarded maintenance-mode flow (ties into [ADR-008](008-finalizers-and-deletion.md) and offline maintenance) once the online per-database path is proven.

### Finalizers

- `Neo4jBackup` / `Neo4jRestore` records: **no finalizer** — deleting the record must not delete cloud artifacts (retention owns pruning). Owned Jobs GC via ownerRef.
- `Neo4jBackupSchedule`: finalizer only if `retention` must prune object-store artifacts on schedule deletion (opt-in), consistent with [ADR-008](008-finalizers-and-deletion.md)'s "separate controller may add cross-finalizer ordering."

### RBAC delta

Operator SA gains `batch/jobs` create/watch/delete and read on referenced backup `Secrets` (`resourceNames`-scoped where possible), plus the three new CRD groups. Detailed rules land in ADR-013 (workload RBAC).

---

## Consequences

### Positive

- Operator image and RBAC stay lean; cloud SDKs and `neo4j-admin` execution live in ephemeral Jobs.
- Backups are auditable owned Jobs with a clean run-to-completion record and GitOps re-apply safety.
- Backup IO is isolated from live query traffic (own pod).
- **Cluster restore is correct by construction** — one seed-from-URI statement lets the DBMS distribute and seed all allocations per topology; the operator never touches individual pod stores. Reuses ADR-007 Bolt/formation gating.

### Negative

- Adds `batch/jobs` to the operator RBAC (backup) plus Bolt admin verbs (restore), and three controllers to maintain.
- Backup and restore take **different execution paths** (Job vs Bolt) — two code paths, not one.
- Restore credentials are the **workload's** cloud identity (ADR-016 on the Neo4j pods), not a Job Secret — a different trust surface to document.
- `system` / whole-cluster DR is **not automated** in the first release; users follow a manual runbook.

### Neutral

- Backup Job image = the Neo4j image by default; a slimmer `neo4j-admin`-only image is a future optimization.
- `differential` backup and continuous/PITR are out of scope; the Job model extends to them later without a controller redesign.
- Designated-seeder restore (`neo4j-admin database restore` on one server, then `seedingServers`) remains available as a fallback if seed-from-URI is unavailable for a source.

---

## References

- [BDR-014](../business/backup-restore/014-backup-restore.md) — CRD contract (this ADR implements it)
- [ADR-001](001-crd-validation-process.md) · [ADR-007](007-formation-and-bolt.md) · [ADR-008](008-finalizers-and-deletion.md) · [ADR-013](013-neo4j-conf-directory-fragments.md) · ADR-016 (cloud identity, backlog)
- [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md) — per-pool StatefulSets / topology that restore must allocate against
- CloudNativePG `BackupReconciler` / `ScheduledBackupReconciler`, barman-cloud in Jobs — [cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md) D3, D5, D10
- [Neo4j — backup and restore](https://neo4j.com/docs/operations-manual/current/backup-restore/) · [`neo4j-admin database backup`](https://neo4j.com/docs/operations-manual/current/backup-restore/online-backup/)
- [Neo4j — seed a cluster database from URI](https://neo4j.com/docs/operations-manual/current/clustering/databases/) · [recreate a database](https://neo4j.com/docs/operations-manual/current/database-administration/standard-databases/recreate-database/) · [cluster disaster recovery](https://neo4j.com/docs/operations-manual/current/clustering/multi-region-deployment/disaster-recovery/)
