# Knowledge Base: Backup & Restore

> **Status: EXEMPLAR.** This is the gold-standard knowledge-base entry — the
> template every other subsystem should follow. It re-homes the backup/restore
> regression rules (the old CLAUDE.md "rules 40–79") into structured,
> enforcement-tagged entries. CLAUDE.md no longer carries the checklist; this
> file is the single home.
>
> Every symbol (file path, function, test) below was verified against the tree
> at the time of writing. Stale references found in the old checklist are called
> out inline with a **STALE REF** badge and the corrected current name. When you
> change backup/restore code, update the matching entry here in the same PR —
> the `Pinned-by` test is the machine-checked half of the contract; this entry
> is the human-readable half.

## How to read this file

Each rule is a numbered entry. **Rule IDs are preserved from the original
CLAUDE.md checklist for traceability** — they are intentionally non-contiguous
(some original numbers were merged or retired upstream; 54, 62, 68, 69, 73 do
not appear). Each entry carries:

- **Scope** — the file(s) the rule governs.
- **Rule** — the invariant, stated imperatively.
- **Why** — the failure mode that justifies it (delete the rule and *this*
  breaks).
- **Pinned-by** — the test that mechanically enforces it, with file:identifier.
  `(none)` means no dedicated test guards this rule — treat it as documentation
  only and add a test if you touch the code.
- **Status** — `Enforced` (a verified test pins it), `Documented` (correct but
  no dedicated test), or `Stale-ref-corrected` (the original checklist named a
  symbol that no longer exists; the corrected name is in the entry).

## Architecture invariant (read first)

Backups are **Job-per-`Neo4jBackup`-CR exclusively**. There is no centralized
`{cluster}-backup` StatefulSet, no `spec.backups` field, no `BuildBackupStatefulSet`
builder, and no standalone backup sidecar — all of that was removed (see rule 79)
and must never be reintroduced. The `Neo4jBackup` CRD spawns a one-shot Job (or a
CronJob for scheduled backups); the `Neo4jRestore` CRD restores via Cypher for
cluster targets (rule 75) and via a Job for standalone targets.

`StorageLocation`, `CloudBlock`, `CloudIdentity`, and `AutoCreateSpec` types
survive (verified in `api/v1beta1/neo4jenterprisecluster_types.go`) because
`Neo4jBackup` / `Neo4jRestore` consume them.

---

## Backup: run identity, history & chaining

### Rule 40 — Shared `--to-path` per CR; per-run identity via filename
- **Scope:** `internal/controller/neo4jbackup_controller.go` (`buildToPath`, `chainRoot`, `backupRunIDEnvVar`, `jobToBackupRun`)
- **Rule:** All runs of one `Neo4jBackup` CR share a single `--to-path = <base>/<chain-root>/` (NOT per-run subfolders). Per-run identity is the ISO-8601 timestamp neo4j-admin embeds in each `.backup` filename, captured to `BackupRun.ArtifactFilename` (standard) / `ShardArtifacts.Filename` (sharded). The `BACKUP_RUN_ID` env var stays on the Pod (downward API → Job name) for log correlation only. One-shot Job name = `<backup>-backup`; CronJob child = `<cronjob>-<unix-seconds>`. Never re-introduce a `${BACKUP_RUN_ID}` subfolder under `--to-path`.
- **Why:** `neo4j-admin database backup --type=DIFF` reads the prior FULL artifact from the *same* directory to compute the delta. Per-run subfolders break chaining — every DIFF would fail to find its parent.
- **Pinned-by:** `internal/controller/neo4jbackup_cloud_test.go:TestBackupRunIDEnvVar` + `internal/controller/neo4jbackup_history_test.go:TestJobToBackupRun`
- **Status:** Enforced

### Rule 41 — CronJob backup defaults are load-bearing
- **Scope:** `internal/controller/neo4jbackup_controller.go` (`createBackupCronJob`, `reconcileScheduledHistory`)
- **Rule:** Keep `ConcurrencyPolicy=Forbid`, `StartingDeadlineSeconds=60`, `TTLSecondsAfterFinished=1800`, `SuccessfulJobsHistoryLimit=10`, `FailedJobsHistoryLimit=3`.
- **Why:** These give `reconcileScheduledHistory` a 30-minute window to record a child Job in `status.history` before Kubernetes GCs it. Relaxing them risks losing run history.
- **Pinned-by:** (none — values asserted indirectly via scheduled-history tests in `internal/controller/neo4jbackup_history_test.go`)
- **Status:** Documented

