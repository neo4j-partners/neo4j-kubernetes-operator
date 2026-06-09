# CLAUDE.md

Guidance to Claude Code (claude.ai/code) when working in this repository.

## Project Overview

Neo4j Enterprise Operator for Kubernetes — manages Neo4j Enterprise deployments (v5.26+) using the Kubebuilder framework.

**Supported Neo4j versions**: 5.26.x (last semver LTS) and 2025.x.x+ (CalVer). No 5.27+ semver — Neo4j switched to CalVer after 5.26.

**Hard constraints (NEVER violate):**
- **KIND ONLY** for dev/test/CI. No minikube, k3s, etc.
- **ENTERPRISE IMAGES ONLY**: `neo4j:5.26-enterprise` / `neo4j:2025.01.0-enterprise`. Never community.
- **NO WEBHOOKS**: no `ValidatingWebhookConfiguration` / `MutatingWebhookConfiguration` / `_webhook.go`. All validation lives in `internal/validation/`, called inline from the reconciler.
- **Discovery**: V2_ONLY mode exclusively.

**Deployment types:**
- **Neo4jEnterpriseCluster**: HA clusters (min 2 servers; self-organize into primary/secondary).
- **Neo4jEnterpriseStandalone**: Single-node (dev/test).

## Architecture

- CRDs: `Neo4jEnterpriseCluster`, `Neo4jEnterpriseStandalone`, `Neo4jDatabase`, `Neo4jShardedDatabase`, `Neo4jBackup`, `Neo4jRestore`, `Neo4jUser`, `Neo4jRole`, `Neo4jRoleBinding`, `Neo4jAuthRule`, `Neo4jPlugin`.
- Controllers: cluster & standalone controllers with controller-side validation. Neo4j client: Bolt protocol.
- **Directories:** `api/v1beta1/` (CRD types), `internal/controller/`, `internal/resources/` (K8s builders), `test/` (unit/integration/e2e).

**Server-based architecture**: single `{cluster-name}-server` StatefulSet with `replicas: N`. Pods are `{cluster-name}-server-0…N-1`. Never use `primary-*` / `secondary-*` pod names. Backups are Job-per-`Neo4jBackup`-CR exclusively (no persistent backup pod, no sidecars, no `spec.backups` field). The legacy `{cluster-name}-backup-0` StatefulSet and standalone backup sidecar were removed — never reintroduce a long-running backup pod.

**Server role hints** (`initial.server.mode_constraint`):
```yaml
topology:
  servers: 3
  serverModeConstraint: "PRIMARY"   # Global: all servers only host primaries
  serverRoles:
    - serverIndex: 0
      modeConstraint: "PRIMARY"     # Per-server (overrides global)
    - serverIndex: 1
      modeConstraint: "SECONDARY"
    - serverIndex: 2
      modeConstraint: "NONE"        # Default: any mode
```
Validator: indices in `[0, servers-1]`, no duplicates, cannot set ALL servers to SECONDARY.

## Essential Commands

Full target reference: `make help-all` and `docs/developer_guide/makefile_reference.md`. Most-used:

```bash
make dev-up / dev-down          # Create Kind cluster + deploy operator / tear down
make deploy-dev-local           # Rebuild + redeploy to Kind (~60s); `tilt up` for ~5s live reload
make test-unit                  # No cluster
make test-one TEST="name"       # Single integration test
make test-integration           # Auto-creates cluster, deploys operator
make sync-all                   # Regenerate every generated artifact (see ## Generated artifacts)
make ship-prep                  # sync-all + bundle + lint + CSV coverage (pre-release)
make check-drift                # CI gate: fails on stale generated files
make fmt / lint / vet / security / tidy
```

**NEVER `make dev-run`** — DNS resolution fails when the operator runs outside the cluster. Always deploy inside via `make operator-setup`.

**Debug reconciliation:**
```bash
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager -f
kubectl describe neo4jenterprisecluster <name>
kubectl describe pod <pod-name> | grep -E "(OOMKilled|Memory|Exit.*137)"
kubectl exec <pod-name> -c neo4j -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
```

## Testing

Ginkgo/Gomega, Kind only, **300-second timeouts** for all integration tests.

**Kind clusters:** dev = `neo4j-operator-dev`, test = `neo4j-operator-test`. Both ship cert-manager v1.20.0 with `ca-cluster-issuer`.

**Test resources:** CPU 50m–500m, memory ≥ 1.5Gi (Enterprise minimum), storage 500Mi–1Gi. `getCIAppropriateResourceRequirements()` + `applyCIOptimizations()` shrink any cluster in CI.

**Test tiers (Ginkgo labels):** every spec's top-level `Describe` carries `Label("core")` or `Label("extended")`. Core (fast reconcile contracts) runs per-PR on 5.26 + CalVer; extended (scaling, split-brain, backup/restore matrix, sharding, cloud) runs nightly + on `run-extended` label + manual dispatch. Run a tier locally: `ginkgo run --label-filter='core' ./test/integration/...`. **Label every new spec file** or it runs in neither lane. See `docs/developer_guide/testing.md` + `ci_and_workflows.md`.

**Property sharding tests**: CI-runnable smoke test (`property_sharding_ci_smoke_test.go`) runs only when the integration-tests workflow is dispatched with `neo4j-version: 2025.12-enterprise+` — gated by `isPropertyShardingCompatible()`. Uses `NEO4J_SHARDING_RELAX_MEMORY_MIN=true` (set only via `config/overlays/integration-test/`) to bypass the 4Gi/1-core floor on a 2×1.5Gi/500m cluster. Richer sharded tests (F3/F4/F5, Phase 2a/2c, multi-property-shard) stay local-only — they need the production 4Gi/server floor.

