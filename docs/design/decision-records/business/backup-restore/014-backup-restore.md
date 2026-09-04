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

### User databases vs `system` — the asymmetry that drives the `databases` field

A Neo4j DBMS hosts two kinds of database, and they are **not restorable by the same mechanism**:

| | Holds | Restore mechanism | Downtime |
|---|-------|-------------------|----------|
| **User databases** (`neo4j`, and any `CREATE DATABASE …`) | Graph data | Online **seed-from-URI** — one Bolt statement, cluster reseeds every allocation | The target database is unavailable while it re-seeds; the DBMS stays up |
| **`system`** | DBMS metadata — database inventory, per-database topology, users, roles, privileges | **Whole-cluster DR runbook** — stop every server, `unbind-system-db` + `dump`/`load system` on each, restart | Entire cluster offline |

Because the two paths differ in blast radius by orders of magnitude, the `databases` field means **different things per CRD**, and this asymmetry is deliberate:

| CRD | `databases` accepts | `"*"` includes `system`? | Rationale |
|-----|---------------------|--------------------------|-----------|
| `Neo4jBackup` / `Neo4jBackupSchedule` | any name, or `"*"` | **yes** — `system` is backed up | The manual DR runbook needs a `system` artifact to load from |
| `Neo4jRestore` | **user database names only**; `"*"` = all user databases | **no** — `system` is rejected at admission | Restoring `system` online is impossible; silently doing a cluster-wide-offline op behind a per-database API would violate the user's expectation of the online, no-downtime restore path |

So you can **back up** `system` but you cannot **restore** it through `Neo4jRestore` in the first release; full `system` / whole-cluster recovery stays a documented manual runbook (Decision §8). This keeps the `Neo4jRestore` contract honest: everything it accepts, it restores online without taking the cluster down.

### Full, incremental, and the backup chain