### Rule 78 — `spec.chainFromBackup` composes mixed-cadence FULL+DIFF
- **Scope:** `internal/controller/neo4jbackup_controller.go` (`chainRoot`, `buildToPath`, `validateChainParent`, `waitForChainConcurrencyClear`, `errChainBusy`)
- **Rule:** A daily FULL and an hourly DIFF CR with `chainFromBackup: daily` share `<base>/<chain-root>/` so `--type=DIFF` finds the prior FULL. `chainRoot(backup)` returns `spec.chainFromBackup` if set, else `backup.Name` — used by `buildToPath`, `BackupRun.BackupsPath`, and the `app.kubernetes.io/part-of` Job label. The validator rejects self-reference; `validateChainParent` enforces parent existence + matching target + matching storage. `waitForChainConcurrencyClear` refuses to start while any Job with the same `part-of` label has `status.active>0`, routing `errChainBusy` to Pending+requeue.
- **Why:** Two backups writing the same directory concurrently corrupt neo4j-admin's chain detection. The PVC path additionally takes a `flock` on `<to-path>/.chain.lock` (`chainLockWaitSeconds`) because two CronJob children can still fire within the reconcile gap that the creation-time gate misses.
- **Pinned-by:** end-to-end by `test/integration/backup_chain_test.go` (the `It("composes a daily FULL + hourly DIFF chain via spec.chainFromBackup", …)` spec — verifies both Jobs share `part-of: inventory-daily` and the DIFF restore applies the full chain). Unit-level: `internal/controller/neo4jbackup_lifecycle_test.go:TestChainConcurrency_SeesCronJobChildren` + `:TestValidateChainParent_NotFoundIsTransient` + `:TestBuildRetentionScript_FileLevelChainScoped`; PVC chain-dir flock by `internal/controller/backup_restore_field_findings_test.go:TestBuildBackupCommand_PVCChainDirFlock`.
- **Status:** Enforced — the original checklist's `backup_chain_test.go` reference is **accurate**: it lives at `test/integration/backup_chain_test.go` (not under `internal/controller/`).

---

## Backup: resolver & transient-vs-terminal routing

### Rule 55 — `ResolveBackupRef` is the canonical name→StorageLocation resolver
- **Scope:** `internal/controller/backup_resolver.go` (`ResolveBackupRef`, `ErrBackupNotReady`)
- **Rule:** Its callers — `Neo4jRestore` (`neo4jrestore_controller.go`, via the `resolveBackupRef` wrapper and a direct call) and `Neo4jShardedDatabase` (`neo4jshardeddatabase_seed.go`) — delegate to `ResolveBackupRef(ctx, client.Reader, backupRef, namespace)`. (`Neo4jDatabase` does NOT use it: it has no `backupRef` field, only a user-supplied `seedURI` string.) It returns a wrapped `ErrBackupNotReady` when the backup exists but has no Succeeded run — callers `errors.Is` to route Pending+requeue. Never duplicate the lookup; never compare error strings.
- **Why:** A single resolver keeps the "backup not ready yet" → transient routing consistent. String-matching error messages (the pre-fix approach) misclassified RBAC denials and other API failures as "not found".
- **Pinned-by:** `internal/controller/neo4jrestore_cloud_test.go:TestResolveRestoreSource_BackupRefNoSucceededRun_IsTransient` + `:TestResolveRestoreSource_BackupRefMissingCR_IsPermanent`
- **Status:** Enforced — note the canonical sentinel is the exported `ErrBackupNotReady`; the package-internal alias `errBackupNotReady` in `neo4jrestore_controller.go` is kept for backward compat (new code uses `ErrBackupNotReady`).

### Rule 43 — `errBackupNotReady` → Pending, not Failed
- **Scope:** `internal/controller/backup_resolver.go`, `internal/controller/neo4jrestore_controller.go` (`startRestore`)
- **Rule:** `ResolveBackupRef` wraps `ErrBackupNotReady` via `fmt.Errorf("…%w")` when history has no Succeeded run; `startRestore` uses `errors.Is` and routes Pending+requeue. Pending is NOT in the "previously failed" guard set, so it re-promotes. Missing-CR errors stay terminal Failed.
- **Why:** `kubectl apply -f dir/` commonly creates the restore before its backup completes. A terminal Failed here is pinned forever (the previously-failed guard never re-enters) even after the backup succeeds.
- **Pinned-by:** `internal/controller/neo4jrestore_cloud_test.go:TestResolveRestoreSource_BackupRefNoSucceededRun_IsTransient` + `:TestResolveRestoreSource_BackupRefMissingCR_IsPermanent`
- **Status:** Enforced

### Rule 42 — `source.type: backup` resolved upstream; the `case "backup"` is dead-code
- **Scope:** `internal/controller/neo4jrestore_controller.go` (`resolveRestoreSource`, `buildRestoreCommand`)
- **Rule:** `resolveRestoreSource` swaps `Spec.Source` away from `"backup"` on a shallow restore copy and threads the concrete `StorageLocation` through every builder. `buildRestoreCommand`'s `case SourceTypeBackup:` is therefore unreachable and carries a defensive `internal:` error (`"source.type=backup reached buildRestoreCommand without being resolved"`).
- **Why:** Without upstream dereference, `type=backup` restores silently pointed at an empty volume (the legacy `case "backup"` branch hardcoded `/backup/<backup-name>` over an EmptyDir).
- **Pinned-by:** (none dedicated — exercised indirectly via the resolver tests in `internal/controller/neo4jrestore_cloud_test.go`)
- **Status:** Documented

---

## Standalone restore (Job + `neo4j-admin restore`)