**Troubleshooting**: timeout → image-pull delays. OOMKilled → Enterprise needs ≥ 1.5Gi. DB-create hangs → use `TOPOLOGY` not `OPTIONS`. Cluster won't form → check discovery RBAC.

**MANDATORY AfterEach cleanup** (always remove finalizers before deletion; never rely on suite cleanup):
```go
AfterEach(func() {
    if cluster != nil {
        if len(cluster.GetFinalizers()) > 0 {
            cluster.SetFinalizers([]string{})
            _ = k8sClient.Update(ctx, cluster)
        }
        _ = k8sClient.Delete(ctx, cluster)
        cluster = nil
    }
    if testNamespace != "" {
        cleanupCustomResourcesInNamespace(testNamespace)
    }
})
```

## CI/CD

- **Unit tests + drift gate**: every push/PR (`ci.yml`). Required for merge.
- **Integration Tests** (`integration.yml`): core subset on 5.26 + CalVer, auto-runs on runtime-path PRs. **A new push cancels the PR's in-flight run** (per-PR concurrency + `cancel-in-progress`) — let a run finish before pushing again if you're waiting on a green result.
- **Extended Integration Tests** (`integration-tests.yml`): full suite on CalVer; nightly + `run-extended` label + manual dispatch.
- **Release**: multi-arch builds on git tags.

Integration tests deploy to `neo4j-operator-system` in prod mode (image `neo4j-operator:integration-test`). `waitForOperatorReady()` hardcodes this namespace.

## Deployment Configuration

**Version-specific discovery** (LIST resolver, static pod FQDNs):

| Setting | 5.26.x (SemVer) | 2025.x+ (CalVer) |
|---|---|---|
| `dbms.cluster.discovery.version` | `V2_ONLY` (required) | not used |
| Endpoints | `dbms.cluster.discovery.v2.endpoints=<fqdns>:6000` | `dbms.cluster.endpoints=<fqdns>:6000` |
| Bootstrap hint | `internal.dbms.cluster.discovery.system_bootstrapping_strategy=me/other` | n/a |

**Ports**: 5000 = V1 discovery (deprecated, never used) · **6000 = V2 discovery (always use)** · 7000 = RAFT. CalVer detection: `ParseVersion()` sets `IsCalver` when `major >= 2025` (handles 2026.x+ automatically).

**Never use** (deprecated 4.x): `dbms.mode=SINGLE`, `causal_clustering.*`, `metrics.bolt.*`, `server.groups`, `dbms.cluster.role`.

**Always use** (5.26+): `server.*` instead of `dbms.connector.*`, env vars over config files, modern `TOPOLOGY` syntax.

**TLS**:
```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer
```
Auto-generates SSL policies for `https`/`bolt`, certs at `/ssl/`. Cluster SSL defaults to strict (`spec.tls.strictPeerValidation: true`): `trust_all=false`, `client_auth=REQUIRE`, `verify_hostname=true`, with the cert-manager Secret's `ca.crt` projected to `/ssl/trusted/ca.crt`. `strictPeerValidation: false` reverts to legacy `trust_all=true` (debugging-only per Neo4j docs). Validator refuses strict if the issuer doesn't populate `ca.crt`. When TLS is enabled: `server.bolt.tls_level=REQUIRED`, scheme is `bolt+s://`; plain `bolt://` is rejected.

## Neo4j Plugin Support

**APOC** (pre-bundled, no internet needed):
- `NEO4J_PLUGINS=["apoc"]` → Docker entrypoint copies `/var/lib/neo4j/labs/apoc-*-core.jar` to `/plugins/` EmptyDir at startup.
- APOC behavior → env vars (`NEO4J_APOC_EXPORT_FILE_ENABLED`, etc.), NOT `neo4j.conf`. Procedure allowlisting (`dbms.security.procedures.unrestricted`) → `neo4j.conf`. `apoc-extended` (not bundled) requires egress internet.

**Other plugins:** GDS auto-adds `dbms.security.procedures.unrestricted=gds.*` + `allowlist=gds.*`. Bloom auto-adds `dbms.bloom.*`, `server.unmanaged_extension_classes`, HTTP auth allowlist. GenAI/N10s/GraphQL: standard handling.

**Validate plugin config:** cluster → StatefulSet env vars (`NEO4J_PLUGINS`); standalone → ConfigMap content.

**`NEO4J_PLUGINS` live-patching**: never bake into the static StatefulSet template in `internal/resources/cluster.go`. Use `MergeNeo4jPluginList` so plugin/fleet/Aura controllers don't overwrite each other.

**`envVarsEqual` = subset + ownership-tracked removal**: verifies desired vars exist with right value; tolerates extras so plugin/fleet/Aura controllers can live-patch without wholesale-replace oscillation. The cluster controller writes its owned names to `neo4j.com/cluster-controller-env-vars` annotation each reconcile; next reconcile enforces removals via `previously-owned ∖ desired` without disturbing foreign vars. Apply path uses `mergeEnvVars` — never wholesale-replace, never strict length+value equality, never drop the annotation.

## Aura Fleet Management

`spec.auraFleetManagement.{enabled, tokenSecretRef{name,key}}` — default key is `token`.

**Two-phase reconciliation** (never collapse):
1. Install plugin via `mergeFleetManagementPlugin` — every reconcile when enabled.
2. Register token via `CALL fleetManagement.registerToken($token)` — only when cluster `Ready` AND token not yet registered.

Plugin-only mode: omit `tokenSecretRef` to defer registration. Implementation: `reconcileAuraFleetManagement` + `mergeFleetManagementPlugin` on both controllers; `RegisterFleetManagementToken` / `IsFleetManagementInstalled` in `internal/neo4j/client.go`; `internal/validation/fleet_validator.go`.

## Live Cluster Diagnostics

