# BDR-014 — Backup and restore CRDs: `Neo4jBackup`, `Neo4jBackupSchedule`, `Neo4jRestore`

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-08-25 |
| **Depends on** | [BDR-001](../neo4j/001-single-neo4j-crd.md) — single `Neo4j` workload CRD, day-2 satellites via `neo4jRef` (accepted) · [BDR-005](../neo4j/005-storage-volume-mode.md) — `spec.volumes.backups` aux volume (accepted) · [BDR-010](../neo4j/010-neo4j-features-catalog.md) — `features.backup` gate (accepted) |
| **Related** | [BDR-013](../database/013-database.md) — databases by name, backup/restore prioritized as a dedicated domain (proposed) · [ADR-015](../../architecture/015-backup-and-restore.md) — execution architecture |
| **Constraints** | Enterprise only · FR `NEO-2-013` (backup) / `NEO-2-014` (restore) · V2 deliverable |
| **Reference** | [`crd-candidates.md`](../../../analysis/helm-fields/crd-candidates.md) · [`spec.md` `features.backup`](../../../crd-spec/neo4j/spec.md) · [`volumes.backups.md`](../../../analysis/helm-fields/fields/volumes.backups.md) |

---

## Context

[BDR-013](../database/013-database.md) already settled the *shape of the domain*: backup and restore are a **dedicated CRD family** (not an `action:` of the generic task CR), scoped to the DBMS (`databases: ["*"]`, `system` included), following the Kubernetes **Job / CronJob** idiom and the CloudNativePG `Backup` + `ScheduledBackup` precedent. This BDR fixes the **customer-facing contract** of that family: which `kind`s exist, what a user declares in YAML, where artifacts land, and what is in / out of the first backup release.

What is already fixed by accepted decisions (cited, not redefined here):

- **`features.backup.enabled`** gates Enterprise online backup; requires `connectivity.listeners.backup`; backup TLS via `spec.trust`; on-pod staging via `spec.volumes.backups` ([BDR-010](../neo4j/010-neo4j-features-catalog.md), `spec.md`).
- **Community edition refuses `features.backup`** — backup/restore is Enterprise only ([BDR-010](../neo4j/010-neo4j-features-catalog.md)).
- **Databases are referenced by name** (string, `["*"]`) everywhere — no `Neo4jDatabase` CR ([BDR-013](../database/013-database.md)).

### Neo4j mechanism (the contract must map to this)

| Operation | Neo4j command | Scope |
|-----------|---------------|-------|
| Online backup | `neo4j-admin database backup <db>` against the **backup listener** (port 6362) | per database; one member's store suffices |
| Restore / seed (cluster) | `CREATE OR REPLACE DATABASE <db> TOPOLOGY … OPTIONS {seedURI}` · `dbms.recreateDatabase(<db>, {seedURI})` | per user database, online — DBMS seeds **all allocations** |
| Restore (standalone) | `neo4j-admin database restore <db>` into a stopped/empty database | single server, offline |
| `system` / whole-cluster DR | all-servers-offline `unbind-system-db` → `dump system` → `load system` | cluster-wide downtime |

Backup is **online** (no downtime, Enterprise). In a **cluster** a user database is allocated across pool StatefulSets ([BDR-009](../neo4j/009-scale-pool-ordinal-semantics.md)), so restore is **online seed-from-URI** — the cluster distributes the seed to every allocation, it is not a per-server offline copy. The `system` database is special: it holds cluster metadata and its recovery is a cluster-wide-downtime runbook. Restore **execution** (Bolt vs Job, credentials, sequencing) is owned by [ADR-015](../../architecture/015-backup-and-restore.md).

### Forces

1. **CNPG parity** — operators in this space (CNPG, Zalando) converge on `Backup` + `ScheduledBackup` + a restore/bootstrap path; deviating raises the learning cost.
2. **GitOps re-apply safety** — a backup or restore manifest re-applied by Argo/Flux must **not** silently re-run a destructive action.
3. **Object-store first** — production backups target S3 / GCS / Azure, not a PVC that dies with the cluster.
4. **API minimalism** — every `kind` adds webhook, RBAC and E2E surface ([BDR-001](../neo4j/001-single-neo4j-crd.md)).
5. **Credentials are the user's** — the operator must not embed cloud secrets; it references a `Secret` or delegates to workload identity (execution detail owned by [ADR-015](../../architecture/015-backup-and-restore.md)).

---

## Options under review

### Option A — Three CRDs: `Neo4jBackup` (one-shot) + `Neo4jBackupSchedule` (cron) + `Neo4jRestore` (one-shot) — **chosen**