### Rule 44 — Standalone `--from-path` is a FILE via shell substitution; `tail -1` picks the latest
- **Scope:** `internal/controller/neo4jrestore_controller.go` (`resolveLocalPVCFromPath`, `buildLocalRestoreFilePath`)
- **Rule:** `resolveLocalPVCFromPath(backupPath, databaseName)` emits `$(ls '<backupPath>'/'<dbname>'-*.backup | tail -1)`. **BOTH the path AND the database name MUST go through `shellQuote()`.** `tail -1` (not `head -1`) selects the latest run in the shared chain dir (rule 40), because neo4j-admin's ISO-8601 timestamp sorts lexicographically into chronological order. Cloud URIs skip this (neo4j-admin's native readers handle file selection). Never pass the directory; never substitute the timestamp in Go; never drop quoting; never revert to `head -1`.
- **Why:** `spec.source.backupPath` and `spec.databaseName` are user-controlled and land in a `/bin/sh -c` command in a Pod that mounts `/data` RW and carries `NEO4J_ADMIN_PASSWORD`. An unquoted value like `foo; rm -rf /data #` would escape the `ls` and run arbitrary commands.
- **Pinned-by:** `internal/controller/neo4jrestore_cloud_test.go:TestResolveLocalPVCFromPath_BackupPathShellInjectionGuard` + `:TestResolveLocalPVCFromPath_NestedCommandSubstitutionGuard` + `:TestResolveLocalPVCFromPath_EmbeddedSingleQuoteGuard`
- **Status:** Enforced — **NOTE:** the checklist named the helper `buildLocalRestoreFilePath`. That function exists but is a thin wrapper; the function that actually emits the `$(ls … | tail -1)` form (and that the three guard tests target) is `resolveLocalPVCFromPath`. One stale doc-comment at `neo4jrestore_controller.go:1774` still says `head -1`; the *emitted command* correctly uses `tail -1` (`:1736`). Treat the comment as a doc nit, not a behavior bug.

### Rule 45 — Restore `--temp-path=/tmp/restore-tmp` default for PVC sources
- **Scope:** `internal/controller/neo4jrestore_controller.go` (`buildRestoreCommand` PVC branch)
- **Rule:** PVC sources emit `--temp-path=/tmp/restore-tmp` plus a `rm -rf /tmp/restore-tmp && mkdir -p /tmp/restore-tmp &&` prelude (neo4j-admin needs an empty staging dir). Explicit `Options.TempStorage` / `Options.TempPath` win. Cloud URIs skip.
- **Why:** The backup PVC is mounted ReadOnly, so neo4j-admin can't extract in place — without `--temp-path` it fails with `FileSystemException: Read-only file system`.
- **Pinned-by:** (none dedicated — covered by `internal/controller/neo4jrestore_cloud_test.go` restore-command assertions)
- **Status:** Documented

### Rule 46 — Restore reconcile race tolerance
- **Scope:** `internal/controller/neo4jrestore_controller.go` (`createRestoreJob`, `startCluster`)
- **Rule:** Job creation treats `AlreadyExists` as "another reconcile got there first" and re-fetches; `startCluster` treats a missing `neo4j.neo4j.com/original-replicas` annotation as "first reconcile already deleted it" and returns nil. Don't revert either.
- **Why:** Two reconciles race during the ~10s `stopCluster` scale-down window. Reverting either flips successful restores to terminal Failed.
- **Pinned-by:** (none dedicated — exercised by coordination specs in `internal/controller/neo4jrestore_coordination_test.go`)
- **Status:** Documented

### Rule 47 — Legacy post-restore re-seed via `dbms.[cluster.]recreateDatabase` (Job/standalone path only)
- **Scope:** `internal/controller/neo4jrestore_controller.go` (`recreateRestoredDatabaseOnCluster`), `internal/neo4j/version.go` (`RecreateDatabaseProcedure`)
- **Rule:** After the Job-based restore writes server-0's PVC, `recreateRestoredDatabaseOnCluster` forces all servers to re-seed from server-0. server-0 is matched by `cluster.Name + "-server-0"` against `SHOW SERVERS YIELD address` (the `name` column is unreliable). The procedure comes from `version.RecreateDatabaseProcedure()`: `dbms.cluster.recreateDatabase` (5.24–2025.03) → `dbms.recreateDatabase` (2025.04+). Skipped for standalone, `Topology.Servers < 2`, and pre-5.24 SemVer / pre-2025.02 CalVer. Non-fatal — failure emits a Warning `DatabaseCreateFailed`.
- **Why:** Post-restart bootstrap picks the database's primary non-deterministically; if a stale-data server wins consensus, the restored data is overwritten when others re-sync. The cluster Cypher path (rule 75) doesn't need this — it seeds every server from the URI in parallel.
- **Pinned-by:** procedure-name selection by `internal/neo4j/version_test.go` (`RecreateDatabaseProcedure` cases); controller wiring (none dedicated)
- **Status:** Documented

---

## Cluster restore (Cypher, never a Job)

### Rule 75 — Cluster `Neo4jRestore` uses Cypher, NOT a Job
- **Scope:** `internal/controller/neo4jrestore_controller.go` (`startRestore`, `isRestoreTargetTrueCluster`, `startClusterCypherRestore`, `validateRestore`)
- **Rule:** `startRestore` branches via `isRestoreTargetTrueCluster`: cluster + standard DB → `startClusterCypherRestore` (`DatabaseExists` → `RecreateDatabaseWithSeedURI` if it exists, `CreateDatabaseWithSeedURIOptions` if not); standalone → Job + `neo4j-admin restore`; sharded → rejected by `validateRestore` pointing at rule 63. Never re-introduce a cluster-target Job path.
- **Why:** `neo4j-admin restore --overwrite-destination` is unsafe on a cluster — clusters carry additional state that becomes inconsistent with a file-level restore.
- **Pinned-by:** (none dedicated unit test for the branch; integration coverage in `test/integration/standard_database_minio_restore_test.go`)
- **Status:** Documented

