# Disaster recovery — `system` database & whole-cluster recovery

| | |
|---|---|
| **Status** | design note (proposed) |
| **Date** | 2026-08-27 |
| **Relates to** | [BDR-014](../decision-records/business/backup-restore/014-backup-restore.md) §8 — `system` out of scope for automated restore · [ADR-015](../decision-records/architecture/015-backup-and-restore.md) — backup/restore execution |
| **Audience** | operators / SRE running the DR runbook; designers scoping future automation |

---

## Why this is a separate document

[BDR-014](../decision-records/business/backup-restore/014-backup-restore.md) and [ADR-015](../decision-records/architecture/015-backup-and-restore.md) cover the **automated, online, per-user-database** path (`Neo4jBackup` / `Neo4jBackupSchedule` / `Neo4jRestore`). The `system` database and whole-cluster recovery are a **fundamentally different process** — cluster-wide downtime, per-member offline commands — so they are documented here rather than bolted onto the online CR contract. The automated CRDs deliberately **refuse `system`**; this note is the manual counterpart, and the reference for any future DR automation.

## `system` vs a user database

`system` is not graph data. It holds DBMS metadata: the **database inventory**, each database's **topology** (how many primaries/secondaries, on which servers), **server identities**, and **users / roles / privileges**. Consequences:

- It **cannot** be seed-restored (`seedURI` / `recreateDatabase` are for user databases).
- Recovering it means reconstructing cluster metadata on **every** server → the cluster must go offline.
- Because it encodes server IDs and topology, restoring `system` onto a *different* cluster is inherently risky.

## Scenarios

| Scenario | Recovery |
|----------|----------|
| **Messed up a single database's data** | `Neo4jRestore` CR (online seed-from-URI) — [ADR-015](../decision-records/architecture/015-backup-and-restore.md). No `system` involved. |
| **Lost / crashed the whole cluster** | Rebuild: new cluster → **restore `system` first** → restart → **then** restore each user database (`Neo4jRestore`). Order matters: `system` defines what databases exist and their topology. |
| **Corrupted / rolled-back security model** (mass-deleted users/roles, bad privilege change) | A `system` restore *can* recover it, but it is heavyweight and also reverts the database catalog/topology. Prefer per-database `--include-metadata` (users/roles/privileges travel with the backup) for routine RBAC recovery; reserve `system` restore for true DR. |

**Is `system` restore ever valuable outside a crash?** Narrowly — security-model or catalog recovery — but it is a DR/rebuild tool, not a routine rollback. Treat it as DR-only.

## Manual runbook (first release)

Whole-cluster / `system` recovery, per [Neo4j cluster disaster recovery](https://neo4j.com/docs/operations-manual/current/clustering/multi-region-deployment/disaster-recovery/):

1. Deploy a fresh cluster (or stop Neo4j on all existing servers).
2. On **each** server: `neo4j-admin dbms unbind-system-db` (resets `system` state and rebinds server identity).
3. On the most up-to-date server: `neo4j-admin database dump system --to-path=<path>`.
4. On **each** server: `neo4j-admin database load system --from-path=<path> --overwrite-destination=true`.
5. Start Neo4j on all servers.
6. For each user database that is not write-available, **recreate** it from a backup (`CREATE … OPTIONS {seedURI}` / `dbms.recreateDatabase`), specifying a `TOPOLOGY` that the current server set can satisfy — this is where the automated `Neo4jRestore` path resumes.

Within the operator today, steps 1–5 are **manual** (they need cluster-wide downtime and per-pod `neo4j-admin` invocations); step 6 is the automated per-database restore.

## Cluster prerequisites for a `system` restore

- **Neo4j version** of the target must be **≥** the backup's.
- **Cluster size need not match**, but every restored database's **topology must be satisfiable** on the current server set. If there are fewer servers than a database's original topology, recreate it with a **new, smaller `TOPOLOGY`**; the number of seeding servers cannot exceed the total allocations.
- **Server identities** are rebound by `unbind-system-db`; do not expect old server IDs to survive.
- **Discovery / cluster config** must be set up for the new cluster before `system` is loaded.
- **Database names** come from the restored `system` — user databases reappear by name once `system` is loaded, but remain non-write-available until recreated/seeded.

## Future automation (not first release)

A guarded, operator-driven `system` DR flow could later orchestrate steps 1–5 behind an explicit maintenance mode (all-members offline, `unbind-system-db`, load, restart), tying into [ADR-008](../decision-records/architecture/008-finalizers-and-deletion.md) (finalizers / offline maintenance). It is deferred until the online per-database path is proven, because its blast radius (an unbootable cluster on error) is high.

## References

- [BDR-014](../decision-records/business/backup-restore/014-backup-restore.md) · [ADR-015](../decision-records/architecture/015-backup-and-restore.md)
- [Neo4j — cluster disaster recovery](https://neo4j.com/docs/operations-manual/current/clustering/multi-region-deployment/disaster-recovery/)
- [Neo4j — recreate a database](https://neo4j.com/docs/operations-manual/current/database-administration/standard-databases/recreate-database/) · [seed from URI](https://neo4j.com/docs/operations-manual/current/clustering/databases/)
