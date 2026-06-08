# Sharded Property Database — Backup & Restore Design

**Issue:** [neo4j-partners/neo4j-kubernetes-operator#138](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues/138)
**Status:** Design (pre-implementation)
**Author:** Priyo Lahiri (Claude-assisted)
**Date:** 2026-06-07

## Context

The operator has zero first-class backup/restore support for property-sharded databases (`Neo4jShardedDatabase`). Users today can create one `Neo4jBackup` per underlying shard (`{name}-g000`, `{name}-p000`, …) — mechanically correct but produces uncoordinated, independent backup chains with no cross-shard consistency at the operator level.

This document captures the design after auditing both the operator code and the upstream Neo4j 2026.05 docs. The original issue is a useful sketch but is missing several non-trivial details that affect Phase 2; this doc supersedes it where they conflict.

## Ratified design decisions

1. **Typed `BackupTargetKind*` constants**: introduce in `api/v1beta1/neo4jbackup_types.go`, migrate all raw-string call sites (~5 places). Avoids growing the magic-string footgun as we add a third kind.
2. **`--remote-address-resolution` default behavior for sharded backups**: when `kind=ShardedDatabase` AND Neo4j version >= 2025.09, default `options.remoteAddressResolution` to `true` if user did not set it explicitly. Field remains user-overrideable. Matches the canonical upstream example without taking control away.
3. **Validator vs reconciler split**: validator handles in-CR checks (kind enum, ClusterRef presence, name format). Reconciler handles cross-CR checks (cluster fetch, `propertySharding.enabled`, version gate, sharded DB CR exists + `Status.ShardingReady=true`, glob safety via `GetDatabases()`). Matches the project's current separation; no k8s client added to `BackupValidator`.
4. **Extract `IsClusterShardingReady(cluster) error` helper**: move the `propertySharding.Enabled` + 2025.12+ version gate currently inlined at `cluster_validator.go:268-288` into a reusable helper in `internal/validation/sharding.go` (or co-located). Backup reconciler in Phase 1 and Phase 2 restore controller both consume it.

## Upstream Neo4j model (verified against 2026.05 docs)

### Backup

Single neo4j-admin invocation with a quoted glob:

```bash
neo4j-admin database backup "foo*" --to-path=/backups \
  --from=localhost:6361 --remote-address-resolution
```

Output is a flat directory of timestamped per-shard files:

```
foo-g000-2025-06-11T21-04-42.backup
foo-p000-2025-06-11T21-04-37.backup
foo-p001-2025-06-11T21-04-40.backup
```

**Key notes:**
- The glob MUST be quoted so the shell doesn't expand `*` before reaching neo4j-admin. (The operator already wraps the database-name argument in single quotes for cluster backups via `GetBackupCommand`, so this generalises cleanly.)
- `--remote-address-resolution` **already exists** as a user-facing field `BackupOptions.RemoteAddressResolution` (`neo4jbackup_types.go:109`), wired through the controller (`neo4jbackup_controller.go:756`), version-gated to 2025.09+ (`SupportsRemoteAddressResolution()`). Docs do not explain its semantics; best inference is that it tells neo4j-admin to resolve cluster member addresses dynamically via the discovery service rather than relying only on the static `--from` list. Phase 1 defaults it to `true` for `kind=ShardedDatabase` + version >= 2025.09 (Decision 2), matching the canonical upstream example without removing user control. Today's cluster-scope backups already pass a comma-separated list of pod FQDNs via `BuildBackupFromAddresses` (`cluster.go:217`), so the flag is genuinely optional for non-sharded backups.

### Cross-shard consistency (lenient)

Per docs verbatim: *"a sharded property database backup is considered valid if the store files of each property shard are within the range of transactions recorded in the graph shard's transaction log."*

Example tolerance: graph shard store at tx 10 with logs covering tx 11–36; property shards at tx 13 and tx 30 can all restore and reconcile up to tx 36.

**Implications for the operator:**
- No `--restore-until` for sharded DBs. PITR is not supported upstream. Don't fake it.
- A backup taken under write load may restore to an earlier transaction than the user expected. Surface this in docs; emit a warning event if `neo4j-admin backup validate` (see below) reports per-shard mismatch beyond a threshold.

### Backup validation

```bash
neo4j-admin backup validate --from-path=s3://bucket/backups --database="foo"
```

Per-shard output (OK / ahead / behind). Natural fit for Phase 3 observability. Not in Phase 1.

### Restore — online, via Cypher

```cypher
CYPHER 25 CREATE DATABASE baz
  SET GRAPH SHARD { TOPOLOGY 3 PRIMARIES 0 SECONDARIES }
  SET PROPERTY SHARDS { COUNT 2 TOPOLOGY 1 REPLICA }
  OPTIONS { seedUri: "s3://bucket/backups/" };
```

**Key notes:**
- `seedUri` is a **directory URI** (trailing slash in canonical example).
- The sharded admin doc only demonstrates `s3://`, but the [generic seedUri page](https://neo4j.com/docs/operations-manual/current/database-administration/standard-databases/seed-from-uri/) confirms `CloudSeedProvider` supports all three of `s3://`, `gs://`, `azb://` for standard DBs. **Inference: same machinery flows through to sharded `CREATE DATABASE`.** Phase 2 must integration-test each cloud target — the inference is reasonable but not officially documented for sharded.
- Restore is **fully online** — `CREATE DATABASE` runs against the system DB while the cluster serves traffic. The existing `Neo4jRestore.spec.stopCluster` flag is irrelevant for sharded restores.
- `DROP DATABASE` semantics on sharded DBs are **not documented**. `replaceExisting` in Phase 2 needs upstream testing, not just operator code.

### seedUri provider details (from the generic seedUri docs)

These mechanics apply to standard `CREATE DATABASE … OPTIONS { seedUri }` and almost certainly carry through to sharded restore — but the sharded admin doc doesn't repeat them, so treat the carry-through as an integration-test target for Phase 2.

**Providers and schemes:**

| Provider | Schemes | Notes |
|---|---|---|
| `CloudSeedProvider` (default) | `s3://`, `gs://`, `azb://` | Default for cloud URIs |
| `FileSeedProvider` | `file://` | Absolute paths only |
| `URLConnectionSeedProvider` | `ftp://`, `http://`, `https://` | No `file://` since 2025.01 |
| `ServerSeedProvider` | `server://` | Introduced 2026.04 — one server seeds from another running server |
| `S3SeedProvider` (deprecated 5.26) | `s3://` | Use `CloudSeedProvider` instead |

**URI form:** both single-file (`s3://bucket/foo.backup`) and directory (`s3://bucket/backups/`) accepted. For directory form, "the system automatically looks for the most recent backup chain with the target database name." For the sharded glob case, the seed mechanism is responsible for matching per-shard files in the directory.

**Cloud auth (documented patterns — important for Phase 2):**
- **S3**: AWS SDK v2. Credentials via `~/.aws/credentials`, environment (`AWS_ENDPOINT_URL_S3` / `AWS_ENDPOINT_URL`), system properties (`aws.endpointUrls3` / `aws.endpointUrlS3` / `aws.endpointUrl`), or IAM bucket policies. **IRSA / IAM instance role NOT explicitly documented** — needs integration testing in EKS, but should work via the AWS SDK's default credential chain.
- **GCS**: `GOOGLE_APPLICATION_CREDENTIALS` (service account JSON), `GOOGLE_CLOUD_PROJECT`, or `gcloud auth activate-service-account`. **Workload Identity NOT explicitly documented** — should work via GCP SDK's default credential chain.
- **Azure**: `DefaultAzureCredential` chain (Azure CLI login, env-based service principal, **managed identity is part of the chain even though not explicitly named in the doc**). SAS tokens / connection strings not mentioned.

**S3-only Cypher options** (do not work for GCS/Azure):
- `seedConfig`: comma-separated `key=value`, e.g. `region=eu-west-1`.
- `seedCredentials`: `<accessKey>;<secretKey>` — requires pre-configured identical keystores across the cluster. Operationally awkward; prefer env-based auth.

**Other options:**
- `seedRestoreUntil`: datetime or int (tx ID). Supported by `CloudSeedProvider` + `FileSeedProvider`. **Sharded docs don't mention this** — restore-up-to-tx for sharded DBs is unproven; the sharded admin doc explicitly says PITR is not supported for sharded DBs, which contradicts the seedUri page's claim. **Treat sharded PITR as unsupported until upstream-verified.**
- `seedSourceDatabase`: Cypher 25 only. Disambiguates source DB name when directory contains multiple backups. May matter for sharded since the directory contains files for `{name}-g000`, `{name}-p000`, … — but the standard seedUri logic uses the target DB name as the search key, and the sharded restore creates each shard with its full per-shard name. **Behavior here is unverified — integration test the case where the same backup directory holds two different sharded DB families.**
- `existingData: 'use'`: required in Cypher 5, deprecated in Cypher 25 (2025.06+). Sharded DBs are 2025.12+ → Cypher 25 → operator never emits `existingData` for sharded restores.

**Failure surface (from generic seedUri docs):**
- On failure: database not available, `SHOW DATABASES` shows `statusMessage: "Unable to start database"`. Root cause is in `debug.log`. The operator should surface the statusMessage on `Neo4jShardedDatabase.status` and route to a Failed phase, but the *cause* of the failure won't be in the status — operators need to look at the actual Neo4j logs.

**Cluster fetch coordination — UNDOCUMENTED.** Whether each cluster member fetches independently or only the primary fetches and replicates is not specified. Real concern for sharded restores where per-shard data must land on per-shard primaries. Phase 2 integration test must verify cloud creds are reachable from every pod that might host a shard, not just one.

### Operational gotchas (2026.01+)

- If property shards fall too far behind the graph shard's tx log pruning, the DB goes read-only. Recovery: `ALTER DATABASE <name> SET ACCESS READ WRITE` once replicas catch up.
- If ALL replicas of a property shard are severed, the DB is **unrecoverable**. Recovery requires drop + restore from backup — i.e., Phase 2 is operationally load-bearing once sharded DBs are in production use.

### Version gate

Property sharding introduced in Neo4j 2025.12. Auto-lag-monitoring read-only safeguard in 2026.01. Both forward-compatible (2026.x+).

## Current operator state (touch-point inventory)

| Concern | Location | Today |
|---|---|---|
| `BackupTarget.Kind` enum | `api/v1beta1/neo4jbackup_types.go:54` | `+kubebuilder:validation:Enum=Cluster;Database` |
| `BackupTarget` struct | `api/v1beta1/neo4jbackup_types.go:53-68` | `Kind`, `Name`, `ClusterRef`, `Namespace` |
| Validator dispatch | `internal/validation/backup_validator.go:79-123` | `validKinds` array; no per-kind branch |
| Command builder | `internal/neo4j/version.go:169` | `GetBackupCommand(version, dbName, toPath, allDatabases, fromAddresses)` — passes quoted `"*"` when `allDatabases=true`, else `dbName` unquoted |
| Controller branch | `internal/controller/neo4jbackup_controller.go:730-736` | `allDatabases = Kind=="Cluster"`; sets `dbName` from `target.Name` otherwise |
| Sharded DB CR | `api/v1beta1/neo4jshardeddatabase_types.go` | `spec.name` is the logical name; per-shard names follow `{name}-g000`, `{name}-p000…` (see `neo4jshardeddatabase_controller.go:493`) |
| Cluster sharding flag | `api/v1beta1/neo4jenterprisecluster_types.go:104` + `PropertyShardingSpec:1476-1485` | `spec.propertySharding.enabled` exists |
| Version helper | `internal/resources/cluster.go:2099-2117` | `IsNeo4jVersion202512OrHigher()` |
| DB listing | `internal/neo4j/client.go:585-637` | `GetDatabases()` returns `[]DatabaseInfo` via `SHOW DATABASES YIELD …` |

## Design

### Phase 1 — Backup (this PR)

#### CRD change

```go
// api/v1beta1/neo4jbackup_types.go
// +kubebuilder:validation:Enum=Cluster;Database;ShardedDatabase
Kind string `json:"kind"`
```

Semantics when `Kind=ShardedDatabase`:
- `Name` = the **logical sharded-database name** (`products`, not `products-g000`).
- `ClusterRef` = REQUIRED (parallels `Database`).
- `Namespace` defaults to the backup's namespace.

#### Validation split (Decision 3)

**Validator (`internal/validation/backup_validator.go`) — in-CR only, no k8s client added:**
1. `Kind` must be a valid enum value (defends against bad CR data even though kubebuilder enforces it).
2. When `Kind=ShardedDatabase`: `ClusterRef` required + non-empty; `Name` non-empty and matches `^[a-zA-Z][a-zA-Z0-9_.\-]*$` (the existing database-name regex applies to the logical sharded name).
3. `Namespace`, if provided, must match the backup's namespace (cross-namespace ClusterRef not supported in v1; same constraint as `Kind=Database`).

**Reconciler (`internal/controller/neo4jbackup_controller.go`) — cross-CR checks before submitting the Job:**
4. Fetch `Neo4jEnterpriseCluster` named `ClusterRef`; verify it exists. (Same code path as today's `Kind=Database`.)
5. Call new helper `IsClusterShardingReady(cluster)` (Decision 4) — checks `spec.propertySharding.enabled=true` AND `IsNeo4jVersion202512OrHigher(image.tag)`. Reject (terminal `Failed` phase with explanatory event) on either failure.
6. Fetch `Neo4jShardedDatabase` CR named `target.Name` in the backup's namespace. Verify it exists; verify its `spec.clusterRef` matches the backup's `target.clusterRef`; verify `status.ShardingReady == true`. If the sharded DB CR is not yet Ready, route to `Pending` + requeue (NOT terminal `Failed` — mirrors CLAUDE.md rule 43's `errBackupNotReady` pattern).
7. **Glob safety**: call `Client.GetDatabases()`. Filter to DBs where `name STARTS WITH "{target.Name}"`. Reject any that don't match the shard regex `^{target.Name}-(g|p)\d{3}$`. The virtual DB does not appear in `SHOW DATABASES` until first access (verified in `neo4jshardeddatabase_controller.go`), so no special-case for `{target.Name}` itself.

#### Command building

Replace the raw-string comparisons with the new constants (Decision 1) and add the third case at `neo4jbackup_controller.go:730`:

```go
allDatabases := backup.Spec.Target.Kind == v1beta1.BackupTargetKindCluster
dbArg := ""
switch backup.Spec.Target.Kind {
case v1beta1.BackupTargetKindDatabase:
    dbArg = backup.Spec.Target.Name           // "products"
case v1beta1.BackupTargetKindShardedDatabase:
    dbArg = backup.Spec.Target.Name + "*"     // "products*" — neo4j-admin expands
}
cmd := neo4j.GetBackupCommand(version, dbArg, toPath, allDatabases, fromAddresses)
```

`GetBackupCommand` already quotes the database-name argument (cluster backups pass `"*"`), so `"products*"` gets the same protection — **no signature change**.

`--remote-address-resolution` wiring is unchanged at `neo4jbackup_controller.go:756`. The defaulting logic (Decision 2) goes earlier, near where `BackupOptions` is read: if `target.Kind == ShardedDatabase`, `version.SupportsRemoteAddressResolution()`, AND `options.RemoteAddressResolution` is nil (i.e. user did not set it), default to `true`.

**Field-type migration (resolved):** `RemoteAddressResolution` is currently `bool` at `neo4jbackup_types.go:109`. To distinguish "user explicitly set false" from "user didn't touch it" — required to honor explicit `false` for debugging without forcing a magic-value workaround — migrate to `*bool`. Touch points (verified by grep):
- `neo4jbackup_types.go:109` — type change.
- `neo4jbackup_controller.go:705` — guard becomes `opts.RemoteAddressResolution != nil && *opts.RemoteAddressResolution && !version.Supports...`.
- `neo4jbackup_controller.go:756` — same dereference pattern.
- Bundle regen via `make sync-all` (CRD + CSV).
- Tests in `client_backup_test.go:13` use the INTERNAL `BackupCommandOptions` struct (`client.go:2723`), which stays `bool` — unaffected.

Storage compatibility: `omitempty` + `*bool` means nil → absent in YAML, `*true` / `*false` → `"true"` / `"false"`. Existing stored CRs with the field omitted (the common case) deserialize to nil with no observable behavior change. Existing CRs with explicit `true` still work. Safe migration.

#### What works unchanged

- Per-run subdirectory via `BACKUP_RUN_ID` (CLAUDE.md rule 70) — users find per-shard `.backup` files in the run subdir.
- CronJob defaults (rule 73): `ConcurrencyPolicy=Forbid`, `StartingDeadlineSeconds=60`, TTL/history limits.
- Retention (per-run subdir is the unit, not per-shard).
- Storage targets: PVC, S3, GCS, Azure — cloud creds, temp-staging PVC, region/endpoint plumbing all unchanged.
- `Neo4jBackup.status.history[]` — each entry covers the whole sharded backup run.
- Suspend, terminal-state guard (issue #116 territory).

#### Effort estimate (revised after ratified decisions)

- Enum + 3 typed constants + raw-string migration (~5 call sites): ~40 lines.
- `IsClusterShardingReady` helper extract + caller migration: ~30 lines + ~50 lines tests.
- Validator in-CR checks: ~30 lines + ~120 lines tests.
- Reconciler cross-CR checks (fetch sharded DB, Ready check, glob safety): ~70 lines + ~200 lines tests.
- `RemoteAddressResolution` `bool` → `*bool` migration (IF needed): ~20 lines + bundle regen. Adds touch points to any existing callers + CSV bump.
- Defaulting logic: ~15 lines + tests.
- Command-builder switch case: ~15 lines.
- Integration test: ~120 lines (local-only via Property Sharding ginkgo focus).
- Bundle regen via `make sync-all` (automatic).

**Total: ~220 lines of code, ~370 lines of tests.** Single PR. The `*bool` migration is the wild card — if the field is already a pointer, this is ~50 lines smaller.

### Phase 2 — Restore (separate future PR; sketched here for completeness)

Make `Neo4jShardedDatabase` the restore vehicle (not `Neo4jRestore`). The existing `Neo4jRestore` flow is built around stop-cluster → extract-archive → start-cluster → recreate, which is fundamentally incompatible with the online `CREATE DATABASE … OPTIONS { seedUri }` model.

```yaml
spec:
  clusterRef: my-cluster
  name: products
  defaultCypherLanguage: "25"
  propertySharding:
    propertyShards: 2
    graphShard: { primaries: 3 }
    propertyShardTopology: { replicas: 1 }
  seed:
    backupRef: products-daily-backup    # Neo4jBackup CR in same ns
  replaceExisting: false                # optional: DROP + CREATE if true
  force: false                          # required alongside replaceExisting=true
```

Controller resolves `seed.backupRef` via the same `resolveBackupRef` helper the restore controller uses today — composes with `errBackupNotReady` → Pending routing (CLAUDE.md rule 72).

**Open questions to validate before Phase 2 ships:**
1. Does `DROP DATABASE {name} DESTROY DATA WAIT` work atomically on a sharded DB? (Undocumented upstream — sharded admin doc is silent on DROP semantics.)
2. Does `seedUri` accept `gs://` and `azb://` in sharded `CREATE DATABASE`? The generic seedUri page confirms `CloudSeedProvider` supports all three, but the sharded admin doc only shows `s3://`. **Probability of working: high. Confidence: needs an integration test.**
3. Does cloud auth on the cluster pods reach every shard primary? IRSA/Workload Identity/Azure Managed Identity work via the SDK default credential chains but aren't documented for sharded restore. Cluster-fetch coordination (primary-only vs. per-member) is undocumented, so credentials must be valid on EVERY pod that might host a shard primary, not just one. **Integration-test in each cloud.**
4. Does resharding-on-restore (4-shard backup → 8-shard target via different `SET PROPERTY SHARDS { COUNT N }`) actually work? (Issue claims yes, docs silent. Plausible because the per-shard files are individually consistent and Neo4j's seed mechanism redistributes — but no upstream confirmation.)
5. Does `seedRestoreUntil` work for sharded `CREATE DATABASE`? The sharded admin doc says PITR isn't supported, but the generic seedUri page says `seedRestoreUntil` is a `CloudSeedProvider` feature. **Likely the sharded doc is authoritative for sharded — don't expose PITR for sharded restores until upstream-verified.**
6. Does `seedSourceDatabase` (Cypher 25 only) interact correctly with sharded restore when the directory contains backups for multiple sharded DBs? The standard logic searches by target DB name; for sharded restore each shard has a different name (`{name}-g000`, `{name}-p000`, …). Unverified.

These are blocking for Phase 2 — needs an integration-test pass against a real 2025.12+ / 2026.05+ cluster across at least S3 + one other cloud.

### Phase 3 — Observability (ship with Phase 1 or shortly after)

Two additions:
1. `Neo4jShardedDatabase.status.lastBackup` (timestamp + Neo4jBackup CR name + run ID) — populated by the backup controller on Succeeded run via reverse-lookup.
2. `Neo4jBackup.status.shardArtifacts[]` (per-shard filename + size) when target is sharded — populated from the backup Job's exit summary OR via a trailing `ls -la` step in the Job command.

Optional bonus: post-backup `neo4j-admin backup validate --from-path=… --database="{name}"` step to surface per-shard health (OK/ahead/behind) in `status.history[].validation`.

**Effort:** ~120 lines + tests. Could ship with Phase 1 or as a fast-follow.

## Risks

1. **Glob-safety regression risk.** `{name}*` matches more than `{name}-{g|p}\d{3}`. The validator's reconcile-time check must use the actual neo4j-admin glob semantics (`STARTS WITH "{name}"`, then exclude shard-pattern matches and reject the rest), not the issue's narrower `STARTS WITH "{name}-"`. Otherwise a `products` sharded DB backup could silently include `productsales` (if it existed) because the validator says "no DB starts with `products-` other than the shards" while neo4j-admin still expands `products*` to include it.

2. **Version drift between 2025.12 (initial sharding support) and 2026.05 (where docs are anchored).** Backup command syntax might differ across point releases. Mitigation: don't add version-specific code paths in Phase 1; rely on neo4j-admin's own behavior. Surface neo4j-admin's exit code & stderr to status.

3. **`--remote-address-resolution` flag semantics undocumented.** Risk that it changes behavior in ways we don't anticipate. Mitigation: integration-test against a real multi-pod sharded cluster before merging.

4. **Phase 2 is operationally load-bearing for production sharded DB users** (severed-replica catastrophic failure mode → drop + restore from backup is the recovery procedure). This raises the priority of Phase 2 above what the issue suggests; shouldn't block Phase 1 but should be tracked.

5. **`gs://` / `azb://` seedUri for sharded restore is inference, not documented.** The generic seedUri docs confirm `CloudSeedProvider` handles all three schemes, but the sharded admin doc only shows `s3://`. Inference is reasonable (same provider plumbing); confidence requires integration testing in each cloud before Phase 2 ships.

6. **Cluster fetch coordination is undocumented.** Whether each cluster member fetches the seed independently or only the primary fetches isn't specified. For sharded restore where each property shard lands on a different primary pod, cloud credentials must be reachable from EVERY pod that could host a shard primary. The operator's existing per-pod credential injection (via env vars or projected Secrets on the StatefulSet) should cover this, but Phase 2 must verify under load.

7. **`seedRestoreUntil` for sharded DBs is contradicted between two docs.** The sharded admin doc says PITR isn't supported; the generic seedUri page says `seedRestoreUntil` is a `CloudSeedProvider` feature. Phase 2 should treat the sharded doc as authoritative and NOT expose PITR for sharded restores, even though Neo4j might silently accept the option.

## Explicitly NOT in scope (for Phase 1)

- Cross-shard distributed transactions during backup — Neo4j doesn't provide the primitive.
- PITR for sharded DBs — Neo4j doesn't support `--restore-until`.
- Coordinated stop-cluster during restore — irrelevant for online sharded restore.
- Resharding on restore as a separate feature — Phase 2, gated on integration testing.
- Backing up the "virtual database" separately — there is no separate store.
- Extending `Neo4jRestore` to handle sharded DBs — different control flow; Phase 2 uses `Neo4jShardedDatabase` instead.

## Phase 1 implementation plan

1. **Branch:** `feat/sharded-db-backup-support` (already created).
2. **Constants + enum (Decision 1):** in `api/v1beta1/neo4jbackup_types.go`:
   - Add `ShardedDatabase` to the kubebuilder enum marker on `Kind`.
   - Define typed constants `BackupTargetKindCluster`, `BackupTargetKindDatabase`, `BackupTargetKindShardedDatabase`.
   - Migrate all raw-string call sites (~5 places per the audit) to use the constants.
3. **Helper extraction (Decision 4):** create `internal/validation/sharding.go` (or a sensible co-location) exposing `IsClusterShardingReady(cluster *v1beta1.Neo4jEnterpriseCluster) error`. Move the inlined logic from `cluster_validator.go:268-288` into the helper; have `validatePropertySharding` call it.
4. **Validator (Decision 3 — in-CR only):** `internal/validation/backup_validator.go` — extend `validateBackupTarget` to accept `ShardedDatabase` in `validKinds` and add the in-CR checks (ClusterRef present, Name regex, namespace match).
5. **Reconciler cross-CR checks (Decision 3):** in `neo4jbackup_controller.go`, in the pre-Job-submit phase that already fetches the cluster, add:
   - Call `IsClusterShardingReady(cluster)`; on error → terminal `Failed` with explanatory event.
   - Fetch `Neo4jShardedDatabase` CR; verify same-cluster + `Status.ShardingReady=true`; not-Ready → `Pending` + requeue (NOT `Failed`).
   - Call `Client.GetDatabases()`; reject any name starting with `target.Name` that doesn't match `^{target.Name}-(g|p)\d{3}$` → terminal `Failed`.
6. **Defaulting (Decision 2):** verify `RemoteAddressResolution` field type. If `bool`, migrate to `*bool` (CRD bump territory — likely fine since the field is recent). When `target.Kind==ShardedDatabase`, version >= 2025.09, and the field is unset, default to `true`. Defaulting happens in the reconciler, not the validator, so the stored CR isn't mutated.
7. **Command builder + controller switch:** add the `BackupTargetKindShardedDatabase` case at `neo4jbackup_controller.go:730`, producing `target.Name + "*"`. `GetBackupCommand` signature unchanged (already quotes the argument).
8. **Tests:**
   - Unit: validator cases (kind enum rejects junk; ClusterRef required; Name regex; namespace match).
   - Unit: reconciler cross-CR checks (cluster missing → Failed; sharding disabled → Failed; version pre-2025.12 → Failed; sharded DB CR missing → Failed; sharded DB CR not Ready → Pending; glob poisoning by unrelated DB → Failed).
   - Unit: command-builder (glob quoting, `--remote-address-resolution` default-true logic).
   - Unit: `IsClusterShardingReady` helper.
   - Integration: `test/integration/backup_sharded_test.go` under the existing `Property Sharding` ginkgo focus (local-only per CLAUDE.md).
9. **Regen:** `make manifests && make generate && make sync-all`.
10. **Drift check:** `make check-drift` before commit.
11. **CLAUDE.md update:** add a new rule capturing the design — `kind=ShardedDatabase` uses `{name}*` glob; controller's glob-safety check uses `^{name}-(g|p)\d{3}$`; `--remote-address-resolution` defaulted-true (not forced) for sharded backups on 2025.09+; sharded DB Ready signal is `Status.ShardingReady`, not the generic `Ready` condition.

## Test plan (Phase 1)

**Unit tests:**
- `backup_validator_test.go`: 6 new cases (one per validator check) + 1 happy-path.
- `version_test.go` (or `client_backup_test.go`): glob quoting + flag presence.
- `neo4jbackup_controller_test.go`: glob-safety reconcile check (mock `GetDatabases()` returning a poisoning DB name).

**Integration test (local-only):**
- Spin up a cluster with `propertySharding.enabled=true` on a 2025.12+ image.
- Apply a `Neo4jShardedDatabase` with name `products`, 2 property shards.
- Apply a `Neo4jBackup` with `target.kind=ShardedDatabase, name=products`.
- Verify the Job's command contains `'products*'` (quoted) and `--remote-address-resolution`.
- Verify the PVC after Job completion contains `products-g000-*.backup`, `products-p000-*.backup`, `products-p001-*.backup`.
- Verify `status.history[0]` records the run.

**Out of scope for Phase 1 tests:**
- Cloud-target backup (S3/GCS/Azure) — already covered for non-sharded paths; sharded glob behavior is the only delta and is verified by the unit tests.
- Retention pruning specifically for sharded backups — same code path as non-sharded.
- Restore — Phase 2.

## Done when (Phase 1)

- User with a property-sharded `Neo4jShardedDatabase` on a `Neo4jEnterpriseCluster` running Neo4j 2025.12+ can:
  1. Create a `Neo4jBackup` with `target.kind: ShardedDatabase, target.name: <logical-name>, target.clusterRef: <cluster>` and have it back up all shards in one Job, recording the run in `status.history`.
  2. Schedule recurring sharded backups via `spec.schedule`.
  3. Apply retention against sharded backups via `spec.retention`.
- Validator rejects: missing clusterRef; sharding disabled on cluster; pre-2025.12 version; non-existent sharded DB CR; mismatched clusterRef between sharded DB and backup.
- Unit + (local) integration tests pass.
- `make check-drift` is clean.

## References

- Upstream sharded admin docs: <https://neo4j.com/docs/operations-manual/2026.05/scalability/sharded-property-databases/admin-operations/>
- Sharded property DB overview: <https://neo4j.com/docs/operations-manual/2026.05/scalability/sharded-property-databases/overview/>
- Generic seedUri docs (canonical for `CREATE DATABASE … OPTIONS { seedUri }`): <https://neo4j.com/docs/operations-manual/current/database-administration/standard-databases/seed-from-uri/>
- Issue #138: <https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues/138>
- Property sharding implementation analysis: `reports/2025-09-03-property-sharding-implementation-analysis.md`
- Backup/restore overhaul: `reports/2026-05-19-backup-restore-overhaul-completion.md`
- CLAUDE.md rules 40–47 (backup/restore invariants the design preserves)