### Rule 76 — seedURI = exact `.backup` FILE; wait for online before Completed
- **Scope:** `internal/controller/neo4jrestore_controller.go` (`startClusterCypherRestore`, `pollClusterRestoreOnline`, `markCypherRestoreIssued`), `internal/neo4j/client.go` (`DatabaseOnlineState`, `DatabaseSeedFailureMessage`)
- **Rule:** The cluster seedURI is the exact `.backup` file, never a directory — Neo4j's seed providers seed a single DB from one file. Cloud → `buildSeedURIFromBackupStorage` builds the dir then the filename is appended via `resolvedOrLiveArtifactFilename` / `latestSucceededArtifactFilename` (latest Succeeded run); PVC → `resolveClusterPVCRestoreURI` spawns the seed proxy (rule 71) at the filename. A single DIFF-file URI suffices — Neo4j resolves the full parent chain from the same dir. **After the asynchronous `dbms.recreateDatabase`, the operator MUST confirm the database converged online before marking Completed** — the procedure returns before the seed finishes, so a bare return is false-success. `CREATE DATABASE … OPTIONS{seedURI} WAIT` blocks; recreate does not.
- **Why:** A directory URI fails (`Can't open seed file: …/<chain-root>`). Declaring Completed on the recreate's early return reports success while the seed is still downloading (or has silently failed — `CREATE … WAIT` returns without error on a failed download and leaves the allocation offline with the reason in `SHOW DATABASE`'s statusMessage).
- **Pinned-by:** (none dedicated unit test; the online-wait/false-success guards are integration-verified in `test/integration/standard_database_minio_restore_test.go`)
- **Status:** Stale-ref-corrected — **STALE REF:** the checklist said the operator "MUST `WaitForDatabaseOnline` before marking Completed". **No function named `WaitForDatabaseOnline` exists.** The wait is implemented as: issue the recreate exactly once (guarded by the `neo4j.com/cypher-restore-issued` annotation), then hand off to the requeue-driven `pollClusterRestoreOnline`, which polls `DatabaseOnlineState` / `DatabaseSeedFailureMessage` per reconcile (bounded by `cypherRestoreOnlineTimeout` = 5m). The intent of the rule is correct; only the symbol name was wrong.

### Rule 77 — `RecreateDatabaseWithSeedURI` vs `RecreateDatabase` (seedingServers)
- **Scope:** `internal/neo4j/client.go` (`RecreateDatabaseWithSeedURI`, `RecreateDatabase`)
- **Rule:** `RecreateDatabaseWithSeedURI` is the cluster-native restore primitive — every server pulls from the URI in parallel, no Job. `RecreateDatabase` (seedingServerIDs) is the post-`neo4j-admin restore` consistency fix that picks server-0 as seed — legacy/standalone only (rule 47). Never use seedingServers where seedURI works.
- **Why:** seedingServers re-seeds from one server's local store, which only makes sense after a Job wrote that store; the seedURI variant is the correct cluster-wide path.
- **Pinned-by:** (none dedicated)
- **Status:** Documented

---

## Sharded backup & restore

### Rule 48 — Sharded backup uses `{name}*` glob + always-quoted db arg
- **Scope:** `internal/controller/neo4jbackup_controller.go` (`buildBackupCommand`), `internal/neo4j/version.go` (`GetBackupCommand`), `internal/controller/neo4jbackup_sharded.go` (`shardedPreflightGlobSafety`, `shardedPreflightStatic`)
- **Rule:** One `neo4j-admin database backup "{name}*"` captures every shard consistently. `GetBackupCommand` ALWAYS double-quotes the database arg so the shell can't pre-expand the glob (`--to-path` is single-quoted via `shellQuoteArg`). `shardedPreflightGlobSafety` rejects any DB matching `{name}*` outside `^{name}-(g|p)\d{3}$` (terminal Failed). `shardedPreflightStatic` routes a not-yet-Ready sharded DB to Pending.
- **Why:** Without quoting, the shell expands `*` before neo4j-admin sees it. Without the glob-safety check, a backup for `products` could silently sweep in an unrelated `productsales` database.
- **Pinned-by:** `internal/neo4j/version_test.go:TestGetBackupCommandQuotesShardedGlob` + `:TestGetBackupCommandQuotesPlainName`
- **Status:** Enforced

### Rule 49 — `--remote-address-resolution` is `*bool` with sharded-aware defaulting
- **Scope:** `internal/controller/neo4jbackup_sharded.go` / `internal/controller/neo4jbackup_controller.go` (`effectiveRemoteAddressResolution`, emitted in `buildBackupCommand`)
- **Rule:** `effectiveRemoteAddressResolution` defaults `true` ONLY when `kind=ShardedDatabase` AND Neo4j ≥ 2025.09 AND the user didn't set it. Explicit values win. The flag is emitted OUTSIDE the `Options != nil` guard (the sharded default fires even for a spec that sets only target + storage). Never re-introduce a `bool` zero-value default.
- **Why:** A `bool` field can't distinguish "user set false" from "unset", so the sharded default would clobber an explicit false; and gating emission on `Options != nil` would swallow the default for minimal specs.
- **Pinned-by:** `internal/controller/neo4jbackup_sharded_test.go:TestEffectiveRemoteAddressResolution`
- **Status:** Enforced

