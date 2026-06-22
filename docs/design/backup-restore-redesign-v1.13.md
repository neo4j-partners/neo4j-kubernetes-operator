# Design: Backup/Restore API redesign (v1.13)

> **Status:** Draft for review — schema names are a strawman, open for `a community reviewer` input (see §11).
> **Issues:** [#244](https://github.com/priyolahiri/neo4j-kubernetes-operator/issues/244) (anchor — scope split), [#222](https://github.com/priyolahiri/neo4j-kubernetes-operator/issues/222) (cluster-wide restore, "Priority #1"), inputs: #242, #185, #186, #227 (item 9).
> **Milestone:** v1.13.0 (transition) → **v1.14.0 (clean end-state)**.
> **Decision (Plan B — clean cut with a one-release overlap):** Redesign `Neo4jBackup`/`Neo4jRestore` **in place on `v1beta1`, Kind names retained** — a **scope-based target** + **restore symmetry** as the *only documented* API. The legacy shape (overloaded `target.*`, `clusterRef`, `force`/`replaceExisting`, top-level `spec.cloud`) and **all dead fields** are honored for **one release (v1.13) behind loud deprecation, then removed in v1.14**. No conversion webhook. The cluster/standalone CRDs are untouched.
>
> Rationale for dropping permanent back-compat: backup/restore CRs are **stateless job-definitions** (artifacts live in storage / un-owned PVCs — see #227), so the migration blast radius is "re-apply a manifest," not data loss; and the operator is **still beta (`v1beta1`) and pre-GA** ("not production-recommended until ~Dec 1 2026"), which is the correct window to clean-cut before the API ossifies at GA.

---

## 1. Why

Both scopes already share one engine (one Job, one `neo4j-admin database backup`, single name vs `"*"`). The pain is **API ergonomics + restore symmetry**, not the engine. Five concrete problems, all hit during v1.12.0/v1.12.1 release-verification:

1. **Overloaded `target`.** `target.name` means three different things by `kind`; `clusterRef` is required-or-ignored by `kind`. It conflates **topology** (cluster/standalone) with **scope** (one DB / all DBs). a community reviewer's key point is correct: *topology is an implementation detail; scope is what the user means.*
2. **Backup↔restore granularity mismatch (#222).** A `kind: Cluster` backup writes N per-DB artifacts, but `BackupRun.ArtifactFilename` is a **single** field keyed off the instance name → empty for cluster backups → **a cluster backup cannot be restored as a cluster**, and restore is single-DB only (`databaseName` singular, required).
3. **"Best-effort but load-bearing" contradiction.** `ArtifactFilename` is documented as opportunistic (parsed from pod logs), yet the cluster Cypher restore depends on it to build the `seedURI`. The sharded path already does it right with a per-shard **map** (`shardArtifacts[]`); standard/cluster scope needs the same.
4. **One schema, three execution engines** chosen by inference: standalone = scale-to-0 + restore Job; cluster = Cypher `seedURI` (no Job); sharded = rejected. `stopCluster`/`timeout`/`hooks`/`resources` silently apply on only some paths.
5. **Debris:** dead fields (`options.verify`, `verifyBackup`, `deletePolicy=Archive`, `identity.serviceAccount`, `autoCreate.enabled`); duplicate `spec.cloud` vs `spec.storage.cloud`; two destructive-confirm flags (`force` vs `replaceExisting`); PVC create-vs-bind inferred from whether `size` is set; cloud-config asymmetry (backup Job gets the S3 endpoint injected; cluster restore needs it hand-placed on server pods).

## 2. The binding constraint — no conversion webhook

Only `v1beta1` exists; there is no second version, no conversion webhook, no hub/spoke. **Invariant 1 forbids webhooks → there can be no conversion webhook.** Therefore:

- **No `v1beta2` rev with conversion** — the apiserver can't convert stored objects without a webhook, and `None`-strategy conversion only works for structurally identical versions.
- **Chosen mechanism:** redesign **in place on `v1beta1`, keeping the `Neo4jBackup`/`Neo4jRestore` Kind names.** v1.13 carries new + legacy fields (legacy deprecated but honored); **v1.14 removes the legacy + dead fields**, leaving a single clean schema.
- **Removing `v1beta1` fields in v1.14 is a breaking change without a version bump** — acceptable here because `v1beta1` is a *beta* API (k8s deprecation policy permits incompatible changes for beta with notice) and we give a full release of loud deprecation first. This **revises #227 item 9** ("removal needs a version bump"): we break `v1beta1` in place, pre-GA, behind a deprecation window.

**Rejected alternatives:** new Kinds (`Neo4jDatabaseBackup`…) — abandons the familiar names and, for the two-CRD variant, triples the scheduling/history/retention surface; `v1beta2` + conversion webhook — barred by invariant 1; additive-forever — never reaches a clean end-state, bakes debris into GA.

## 3. User experience

### 3.1 Existing users (v1.12.x manifests) — one release to migrate

On **v1.13** every current field keeps working and keeps its meaning: legacy `target.{kind,name,clusterRef}` and restore `clusterRef`/`databaseName` are **honored** behind a **loud deprecation warning** (event + validator) nudging toward the new shape. On **v1.14** the legacy fields are removed — so the one practical task is to re-apply backup/restore manifests in the new shape within the v1.13 window. **No data is affected** at any point (artifacts live in storage; see §7).

```yaml
# v1.12.x manifest — still valid on v1.13 (deprecated), removed in v1.14
kind: Neo4jBackup
spec:
  target: { kind: Cluster, name: my-neo4j }   # warns: "prefer spec.instanceRef + allDatabases"
  storage: { type: s3, bucket: backups }
```

### 3.2 New users — scope, not topology

The new shape expresses **what to back up** (scope) and **which deployment** (a topology-agnostic ref). The operator resolves whether the deployment is a cluster, a standalone, or holds a sharded database, and picks the engine.

```yaml
# Back up ONE database
kind: Neo4jBackup
spec:
  instanceRef: my-neo4j        # cluster OR standalone — operator resolves
  database: customers          # single DB (sharded auto-detected & handled transparently)
  storage: { type: s3, bucket: backups }
```

```yaml
# Back up the WHOLE instance (all user databases; system excluded)
kind: Neo4jBackup
spec:
  instanceRef: my-neo4j
  allDatabases: true
  storage: { type: s3, bucket: backups }
```

Restore is **symmetric**:

```yaml
# Restore one database
kind: Neo4jRestore
spec:
  instanceRef: my-neo4j        # the TARGET deployment; alias of clusterRef
  database: customers
  source: { type: backup, backupRef: nightly }
```

```yaml
# Restore everything from a whole-instance backup (the #222 fix)
kind: Neo4jRestore
spec:
  instanceRef: my-neo4j
  allDatabases: true           # enumerates per-DB artifacts; restores each; system excluded
  source: { type: backup, backupRef: nightly }
```

The win for newcomers: no overloaded `name`, no "is `clusterRef` used here?", and the same two-line mental model for backup and restore (*which deployment* × *which scope*). Sharding and cluster-vs-standalone disappear from the API surface.

### 3.3 Common-flow comparison

| Flow | v1.12.x (today) | v1.13 (new shape) |
|---|---|---|
| Back up one DB on a cluster | `target: {kind: Database, name: customers, clusterRef: my-neo4j}` | `instanceRef: my-neo4j` + `database: customers` |
| Back up all DBs | `target: {kind: Cluster, name: my-neo4j}` (name = instance ⚠️) | `instanceRef: my-neo4j` + `allDatabases: true` |
| Back up one DB on a standalone | `target: {kind: Database, name: customers, clusterRef: my-standalone}` | identical to cluster — `instanceRef` + `database` |
| Restore one DB | `clusterRef` + `databaseName` | `instanceRef` + `database` |
| Restore a whole cluster | ❌ not possible (#222) | `instanceRef` + `allDatabases: true` |

## 4. Cross-topology restore — "can a standalone backup be restored into a cluster?"

**Yes.** A backup artifact is **database-scoped**, not topology-scoped: `neo4j-admin database backup` produces a `.backup` store for one logical database, identical whether that database lived on a standalone or a cluster. Restore applies the *target's* topology at restore time:

- **Into a cluster** — the operator uses Cypher `CREATE/RECREATE DATABASE … OPTIONS { seedURI: '<artifact>' }`; the cluster seeds the new database from the artifact and allocates it across servers per the cluster's topology (e.g. 3 primaries).
- **Into a standalone** — the operator scales to 0 and runs `neo4j-admin database restore`, then brings the DB online.

The **engine is chosen by the TARGET's topology** (`instanceRef` resolves to cluster ⇒ seedURI; standalone ⇒ restore Job), **independent of where the backup came from.** So all of these work:

- standalone → cluster (single DB): ✅ seeded via `seedURI`, gets cluster topology.
- standalone → cluster (whole instance): ✅ `allDatabases` seeds each user DB.
- cluster → standalone: ✅ each DB restored via the Job path.

**This is a headline benefit of the scope-based design:** because the API is topology-agnostic, "back up DB X here, restore it there" is a first-class flow — the user never reasons about which engine runs.

**Caveats (Neo4j constraints, surfaced via validation/docs, not operator bugs):**
- **Version direction:** target Neo4j version must be **≥** source version (restoring an older backup into newer Neo4j is fine, with store upgrade; newer→older is not).
- **`system` is not portable across topologies** — cluster and standalone have different `system` stores (membership, etc.). `allDatabases` **excludes `system`**; account/role portability is via `options.includeMetadata: users|roles|all` on backup, not by restoring the `system` store. A restore targeting `system` is rejected (#222 item 2).
- **Sharded databases** remain a cluster-topology operation (restore via `Neo4jShardedDatabase.spec.seedBackupRef`, not `Neo4jRestore`); standalones don't host sharded DBs, so standalone→cluster-sharded is N/A.

## 5. Proposed API (additive on `v1beta1`)

> Field names are a **strawman** — see §11 open questions.

### 5.1 `Neo4jBackup.spec` (additions)

| New field | Type | Meaning |
|---|---|---|
| `instanceRef` | `string` | The deployment to back up (Neo4jEnterpriseCluster **or** Neo4jEnterpriseStandalone). Topology-agnostic. |
| `database` | `string` | Single-database scope. Sharded databases auto-detected and handled (glob + per-shard). |
| `allDatabases` | `bool` | Instance-wide scope: every user database (system excluded). |
| `storage.pvc.create` | `*bool` | Explicit create-vs-bind intent (default: inferred from `size` for back-compat; validated). |
| `options.overwriteExisting` | `*bool` | Unified destructive-confirm (supersedes `force`/`replaceExisting`, kept as aliases). |

**Discriminator rule:** exactly one of `database` / `allDatabases` when `instanceRef` is set. `instanceRef`+scope and legacy `target.*` are mutually exclusive (both set → error; legacy-only → honored + deprecation warning).

### 5.2 `BackupRun` status (additions — the #222 + load-bearing fix)

| New field | Type | Meaning |
|---|---|---|
| `databaseArtifacts` | `[]{database, filename, size}` | **Authoritative per-DB artifact map** for standard + instance-wide scope. Mirrors a `manifest.json` written alongside the artifacts in storage (so it does **not** depend on pod-log parsing). |

`artifactFilename` (single) retained for single-DB back-compat; `shardArtifacts[]` retained for sharded. Restore consumes `databaseArtifacts`/manifest to enumerate DBs and locate each seed file — eliminating the empty-`ArtifactFilename` dead end.

### 5.3 `Neo4jRestore.spec` (additions)

| New field | Type | Meaning |
|---|---|---|
| `instanceRef` | `string` | Alias of `clusterRef` (topology-agnostic name); `clusterRef` kept + deprecated. |
| `database` | `string` | Alias of `databaseName`. |
| `allDatabases` | `bool` | Restore every user DB found in the resolved source (system excluded); per-DB status. |
| `databases` | `[]string` | Optional explicit subset (alternative to `allDatabases`). |

Restore that resolves a `source.type: backup` ref **auto-projects the backup's cloud endpoint/creds** onto the target (gated, like seed-creds auto-inherit already is) — fixes the MinIO/S3 endpoint hand-placement asymmetry.

## 6. Engine / topology resolution (unchanged engines, new dispatch)

- **Backup:** `instanceRef` → resolve Cluster or Standalone (`standaloneAsCluster`). `database` → single name (sharded auto-detected → `"name*"` glob + per-shard map); `allDatabases` → `"*"` + per-DB map + manifest.
- **Restore:** `instanceRef` (target) → Cluster ⇒ Cypher `seedURI`; Standalone ⇒ scale-to-0 Job. `allDatabases`/`databases` ⇒ loop per DB from the artifact map. Source topology is irrelevant.

## 7. Compatibility & deprecation timeline

**v1.13.0 — transition release:**
- ✅ Every v1.12.x manifest still applies and runs (`target.kind/name/clusterRef`, `databaseName`, `force`, `replaceExisting`, `spec.cloud`, and the dead fields all still accepted).
- ✅ The new scope-based API is the **only documented** path; new fields are additive.
- ⚠️ Legacy shape + dead fields emit **loud deprecation warnings** — a validator warning **and** a `BackupAPIDeprecated`/`RestoreAPIDeprecated` Warning event naming the replacement field and linking the migration guide. Warnings, never errors.

**v1.14.0 — clean end-state:**
- 🚫 Legacy fields **removed** from the `v1beta1` schema: `spec.target.*` (→ `instanceRef` + scope), restore `clusterRef` (→ `instanceRef`), `force`/`replaceExisting` (→ `options.overwriteExisting`), top-level `spec.cloud` (→ `storage.cloud`).
- 🚫 Dead fields **removed**: `options.verify`, `verifyBackup`, `deletePolicy=Archive`, `identity.serviceAccount`, `autoCreate.enabled`.
- An un-migrated CR then fails validation with a message pointing at the new field. **Backup artifacts in storage remain fully restorable** via a new-shape `Neo4jRestore` — no data is at risk at any point.

**Never touched:** `Neo4jEnterpriseCluster` / `Neo4jEnterpriseStandalone` and their data. This redesign is scoped to the **stateless** backup/restore CRDs only.

**Migration aid:** docs guide (old→new field mapping) + an optional one-shot helper that reads any old-shape CRs and prints equivalent new manifests (external tooling, since there is no conversion webhook).

## 8. Cleanups included

- Unified `options.overwriteExisting` (v1.13: `force`/`replaceExisting` deprecated aliases; v1.14: removed).
- Explicit `storage.pvc.create` (replaces size-presence inference).
- Restore auto-projects the resolved backup's cloud config onto the target (fixes the MinIO/S3 endpoint asymmetry).
- Single cloud block `storage.cloud` (v1.13: top-level `spec.cloud` deprecated; v1.14: removed).
- Dead fields removed in v1.14 (see §7).

## 9. Phasing

- **v1.13:** new scope-based API (only documented), restore symmetry (#222), per-DB artifact map + manifest, the §8 cleanups; legacy shape honored + loudly deprecated.
- **v1.14:** remove legacy + dead fields (§7) → clean single-path `v1beta1` schema.
- **Out of scope (revisit later):** the literal two-CRD split (`Neo4jDatabaseBackup` / `Neo4jMultipleDatabasesBackup`, #244) — viable later as new Kinds (no conversion needed); converting the remaining blocking restore waits to requeue-driven (#227 items 1–4) — separable, can ride alongside if bandwidth allows.

## 10. Test & verification

- Unit: validator table tests for the discriminator (exactly-one-scope, legacy-vs-new mutual exclusion, `system` rejection, version-direction guard).
- Integration (extended tier): instance-wide backup → `allDatabases` restore; **standalone → cluster single-DB restore**; cluster → standalone restore; per-DB artifact map / manifest authority.
- Add the cross-topology and all-DBs scenarios to `docs/developer_guide/release_verification.md` in the same PR (per the repo rule).

## 11. Open questions (input owed to a community reviewer)

1. **Naming:** `instanceRef` vs `deploymentRef` vs `targetRef`? `database`/`allDatabases` (implicit one-of) vs an explicit `scope: SingleDatabase|AllDatabases` enum?
2. **Restore alias:** keep `clusterRef` as the alias, or promote `instanceRef` and hard-deprecate `clusterRef`?
3. **Sharded ergonomics:** auto-detect sharded under `database:` (proposed), or keep an explicit selector?
4. **Manifest format:** `manifest.json` alongside artifacts as the authoritative source of the per-DB map — agree?

## 12. Decision log

- **Plan B (clean cut, one-release overlap) over additive-forever** — backup/restore CRs are stateless (no data blast radius) and the operator is pre-GA/beta, so the migration cost is bounded and the timing is right to reach a clean GA API.
- **Kind names retained, redesigned in place on `v1beta1`** — over new Kinds (abandons familiar names / triples surface) and over `v1beta2`+webhook (barred by invariant 1).
- **Scope-based discriminator over the two-CRD split** — same ergonomic win (scope not topology), single scheduling/history/retention surface, unblocks #222 with the same artifact-map change; the two-CRD split stays a later option as new Kinds.
- **Legacy + dead fields removed in v1.14**, behind a full-release v1.13 deprecation window (revises #227 item 9).
