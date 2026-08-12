# BDR-013 — Logical database management: CR `Neo4jDatabase` or not

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-08-06 |
| **Reviewers** | Charles Boudry |
| **Depends on** | [BDR-001](../neo4j/001-single-neo4j-crd.md) — single `Neo4j` CRD, day-2 satellites via `neo4jRef` (accepted) · [BDR-009](../neo4j/009-scale-pool-ordinal-semantics.md) — scale drain `DEALLOCATE` / `DROP SERVER` (accepted) |
| **Related** | [BDR-012](../identity-user-roles/012-identity-management.md) — identity CRDs reference databases by name · `Neo4jBackup` / `Neo4jRestore` (V2, `NEO-2-013` / `NEO-2-014`) |
| **Reference** | [`20-operator-proposal.md`](../../../../00-discovery/20-operator-proposal.md) §3.2 · [`crd-candidates.md`](../../../analysis/helm-fields/crd-candidates.md) · [`09-api.md`](../../../../01-prd/09-api.md) · [`13-roadmap.md`](../../../../01-prd/13-roadmap.md) |

---

## Context

Neo4j Enterprise is **multi-database**: a single DBMS hosts many named databases (`neo4j`, `system`, plus user databases created with `CREATE DATABASE`). The operator exposes **one `Neo4j` workload CRD** ([BDR-001](../neo4j/001-single-neo4j-crd.md)). This BDR decides whether a **logical database** should be a first-class Kubernetes resource (`Neo4jDatabase`), embedded in the workload spec, or kept out of the declarative model entirely.

The decision is non-trivial because a database is **not** a passive content unit — it is the granularity of three day-2 operations the operator must eventually pilot.

### The three coupling axes

| Axis | Neo4j mechanism | Granularity |
|------|-----------------|-------------|
| **Scale in / out** | `DEALLOCATE DATABASE … FROM <id>` (drain before `DROP SERVER`), `REALLOCATE DATABASE(S)` / `ALTER DATABASE … SET TOPOLOGY` (rebalance after `ENABLE SERVER`) | **per database** |
| **Backup / restore** | `neo4j-admin database backup <db>`, restore / `CREATE DATABASE … OPTIONS {seedURI}` — plus `system` for full DR | **per database** |
| **RBAC** | `GRANT ACCESS ON DATABASE <db>`, `GRANT MATCH {*} ON GRAPH <db>`, `ALTER USER … SET HOME DATABASE <db>` | **per database** |

### What each axis actually needs

- **Scale** does **not** need a CR: the operator enumerates databases at runtime via `SHOW DATABASES` to drain/reallocate ([BDR-009](../neo4j/009-scale-pool-ordinal-semantics.md)). A CR only helps carry a *desired per-database topology* across scale events — at the cost of coordinating two controllers (workload scale ↔ database allocation).
- **Backup / restore** does **not** need a CR: `Neo4jBackup` / `Neo4jRestore` reference databases **by name** (`databases: ["*"]` or a list). A CR only adds a GitOps anchor for per-database policy, and creates an ownership conflict (restore recreates a database the DB reconciler claims to own).
- **RBAC** is the **only** axis a CR genuinely improves: a typed `databaseRef` gives admission-time referential integrity, an ordering `Database → Role → Grant → User`, and a finalizer that blocks dropping a database still referenced by grants. Without a CR, [BDR-012](../identity-user-roles/012-identity-management.md) references databases as **strings** (`databases: []`) — valid, but failures surface only at runtime.

### Forces

1. **Completeness** — the operator will eventually cover database creation, permissions, and backup.
2. **Sequencing** — backup / restore is the V2 production priority; identity RBAC is V3+.
3. **GitOps tension** — operator-materialized child CRs for built-in databases (`neo4j`, `system`) reintroduce the ownership friction flagged in [BDR-001](../neo4j/001-single-neo4j-crd.md) Option D.
4. **API minimalism** — every new `kind` adds webhook, RBAC, and E2E surface.

---

## State vs action — a prerequisite distinction

A recurring confusion is "CR vs script". The real split is **durable state** (reconciled forever → declarative CR) vs **point-in-time action** (run once → task CR or CLI). A pure operator action still needs *some* object to watch; a truly object-less action is a **client** action (CLI / driver), not an operator action.

