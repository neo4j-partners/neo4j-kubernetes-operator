# Release Verification

The **pre-release verification journey**: a manual + LLM-driven walk of the real
user scenarios on a clean cluster, run from `main` before cutting any release.
It is the canonical source of truth for **what we verify, on which deployment,
at what size, and why** — keep it current as the product grows.

!!! info "Why this exists alongside the automated suites"
    Unit and integration tests ([Testing](testing.md)) validate the **code
    against itself**. This journey validates the **published instructions and
    end-to-end behaviour against a clean machine** — the only check that catches
    a doc that lies, a dead image tag, a tutorial ordering bug, or a
    CR-name/logical-name mismatch. Real passes have caught all of these
    (most recently the sharded `target.name` doc bug, a `#270` follow-up).

The [`verify-journey` skill](https://github.com/neo4j-partners/neo4j-kubernetes-operator/blob/main/.claude/skills/verify-journey/SKILL.md)
executes this document. **LLM agents and developers should follow the matrix
below verbatim**; the skill holds only the mechanics (build, kind-load retry,
teardown, reporting) and defers to this page for *what* to test.

## Ground rules

- **KIND only. Enterprise images only** (`neo4j:*-enterprise`) — the project
  [invariants](../knowledge/invariants.md).
- **Follow the published docs literally** for Phases 1–2. A wrong/out-of-order
  step *is* a finding — record the exact doc location, don't silently fix it.
- **One Enterprise deployment in the cluster at a time** (the *anti-wedge*
  rule). Running standalone + cluster + sharding JVMs concurrently on a laptop
  VM wedges Bolt (the HTTP probe reports `Ready` but ports 7687/6362 time out).
  Phases run **sequentially with full teardown between them**; the Kind cluster,
  operator, and cert-manager stay up across phases.
- **Restore must be walked on BOTH standalone and cluster** — the mechanism
  differs (see the routing table) and one never covers the other.

## Capability routing — why each lands where it does

Each capability is verified on the **minimum deployment that actually exercises
its code path**. Standalone is the cheap workhorse; a capability moves to the
cluster only when it *needs* clustering.

| Capability | Where | Why |
|---|---|---|
| Database lifecycle (create/show/drop) | Standalone | Same `CREATE DATABASE` Cypher everywhere |
| Database **topology** (`n PRIMARIES m SECONDARIES`) | Cluster | Meaningless on standalone (validator only warns) |
| Users / Roles / RoleBindings | Standalone | Auth model is deployment-independent |
| Plugins (APOC, …) | Standalone (ConfigMap path) | Cluster uses the **env-var** path; ConfigMap path is the cheaper of the two to smoke |
| **Backup → restore** | **Both** | Standalone restores via the `neo4j-admin` path (supports PITR `--restore-until`); cluster restores via the in-place **Cypher** path (PITR is *rejected* on clusters). Separate code paths. |
| Property sharding | Cluster (CalVer) | Cluster-only by nature; needs a CalVer image and is memory-heavy |

## The phase plan

Run **all three phases every time** (Phase 3 included — sharding regularly
surfaces issues the lighter phases miss).

### Phase 1 — Standalone (1 pod, ~2Gi)

`Neo4jEnterpriseStandalone` → `Ready`, then on it:

| Scenario | Verify |
|---|---|
| Standalone reconciles | `status.phase=Ready`, pod `1/1` |
| Database lifecycle | `Neo4jDatabase` Ready; `SHOW DATABASES` shows it `online` |
| Users / Roles / RoleBinding | `Neo4jUser`/`Role`/`RoleBinding` Ready; `SHOW USERS`/`SHOW ROLES`. **Reference at least one role by its hyphenated CR `metadata.name`** (not `spec.name`) and confirm the grant lands (no `RolesPending`) and a `RolesResolved` event fires. |
| Plugin (APOC) | `Neo4jPlugin` Ready; `RETURN apoc.version()` returns a version (standalone **ConfigMap** path) |
| Backup → restore | `neo4j-admin` path with `stopCluster: true`: add a marker node → back up → delete the marker → restore → confirm the marker returns |
| Standalone recommended labels | `kubectl get pods -l app.kubernetes.io/name=neo4j` returns the standalone pod (it carries `app.kubernetes.io/{name,instance,managed-by}`) |
| `system` is not restorable | a `Neo4jRestore` with `databaseName: system` → `Failed` with an actionable message |

→ **Tear down the standalone fully** before Phase 2.

### Phase 2 — Cluster, 3 servers (~6Gi)

`Neo4jEnterpriseCluster` with `servers: 3` → `Ready`; pods
`{cluster}-server-0..2`; `SHOW SERVERS` lists 3 `Enabled`/`Available`. Then:

| Scenario | Verify |
|---|---|
| Cluster forms | 3 members `Enabled`/`Available`; server-based pod names |
| Database **with topology** | e.g. `3 PRIMARIES`; `SHOW DATABASE <db>` shows the primaries `online` |
| Backup → restore (cluster) | in-place **Cypher** path: back up one DB (`kind: Database`), restore into a **new** database, confirm the data round-trips |

3 servers (not 2) keeps split-brain / 3-primary quorum behaviour in the routine
walk. → **Tear down the cluster fully** before Phase 3.

### Phase 3 — Property sharding (cluster, `2026.04-enterprise`, 3 × 2Gi)

Sharding's documented floor is **4Gi + 1 core per server**. To fit a laptop we
deliberately relax it — **this phase is operator-mechanics verification, not
doc-following**. The relax is operator-side and DEV/TEST only:

```bash
# Downgrades the 4Gi/1-core hard rejects to warnings.
kubectl -n <operator-ns> set env deployment/<operator-deploy> \
  NEO4J_SHARDING_RELAX_MEMORY_MIN=true
kubectl -n <operator-ns> rollout status deployment/<operator-deploy>
```

Use **`2026.04-enterprise`** (the pinned CI anchor) so local sharding matches
what the [extended suite](ci_and_workflows.md) validates. On a `servers: 3`
cluster (~2Gi each):

| Scenario | Verify |
|---|---|
| Sharding cluster | `CALL dbms.components()` → `2026.04.x` enterprise; `status.propertyShardingReady=true` |
| Sharded database | `Neo4jShardedDatabase` whose **`metadata.name` differs from `spec.name`** (e.g. CR `products-sharded` / `spec.name: products`) → Ready; `SHOW DATABASES WHERE name STARTS WITH '<logical>'` lists the graph + property shards `online` |
| Sharded backup **by CR name** | `Neo4jBackup` `kind=ShardedDatabase` with `target.name` = the **CR metadata name** → `Succeeded`; `status.history[].shardArtifacts` lists every shard. (Using the *logical* name fails preflight — the operator resolves the logical name from the CR's `spec.name`.) |

→ **Tear down**, then delete the Kind cluster.

## Coverage at a glance

| | Standalone | Cluster (3) | Sharding (2026.04) |
|---|:---:|:---:|:---:|
| Reconcile → Ready | ✅ | ✅ | ✅ |
| Database lifecycle | ✅ | | |
| Database topology | | ✅ | |
| Users / Roles / Bindings | ✅ | | |
| Plugins (APOC) | ✅ (ConfigMap) | | |
| Backup → restore | ✅ (neo4j-admin) | ✅ (Cypher) | ✅ (sharded) |
| Property sharding | | | ✅ |

## Keeping this current

When you **add or change a capability**, update this page in the same PR:

1. Add a row to the **routing table** (which deployment, and *why* there).
2. Add the scenario + its in-DB check to the relevant **phase**.
3. Tick the **coverage** matrix.
4. If it changes the operator install or sizing, update the phase headers.

When a release fixes a specific bug, add a one-line scenario that would have
caught it (the v1.12.2 pass added the standalone-label, `system`-reject,
role-CR-name, and sharded-backup-by-CR-name checks). Record each run below.

## Verification log

| Release | Date | Result | Findings |
|---|---|---|---|
| v1.12.2 | 2026-06-14 | ✅ all phases pass | Doc bug: `backup_restore.md` sharded `target.name` said *logical name*, must be *CR name* (fixed, `#270` follow-up). v1.12.2 surfaces (#260/#268/#269/#270) verified live. |

## See also

- [Testing](testing.md) — the automated unit/integration suites and the
  core/extended label split.
- [CI/CD & Workflows](ci_and_workflows.md) — what runs per-PR vs. nightly/extended.
- [Backup & Restore](../user_guide/guides/backup_restore.md),
  [Property Sharding](../user_guide/property_sharding.md) — the user docs this
  journey follows.
- [Project Invariants](../knowledge/invariants.md) — the hard constraints
  (KIND-only, Enterprise-only, etc.) every phase respects.