Direct CNPG mapping. `Neo4jBackup` is an **immutable run-to-completion record**; `Neo4jBackupSchedule` owns cron **and** retention/pruning and spawns `Neo4jBackup` objects; `Neo4jRestore` is a one-shot record that restores/seeds databases into a target `Neo4j`.

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: nightly-adhoc
spec:
  neo4jRef: { name: my-graph }          # BDR-001 satellite ref, same namespace
  databases: ["*"]                       # "*" = all incl. system; or explicit list
  destination:
    type: s3                             # s3 | gcs | azure | pvc
    bucket: my-neo4j-backups
    path: prod/                          # object key prefix
    credentials:                         # omit → workload identity (ADR-015 / ADR-016)
      secretName: backup-cloud-creds
  # optional: type: full | differential (default full)
status:
  phase: Succeeded                       # Pending | Running | Succeeded | Failed
  artifacts: [{ database: neo4j, uri: "s3://…", sizeBytes: 123, startedAt: …, completedAt: … }]
```

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackupSchedule
metadata:
  name: nightly
spec:
  neo4jRef: { name: my-graph }
  schedule: "0 2 * * *"                  # standard cron
  suspend: false
  backupTemplate:                        # inline Neo4jBackup.spec (minus neo4jRef)
    databases: ["*"]
    destination: { type: s3, bucket: my-neo4j-backups, path: prod/, credentials: { secretName: backup-cloud-creds } }
  retention:
    keepLast: 14                         # or a duration policy; prunes emitted Neo4jBackup + object-store artifacts
status:
  lastScheduleTime: …
  lastBackup: nightly-1724560000         # name of most recent emitted Neo4jBackup
```

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: restore-prod
spec:
  neo4jRef: { name: my-graph }           # target cluster (must exist, formation-stable)
  databases: ["neo4j"]                    # user databases only; "system" is rejected (see Decision §8)
  source:
    type: s3
    bucket: my-neo4j-backups
    path: prod/2026-08-24/
    credentials: { secretName: backup-cloud-creds }   # read by the workload pods, not a Job (ADR-015)
status:
  phase: Succeeded
