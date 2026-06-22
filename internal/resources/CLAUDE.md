# internal/resources

Pure, side-effect-free **builders** that turn a `Neo4jEnterpriseCluster` / `Neo4jEnterpriseStandalone`
CR into Kubernetes objects (StatefulSet, Services, ConfigMap, Certificate, RBAC, NetworkPolicy,
Route, MCP). No client calls, no reconcile logic — controllers in `../controller/` call these and
apply the results.

## Key files

| File | What it does |
|---|---|
| `cluster.go` | The bulk: StatefulSet, Services, ConfigMap (startup script + version-gated discovery), TLS Certificate, discovery RBAC, Ingress, auth/audit/monitoring conf, conf merge helpers |
| `memory_config.go` | Heap/pagecache sizing (`CalculateOptimalMemorySettings`, `…ForNeo4j526Plus`, `GetMemoryConfigForCluster`) |
| `plugin_init_container.go` | Verified-download init container for `Neo4jPlugin` (`BuildPluginVerifiedDownloadInitContainer`, plugin auth/CA volumes) |
| `networkpolicy.go` | `BuildNetworkPolicyForEnterprise` / `…ForStandalone` |
| `route.go` | OpenShift `Route` builders (unstructured) |
| `mcp.go` | MCP sidecar Deployment/Service/Ingress/Route builders |
| `resource_recommendation.go` | `NewResourceRecommender` resource sizing helper |
| `security_context.go` | `DefaultNeo4jPodSecurityContext` / `DefaultNeo4jContainerSecurityContext` (single source of truth) |
| `*_test.go` | Unit tests (see [Tests](#tests)); `cluster_startup_test.go` pins naming + discovery |

## Key types & functions

- `BuildServerStatefulSetForEnterprise(cluster)` — the **one** server StatefulSet (delegates to
  `buildStatefulSetForEnterprise(cluster, "server", Spec.Topology.Servers)`). `BuildServerStatefulSetsForEnterprise` (plural) is **Deprecated**; don't use it.
- `buildVersionSpecificDiscoveryConfig(cluster)` — version-gated discovery conf string; branches on `isCalverImage(tag)`.
- `BuildConfigMapForEnterprise`, `BuildPodSpecForEnterprise` — startup script + pod spec.
- `BuildHeadlessServiceForEnterprise`, `BuildClientServiceForEnterprise`, `BuildDiscoveryServiceForEnterprise`, `BuildInternalsServiceForEnterprise`, `BuildMetricsServiceForEnterprise`.
- `BuildBackupFromAddresses` (cluster) / `BuildStandaloneBackupFromAddress` — `pod-fqdn:6362` lists for `neo4j-admin database backup --from`.
- `ServerPodSelector` / `StandalonePodSelector` / `PVCSelectorByInstance` — label selectors.
- Conf helpers: `DedupeNeo4jConf`, `UpsertNeo4jConfSettings`, `Neo4jConfSettings`, `MergeConfListValues`, `IsAdditiveConfKey`, `Neo4jSettingEnvVarName`.
- Port constants: `DiscoveryPort=6000`, `RaftPort=7000`, `BackupPort=6362`, `BoltPort=7687`. `LegacyClusterPort=5000` is V1 discovery — **never used at runtime** (invariant 4).

## Conventions & gotchas

- **Invariant 5 — single server StatefulSet**: `BuildServerStatefulSetForEnterprise` builds one `{cluster}-server` StatefulSet with `replicas: N`. Pods are `{cluster}-server-0…N-1`. **Never** emit `primary-*` / `secondary-*` names, and never reintroduce a centralized `{cluster}-backup` StatefulSet or backup sidecar (backups are Job-per-`Neo4jBackup`-CR). See `../../docs/knowledge/invariants.md`.
- **Invariant 4 — V2_ONLY discovery on port 6000**: `buildVersionSpecificDiscoveryConfig` emits `dbms.cluster.discovery.version=V2_ONLY` + `dbms.cluster.discovery.v2.endpoints=…:6000` on SemVer 5.26.x, and on CalVer (2025.x+) omits the version flag and uses `dbms.cluster.endpoints=…:6000`. Endpoint FQDNs are static `{cluster}-server-{i}.{cluster}-headless.{ns}.svc.cluster.local:6000`. Both branches are pinned by tests — change both sides and the assertions together.
- **Never bake `NEO4J_PLUGINS` into the static template** (see the explicit NOTE near `cluster.go:1169`). The plugin/fleet/Aura controllers live-patch it via `MergeNeo4jPluginList` (defined in `../controller/plugin_controller.go`, not here) so they don't overwrite each other. See the plugin/env-var rules in `../../CLAUDE.md`.
- **Builders are pure**: deterministic output, no `client.Client`, no I/O. Keep them that way — reconcile decisions belong in `../controller/`.
- **No webhooks** (invariant 1): nothing here validates admission; validation is inline in `../validation/`.
- Don't hand-edit generated RBAC/CRD/Helm artifacts; `+kubebuilder:rbac:` markers live on controllers. See "Generated artifacts" in `../../CLAUDE.md`.

## Tests

All co-located here as `_test.go`. Run the package:

```bash
go test ./internal/resources/...
```

Pinned contracts to respect when editing: `cluster_startup_test.go` (`TestBuildConfigMapForEnterprise_ClusterFormation`, `TestListDiscoveryConfiguration` — assert FQDNs, `V2_ONLY` presence/absence, server-0 `me` bootstrap) and `cluster_test.go` (`TestBuildServerStatefulSetForEnterprise_EmptyStorageClassUsesDefault`, `TestBuildStatefulSetForEnterprise_*`, `TestBuildCertificateForEnterprise_DNSNames`).

## See also

- `../../AGENTS.md` — repo-wide agent guidance.
- `../../CLAUDE.md` — invariants, plugin/env-var rules, generated-artifacts table.
- `../../docs/knowledge/invariants.md` — hard constraints (4 and 5 above).
- `../controller/` — callers/appliers of these builders; `MergeNeo4jPluginList` lives in `plugin_controller.go`.
- `../validation/` — all CR validation (no webhooks).
