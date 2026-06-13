# internal/controller

> controller-runtime reconcilers for every Neo4j CRD (cluster, standalone, database,
> sharded database, backup, restore, user, role, rolebinding, authrule, plugin), plus
> the shared helpers (events, conditions, clusterRef resolution, env-var/owned-key
> merging, split-brain detection, topology scheduling, rolling upgrade) they lean on.

## Key files

| File | What it does |
|---|---|
| `neo4jenterprisecluster_controller.go` | HA cluster reconciler. Owns the `{cluster}-server` StatefulSet, `mergeEnvVars`/`envVarsEqual`, `CollectDiagnostics`, fleet-management plugin merge. |
| `neo4jenterprisestandalone_controller.go` | Single-node reconciler (shares helpers via `standaloneAsCluster` in `util.go`). |
| `neo4jbackup_controller.go` | Job-per-`Neo4jBackup`-CR reconciler (invariant 5 — no long-running backup pod). `neo4jbackup_sharded.go`, `neo4jbackup_log_parser.go` support it. |
| `neo4jrestore_controller.go` | Restore reconciler (heaviest user of `RetryOnConflict` status writes). |
| `neo4jdatabase_controller.go` / `neo4jshardeddatabase_controller.go` | `Neo4jDatabase` / `Neo4jShardedDatabase` reconcilers. |
| `neo4juser_controller.go` / `neo4jrole_controller.go` / `neo4jrolebinding_controller.go` / `neo4jauthrule_controller.go` | Auth-model reconcilers. |
| `plugin_controller.go` | `Neo4jPlugin` reconciler; home of `MergeNeo4jPluginList`. |
| `events.go` | All `EventReason*` string constants — emit these, never raw strings. |
| `conditions.go` | `Condition{Type,Reason}*` constants + `SetReadyCondition` / `SetNamedCondition`. |
| `cluster_resolver.go` | `ResolveClusterRef`, `ResolvedTarget`, `EnqueueDependentsForClusterChange`. |
| `owned_keys.go` | Owned env-var/annotation tracking (`neo4j.com/cluster-controller-env-vars`). |
| `splitbrain_detector.go` | `SplitBrainDetector` — compares per-pod cluster views, restarts orphans. |
| `topology_scheduler.go` | `TopologyScheduler` — spread constraints / anti-affinity / placement. |
| `rolling_upgrade.go` / `rolling_upgrade_statemachine.go` / `scale_down.go` / `storage_expansion.go` | In-place upgrade, drain, and PVC-expansion flows. |
| `configmap_manager.go` / `cache_manager.go` / `connection_helper.go` / `backup_resolver.go` / `cluster_seed_creds.go` / `finalizer_deletion.go` | Supporting managers/helpers. |
| `suite_test.go` | envtest harness (`TestControllers` → `RunSpecs`). |

## Key types & functions

- `ResolveClusterRef(ctx, c, ns, name) (ResolvedTarget, error)` — tries cluster first, then standalone (`cluster_resolver.go`). `ResolvedTarget` exposes `IsStandalone`, `IsReady`, `NewClient`, `AdminSecret`.
- `EnqueueDependentsForClusterChange(...)` — watch wiring so user/role/binding reconcilers re-run on cluster status flips (used in each `SetupWithManager`).
- `SetReadyCondition` / `SetNamedCondition` (`conditions.go`) — `SetNamedCondition` for `ServersHealthy`/`DatabasesHealthy` etc.; `SetReadyCondition` ONLY for the `Ready` type.
- `(r *Neo4jEnterpriseClusterReconciler) envVarsEqual(...)` + package-level `mergeEnvVars(...)` (`neo4jenterprisecluster_controller.go`).
- `MergeNeo4jPluginList(existing, newPlugin)` (`plugin_controller.go`) — live-patch `NEO4J_PLUGINS` without clobbering other controllers.
- Each reconciler's `Reconcile` + `SetupWithManager`.

## Conventions & gotchas

- **Validation is INLINE, never a webhook (invariant 1).** Reconcilers hold a `*validation.*Validator` (e.g. cluster controller's `Validator *validation.ClusterValidator`) and call it from `Reconcile` — there is no `_webhook.go`.
- **Status writes wrap `retry.RetryOnConflict(retry.DefaultRetry, ...)`** (or `DefaultBackoff`) — required for 2025.01.0 cluster formation. See `neo4jenterprisecluster_controller.go:784`. Re-fetch the object inside the closure.
- **StatefulSet existence check is `sts.UID != ""`, NOT `ResourceVersion`** (ResourceVersion is set even on never-created objects). See `neo4jenterprisecluster_controller.go:869`.
- **Emit events via `EventReason*` constants from `events.go`** with `corev1.EventTypeNormal` / `corev1.EventTypeWarning` — never raw strings.
- **Env vars: subset-merge, never wholesale-replace.** `envVarsEqual` checks desired vars exist with the right value but tolerates foreign extras; `mergeEnvVars` merges and enforces removals via the owned-keys annotation (`owned_keys.go`). Do not bake `NEO4J_PLUGINS` into the static StatefulSet template; use `MergeNeo4jPluginList`. (CLAUDE.md "Key Implementation Patterns".)
- **`CollectDiagnostics` is non-fatal** — surface errors to `status.diagnostics.collectionError`, never `return err`.
- **Server-based naming only**: `{cluster}-server-0…N-1`; never `primary-*`/`secondary-*` (invariant 5). `splitbrain_detector.go` enumerates pods by this scheme.

## Tests

`*_test.go` here run under the envtest suite (`suite_test.go`, no real cluster). Run the whole package with `make test-unit`, or directly: `go test ./internal/controller/...`. The core/extended ginkgo label split lives in `test/integration/`, not in this package. A single integration spec: `make test-one TEST="name"`.

## See also

- `../../AGENTS.md` and `../../CLAUDE.md` — repo-wide invariants and domain reference.
- `../../docs/knowledge/{invariants.md,operations.md,backup-restore.md}` — enforcement-tagged regression rules.
- `../validation/` — the validators these reconcilers call inline.
- `../resources/` — the K8s object builders (StatefulSet, Services, ConfigMaps) reconcilers apply.
- `../neo4j/` — the Bolt client (`NewClientForEnterprise*`, Cypher for users/privileges).