| Concern | Nature | Adapted form |
|---------|--------|--------------|
| Database **exists** + topology | state | declarative CR (`Neo4jDatabase`) |
| Users / roles / grants | state | declarative CR ([BDR-012](../identity-user-roles/012-identity-management.md)) |
| **Scheduled** backup | recurring action | `Neo4jBackupSchedule` (cron → spawns `Neo4jBackup`) |
| Backup **now** | one-shot action | `Neo4jBackup` — **dedicated domain** (not the generic task) |
| Restore / seed | one-shot action | `Neo4jRestore` — **dedicated domain** |
| Create / drop a database, ad-hoc Cypher (post-install) | one-shot action | **generic task CR** `Neo4jDatabaseTask` / `kubectl-neo4j` |
| Manual `REALLOCATE`, dump/load, consistency check | one-shot action | task CR / CLI (later) |

A **task CR** is idempotency-guarded: `status.phase` + `spec` hash prevent re-running a destructive operation on GitOps re-apply; the object is an immutable **record** once complete (CNPG `Backup` pattern).

### Task scope vs backup domain

One-shot actions split into **two families** — a generic lightweight task and a dedicated backup domain — following the Kubernetes **Job / CronJob** idiom and the CNPG precedent already reflected in [`20-operator-proposal.md`](../../../../00-discovery/20-operator-proposal.md):

| Family | Kinds | Why |
|--------|-------|-----|
| **Generic task** — schema/lifecycle | `Neo4jDatabaseTask` (`CreateDatabase` / `DropDatabase` / `RunCypher`) | Small, homogeneous spec: `neo4jRef` + `action` + `databaseName` / cypher. One-shot record. |
| **Backup domain** — dedicated | `Neo4jBackup` (one-shot), `Neo4jBackupSchedule` (cron → emits `Neo4jBackup`), `Neo4jRestore` | Rich, backup-specific schema — `destination` (S3/GCS/Azure), `credentials` / workload identity, `databases: ["*"]` incl. `system`, `retention`, object-store pruning. Folding this into the generic task would turn it into a discriminated **union dumping ground**, and retention/pruning are scheduler concerns, not one-shot-task concerns. |

Rationale for **not** making `Backup` an `action:` of `Neo4jDatabaseTask`:

1. **Schema purity** — the generic task keeps a tiny, validatable spec; backup keeps its cloud/destination/retention fields where they belong.
2. **Naming / scope** — a backup targets the DBMS (`["*"]`, `system` included), not a single database; it is not "database-scoped" and does not fit a `Neo4jDatabaseTask`.
3. **Lifecycle semantics** — retention and pruning of stored artifacts are properties of the **schedule** (`Neo4jBackupSchedule`), which spawns `Neo4jBackup` executions — the literal CronJob → Job mapping.
4. **Alignment** — matches CNPG (`Backup` + `ScheduledBackup`) and the backup / restore CRDs already sketched in the operator proposal and V2 roadmap (`NEO-2-013` / `NEO-2-014`).

---

## Options under review

### Option A — No declarative CR; databases by name + creation via task CR — **chosen**

The database lifecycle is **not** a reconciled Kubernetes object. The default `neo4j` database is implicit. Additional databases are created by an **imperative run-to-completion task CR** (post-install convenience) or by the user via Cypher / CLI. Backup, restore, and RBAC reference databases **by name** (string), consistent with the existing `databases: []` in [BDR-012](../identity-user-roles/012-identity-management.md) and `databases: ["*"]` in the backup CRDs. Scale drain / reallocation operates at runtime (`SHOW DATABASES`).

```yaml
# Post-install task CR — imperative, run-once, records status; does NOT reconcile existence
apiVersion: neo4j.com/v1beta1
kind: Neo4jDatabaseTask          # working name — final naming in CRD spec
metadata:
  name: create-orders
spec:
  neo4jRef: { name: my-graph }
  action: CreateDatabase         # CreateDatabase | DropDatabase | RunCypher
  databaseName: orders
  topology: { primaries: 3, secondaries: 0 }   # optional; else cluster default
status:
  phase: Succeeded               # Pending | Running | Succeeded | Failed
```