```

> Cluster restore is executed as an online **seed-from-URI** — the DBMS distributes the seed to every allocation of each database. Full `system` / whole-cluster disaster recovery is a manual runbook, not this CR. See [ADR-015](../../architecture/015-backup-and-restore.md).

| Advantages | Disadvantages |
|------------|---------------|
| 1:1 with CNPG mental model — lowest learning cost | Three `kind`s = three webhooks + RBAC + E2E surfaces |
| Immutable `Neo4jBackup` record is GitOps-safe (re-apply is a no-op once `Succeeded`) | Users must understand record-vs-desired-state semantics |
| Retention/pruning lives on the schedule where it belongs (CronJob idiom) | — |
| Restore is an explicit, auditable object with its own status | — |

### Option B — Two CRDs: fold cron into `Neo4jBackup.spec.schedule`; keep `Neo4jRestore`

`Neo4jBackup` carries an optional `schedule`; when set it behaves as a recurring backup, else one-shot.

| Advantages | Disadvantages |
|------------|---------------|
| One fewer `kind` | Discriminated-union spec: one object is both a record *and* a policy |
| — | Retention semantics muddy — a "record" that also prunes itself |
| — | Diverges from CNPG; breaks the Job/CronJob analogy BDR-013 chose |

### Option C — Embed backup config in `Neo4j.spec.backup`

Backup destination + schedule become fields of the workload CR; no satellite `kind`.

| Advantages | Disadvantages |
|------------|---------------|
| No new `kind` | Bloats the already-largest `Neo4j.spec` ([BDR-001](../neo4j/001-single-neo4j-crd.md)) |
| Single manifest | No per-run record or status; ad-hoc "backup now" impossible |
| — | Restore has no home — cannot be a spec field of a running cluster |
| — | Contradicts BDR-013's accepted dedicated-domain split |

---

## Comparison

| Criterion | A — three CRDs (chosen) | B — two CRDs | C — embedded |
|-----------|-------------------------|--------------|--------------|
| CNPG parity / learning cost | ✅ | ⚠️ | ❌ |
| GitOps re-apply safety | ✅ immutable record | ⚠️ | ❌ |
| Retention where it belongs | ✅ schedule | ⚠️ | ❌ |
| Ad-hoc "backup now" | ✅ | ✅ | ❌ |
| Restore as first-class object | ✅ | ✅ | ❌ |
| API surface cost | high | medium | low |
| Aligns with BDR-013 | ✅ | ⚠️ | ❌ |

---

## Decision

**Proposed** — 2026-08-25.

**We will adopt Option A** — three dedicated CRDs in the `neo4j.com` group.

1. **`Neo4jBackup`** — one-shot, immutable run-to-completion **record**. `spec` = `neo4jRef` + `databases` (`["*"]` includes `system`) + `destination`; `status.phase` + `status.artifacts[]`. Re-applying a `Succeeded` backup is a no-op (idempotency guard via `status.phase`).
2. **`Neo4jBackupSchedule`** — cron owner. Holds `schedule`, `suspend`, an inline `backupTemplate`, and **`retention`**. It **emits** `Neo4jBackup` objects and prunes both the emitted records and their object-store artifacts. This is the literal CronJob → Job mapping.
3. **`Neo4jRestore`** — one-shot, immutable record. `spec` = target `neo4jRef` + `databases` + `source`. Restores/seeds the named databases into an existing target cluster.
4. **Destinations** — `type: s3 | gcs | azure | pvc`. Object storage is the production path; `pvc` (`spec.volumes.backups`) is the local/dev fallback. **Credentials** are referenced by `Secret` name or omitted to use workload identity — the *how* is owned by [ADR-015](../../architecture/015-backup-and-restore.md) and cloud-identity ADR-016; this BDR fixes only the field.
5. **Databases by name** — `databases: ["*"]` (all, incl. `system`) or an explicit list, consistent with [BDR-013](../database/013-database.md). No `databaseRef`.
6. **Enterprise only** — all three refuse to run against `spec.edition: community`; admission mirrors the `features.backup` edition guard ([BDR-010](../neo4j/010-neo4j-features-catalog.md)).
7. **Scope** — this is a **V2** deliverable (`NEO-2-013` / `NEO-2-014`); the `Neo4j` workload already ships the `features.backup` gate, backup listener and `spec.volumes.backups`. `differential` vs `full` backup type is **reserved** (default `full`); point-in-time / WAL-style continuous backup is a **non-goal** for the first release.
8. **`Neo4jRestore` covers user databases only in the first release** — it seeds/recreates named databases online across the cluster. **`system` / whole-cluster disaster recovery is out of scope** for automated restore (it needs cluster-wide downtime) and is a **documented manual runbook**; `databases: ["*"]` on `Neo4jRestore` restores user databases, not `system`. Admission rejects `system` in a `Neo4jRestore`. Full DR automation is deferred to a future guarded maintenance flow.

**Rejected:** Option B (union spec, breaks Job/CronJob analogy), Option C (workload bloat, no restore home, contradicts BDR-013).

---

## Consequences

### Positive

- Users familiar with CNPG transfer their mental model directly.
- `Neo4jBackup` / `Neo4jRestore` are auditable, `kubectl get`-able records; GitOps re-apply is safe.
- Retention/pruning is scoped to the schedule, not scattered across one-shot records.
- Backup/restore ships without a `Neo4jDatabase` controller in the critical path ([BDR-013](../database/013-database.md)).

### Negative

- Three new `kind`s: three validating webhooks, RBAC rules (`batch/jobs`), and E2E scenarios (S3/GCS/Azure/MinIO).
- Users must learn record-vs-desired-state semantics (a deleted `Neo4jBackup` does not delete the artifact unless retention pruned it).
- Restore of an existing database is offline for that database — a documented operational caveat, not a silent one.

### Neutral

- CRD group/version (`neo4j.com/v1beta1`) and exact field spelling are finalized in the CRD spec, not here.
- `differential` backup and continuous/PITR remain future work; the `type` field reserves the seam.

---

## References

- [BDR-001](../neo4j/001-single-neo4j-crd.md) · [BDR-005](../neo4j/005-storage-volume-mode.md) · [BDR-010](../neo4j/010-neo4j-features-catalog.md) · [BDR-013](../database/013-database.md)
- [ADR-015](../../architecture/015-backup-and-restore.md) — execution architecture (triggered by this BDR)
- [`crd-candidates.md`](../../../analysis/helm-fields/crd-candidates.md) — separate-CRD inventory
- CloudNativePG `Backup` + `ScheduledBackup` — [cloudnative-pg.md](../../../architecture/operator-benchmark/operators/cloudnative-pg.md)
- [Neo4j — backup and restore](https://neo4j.com/docs/operations-manual/current/backup-restore/) · [seed from URI](https://neo4j.com/docs/operations-manual/current/clustering/databases/#cluster-seed-uri)