When `spec.monitoring.enabled=true` and cluster is `Ready`:
- `status.diagnostics.servers[]` from `SHOW SERVERS`; `status.diagnostics.databases[]` from `SHOW DATABASES` (`system` DB excluded from health checks).
- Conditions `ServersHealthy`, `DatabasesHealthy` via `SetNamedCondition` (NOT `SetReadyCondition` — that's only for the `Ready` type).
- Prometheus metric `neo4j_operator_server_health{cluster_name, namespace, server_name, server_address}`: 1=healthy, 0=degraded.

**`CollectDiagnostics` is non-fatal**: errors go to `status.diagnostics.collectionError` only — never `return err`. Standalone has its own non-fatal `collectStandaloneDiagnostics()` (`SHOW DATABASES`) under the same conditions.

## Neo4j Database Syntax (5.26+ and 2025.x)

```cypher
-- 5.26+ (Cypher 5)
CREATE DATABASE name [IF NOT EXISTS]
[TOPOLOGY n PRIMAR{Y|IES} [m SECONDAR{Y|IES}]]
[WAIT]

-- 2025.x (Cypher 25)
CREATE DATABASE name [IF NOT EXISTS]
[[SET] DEFAULT LANGUAGE CYPHER {5|25}]
[[SET] TOPOLOGY n PRIMARIES [m SECONDARIES]]
[WAIT]
```

**Never use** (4.x — fails in 5.26+):
```cypher
CREATE DATABASE baddb OPTIONS {primaries: 1, secondaries: 1}  -- DEPRECATED
CALL dbms.cluster.role()  -- REMOVED in 5.0, use SHOW DATABASES
```

## Default Database Behavior

Neo4j creates a default `neo4j` database at bootstrap. The operator does not create, manage, or interfere with it.

**Default topology**: 1 primary, 0 secondaries — regardless of cluster size.

**Control at bootstrap** (first bootstrap only, no effect after): `initial.dbms.default_primaries_count` / `initial.dbms.default_secondaries_count` in `spec.config`. **Change post-bootstrap**: `ALTER DATABASE neo4j SET TOPOLOGY 3 PRIMARIES 1 SECONDARY`. Neo4j has no setting to skip creation.

**Operator interaction:**
- Diagnostics: included in `status.diagnostics.databases[]`, counts toward `DatabasesHealthy` (unlike `system`, which is excluded).
- `Neo4jDatabase` CRD named `neo4j`: allowed with a warning ("will shadow the default database"); `IF NOT EXISTS` makes creation a no-op; CRD deletion drops it.
- `dbms.default_database` in `spec.config`: rejected by validator (deprecated — use `dbms.setDefaultDatabase()` procedure).

## Neo4jDatabase CRD

Works with both cluster and standalone. `DatabaseValidator` tries cluster lookup first, then standalone.

**Separation of concerns** (strict): Cluster/Standalone CRDs own infrastructure, server config, auth, TLS, plugins, backup, images. Neo4jDatabase owns ONLY database name, topology, Cypher version, CREATE DATABASE options. It MUST NOT override cluster/server-level settings.

**Standalone needs `NEO4J_AUTH`** env var for automatic password setup (required for Neo4jDatabase support).

## Neo4jUser, Neo4jRole & Neo4jRoleBinding CRDs

Three CRDs, one design rule: **privileges live on `Neo4jRole`, not `Neo4jUser` or `Neo4jRoleBinding`**. Users carry only `roles: []`; roles carry `privileges: []`; bindings carry only `roles: []`. See `docs/user_guide/user_role_management.md`.

**Files:** types under `api/v1beta1/neo4j{user,role,rolebinding}_types.go`; controllers under `internal/controller/neo4j{user,role,rolebinding}_controller.go`; cluster ref resolution in `cluster_resolver.go`; Cypher in `internal/neo4j/{users,privileges}.go`; validators in `internal/validation/{user,role,rolebinding}_validator.go`.

**Source of truth:**
- `Neo4jUser.spec` authoritative for password (via Secret hash), `accountStatus`, `homeDatabase`, `roles`, `externalAuth`. Drift reverted every loop.
- `Neo4jRole.spec.privileges` authoritative when `enforcePrivileges: true` (default). `enforcePrivileges: false` skips the revoke pass.
- Built-in roles (`PUBLIC`, `reader`, `editor`, `publisher`, `architect`, `admin`) require `adoptBuiltin: true` to manage; never dropped on CR delete (only finalizer released). Validator rejects unmanaged built-in names.
- `PUBLIC` is auto-assigned and never granted/revoked; user controller filters it from both sides. Listing it in `Neo4jUser.spec.roles` → warning, not error.

**Watches:** the `Neo4jUser` controller watches `Neo4jRole` in `SetupWithManager` so users with missing custom roles re-reconcile when the role lands.

**Key Cypher** (all against `system` DB):
- User: `CREATE USER`, `ALTER USER` (compound, REMOVE before SET), `DROP USER IF EXISTS`, `SHOW USERS WITH AUTH`, `GRANT/REVOKE ROLE`.
- Role: `CREATE ROLE [AS COPY OF]`, `DROP ROLE IF EXISTS`, `SHOW ROLES`, `SHOW ROLE <r> PRIVILEGES AS COMMANDS YIELD command, immutable`.
- Privileges: `GRANT/DENY/REVOKE ... ON ... TO/FROM ...`. REVOKEs derived textually, not user-supplied.

## Key Implementation Patterns

- **Resource Version Conflict**: always wrap with `retry.RetryOnConflict(retry.DefaultRetry, ...)` — required for Neo4j 2025.01.0 cluster formation.
- **Template Comparison**: use `sts.UID != ""` to check if a StatefulSet exists, NOT `sts.ResourceVersion != ""` (ResourceVersion is populated even for new resources).
- **Split-Brain Detection**: `internal/controller/splitbrain_detector.go` connects to each pod, compares cluster views, auto-restarts orphans. Events: `kubectl get events --field-selector reason=SplitBrainDetected -A`.
- **Edition field removed**: no `edition: enterprise` in CRDs. Operator always assumes enterprise; client checks actual edition via `CALL dbms.components()`.
- **Structured Events**: constants from `internal/controller/events.go`. Use `corev1.EventTypeNormal` / `corev1.EventTypeWarning`, never raw strings.

## Regression Prevention Checklist

Numbered rules are not duplicated elsewhere — read in full.

**Standalone-specific:**
1. **`status.phase="Ready"`** required before database ops.
2. **Backup `--to-path`** syntax for Neo4j 5.26+.
3. **`ObservedGeneration`**: set `status.observedGeneration = latest.Generation` on every status update (both controllers).
4. **Name length validation**: cluster ≤ 56 chars (DNS label 63 minus `-server`); standalone ≤ 63; database ≤ 65 and must match `^[a-zA-Z][a-zA-Z0-9.\-]*$`.
5. **Standalone `UpgradeStrategy`**: pre-upgrade health check via `VerifyConnectivity`; `autoPauseOnFailure` blocks upgrade on failure; STS update strategy from spec.
6. **Standalone health probes**: readiness/liveness/startup via `/conf/health.sh` (process + HTTP 7474). ConfigMap includes `health.sh` alongside `neo4j.conf` with `DefaultMode: 0755`.
7. **Deprecated config keys**: validator *rejects* (`field.Invalid`, not a warning) deprecated `spec.config` keys — `dbms.logs.query.enabled` (use `db.logs.query.enabled`), `dbms.default_database`, `db.format`, `dbms.integrations.cloud_storage.s3.region`. Always use `db.*` namespace for 5.x+.
8. **Storage expansion**: orphan-delete STS (not regular delete); compare spec vs actual PVC sizes (not old vs new spec); `retry.RetryOnConflict` on PVC patches; validate `allowVolumeExpansion` before patching; never shrink PVCs.

**TLS / Bolt client:**
9. **TLS CA auto-discovery**: `buildTLSConfig()` in `internal/neo4j/client.go` loads CA from cert-manager Secret (`{name}-tls-secret`) automatically. `TrustedCASecret` is an override; `InsecureSkipVerify` is fallback only.
10. **All client functions must handle TLS**: `NewClientForEnterprise`, `NewClientForEnterpriseStandalone`, AND `NewClientForPod` all call `buildTLSConfig()`. Split-brain detector uses dynamic `bolt+s://` scheme.
11. **Operator's outbound Bolt URI uses the routing scheme** (`neo4j://` / `neo4j+s://`), never `bolt://`. Go driver only honors `AccessModeWrite` under routing; `bolt://` silently lands wherever the ClusterIP steered → `Neo.ClientError.Cluster.NotALeader`. Only legitimate `bolt://` user is `splitbrain_detector.go`. Pinned by `internal/neo4j/uri_test.go`.
12. **Tight Bolt driver timeouts**: `ConnectionAcquisitionTimeout=10s`, `SocketConnectTimeout=5s`, `MaxTransactionRetryTime=15s`. Under routing these gate routing-table fetch retries against an unreachable cluster; bumping to 30s+ stalls the reconcile queue.
13. **TLS Secret volume `DefaultMode=0440`** (owner+group read). Neo4j runs UID/GID 7474 with `FSGroup=7474`. Pinned by `TestBuildStatefulSet_TLSVolumeDefaultMode0440`.

**Users / Roles / Privileges:**
14. **`GetUserRoles` is buggy** — queries `SHOW USER PRIVILEGES YIELD role`, returns one row per privilege. Use `Client.ListUserRoles` or `Client.ShowUser`.
15. **Password rotation via Secret hash**: `Neo4jUser.status.passwordSecretHash` stores SHA-256 of the Secret value; rotation detected on hash change. Password never persisted in CR fields. Skip `SET PASSWORD` when only `externalAuth` is configured.
16. **`ALTER USER` clause ordering**: REMOVE clauses MUST precede SET clauses on a single statement. Use the `AlterUserOptions` builder — never hand-roll ALTER USER strings.
17. **Missing custom roles**: do NOT fail reconcile. Set `PendingDependencies` condition and requeue; the user controller watches `Neo4jRole` and re-reconciles when the role lands.
18. **Same-namespace `clusterRef`** for users/roles — cross-namespace refs not supported in v1. Multi-tenant patterns go through an opt-in `Neo4jClusterAccessGrant` CR.
19. **Identifier quoting in Cypher**: role/user names go through `escapeBackticks()`. Never `fmt.Sprintf` user-controlled names into Cypher; passwords / provider IDs go through driver parameters.
20. **Privilege drift via `SHOW ROLE PRIVILEGES AS COMMANDS`**: source of truth is `Neo4jRole.spec.privileges`. Controller canonicalises both sides (`CanonicalisePrivilegeStatement`), diffs as sets, derives REVOKEs (`DerivePrivilegeRevoke`). Immutable rows excluded from revokes; surfaced via `status.privilegeDrift`.
21. **Privilege statement validation**: entries in `Neo4jRole.spec.privileges` MUST start with `GRANT` or `DENY` (REVOKE rejected — operator derives) and end with `TO <spec.name>`.
22. **`Neo4jRoleBinding` never creates or drops users** — only manages role grants for externally-provisioned users (SSO/LDAP first-login). Absent user → `UserNotFound` and waits.
23. **`Neo4jRoleBinding` overlap with `Neo4jUser`** rejected by validator when `clusterRef`+`username` match an existing user in the same ns.
24. **`Neo4jRoleBinding.spec.enforceExclusive`** defaults to false (manages only `.spec.roles` + `status.grantedRoles`). `true` revokes any role on the user not in `.spec.roles`. Never flip the default.
25. **Diagnostics user/role lists bounded** by `maxDiagnosticUsers` / `maxDiagnosticRoles`; full count in `UserCount` / `RoleCount`. Never remove caps without a pruning strategy.

**TrustStore / volumes:**
26. **Truststore init container seeds from JDK cacerts**: `BuildTrustStoreInitContainer` MUST copy `$JAVA_HOME/lib/security/cacerts` to `/truststore/truststore.jks` before importing user CAs — otherwise public CAs break. Seed makes `spec.trustedCASecrets` purely additive.
27. **`spec.trustedCASecrets` Secret-name = keytool alias** — must be unique in the JKS; validator rejects duplicate Secret names. Keep alias derivation spec-statically-derivable.
28. **Legacy `spec.auth.trustStore` folds into `spec.trustedCASecrets`** via `CollectTrustedCASecrets`. Never wire legacy directly — both paths produce duplicate volumes/init containers and the JKS build fails with duplicate-alias.
29. **`spec.extraVolumeMounts` reserved paths**: validator rejects mounts at `/data`, `/logs`, `/conf`, `/ssl`, `/plugins`, `/truststore`, `/truststore-ca`, `/var/lib/neo4j` and its standard subdirectories.

**Auth / authrule / OIDC:**
30. **AUTH RULE Cypher requires `CYPHER 25` prefix** — every statement in `internal/neo4j/auth_rules.go` prepends `cypher25Prefix`. 2026.x system DB defaults to Cypher 5; without it: `42I06: Invalid input 'AUTH'`. Keep even when default flips.
31. **`oidc-`-prefixed provider name in ABAC config** — `dbms.security.abac.authorization_providers` values must match `dbms.security.authorization_providers` form (`oidc-<name>` for OIDC).
32. **Authrule controller in `--controllers` default list** — `cmd/main.go` dev-mode default MUST include `authrule`. Production (`setupProductionControllers`) wires unconditionally.
33. **LDAP `useStartTLS` defaults to true for plain `ldap://` hosts**: nil + `ldap://` → `dbms.security.ldap.use_starttls=true`. `ldaps://` skips. Explicit `false` honored. Pinned by `TestBuildAuthConfig_LDAP_UseStartTLSDefault` (6 cases).

**Network / metrics / audit:**
34. **NetworkPolicy peer-rule ports** mirror cluster pod ContainerPorts: `BuildNetworkPolicyForEnterprise` peer rule covers `6000/7000/7688/7689`. Adding intra-cluster ports to STS without updating here silently breaks pod-to-pod on enforcing CNIs. Pinned by `TestBuildNetworkPolicyForEnterprise_PeerPortsRestrictedToCluster`.
35. **NetworkPolicy public rule MUST include port 2004 (Prometheus)** — once any rule selects a pod it's isolated. Pinned by `TestBuildNetworkPolicyForEnterprise_PublicPortsOpen`.
36. **`BuildNetworkPolicy*` returns nil when disabled** — reconcilers short-circuit. Standalone also uses `reflect.DeepEqual` to skip resourceVersion churn.
37. **Metrics JMX + CSV disabled unconditionally** — `server.metrics.{jmx,csv}.enabled=false` emitted by both builders regardless of `monitoring.enabled` (JMX is unauthenticated management; CSV writes pod-ephemeral files). Kill-switches MUST be outside the monitoring branch. Pinned by `TestBuildConfigMapForEnterprise_MetricsHardening` + `TestBuildMonitoringConfig`.
38. **`spec.audit` emission order**: `BuildAuditConfig` runs AFTER `BuildMonitoringConfig`; both touch `db.logs.query.obfuscate_literals` and last-write-wins gives audit priority. User `spec.config` appends last and wins over both. Pinned by `TestBuildAuditConfig_PrecedenceOverMonitoring`. No `dbms.security.audit.*` keys (4.x; removed) — use `security.log` / `query.log`.
39. **`spec.audit.Enabled` is a hint, not a stomping default** — `Enabled=true` + `ObfuscateQueryLiterals` nil → emit `obfuscate_literals=true`. Explicit values (true OR false) win. Exactly ONE `obfuscate_literals` line emitted. Pinned by `TestBuildAuditConfig_ExplicitObfuscateFalseDespiteEnabled`.

**Backup / restore:**
40. **All runs of one Neo4jBackup CR share one `--to-path = <base>/<chain-root>/`** (NOT per-run subfolders) — required for `--type=DIFF` chaining. Per-run identity via ISO-8601 timestamp in each `.backup` filename, captured to `BackupRun.ArtifactFilename` (standard) / `ShardArtifacts.Filename` (sharded). `BACKUP_RUN_ID` env var stays on the Pod (downward API → Job name) for log correlation; one-shot Job name = `<backup>-backup`; CronJob child = `<cronjob>-<unix-seconds>`. Never re-introduce the `${BACKUP_RUN_ID}` subfolder under `--to-path`. Pinned by `TestBackupRunIDEnvVar` + `TestJobToBackupRun`.
41. **CronJob backup defaults are load-bearing**: `ConcurrencyPolicy=Forbid`, `StartingDeadlineSeconds=60`, `TTLSecondsAfterFinished=1800`, `SuccessfulJobsHistoryLimit=10`, `FailedJobsHistoryLimit=3` — give `reconcileScheduledHistory` a 30-min window before K8s GCs the Jobs. Don't relax without cause.
42. **`source.type: backup` resolved upstream via `resolveRestoreSource`** — swaps `Spec.Source` on a shallow restore copy, threads through every builder. `buildRestoreCommand`'s `case "backup":` is dead-code with defensive `internal:` error.
43. **`errBackupNotReady` → Pending, not Failed**: `ResolveBackupRef` wraps `errBackupNotReady` via `fmt.Errorf %w` when history has no Succeeded run; `startRestore` `errors.Is` and routes Pending+requeue (Pending NOT in the "previously failed" guard set). Missing-CR errors stay terminal Failed. Pinned by `TestResolveRestoreSource_BackupRefNoSucceededRun_IsTransient` + `_BackupRefMissingCR_IsPermanent`.
44. **Standalone restore `--from-path` is a FILE via shell substitution; `tail -1` picks the LATEST in the shared dir** (rule 40): `buildLocalRestoreFilePath` emits `$(ls '<backupPath>'/'<dbname>'-*.backup | tail -1)`. **BOTH path AND database name MUST go through `shellQuote()`** — unquoted user-controlled `spec.source.backupPath` / `spec.databaseName` would escape the `ls` and execute arbitrary commands in the restore Pod (mounts `/data` RW, carries `NEO4J_ADMIN_PASSWORD`). Pinned by `TestResolveLocalPVCFromPath_BackupPathShellInjectionGuard` + `_NestedCommandSubstitutionGuard` + `_EmbeddedSingleQuoteGuard`. Cloud URIs skip. Never pass the directory; never substitute the timestamp in Go; never drop quoting; never revert to `head -1`.
45. **Restore `--temp-path=/tmp/restore-tmp` default for PVC sources** — backup PVC mounted ReadOnly, so neo4j-admin can't extract in-place. Emits `--temp-path` + `rm -rf && mkdir -p` prelude (needs empty dir). Explicit `Options.TempStorage` / `Options.TempPath` win. Without it: `FileSystemException: Read-only file system`.
46. **Restore reconcile race tolerance**: Job creation treats `AlreadyExists` as "another reconcile got there first" and re-fetches; `startCluster` treats missing `neo4j.neo4j.com/original-replicas` annotation as "first reconcile already deleted it" and returns nil. Two reconciles race during the 10s stopCluster scale-down. Reverting either re-flips successful restores to terminal `Failed`.
47. **Legacy post-restore re-seed via `dbms.[cluster.]recreateDatabase`** (Job-based standalone path only; rule 75's Cypher path doesn't need it). `recreateRestoredDatabaseOnCluster` uses server-0 as seed (matched by `cluster.Name + "-server-0"` against `SHOW SERVERS YIELD address` — `name` column unreliable). Procedure from `version.RecreateDatabaseProcedure()`: `dbms.cluster.recreateDatabase` (5.24–2025.03) → `dbms.recreateDatabase` (2025.04+). Skipped for standalone, `Topology.Servers < 2`, pre-5.24 SemVer / pre-2025.02 CalVer. Non-fatal — failure emits Warning `DatabaseCreateFailed`.
48. **Sharded backup uses `{name}*` glob + always-quoted db arg**: one `neo4j-admin database backup "{name}*"` captures every shard consistently. `GetBackupCommand` ALWAYS double-quotes so the shell can't pre-expand. `shardedPreflightGlobSafety` rejects any DB matching `{name}*` outside `^{name}-(g|p)\d{3}$` (terminal Failed). `shardedPreflightStatic` routes missing-Ready to Pending. Pinned by `TestGetBackupCommandQuotesShardedGlob` + `TestGetBackupCommandQuotesPlainName`.
49. **`--remote-address-resolution` is `*bool` with sharded-aware defaulting**: `effectiveRemoteAddressResolution` defaults `true` ONLY when `kind=ShardedDatabase` AND Neo4j ≥ 2025.09 AND user didn't set it. Explicit values win. Never re-introduce a `bool` zero-value default. Pinned by `TestEffectiveRemoteAddressResolution`.
50. **`IsClusterShardingReady`** (`internal/validation/sharding.go`) is the canonical sharding-precondition helper — returns nil only when `cluster.spec.propertySharding.enabled=true` AND `IsNeo4jVersion202512OrHigher(image.tag)`. Used by cluster validator + backup reconciler preflight; never inline at new callers.
51. **Sharded DB Ready signal is `Status.ShardingReady` (bool pointer)**, not the generic Ready condition (which is coarser and would let backups run before shards exist).
52. **`Neo4jShardedDatabase.status.lastBackup` reverse-lookup is non-fatal observability**: populated by `updateShardedDBLastBackup` from both one-shot (`recordOneShotBackupRun`) and CronJob (`reconcileScheduledHistory`) paths. Only Succeeded runs update; Failed runs don't overwrite. CR-not-found logged and swallowed. Source of truth remains `Neo4jBackup.status.history`.
53. **`BackupRun.ShardArtifacts.ShardName` is derived from `Neo4jShardedDatabase.spec`** (`expectedShardArtifactsForBackup` reads `propertySharding.propertyShards`, emits `{name}-g000` + `{name}-p000…p{N-1}`). Filename/Size populated by Pod-log parsing (rule 67). The audit question ("did all shards back up?") is answered by `ShardName` alone, so parse-derived fields stay informational.
55. **`ResolveBackupRef` is the canonical Neo4jBackup-name → StorageLocation resolver** (`internal/controller/backup_resolver.go`, free function taking `client.Reader`). All callers delegate. Returns wrapped `ErrBackupNotReady` when the backup exists but has no Succeeded run — callers `errors.Is` to route Pending+requeue. Never duplicate the lookup; never compare error strings.
56. **`spec.seedBackupRef` supports cloud (CloudSeedProvider) and PVC (HTTP proxy + URLConnectionSeedProvider — rule 71)**. Other storage types rejected.
57. **`spec.seedBackupRef` mutex with `seedURI` / `seedURIs`**: validator rejects combinations. seedBackupRef materialises into seedURI at reconcile time on a shallow in-memory copy — the original spec is not persisted with the resolved URI.
58. **`Neo4jShardedDatabase` phase "Pending" is reserved for `seedBackupRef` waits**: when `resolveShardedSeed` returns `errors.Is(err, ErrBackupNotReady)`, set `status.phase=Pending` and requeue. Don't route other transient conditions through Pending without explicit design.
59. **Seed-creds projection — `spec.extraEnvFrom` on cluster + standalone**: `CREATE DATABASE … OPTIONS { seedURI }` runs on the Neo4j server pods, so cloud creds must be in their env or the JVM's SDK default chain can't authenticate. Both CRs' `spec.extraEnvFrom` wire onto the neo4j container's `envFrom`. Generic (cloud creds, plugin tokens, any Secret-projected env). Empty `credsSecretName` is a no-op (user on IRSA / Workload Identity).
60. **`SeedCredsTarget` interface + `EnsureSeedCredsProjected`** in `internal/controller/cluster_seed_creds.go` are the canonical projection check. Both CRs implement it via `api/v1beta1/seed_creds_target.go`. `ResolveShardedSeed` returns `(uri, credsSecretName, err)`; `credsSecretName` from the resolved backup's `Spec.Cloud.CredentialsSecretRef` (empty for workload-identity). Called from `Neo4jShardedDatabaseReconciler`, `Neo4jDatabaseReconciler`, `Neo4jRestoreReconciler`. New callers go through the interface.
61. **Auto-inherit seed creds is annotation-gated and triggers a rolling restart**: without `neo4j.com/auto-inherit-seed-creds=true`, missing-projection emits an actionable error (copy-pasteable snippet). With it, operator patches `spec.extraEnvFrom`, records source in `neo4j.com/seed-creds-auto-inherited-from`, rolls out the STS, DB controller routes to Pending+requeue. Never auto-inherit without the annotation.
63. **`spec.replaceExisting` + `spec.force` on `Neo4jShardedDatabase` = destructive restore**: both true → `CYPHER 25 DROP DATABASE {name} IF EXISTS DESTROY DATA WAIT` before the standard CREATE. Validator: `replaceExisting=true` requires `force=true`; mutex with `ifNotExists=true`; requires a seed source. DROP is idempotent across requeues.
64. **`Status.LastDestructiveRestoreGeneration` gates replaceExisting**: destructive branch fires only when `LastDestructiveRestoreGeneration < Generation`; stamps `= Generation` on success. Re-trigger by mutating spec (bumps generation) — typically editing `seedBackupRef`.
65. **Sharded DDL (CREATE / DROP) requires `CYPHER 25` prefix**: 2026.x system DB defaults to Cypher 5; without it the sharded syntax fails to parse. Same invariant as AUTH RULE (rule 30).
66. **`Neo4jShardedDatabase.spec.IfNotExists` is `*bool`**: kubebuilder `+default=true` on `bool omitempty` silently re-applies the default when the user sets `false`. Pointer preserves explicit-false. Callers MUST use `Spec.IfNotExistsEffective()`, not dereference. Pinned by Phase 2c replaceExisting integration test.
67. **Backup-Pod log parsing is opportunistic, not load-bearing**: `Neo4jBackupReconciler.Clientset` enables Pod-log fetches that populate `BackupRun.ShardArtifacts.Filename/Size`, `BackupRun.ArtifactFilename`, `BackupRun.Validation`. All best-effort: log-fetch failures, format drift, or `Clientset == nil` (unit tests) leave fields empty without failing reconcile. ShardName / RunID / Status are load-bearing — never gate reconcile state on parsed filename/size.
70. **`BackupOptions.Validate` is `*bool` opt-in**: when `*true`, appends `&& (neo4j-admin backup validate --from-path=… --database="…" || true)` (validate failures don't fail the Job). Operator parses Pod log into `BackupRun.Validation`: all rows OK → OK; any Ahead/Behind → Degraded; no parseable rows → Unknown + truncated `RawOutput` (capped 2 KiB). For sharded, dbArg is the literal name (validate auto-discovers shards).
71. **PVC seed proxy + `URLConnectionSeedProvider`**: PVC-backed `seedBackupRef` (sharded) and PVC-backed cluster Neo4jRestore both spawn a `backup-seed-proxy-<owner>` Deployment + Service via generic `ensurePVCSeedProxyResources` (`internal/controller/pvc_seed_proxy.go`) — backup PVC mounted RO, busybox httpd on `:8080`, owner-ref'd to the consuming CR. URLs target the exact `.backup` filename; `URLConnectionSeedProvider` only supports single-file URIs. Hard-gated on F3 filename capture — empty filename → validator rejects.
72. **ResolvedShardedSeed.URI vs PerShardURIs is mutually exclusive**: cloud → URI (`OPTIONS { seedURI }`); PVC → PerShardURIs (`OPTIONS { seedURIs: { … } }`). Wire ONE and clear the OTHER — validator rejects both. `ProxyAvailable=false` → requeue while proxy rolls out.
74. **`dbms.databases.seed_from_uri_providers` is version-gated** via `SeedFromURIProvidersConfigValue(imageTag)`. Base: `CloudSeedProvider,FileSeedProvider,URLConnectionSeedProvider`; `ServerSeedProvider` appended only on `IsNeo4jVersion202604OrHigher`. Both builders call the helper — never inline. **Deprecated `S3SeedProvider` excluded across all versions** — `CloudSeedProvider` handles `s3://` via the SDK default credential chain (rule 59). Pinned by `TestSeedFromURIProvidersConfigValue` + `TestIsNeo4jVersion202604OrHigher`.
75. **Cluster `Neo4jRestore` uses Cypher, NOT a Job** (neo4j-admin restore is unsafe on clusters). `startRestore` branches via `isRestoreTargetTrueCluster`: cluster + standard DB → `startClusterCypherRestore` (`DatabaseExists` → `RecreateDatabaseWithSeedURI` if exists, `CreateDatabaseWithSeedURIOptions` if not — both block until online; no `stopCluster`); standalone → Job + `neo4j-admin restore`; sharded → rejected by `validateRestore` pointing at rule 63. Never re-introduce the cluster-target Job path.
76. **seedURI for cluster restore = the exact `.backup` FILE, never a directory**: Neo4j's seed providers seed a single DB from one backup file; a directory URI fails (`Can't open seed file: …/<chain-root>`). Both paths point at `BackupRun.ArtifactFilename` (latest Succeeded run): cloud → `buildSeedURIFromBackupStorage` builds the dir then `startClusterCypherRestore` appends the filename via `latestSucceededArtifactFilename`; PVC → `resolveClusterPVCRestoreURI` spawns the seed proxy (rule 71) at the filename. A single DIFF-file URI suffices — Neo4j resolves the FULL parent chain from the same dir. Standalone Job uses `tail -1` (rule 44). **After `dbms.recreateDatabase` (async), the operator MUST `WaitForDatabaseOnline` before marking Completed** — the procedure returns before the seed finishes, so a bare return is false-success. `CREATE DATABASE … OPTIONS{seedURI} WAIT` already blocks; recreate does not.
77. **`RecreateDatabaseWithSeedURI` vs `RecreateDatabase` (seedingServers)**: `RecreateDatabaseWithSeedURI` is the cluster-native restore primitive — every server pulls from the URI in parallel, no Job. `RecreateDatabase` (seedingServerIDs) is a post-`neo4j-admin restore` consistency fix picking server-0 as seed — legacy/standalone only. Never use seedingServers where seedURI works.
78. **`spec.chainFromBackup` composes mixed-cadence FULL+DIFF**: daily FULL + hourly DIFF CR with `chainFromBackup: daily` share `<base>/<chain-root>/` so `--type=DIFF` finds the prior FULL. `chainRoot(backup)` returns `spec.chainFromBackup` if set, else `backup.Name` — used by `buildToPath`, `BackupRun.BackupsPath`, `app.kubernetes.io/part-of` Job label. Validator rejects self-reference; reconciler's `validateChainParent` enforces parent existence + matching target + matching storage. `waitForChainConcurrencyClear` refuses to start while any Job with same `part-of` is `status.active>0`; routes `errChainBusy` to Pending+requeue. Pinned by `backup_chain_test.go`.
79. **`spec.backups` and all centralized-backup plumbing are REMOVED**: no `spec.backups` / `spec.storage.backupStorage` fields, no `BackupsSpec` / `BackupStorageSpec` types, no `BuildBackupStatefulSet` / `buildCentralizedBackup*` builders, no standalone `buildBackupSidecarContainer`, no `cloud_validator.go`. The Neo4jBackup CRD (Job-per-CR) is the only backup path. `StorageLocation` / `CloudBlock` / `CloudIdentity` / `AutoCreateSpec` types survive because Neo4jBackup/Neo4jRestore use them. Never reintroduce a `spec.backups` field or a long-running backup pod/sidecar.

## Generated artifacts

Several files are generated, not hand-written — each carries a `# This file is GENERATED. DO NOT EDIT.` header and `check-drift` reverts tampering. **Never hand-edit.**

| Generated file | Source | Regenerate via |
|---|---|---|
| `config/rbac/role.yaml` | `+kubebuilder:rbac:` markers in `internal/controller/*.go` | `make manifests` |
| `config/crd/bases/*.yaml` | Go types in `api/v1beta1/*` + kubebuilder markers | `make manifests` |
| `api/v1beta1/zz_generated.deepcopy.go` | Go types in `api/v1beta1/*` | `make generate` |
| `config/crd/kustomization.yaml` (resources) | files in `config/crd/bases/` | `make sync-kustomize` |
| `config/samples/kustomization.yaml` (resources) | `config/samples/neo4j_*.yaml` filenames | `make sync-kustomize` |
| `config/rbac/<crd>_{editor,viewer}_role.yaml` + kustomization | `spec.{group,names.plural,names.singular}` from each CRD base | `make sync-editor-viewer-roles` |
| `charts/neo4j-operator/crds/*.yaml` | `config/crd/bases/*.yaml` | `make helm-sync-crds` |
| `charts/neo4j-operator/templates/clusterrole.yaml` | `config/rbac/role.yaml` rules | `make helm-sync-rbac` |
| `charts/neo4j-operator/Chart.yaml` (`artifacthub.io/crds`) | CRD bases + curated descriptions in `scripts/helm-sync-artifacthub-crds.sh` | `make helm-sync-artifacthub-crds` |
| `bundle/manifests/*` and `bundle/metadata/*` (OperatorHub) | `config/manifests/bases/*.csv.yaml` + everything above | `make bundle` |

Umbrella targets: **`make sync-all`** (every regeneration step, no bundle); **`make ship-prep`** (`sync-all` + `bundle` + `helm-lint` + `check-csv-coverage`, run before tagging a release).

CI gate: **`make check-drift`** runs `sync-all` + `bundle` then `git diff --exit-code`. `make bundle` pins the CSV's `createdAt:` to a stable placeholder so concurrent PRs don't conflict; release flow stamps the real value via `make bundle-release`.

**`scripts/helm-sync-artifacthub-crds.sh` requires a description per CRD**: when adding a CRD, add a `case "$kind" in ... esac` row or the script exits non-zero.