| Advantages | Disadvantages |
|------------|---------------|
| Smallest API surface — no new declarative `kind` to reconcile forever | No GitOps declarative lifecycle: a dropped database is **not** recreated |
| **Backup / restore prioritized** without waiting on a database controller | RBAC references stay **string-based** — no admission integrity, runtime failure only |
| Zero operator-created-children GitOps tension (no built-in CRs) | No finalizer anti-drop — dropping a referenced database is not blocked declaratively |
| Zero cross-controller coordination on scale (runtime `DEALLOCATE` / `REALLOCATE`) | Task CR needs idempotency guards to avoid re-running destructive actions |
| Consistent with string refs already accepted in BDR-012 and backup CRDs | Per-database desired topology not persisted — scale-out uses a default reallocation policy |

### Option B — Embedded list `Neo4j.spec.databases[]`

Databases (name + topology) become a field of the workload CR. The same reconciler sees scale **and** database topologies.

| Advantages | Disadvantages |
|------------|---------------|
| One reconciler → scale ↔ allocation solved natively (no controller race) | Bloats `Neo4j.spec` (already the largest schema — BDR-001) |
| Desired per-database topology available at scale time | A "add a database" PR touches the cluster CR (multi-team tension) |
| Single source of truth for topology + scale + databases | Databases are not referenceable objects → no clean `databaseRef` for RBAC |
| Consistent with BDR-001 (embedded infra, no proliferation of CRDs) | Restore drift unresolved; does not scale to many independent database lifecycles |

### Option C — Full declarative `Neo4jDatabase` CR (built-ins materialized by the operator)

One CR per database, own reconciler. The operator **materializes** `Neo4jDatabase/neo4j` and `/system` (owner-referenced, adopted if user-authored, `builtin` flag = non-deletable / reflective). RBAC and backup reference any database uniformly via `databaseRef`.

| Advantages | Disadvantages |
|------------|---------------|
| **Uniform referent** — `databaseRef` for RBAC everywhere, incl. default `neo4j` and `system` | Two controllers to coordinate: workload scale ↔ `Neo4jDatabase` reallocation |
| `system` is a CR → uniform backup / DR policy | Ownership contract required with `Neo4jRestore` (both mutate the same database) |
| Admission integrity + ordering `Database → Role → Grant → User` + finalizer anti-drop | Operator-created children reintroduce BDR-001 Option D GitOps tension (adoption/protection semantics) |
| Per-database status observable (`kubectl get`) | Retro-impacts BDR-012 (`databaseRef` vs `databases: []`); largest webhook / E2E surface |

### Option D — Hybrid: default database implicit + `Neo4jDatabase` for business databases

`neo4j` / `system` implicit; business databases as CRs referenceable by RBAC. Captures integrity where it matters without imposing a CR on built-ins.

| Advantages | Disadvantages |
|------------|---------------|
| The simple case (1 database) does not pay the CR cost | Two mental models (implicit vs CR) to document |
| Declarative multi-database becomes opt-in when needed | Default-database grants fall back to string refs — integrity lost for the most common case |
| Clear ownership: CR = source of truth, restore coordinated via CR | Scale ↔ CR coordination still required for CR-backed databases |

---

## Comparison (three axes)

| Criterion | A — no CR (chosen) | B — embedded | C — full CR | D — hybrid |
|-----------|--------------------|--------------|-------------|------------|
| Drain / reallocation at scale | runtime ✅ | ✅ | ✅ | ✅ |
| Scale coupling without controller race | ✅ | ✅ | ❌ | ❌ |
| Backup / restore by name | ✅ | ✅ | ✅ | ✅ |
| Restore without ownership conflict | ✅ | ⚠️ | ❌ | ⚠️ |
| RBAC referential integrity (`databaseRef`) | ❌ | ❌ | ✅ | ✅ (business dbs) |
| RBAC ordering `Database→Role→Grant→User` | ❌ (runtime) | ⚠️ | ✅ | ✅ |
| RBAC safe deletion (finalizer anti-drop) | ❌ | ❌ | ✅ | ✅ |
| GitOps declarative lifecycle per database | ❌ | ⚠️ | ✅ | ✅ |
| Cost (schema / webhook / E2E / BDR-012 impact) | low | medium | high | medium-high |

---

## Decision

**Proposed** — Charles Boudry, 2026-08-06.