`neo4j-admin database backup` produces immutable artifacts of two kinds ([online-backup](https://neo4j.com/docs/operations-manual/current/backup-restore/online-backup/)):

- a **full** backup — the whole store;
- a **differential** backup — a **log of transactions replayed onto the previous artifact**.

A **backup chain** is one full followed by *n* **contiguous** differentials. Restore points at the **last** differential and walks the chain back to the full, replaying every link.

**Terminology — we call it `Incremental`, not `Differential`.** Neo4j's on-disk term is "differential", but its behaviour is **incremental**: each link depends on the one before it, so recovery needs the **entire unbroken chain**, not `full + latest diff`. A classic *differential* (cumulative-since-full, where only `full + newest` is needed) is **not** what Neo4j does. To avoid misleading users, our API surface uses **`type: Full | Incremental | Auto`** (mapping to `neo4j-admin --type=FULL|DIFF|AUTO`), and documents the mapping. This is a deliberate divergence from the vendor label.

**Consequences of chain semantics (they drive the schedule design):**

1. **A lone full + infinite incrementals is unsafe** — the chain grows without bound (restore replay time ↑, RTO ↑), one lost/corrupt link breaks all recovery after it, and old data can never be pruned.
2. **Links cannot be pruned individually** — you may drop a **whole chain**, or compact from the front with **`neo4j-admin backup aggregate`** (collapses a chain into a single recovered full), but never delete a middle differential.
3. **Full and incremental cadences are independent** — Neo4j supports scheduling them separately (the first differential may overlap its parent full), so each can target a different recovery objective.

These are the reasons a single `schedule` + single `retention` is insufficient: the schedule needs a **full cron** and an **incremental cron**, and retention must be **two-tier and chain-aware**.

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
  destination:                           # object storage (prod) — see §4 for the PVC form
    type: s3                             # s3 | gcs | azure | pvc
    url: "s3://my-neo4j-backups/prod/"   # provider URI (s3:// | gs:// | azb://) → neo4j-admin --to-path
    credentials:                         # omit → workload identity (ADR-015 / ADR-016)
      secretName: backup-cloud-creds
  type: Auto                             # Full | Incremental | Auto (default) — see §9
  options:                               # OPTIONAL neo4j-admin passthrough (all default to Neo4j's defaults) — §12
    compress: true                       # --compress            (default true)
    keepFailed: false                    # --keep-failed         (default false) — keep failed artifact for analysis
    verbose: false                       # --verbose
    includeMetadata: all                 # --include-metadata=none|all|users|roles (default all; ignored for system)
    extraArgs: []                        # allow-listed escape hatch for advanced/version-gated flags (§12)
status:
  phase: Succeeded                       # Pending | Running | Succeeded | Failed
  chain: nightly-2026-08-24T01-00        # the chain this artifact belongs to (full that anchors it)
  artifacts: [{ database: neo4j, type: Incremental, uri: "s3://…", sizeBytes: 123, startedAt: …, completedAt: … }]
```

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackupSchedule
metadata:
  name: nightly
spec:
  neo4jRef: { name: my-graph }
  suspend: false
  # Two independent cadences — Neo4j supports scheduling full and incremental separately.
  full:
    schedule: "0 1 * * 0"                # weekly full → anchors a new backup chain
    retention: { keepLast: 4 }           # keep the last 4 WHOLE chains (~4 weeks); older chains pruned entirely
  incremental:
    schedule: "0 1 * * 1-6"              # daily incremental → attaches to the current chain (--type=Auto self-seeds a full if none)
    # retention: RESERVED — not enforced (mid-chain links can't be deleted safely). Within-chain
    # bounding is done by `aggregate` below, and chain count by `full.retention`. See §10.
  aggregate:                             # optional: collapse each CLOSED chain into one recovered full
    enabled: true                        # boundary-triggered relative to the full cadence — NO separate cron
  backupTemplate:                        # inline Neo4jBackup.spec (minus neo4jRef / type — set per cadence)
    databases: ["*"]
    destination: { type: s3, url: "s3://my-neo4j-backups/prod/", credentials: { secretName: backup-cloud-creds } }
    options: { compress: true, keepFailed: false, verbose: false }   # inherited by every emitted Neo4jBackup (§12)
status:
  lastFullTime: …
  lastIncrementalTime: …
  currentChain: nightly-2026-08-24T01-00 # full anchoring the active chain
```

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: restore-prod
spec:
  neo4jRef: { name: my-graph }           # target cluster (must exist, formation-stable)
  databases: ["neo4j"]                    # user databases only; "system" is rejected (see Decision §8)
  overwrite: false                        # target already exists? false → refuse (safe default); true → CREATE OR REPLACE / recreate (§11)
  forceOffline: false                     # STOP DATABASE before replacing, to fence writes during the swap (§11)
  source:                                 # reference a record (recommended) OR a raw URI — see §13
    backupRef: nightly-2026-08-24T01-00   # a Neo4jBackup name; operator resolves url + walks the chain
    # --- raw form, for external/manual artifacts (mutually exclusive with backupRef): ---
    # type: s3
    # url: "s3://my-neo4j-backups/prod/neo4j/"
    # credentials: { secretName: backup-cloud-creds }   # read by the workload pods, not a Job (ADR-015)
status:
  phase: Succeeded                        # Failed + reason=DatabaseExists if a target exists and overwrite=false
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

1. **`Neo4jBackup`** — one-shot, immutable run-to-completion **record**. `spec` = `neo4jRef` + `databases` (`["*"]` includes `system`) + `destination` + **`type: Full | Incremental | Auto | Aggregate`** (default `Auto`, §9; `Aggregate` requires `spec.source.backupRef`); `status.phase` + `status.chain` + `status.artifacts[]` (each artifact reports its own `type`). An ad-hoc `Neo4jBackup` can therefore take a one-off full, attach an incremental to an existing chain, or collapse a chain into a recovered full — independent of any schedule. Re-applying a `Succeeded` backup is a no-op (idempotency guard via `status.phase`).
2. **`Neo4jBackupSchedule`** — cron owner and **chain owner**. Holds **two independent cadences** — `full.schedule` and `incremental.schedule` — plus `suspend`, an inline `backupTemplate`, an optional **boundary-triggered `aggregate`** compaction (`aggregate.enabled`, no cron), and **chain-aware `full.retention`** (`incremental.retention` is **reserved** — see §10). It **emits** `Neo4jBackup` objects (including the `type: Aggregate` recovered-full at chain boundaries) and prunes chain-aware (see §10). A single schedule object owns the whole chain so pruning is not split across controllers. This is the CronJob → Job mapping, doubled for full/incremental.
3. **`Neo4jRestore`** — one-shot, immutable record. `spec` = target `neo4jRef` + `databases` + `source`. Restores/seeds the named databases into an existing target cluster.
4. **Destinations** — `type: s3 | gcs | azure | pvc`, modelled provider-neutrally:
    - **Object storage** (production / DR path) → a single **`url`** (`s3://…`, `gs://…`, `azb://…`) mapping straight to `neo4j-admin --to-path`. **No `bucket` field** — Azure has no buckets and one URI covers all three providers.
    - **PVC** (dev/local fallback) → `pvc.claimName` for an existing claim, or `pvc: { size, storageClassName }` for the operator to provision one — the **same Dynamic/Existing model** as [BDR-005](../neo4j/005-storage-volume-mode.md). A PVC target never leaves the cluster, so it is **not** a DR path; ownership/retention of an operator-created PVC is owned by [ADR-015](../../architecture/015-backup-and-restore.md) / [ADR-008](../../architecture/008-finalizers-and-deletion.md).
    - **Credentials** are the user's — `Secret` name or omitted for workload identity; the *how* is owned by [ADR-015](../../architecture/015-backup-and-restore.md) and cloud-identity ADR-016.
5. **Databases by name** — `databases: ["*"]` (all, incl. `system`) or an explicit list, consistent with [BDR-013](../database/013-database.md). No `databaseRef`.
6. **Enterprise only** — all three refuse to run against `spec.edition: community`; admission mirrors the `features.backup` edition guard ([BDR-010](../neo4j/010-neo4j-features-catalog.md)).
7. **Scope** — this is a **V2** deliverable (`NEO-2-013` / `NEO-2-014`); the `Neo4j` workload already ships the `features.backup` gate, backup listener and `spec.volumes.backups`. Full **and** incremental backup are **in scope** (§9–§10). WAL-style *continuous* backup is a **non-goal**; within-chain point-in-time recovery (`neo4j-admin --restore-until`) is a **possible later enhancement**, not first-release.
8. **`Neo4jRestore` covers user databases only in the first release** — it seeds/recreates named databases online across the cluster. **`system` / whole-cluster disaster recovery is out of scope** for automated restore (it needs cluster-wide downtime) and is a **documented manual runbook** — see [Disaster recovery](../../../backup-restore/disaster-recovery.md) for the DR procedure, scenarios, and cluster prerequisites. `databases: ["*"]` on `Neo4jRestore` restores user databases, not `system`. Admission rejects `system` in a `Neo4jRestore`. Full DR automation is deferred to a future guarded maintenance flow.
9. **Backup type & terminology** — the API exposes **`type: Full | Incremental | Auto`** (default `Auto`), mapping to `neo4j-admin --type=FULL|DIFF|AUTO`. We deliberately name it **`Incremental`, not Neo4j's on-disk "Differential"**, because the artifacts form a dependent **chain** (restore needs the whole chain, not `full + latest`) — the vendor label would mislead. `Auto` self-seeds a full when no chain exists, so an incremental schedule is safe to start cold. A fourth type, **`Aggregate`**, collapses an existing chain into a single **recovered full** via `neo4j-admin backup aggregate`; it requires `spec.source.backupRef` (the chain's tip) and is what the schedule emits at chain boundaries (§10). It can also be created ad-hoc.
10. **Scheduling & retention model** — **rejected: a single full + unbounded incrementals** (chain grows forever → RTO ↑, one lost link breaks recovery, no pruning). **Adopted: independent full + incremental crons with chain-aware retention plus boundary-triggered aggregation.** Each full **anchors a new chain**; incrementals attach until the next full.
    - **`full.retention`** (`keepLast` *N* / `keepDays` *T*) keeps that many **whole chains**; older chains are pruned **entirely** — every artifact file *and* its `Neo4jBackup` record, files-before-records so nothing is orphaned. The active chain is never pruned. A **compacted** chain (see below) is by then just its recovered full, so it too is dropped wholesale when it expires.
    - **`aggregate.enabled`** compacts each chain **at its boundary** — when the next full closes the previous chain — **not on its own cron** (so it is always relative to the full cadence). The operator emits a `Neo4jBackup` of **`type: Aggregate`** for the closed chain (`neo4j-admin backup aggregate`, `--keep-old-backup=true`, source = the chain's tip); once that **recovered full** is verified and cataloged, it prunes the chain's **original links**, keeping the recovered full. This **preserve-then-clean** order guarantees a chain is never left without a restorable artifact. The recovered full is itself a first-class, restorable `Neo4jBackup`.
    - **`incremental.retention` is reserved and not enforced in the first release.** Deleting an individual mid-chain link would break every later link's restore, so there is no safe per-link retention; link growth is bounded by the aggregate cadence (collapse the chain) and the full cadence (start a fresh one). **Individual mid-chain links are never deleted directly.**
    - **Object-store destinations are not pruned yet** (pending ADR-016 cloud identity); such chains are kept and the schedule records why. PVC destinations prune in full.
11. **Restore onto an existing database — safe by default, destructive only on opt-in.** A restore whose target database **already exists** is **refused** unless `spec.overwrite: true`. Rationale: `CREATE DATABASE … {seedURI}` fails on a name clash and `CREATE OR REPLACE` / `dbms.recreateDatabase` **destroys the current store** — the operator must never pick "destroy" implicitly. Behaviour:
    - `overwrite: false` (default) + target exists → `status.phase=Failed`, `reason=DatabaseExists`, Event; **no data touched**. Existence is a runtime fact, so this is checked by the reconciler via `SHOW DATABASES`, not the webhook ([ADR-001](../../architecture/001-crd-validation-process.md) — no Bolt in admission).
    - `overwrite: true` → the reconciler replaces the store (`CREATE OR REPLACE DATABASE` / `dbms.recreateDatabase`) across all allocations.
    - `forceOffline: true` (optional) → `STOP DATABASE <db>` before the replace, fencing in-flight writes so the swap is clean; the operator restarts it after. Default `false` (the recreate already makes the database unavailable while it re-seeds; `forceOffline` is the explicit, write-fencing variant for a live target). Sequencing owned by [ADR-015](../../architecture/015-backup-and-restore.md).

12. **Backup options — typed passthrough for the safe knobs, escape hatch for the rest.** `Neo4jBackup.spec.options` (and `backupTemplate.options`, inherited) exposes the commonly wanted, stable `neo4j-admin database backup` flags as typed optional fields; everything else the operator either **owns** (it must set them correctly) or accepts through an **allow-listed `extraArgs`** so we don't model a dozen version-gated flags.

    | CRD field | `neo4j-admin` flag | Default | Notes |
    |-----------|--------------------|---------|-------|
    | `options.compress` | `--compress` | `true` | Smaller artifacts; CPU/IO trade-off |
    | `options.keepFailed` | `--keep-failed` | `false` | Preserve a failed backup dir for analysis |
    | `options.verbose` | `--verbose` | `false` | Verbose Job logs |
    | `options.includeMetadata` | `--include-metadata=none\|all\|users\|roles` | `all` | Users/roles/privileges for the DB; **ignored for `system`** (Neo4j forbids it there) |
    | `options.extraArgs[]` | (allow-listed) | — | `--pagecache`, `--parallel-download`, `--parallel-recovery` (experimental), `--skip-recovery`, `--prefer-diff-as-parent`, `--remote-address-resolution`, … — validated against an allowlist, rejected otherwise |
    | `type` | `--type` | `Auto` | §9 |

    **Operator-owned, never user fields** (the operator sets them; exposing them would let a user break the backup): `--from` (backup-listener address of the target's pods), `--to-path` (derived from `destination`), `--temp-path` (local scratch volume — see [ADR-015](../../architecture/015-backup-and-restore.md)), `--additional-config` / `--expand-commands`. `extraArgs` is validated to **not** contain any of these.
13. **Backups are discoverable as objects; restore references them.** A user does **not** inspect the object-store folder to find restore points. Every `Neo4jBackup` is a queryable record labelled `neo4j.com/database`, `neo4j.com/chain`, `neo4j.com/type`, so `kubectl get neo4jbackup -l neo4j.com/database=orders` lists what is restorable, and `Neo4jBackupSchedule.status` summarizes the latest restorable point per chain. `Neo4jRestore.source` therefore accepts **either** `backupRef: <Neo4jBackup name>` (operator resolves the artifact `url` and walks the chain — **recommended**) **or** a raw `{ type, url, credentials }` for external/manual artifacts (mutually exclusive). Restore-by-record is the ergonomic default; folder-spelunking is never required.

**Rejected:** Option B (union spec, breaks Job/CronJob analogy), Option C (workload bloat, no restore home, contradicts BDR-013). Also rejected: single-cadence schedule / single-tier retention (§10), vendor "differential" naming (§9), **implicit overwrite** on restore (§11), **modelling every `neo4j-admin` flag as a first-class field** (§12 — churny and version-coupled; use `extraArgs`), a cloud-specific **`bucket`** field (§4 — use provider-neutral `url`), and **folder-path-only restore discovery** (§13 — records are the catalog).

---

## Consequences

### Positive

- Users familiar with CNPG transfer their mental model directly.
- `Neo4jBackup` / `Neo4jRestore` are auditable, `kubectl get`-able records; GitOps re-apply is safe.
- Retention/pruning is scoped to the schedule, not scattered across one-shot records.
- Independent full/incremental cadences let users tune RPO (backup frequency) and RTO (chain length via `aggregate`) separately, matching Neo4j's own capability.
- Restore points are discoverable via `kubectl get neo4jbackup` and referenced by name (`source.backupRef`) — no object-store folder inspection.
- Provider-neutral `url` destination covers S3/GCS/Azure with one field and maps 1:1 to `neo4j-admin --to-path`.
- Backup/restore ships without a `Neo4jDatabase` controller in the critical path ([BDR-013](../database/013-database.md)).

### Negative

- Three new `kind`s: three validating webhooks, RBAC rules (`batch/jobs`), and E2E scenarios (S3/GCS/Azure/MinIO).
- Users must learn record-vs-desired-state semantics (a deleted `Neo4jBackup` does not delete the artifact unless retention pruned it).
- Restore of an existing database makes that database unavailable while it re-seeds — a documented operational caveat, not a silent one.
- Chain-aware retention is more complex than "keep last N" — the controller must track chain membership and never orphan a link (execution owned by [ADR-015](../../architecture/015-backup-and-restore.md)).
- Naming `Incremental` while the CLI/artifacts say "differential" needs a one-line note in user docs to avoid confusion.

### Neutral

- CRD group/version (`neo4j.com/v1beta1`) and exact field spelling (`keepLast` vs `keepDays`, `type` casing) are finalized in the CRD spec, not here.
- Continuous/WAL backup remains future work; within-chain PITR (`--restore-until`) is a later enhancement the chain model already supports.

---

## References

- [BDR-001](../neo4j/001-single-neo4j-crd.md) · [BDR-005](../neo4j/005-storage-volume-mode.md) · [BDR-010](../neo4j/010-neo4j-features-catalog.md) · [BDR-013](../database/013-database.md)
- [ADR-015](../../architecture/015-backup-and-restore.md) — execution architecture (triggered by this BDR)
- [Disaster recovery](../../../backup-restore/disaster-recovery.md) — `system` restore, DR order, scenarios, cluster prerequisites
- [`crd-candidates.md`](../../../analysis/helm-fields/crd-candidates.md) — separate-CRD inventory
- CloudNativePG `Backup` + `ScheduledBackup` — [cloudnative-pg.md](../../../architecture/operator-benchmark/operators/cloudnative-pg.md)
- [Neo4j — backup and restore](https://neo4j.com/docs/operations-manual/current/backup-restore/) · [seed from URI](https://neo4j.com/docs/operations-manual/current/clustering/databases/#cluster-seed-uri)
- [Neo4j — online backup & backup chain](https://neo4j.com/docs/operations-manual/current/backup-restore/online-backup/#backup-chain) · [aggregate a backup chain](https://neo4j.com/docs/operations-manual/current/backup-restore/aggregate/)