### Rule 50 — `IsClusterShardingReady` is the canonical sharding precondition
- **Scope:** `internal/validation/sharding.go` (`IsClusterShardingReady`), callers in `internal/controller/neo4jbackup_sharded.go` + `internal/validation/cluster_validator.go`
- **Rule:** `IsClusterShardingReady(cluster)` returns nil only when `cluster.spec.propertySharding.enabled=true` AND `IsNeo4jVersion202512OrHigher(image.tag)`. Used by the cluster validator + backup reconciler preflight; never inline the check at new call sites.
- **Why:** Centralizing the precondition keeps backup preflight and cluster validation in lockstep — a property-sharded backup must never run against a cluster that isn't configured for sharding on a sufficiently new Neo4j.
- **Pinned-by:** (none dedicated for the helper; `IsNeo4jVersion202512OrHigher` is exercised in `internal/resources` tests)
- **Status:** Documented

### Rule 51 — Sharded DB Ready signal is `Status.ShardingReady` (bool pointer)
- **Scope:** `api/v1beta1/neo4jshardeddatabase_types.go` (`Status.ShardingReady`), read in `internal/controller/neo4jbackup_sharded.go`
- **Rule:** Sharded-DB readiness for backups is `Neo4jShardedDatabase.Status.ShardingReady` (a `*bool`), NOT the generic `Ready` condition.
- **Why:** The generic Ready condition is coarser and would let backups run before all shards exist. (The *cluster* CR has a separate `Status.PropertyShardingReady` field — don't confuse the two.)
- **Pinned-by:** (none dedicated)
- **Status:** Documented

### Rule 52 — `Neo4jShardedDatabase.status.lastBackup` reverse-lookup is non-fatal observability
- **Scope:** `internal/controller/neo4jbackup_sharded.go` (`updateShardedDBLastBackup`), `internal/controller/neo4jbackup_controller.go` (`recordOneShotBackupRun`, `reconcileScheduledHistory`)
- **Rule:** `updateShardedDBLastBackup` populates the sharded DB CR's `status.lastBackup` from both the one-shot (`recordOneShotBackupRun`) and CronJob (`reconcileScheduledHistory`) paths. Only Succeeded runs update it; Failed runs don't overwrite. CR-not-found is logged and swallowed. The source of truth stays `Neo4jBackup.status.history`.
- **Why:** It's a UX hint so operators can audit backup health from the sharded DB CR without grepping `Neo4jBackup` CRs — never gate reconcile state on it.
- **Pinned-by:** (none dedicated)
- **Status:** Documented

### Rule 53 — `BackupRun.ShardArtifacts.ShardName` derived from spec
- **Scope:** `internal/controller/neo4jbackup_sharded.go` (`expectedShardArtifactsForBackup`)
- **Rule:** `expectedShardArtifactsForBackup` reads `propertySharding.propertyShards` from the `Neo4jShardedDatabase` spec and emits `{name}-g000` + `{name}-p000…p{N-1}`. `ShardName` is the audit-load-bearing field; `Filename`/`Size` are populated opportunistically by Pod-log parsing (rule 67) and stay informational.
- **Why:** The audit question "did all shards back up?" is answered by `ShardName` alone, which is derivable from the spec without log parsing.
- **Pinned-by:** (none dedicated)
- **Status:** Documented

### Rule 63 — `replaceExisting` + `force` = destructive sharded restore
- **Scope:** `api/v1beta1/neo4jshardeddatabase_types.go`, `internal/controller/neo4jshardeddatabase_controller.go`
- **Rule:** Both `replaceExisting` and `force` true → `CYPHER 25 DROP DATABASE {name} IF EXISTS DESTROY DATA WAIT` before the standard CREATE. Validator: `replaceExisting=true` requires `force=true`; mutex with `ifNotExists=true`; requires a seed source. The DROP is idempotent across requeues.
- **Why:** Destructive data loss must require an explicit second confirmation field (`force`) so an accidental `replaceExisting` flip can't wipe a database.
- **Pinned-by:** `test/integration/property_sharding_minio_restore_test.go` (the `It("destructively replaces an existing sharded DB via replaceExisting+force", …)` spec, ~line 323)
- **Status:** Enforced (integration)

### Rule 64 — `Status.LastDestructiveRestoreGeneration` gates `replaceExisting`
- **Scope:** `api/v1beta1/neo4jshardeddatabase_types.go` (`Status.LastDestructiveRestoreGeneration`), `internal/controller/neo4jshardeddatabase_controller.go`
- **Rule:** The destructive branch fires only when `LastDestructiveRestoreGeneration < Generation`; it stamps `= Generation` on success. Re-trigger by mutating the spec (which bumps generation) — typically editing `seedBackupRef`.
- **Why:** Without the generation gate, every reconcile of a `replaceExisting=true` CR would re-drop and re-seed the database in a loop.
- **Pinned-by:** `internal/controller/neo4jrestore_cloud_test.go` (`LastDestructiveRestoreGeneration < sd.Generation` gating logic, ~line 1057); integration in `property_sharding_minio_restore_test.go` (`LastDestructiveRestoreGeneration` stamped, ~line 432)
- **Status:** Enforced

### Rule 65 — Sharded DDL requires `CYPHER 25` prefix
- **Scope:** `internal/controller/neo4jshardeddatabase_controller.go` (CREATE / DROP statements)
- **Rule:** Every sharded CREATE/DROP statement must prepend `CYPHER 25`.
- **Why:** The 2026.x system DB defaults to Cypher 5; without the prefix the sharded syntax fails to parse. Same invariant as AUTH RULE (the user/auth subsystem's rule 30).
- **Pinned-by:** (none dedicated; integration-exercised in `property_sharding_minio_restore_test.go`)
- **Status:** Documented

### Rule 66 — `Spec.IfNotExists` is `*bool`; callers use `IfNotExistsEffective()`
- **Scope:** `api/v1beta1/neo4jshardeddatabase_types.go` (`IfNotExistsEffective`)
- **Rule:** `Neo4jShardedDatabase.spec.IfNotExists` is a `*bool`. Callers MUST use `Spec.IfNotExistsEffective()`, never dereference directly.
- **Why:** kubebuilder `+default=true` on a `bool omitempty` field silently re-applies the default when the user sets `false`. A pointer preserves explicit-false.
- **Pinned-by:** `test/integration/property_sharding_minio_restore_test.go` (Phase 2c replaceExisting spec — explicit-false `ifNotExists` is required for the destructive path)
- **Status:** Enforced (integration)

### Rule 72 — `ResolvedShardedSeed.URI` vs `PerShardURIs` are mutually exclusive
- **Scope:** `internal/controller/neo4jshardeddatabase_seed.go` (`ResolvedShardedSeed`, `resolveShardedSeed`, `resolvePVCShardedSeed`)
- **Rule:** Cloud → `URI` (`OPTIONS { seedURI: <uri> }`); PVC → `PerShardURIs`, emitted under the **same singular** Cypher key with a map value (`OPTIONS { seedURI: { `shard`: <uri>, … } }`). The emitted Cypher OPTIONS key is `seedURI` (singular) in BOTH cases — even though the CR spec field for the per-shard map is `spec.seedURIs` (plural); see `buildShardedDatabaseOptions` (`neo4jshardeddatabase_controller.go`, ~L639). Wire ONE and clear the OTHER — the validator rejects both populated. `ProxyAvailable=false` → requeue while the proxy rolls out.
- **Why:** The two seed shapes are different Cypher OPTIONS clauses; populating both is ambiguous.
- **Pinned-by:** (none dedicated unit test; integration in `property_sharding_minio_restore_test.go`)
- **Status:** Documented — **NOTE:** the checklist referred to `resolveShardedSeed`/`ResolveShardedSeed` with the signature `(uri, credsSecretName, err)`. The actual function is **lowercase** `resolveShardedSeed` and returns `(*ResolvedShardedSeed, error)` — the `URI`, `PerShardURIs`, `CredsSecretName`, and `ProxyAvailable` are fields on the returned struct, not a multi-value return.

---

## PVC seed proxy

### Rule 71 — PVC seed proxy + `URLConnectionSeedProvider`
- **Scope:** `internal/controller/pvc_seed_proxy.go` (`ensurePVCSeedProxyResources`, `pvcSeedProxyURL`, `teardownPVCSeedProxyResources`, `ensurePVCSeedProxyNetworkPolicy`), gate in `internal/controller/neo4jshardeddatabase_seed.go` (`resolvePVCShardedSeed`)
- **Rule:** PVC-backed `seedBackupRef` (sharded) and PVC-backed cluster `Neo4jRestore` spawn a `backup-seed-proxy-<owner>` Deployment + Service via `ensurePVCSeedProxyResources` — the backup PVC is mounted RO, busybox httpd serves it on `:8080`, owner-ref'd to the consuming CR and torn down explicitly on finalize. URLs target the exact `.backup` filename (`URLConnectionSeedProvider` only supports single-file URIs). **Hard-gated on F3 filename capture: an empty `ShardArtifacts.Filename` is rejected** before any proxy URL is built.
- **Why:** `URLConnectionSeedProvider` fetches one file per URL — it can't enumerate a directory. If F3 Pod-log parsing didn't capture the per-shard filenames, there's no file to point the proxy at, so the seed must fail fast with an actionable error rather than build a broken URL.
- **Pinned-by:** (none dedicated unit test; integration in `test/integration/property_sharding_minio_restore_test.go` + `standard_database_minio_restore_test.go`)
- **Status:** Documented — **NOTE:** the checklist said "empty filename → validator rejects." There is no separate admission validator (the project forbids webhooks); the gate is enforced inline in the reconciler at `neo4jshardeddatabase_seed.go:140-148` (`resolvePVCShardedSeed`), which returns an actionable error for empty `Filename`. Substance correct; "validator" is imprecise — it's controller-side validation.

---

## Seed credentials projection

### Rule 56 — `spec.seedBackupRef` supports cloud + PVC only
- **Scope:** `internal/controller/neo4jshardeddatabase_seed.go` (`resolveShardedSeed`)
- **Rule:** `spec.seedBackupRef` supports cloud (CloudSeedProvider) and PVC (HTTP proxy + URLConnectionSeedProvider — rule 71). Other storage types are rejected (`"seedBackupRef does not support storage type %q"`).
- **Why:** Only these two have a defined seed path; anything else would silently produce a non-functional seedURI.
- **Pinned-by:** (none dedicated)
- **Status:** Documented

### Rule 57 — `seedBackupRef` mutex with `seedURI` / `seedURIs`
- **Scope:** sharded DB validator + `internal/controller/neo4jshardeddatabase_seed.go`
- **Rule:** The validator rejects combining `seedBackupRef` with `seedURI` / `seedURIs`. `seedBackupRef` materializes into `seedURI` at reconcile time on a shallow in-memory copy — the original spec is not persisted with the resolved URI.
- **Why:** Two competing seed sources are ambiguous; resolving on a copy keeps the user's spec clean.
- **Pinned-by:** (none dedicated)
- **Status:** Documented

### Rule 58 — Sharded phase "Pending" reserved for `seedBackupRef` waits
- **Scope:** `internal/controller/neo4jshardeddatabase_controller.go` (`resolveShardedSeed` → phase routing)
- **Rule:** When `resolveShardedSeed` returns `errors.Is(err, ErrBackupNotReady)`, set `status.phase=Pending` and requeue. Don't route other transient conditions through Pending without explicit design.
- **Why:** Reserving Pending for the seed-wait keeps the phase semantically meaningful for operators reading status.
- **Pinned-by:** (none dedicated)
- **Status:** Documented

### Rule 59 — Seed-creds projection via `spec.extraEnvFrom` (cluster + standalone)
- **Scope:** `api/v1beta1/seed_creds_target.go`, `internal/controller/cluster_seed_creds.go`
- **Rule:** `CREATE DATABASE … OPTIONS { seedURI }` runs on the Neo4j server pods, so cloud creds must be in their env (or the JVM's SDK default chain can't authenticate). Both CRs' `spec.extraEnvFrom` wire onto the neo4j container's `envFrom`. Generic (cloud creds, plugin tokens, any Secret-projected env). An empty `credsSecretName` is a no-op (user on IRSA / Workload Identity).
- **Why:** The server JVM — not the operator — fetches the seed, so the creds must live on the server pods.
- **Pinned-by:** (none dedicated for the env wiring)
- **Status:** Documented

### Rule 60 — `SeedCredsTarget` interface + `EnsureSeedCredsProjected`
- **Scope:** `internal/controller/cluster_seed_creds.go` (`SeedCredsTarget`, `EnsureSeedCredsProjected`, `EnsureClusterHasSeedCreds`), `api/v1beta1/seed_creds_target.go` (interface impls)
- **Rule:** `SeedCredsTarget` + `EnsureSeedCredsProjected` are the canonical projection check. Both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone` implement the interface via `api/v1beta1/seed_creds_target.go`. New callers go through the interface (the concrete `EnsureClusterHasSeedCreds` wrapper is retained only for the existing sharded-DB call site and is marked Deprecated in code).
- **Why:** The interface lets cluster + standalone restore paths share the projection plumbing without templated duplication.
- **Pinned-by:** (none dedicated)
- **Status:** Documented — **NOTE:** the checklist also described `ResolveShardedSeed` returning `(uri, credsSecretName, err)`. As corrected under rule 72, the function is `resolveShardedSeed` and `CredsSecretName` is a field on `ResolvedShardedSeed`. The wiring into `EnsureSeedCredsProjected` reads that field.

### Rule 61 — Auto-inherit seed creds is annotation-gated, triggers a rolling restart
- **Scope:** `internal/controller/cluster_seed_creds.go` (`AutoInheritSeedCredsAnnotation`, `AutoInheritedFromAnnotation`, `EnsureSeedCredsProjected`)
- **Rule:** Without `neo4j.com/auto-inherit-seed-creds=true` on the *hosting* CR, missing projection emits an actionable copy-pasteable error. With it, the operator patches `spec.extraEnvFrom`, records the source in `neo4j.com/seed-creds-auto-inherited-from`, rolls out the STS, and the DB controller routes to Pending+requeue. Never auto-inherit without the annotation. The `RetryOnConflict` block re-fetches inside the loop (the owning controller rewrites its CR every reconcile).
- **Why:** Patching `extraEnvFrom` triggers a rolling restart of server pods — a sharded-DB controller must not do that unsolicited. The annotation lives on the cluster/standalone CR because infrastructure operators own that authorization.
- **Pinned-by:** (none dedicated)
- **Status:** Documented

---

## Backup options: validate & log parsing

### Rule 70 — `BackupOptions.Validate` is `*bool` opt-in
- **Scope:** `internal/controller/neo4jbackup_controller.go` (`buildBackupCommand` validate clause, `warnValidateUnsupported`), `internal/controller/neo4jbackup_log_parser.go` (`parseValidationFromLog`)
- **Rule:** When `*Validate == true`, append `&& (neo4j-admin backup validate --from-path=… --database="…" || true)` (validate failures don't fail the Job). The operator parses the Pod log into `BackupRun.Validation`: all rows OK → OK; any Ahead/Behind → Degraded; no parseable rows → Unknown + truncated `RawOutput` (capped 2 KiB). For sharded, the validate dbArg is the literal name with the trailing `*` stripped (validate auto-discovers shards). `neo4j-admin backup validate` exists only on CalVer — on 5.26 the clause is skipped and `warnValidateUnsupported` emits a one-time Warning at Job/CronJob creation.
- **Why:** Validate is informational; failing the Job on it would discard a successful backup. The `*` glob is stripped because `validate` evaluates `foo*-g000` literally and errors, whereas `backup` needs the glob to capture all shards in one invocation.
- **Pinned-by:** (none dedicated unit test for the clause; log-parser behavior in `internal/controller/neo4jbackup_log_parser.go` paths)
- **Status:** Documented

### Rule 67 — Backup-Pod log parsing is opportunistic, not load-bearing
- **Scope:** `internal/controller/neo4jbackup_controller.go` (`Neo4jBackupReconciler.Clientset`), `internal/controller/neo4jbackup_log_parser.go` (`fetchBackupPodLog`, `parseShardArtifactsFromLog`, `parseStandardArtifactFromLog`, `parseValidationFromLog`, `mergeShardArtifactsFromLog`)
- **Rule:** `Reconciler.Clientset` (typed Kubernetes client) enables Pod-log fetches that populate `BackupRun.ShardArtifacts.Filename/Size`, `BackupRun.ArtifactFilename`, and `BackupRun.Validation`. All best-effort: log-fetch failures, format drift, or `Clientset == nil` (unit tests with a fake client) leave the fields empty without failing the reconcile. `ShardName` / `RunID` / `Status` are load-bearing — never gate reconcile state on parsed filename/size.
- **Why:** Pod logs are a soft signal (TTL'd, format may drift). Gating reconcile correctness on them would make backups fail for cosmetic reasons.
- **Pinned-by:** (none dedicated; covered by parser unit paths in `internal/controller/neo4jbackup_log_parser.go`)
- **Status:** Documented

---

## Seed-from-URI providers config

### Rule 74 — `dbms.databases.seed_from_uri_providers` is version-gated
- **Scope:** `internal/resources/cluster.go` (`SeedFromURIProvidersConfigValue`, `IsNeo4jVersion202604OrHigher`)
- **Rule:** `SeedFromURIProvidersConfigValue(imageTag)` returns the base `CloudSeedProvider,FileSeedProvider,URLConnectionSeedProvider`; `ServerSeedProvider` is appended only on `IsNeo4jVersion202604OrHigher`. Both the cluster and standalone config builders call the helper — never inline the value. The deprecated `S3SeedProvider` is excluded across all versions (`CloudSeedProvider` handles `s3://` via the SDK default credential chain — rule 59).
- **Why:** `ServerSeedProvider` doesn't exist before 2026.04; listing it on an older image makes Neo4j refuse to bootstrap. `S3SeedProvider` is deprecated and redundant with `CloudSeedProvider`.
- **Pinned-by:** `internal/resources/seed_providers_test.go:TestSeedFromURIProvidersConfigValue` + `:TestIsNeo4jVersion202604OrHigher`
- **Status:** Enforced

---

## Removal invariant

### Rule 79 — `spec.backups` and all centralized-backup plumbing are REMOVED
- **Scope:** repo-wide (`api/v1beta1/`, `internal/resources/`, `internal/controller/`, `internal/validation/`)
- **Rule:** There is no `spec.backups` / `spec.storage.backupStorage` field, no `BackupsSpec` / `BackupStorageSpec` type, no `BuildBackupStatefulSet` / `buildCentralizedBackup*` builder, no standalone `buildBackupSidecarContainer`, and no `cloud_validator.go`. The `Neo4jBackup` CRD (Job-per-CR) is the only backup path. `StorageLocation` / `CloudBlock` / `CloudIdentity` / `AutoCreateSpec` types survive because `Neo4jBackup` / `Neo4jRestore` use them. **Never reintroduce a `spec.backups` field or a long-running backup pod/sidecar.**
- **Why:** The centralized backup StatefulSet / sidecar architecture was deliberately removed in the backup-restore overhaul; reintroducing it resurrects a banned architecture and a long-running pod that holds cloud credentials indefinitely.
- **Verified (this audit):** `grep` confirmed `BuildBackupStatefulSet`, `BackupsSpec`, `BackupStorageSpec`, `buildBackupSidecarContainer`, and `cloud_validator.go` are ALL absent from the tree (worktrees excluded); the four surviving types are present in `api/v1beta1/neo4jenterprisecluster_types.go`.
- **Pinned-by:** (enforced by absence — a CI guard script that greps for these banned symbols is the recommended machine check; none exists in the current tree)
- **Status:** Enforced (by absence) — candidate for a dedicated CI guard script

---

## Cross-references

- **AUTH RULE / sharded DDL `CYPHER 25` prefix** (rules 30, 65) — same root cause: 2026.x system DB defaults to Cypher 5.
- **`RetryOnConflict` on every status/spec write** (Key Implementation Patterns) — required across all backup/restore status updates for Neo4j 2025.01.0 cluster formation; `updateShardedDBLastBackup` and `markCypherRestoreIssued` both follow it.
- **Shell-quoting discipline** — `shellQuote` (`neo4jbackup_controller.go`) for any user-controlled value entering `/bin/sh -c`; `GetBackupCommand` / `GetRestoreCommand` (`internal/neo4j/version.go`) own quoting for the admin command itself.