**We will adopt Option A** — **no declarative `Neo4jDatabase` CR**. Logical-database *existence* is not a reconciled Kubernetes object.

1. **No `Neo4jDatabase` declarative CR** on the roadmap. Databases are referenced **by name** (string) everywhere — consistent with `databases: []` ([BDR-012](../identity-user-roles/012-identity-management.md)) and `databases: ["*"]` (backup CRDs).
2. **Priority to backup / restore CRs, as a dedicated domain** — `Neo4jBackup` (one-shot), `Neo4jBackupSchedule` (cron → emits `Neo4jBackup`), `Neo4jRestore` are the V2 production deliverable and do not depend on a database controller. `system` is covered via `databases: ["*"]`. Backup is **not** an `action:` of the generic task — its rich schema (destination, credentials, retention, pruning) and DBMS-wide scope keep it in its own kinds (Job / CronJob idiom, CNPG precedent).
3. **Task CR — still under reflection, not committed** — the priority is firmly on backup / restore (point 2); the task model is an **open design direction**, not a locked deliverable. `Neo4jDatabaseTask` (working name) is the **envisaged solution for post-install operations** — an imperative, run-to-completion resource scoped to **`CreateDatabase` / `DropDatabase` / `RunCypher`** (schema/lifecycle only, no backup/restore actions), with run-once, idempotency-guarded, no-drift semantics. Final decision, naming, and shape are deferred to CRD spec once backup / restore lands.
4. **Scale in / out stays runtime** — drain (`DEALLOCATE`) and rebalance (`REALLOCATE` / `ALTER … SET TOPOLOGY`) operate on databases discovered via `SHOW DATABASES` ([BDR-009](../neo4j/009-scale-pool-ordinal-semantics.md)); no per-database CR required.
5. **Unlikely to revisit** — a full declarative CR (Option C) would only be reopened if admission-time RBAC integrity (`databaseRef`) and GitOps multi-database lifecycle become **hard** requirements. Until then the string-reference model stands.

**Rejected / not adopted:** Option B (workload bloat), Option C and Option D (full/partial declarative CR) — deferred, not on the roadmap.

---

## Consequences

### Positive

- Minimal API surface; backup / restore delivered without a database controller in the critical path.
- No operator-created-children GitOps tension (no built-in database CRs to adopt/protect).
- No cross-controller coordination for scale — database allocation stays a runtime reconcile concern of the `Neo4j` controller.
- String references remain consistent across BDR-012 identity CRDs and backup CRDs.

### Negative

- No declarative GitOps lifecycle for databases — a manually dropped database is not recreated.
- RBAC keeps string references: typos and missing databases fail at **runtime** (`Ready=False`, `CypherError`), with no admission-time integrity or finalizer anti-drop.
- Per-database desired topology is not persisted; scale-out relies on a default reallocation policy rather than a reconciled intent.
- The task CR must carry explicit idempotency guards to avoid re-running destructive actions on re-apply.

### Neutral

- Task CR naming (`Neo4jDatabaseTask` vs a generic `Neo4jExec` / `Neo4jQuery`) is deferred to CRD spec.
- Option C remains a future possibility; keeping the `neo4j.com` group free of a `Neo4jDatabase` `kind` avoids pre-committing the name.
- `03-variant_matrix.csv` / test catalog gain a "post-install database task" scenario rather than a database-lifecycle reconcile matrix.

---

## References

- [`20-operator-proposal.md`](../../../../00-discovery/20-operator-proposal.md) — §3.2 `Neo4jDatabase`, §3.3–3.4 backup / restore
- [`crd-candidates.md`](../../../analysis/helm-fields/crd-candidates.md) — separate-CRD evaluation
- [`09-api.md`](../../../../01-prd/09-api.md) — post-V1 API phasing
- [`13-roadmap.md`](../../../../01-prd/13-roadmap.md) — V2 backup / restore, `Neo4jDatabase`
- [Neo4j — managing databases](https://neo4j.com/docs/operations-manual/current/database-administration/) · [clustering: servers](https://neo4j.com/docs/operations-manual/current/clustering/servers/)
- [BDR-001](../neo4j/001-single-neo4j-crd.md) · [BDR-009](../neo4j/009-scale-pool-ordinal-semantics.md) · [BDR-012](../identity-user-roles/012-identity-management.md)
