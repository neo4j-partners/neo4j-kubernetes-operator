# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Neo4j Enterprise Operator for Kubernetes - manages Neo4j Enterprise deployments (v5.26+) using Kubebuilder framework.

**Supported Neo4j Versions**: 5.26.x (last semver LTS) and 2025.x.x+ (CalVer). Neo4j moved from semver to CalVer after 5.26 — no 5.27+ semver releases exist or will exist.
**CRITICAL: KIND IS MANDATORY**: This project exclusively uses Kind (Kubernetes in Docker) for ALL development, testing, and CI workflows. No alternatives (minikube, k3s) are supported.
**CRITICAL: ENTERPRISE IMAGES ONLY**: Never use Neo4j community images (neo4j:5.26), only enterprise ones (neo4j:5.26-enterprise, neo4j:2025.01.0-enterprise)
**CRITICAL: NO WEBHOOK VALIDATION**: This project does NOT use admission webhooks (ValidatingWebhookConfiguration/MutatingWebhookConfiguration). All validation is performed inline during reconciliation in the controller. Never introduce webhook-based validation — all validation logic belongs in `internal/validation/` and is called directly from the reconciler.
**Discovery**: V2_ONLY mode exclusively

**Deployment Types:**
- **Neo4jEnterpriseCluster**: High availability clusters (minimum 2 servers that self-organize into primary/secondary roles)
- **Neo4jEnterpriseStandalone**: Single-node deployments (development/testing)

## Architecture

**Key Components:**
- CRDs: Neo4jEnterpriseCluster, Neo4jEnterpriseStandalone, Neo4jBackup/Restore
- Controllers: Cluster & standalone controllers with client-side validation
- Neo4j Client: Bolt protocol communication

**Directory Structure:**
- `api/v1beta1/` - CRD definitions
- `internal/controller/` - Controller logic
- `internal/resources/` - K8s resource builders
- `test/` - Unit, integration, e2e tests

**Server-Based Architecture**: Single `{cluster-name}-server` StatefulSet with `replicas: N`. Pods: `{cluster-name}-server-0`, `{cluster-name}-server-1`, etc. Backup: `{cluster-name}-backup-0` (centralized single StatefulSet per cluster, ~70% fewer resources than sidecars).

**Server Role Hints** (`initial.server.mode_constraint`):
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
Validation: indices in range (0 to servers-1), no duplicates, cannot set ALL servers to SECONDARY.

## Essential Commands

```bash
# Getting started (one-command bootstrap)
make dev-up                 # Create cluster + deploy operator (first time)
make dev-down               # Tear down everything
make check-prereqs          # Verify all tools installed

# Inner dev loop
make deploy-dev-local       # Rebuild + redeploy to Kind (~60s)
make dev-watch              # Auto-rebuild on file changes (watchexec/fswatch)
tilt up                     # Live-reload with dashboard (~5s, requires Tilt)

# Build
make build                  # Operator binary
make docker-build           # Container image
make manifests              # Generate CRDs and RBAC
make generate               # Generate DeepCopy methods

# Dev cluster (Kind: neo4j-operator-dev)
make dev-cluster            # Create
make dev-cluster-reset      # Delete and recreate
make dev-cluster-delete     # Delete
make dev-destroy            # Completely destroy environment

# Deploy
make deploy-dev-local       # Build + deploy local dev image to Kind
make deploy-prod-local      # Build + deploy local prod image to Kind
make operator-setup         # Deploy operator to available Kind cluster
make undeploy-dev / make undeploy-prod

# Test
make test-unit              # Unit tests (no cluster required)
make test-one TEST="name"   # Single integration test by name
make test-integration       # Integration tests (auto-creates cluster, deploys operator)
make test-integration-ci    # CI mode (assumes cluster exists)
make test-ci-local          # Emulate CI locally (logs saved to logs/ci-local-*.log)
make test                   # Unit + integration
make test-coverage
make smoke-test             # Deploy standalone + verify Ready state

# Specific test
go test ./internal/controller -run TestClusterReconciler
ginkgo run -focus "should create backup" ./test/integration

# Code quality
make fmt / make lint / make lint-lenient / make vet / make security / make tidy

# CRDs
make install / make uninstall

# Generators / packaging sync (see ## Generated artifacts below)
make sync-all                # regenerate every artifact from sources
make ship-prep               # sync-all + bundle + lint + CSV coverage; pre-release one-shot
make check-drift             # CI gate: fails if any generated file is out of sync

# Operator logs/status
make operator-logs / make operator-status
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager
```

**CRITICAL: NEVER run `make dev-run`** — DNS resolution fails when operator runs outside cluster. Always deploy inside cluster via `make operator-setup`.

**Quick test deploy**:
```bash
kubectl create secret generic neo4j-admin-secret --from-literal=username=neo4j --from-literal=password=admin123
kubectl apply -f examples/standalone/single-node-standalone.yaml
kubectl apply -f examples/clusters/minimal-cluster.yaml
kubectl port-forward svc/minimal-cluster-client 7474:7474 &
```

**Debug reconciliation**:
```bash
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager -f
kubectl describe neo4jenterprisecluster <name>
# Enable debug logging
kubectl patch -n neo4j-operator-dev deployment/neo4j-operator-controller-manager \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--mode=dev","--zap-log-level=debug"]}]}}}}'
# OOM troubleshooting
kubectl describe pod <pod-name> | grep -E "(OOMKilled|Memory|Exit.*137)"
kubectl exec <pod-name> -c neo4j -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
```

## Testing

**Test Suite**: Ginkgo/Gomega. Kind clusters only. 300-second timeouts for all integration tests.

**Kind Clusters**:
- Development: `neo4j-operator-dev` | Test: `neo4j-operator-test`
- Both include cert-manager v1.20.0 with `ca-cluster-issuer`

**Test Resource Config**: CPU 50m–200m, memory ≥ 1.5Gi (Enterprise minimum), storage 500Mi–1Gi.

**Property Sharding Tests** (local only, skipped in CI):
- Requires Neo4j 2025.12+ images, 5+ servers, 4-8Gi/server, 2+ CPU/server, ~130s runtime
- `ginkgo run -focus "Property Sharding" ./test/integration`

**Test Troubleshooting**:
- Timeout → image pull delays in CI
- OOMKilled → Neo4j Enterprise needs ≥ 1.5Gi
- DB creation hangs → use `TOPOLOGY` clause, not `OPTIONS`
- Cluster formation fails → check discovery service RBAC

**MANDATORY AfterEach cleanup**:
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
Always remove finalizers before deletion. Never rely on test suite cleanup alone.

## CI/CD

**GitHub Actions**:
- Unit Tests: always run on push/PR
- Integration Tests: on-demand — trigger with label `run-integration-tests`, `[run-integration]` in commit message, or manual dispatch
- E2E Tests: manual dispatch only
- Release: multi-arch builds on git tags

**Integration tests** deploy operator to `neo4j-operator-system` in production mode (100m–1000m CPU, 256Mi–1Gi, image tag `neo4j-operator:integration-test`). `waitForOperatorReady()` hardcodes lookup to this namespace.

**Dev mode** (manual debugging only): `make deploy-dev` → logs in `neo4j-operator-dev`.

## Deployment Configuration

**Version-Specific Discovery** (LIST resolver, static pod FQDNs):

| Setting | 5.26.x (SemVer) | 2025.x+ (CalVer) |
|---|---|---|
| `dbms.cluster.discovery.version` | `V2_ONLY` (required) | not used |
| Endpoints | `dbms.cluster.discovery.v2.endpoints=<fqdns>:6000` | `dbms.cluster.endpoints=<fqdns>:6000` |
| Bootstrap hint | `internal.dbms.cluster.discovery.system_bootstrapping_strategy=me/other` | not applicable |

**Ports**: 5000 = V1 discovery (deprecated, **never used**) | **6000 = V2 discovery (always use)** | 7000 = RAFT.
CalVer detection: `ParseVersion()` → `IsCalver` when `major >= 2025` (handles 2026.x, 2027.x automatically).

**Never Use** (deprecated 4.x settings):
- `dbms.mode=SINGLE`, `causal_clustering.*`, `metrics.bolt.*`, `server.groups`, `dbms.cluster.role`

**Always Use** (5.26+):
- `server.*` instead of `dbms.connector.*`, env vars over config files, modern `TOPOLOGY` syntax

**TLS**:
```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer
```
Auto-generates SSL policies for `https`/`bolt`, certs at `/ssl/`, sets `dbms.ssl.policy.cluster.trust_all=true`.
TLS-enabled → `server.bolt.tls_level=REQUIRED`, `bolt+s://` scheme; TLS-disabled → `bolt://`. Plain `bolt://` connections are rejected when TLS is enabled.

## Neo4j Plugin Support

**APOC** (pre-bundled, no internet required):
- Operator sets `NEO4J_PLUGINS=["apoc"]` → Docker entrypoint copies `/var/lib/neo4j/labs/apoc-*-core.jar` to `/plugins/` EmptyDir at pod startup
- APOC behavioral settings → env vars (`NEO4J_APOC_EXPORT_FILE_ENABLED`, etc.), NOT `neo4j.conf`
- Procedure allowlisting (`dbms.security.procedures.unrestricted`) → `neo4j.conf`
- `apoc-extended` (not bundled) requires egress internet access

**Neo4j Config Plugins**:
- **GDS**: auto-adds `dbms.security.procedures.unrestricted=gds.*` and `allowlist=gds.*`
- **Bloom**: auto-adds `dbms.bloom.*`, `server.unmanaged_extension_classes`, HTTP auth allowlist
- **GenAI, N10s, GraphQL**: standard plugin config handling

**Plugin configuration validation**:
- Cluster deployments: check StatefulSet env vars (`NEO4J_PLUGINS`)
- Standalone deployments: check ConfigMap content (Neo4j reads config from there)

**`NEO4J_PLUGINS` live-patching**: Never bake into static StatefulSet template in `internal/resources/cluster.go`. Use `MergeNeo4jPluginList` helper so multiple controllers (plugin controller, fleet management) don't overwrite each other.

**`envVarsEqual` is an intentional subset check + ownership-tracked removal**: verifies desired vars present in current with the correct value, and tolerates extras (so plugin/fleet/Aura controllers can live-patch their own env vars without triggering a wholesale-replace oscillation). On top of the subset, the cluster controller writes the set of names it owns to the `neo4j.com/cluster-controller-env-vars` annotation each reconcile; the next reconcile uses that annotation plus the new desired set to enforce removals (`previously-owned ∖ desired`) without disturbing foreign vars (`current ∖ previously-owned ∖ desired`). Never revert to strict length+value equality, never drop the annotation tracking, never wholesale-replace the env array on the apply path — use `mergeEnvVars`.

## Aura Fleet Management

```yaml
spec:
  auraFleetManagement:
    enabled: true
    tokenSecretRef:
      name: aura-fleet-token
      key: token              # default: "token"
```

**Two-phase reconciliation** (never collapse into one step):
1. Install plugin via `mergeFleetManagementPlugin` — runs on every reconcile when enabled
2. Register token via `CALL fleetManagement.registerToken($token)` — runs only when cluster `Ready` and token not yet registered

Plugin-only mode: omit `tokenSecretRef` to defer registration.

**Key files**:
- `internal/controller/neo4jenterprisecluster_controller.go` — `reconcileAuraFleetManagement`, `mergeFleetManagementPlugin`
- `internal/controller/neo4jenterprisestandalone_controller.go` — standalone equivalents
- `internal/controller/plugin_controller.go` — `MergeNeo4jPluginList`
- `internal/neo4j/client.go` — `RegisterFleetManagementToken`, `IsFleetManagementInstalled`
- `internal/validation/fleet_validator.go`

## Live Cluster Diagnostics

When `spec.monitoring.enabled=true` and cluster is `Ready`:
- `status.diagnostics.servers[]` — from `SHOW SERVERS`: name, address, state, health
- `status.diagnostics.databases[]` — from `SHOW DATABASES`; `system` DB excluded from health checks
- Conditions: `ServersHealthy`, `DatabasesHealthy` (use `SetNamedCondition`, not `SetReadyCondition`)
- Prometheus metric: `neo4j_operator_server_health{cluster_name, namespace, server_name, server_address}` — 1=healthy, 0=degraded

**CollectDiagnostics is non-fatal**: errors → `status.diagnostics.collectionError` only. Never `return err` from diagnostics.

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

**Never use** (4.x syntax — fails in 5.26+):
```cypher
CREATE DATABASE baddb OPTIONS {primaries: 1, secondaries: 1}  -- DEPRECATED
CALL dbms.cluster.role()  -- REMOVED in 5.0, use SHOW DATABASES
```

## Default Database Behavior

Neo4j automatically creates a default `neo4j` database at cluster bootstrap. The operator does not create, manage, or interfere with it.

**Default Topology**: The `neo4j` database is created with Neo4j's built-in defaults: **1 primary, 0 secondaries** — regardless of how many servers the cluster has. This means on a 3-server cluster, the `neo4j` database only runs on 1 server.

**Controlling default topology at bootstrap** (initial.* settings — first bootstrap only, no effect after):
```yaml
spec:
  config:
    initial.dbms.default_primaries_count: "3"
    initial.dbms.default_secondaries_count: "1"
```

**Changing topology after bootstrap**: Use `ALTER DATABASE neo4j SET TOPOLOGY 3 PRIMARIES 1 SECONDARY`.

**Cannot skip creation**: Neo4j has no setting to prevent the default database from being created.

**Operator interaction**:
- Diagnostics: included in `status.diagnostics.databases[]` and counts toward `DatabasesHealthy` condition (unlike `system` which is excluded)
- Neo4jDatabase CRD named `neo4j`: allowed with a warning ("will shadow the default database"); `IF NOT EXISTS` makes creation a no-op since it already exists; deletion via CRD will drop it
- `dbms.default_database` in `spec.config`: rejected by validator (deprecated — use `dbms.setDefaultDatabase()` procedure instead)

## Neo4jDatabase CRD

Works with both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone`. `DatabaseValidator` tries cluster lookup first, then standalone automatically.

**CRD Separation of Concerns** (strict, never violate):
- Cluster/Standalone CRDs own: infrastructure, server config, auth, TLS, plugins, backup, images
- Neo4jDatabase CRD owns: database name, topology, Cypher version, CREATE DATABASE options only
- ❌ Neo4jDatabase MUST NOT override cluster/server-level settings

**Standalone deployments require `NEO4J_AUTH` env var** for automatic password setup (critical for Neo4jDatabase support).

## Neo4jUser, Neo4jRole & Neo4jRoleBinding CRDs

Three CRDs, one design rule: **privileges live on `Neo4jRole`, not on `Neo4jUser` or `Neo4jRoleBinding`**. Users carry only `roles: []` bindings; roles carry `privileges: []`; bindings carry only `roles: []`. See `docs/user_guide/user_role_management.md` for the end-to-end picture.

**Files:**
- `api/v1beta1/neo4juser_types.go`, `api/v1beta1/neo4jrole_types.go`, `api/v1beta1/neo4jrolebinding_types.go` — CRD types
- `internal/controller/neo4juser_controller.go`, `neo4jrole_controller.go`, `neo4jrolebinding_controller.go` — reconcilers
- `internal/controller/cluster_resolver.go` — shared `ResolveClusterRef` helper
- `internal/controller/diagnostics_users_roles.go` — shared `collectUsersAndRoles` helper used by both cluster and standalone diagnostic collectors
- `internal/neo4j/users.go` — `ShowUser`, `AlterUser` (with `AlterUserOptions` builder), `ShowRole`, `CreateRoleAdvanced`, `DropRoleIfExists`, `DropUserIfExists`, `ShowRolePrivileges`, `ListUserRoles` (replaces buggy `GetUserRoles`), `ListUsers`, `ListRoles`
- `internal/neo4j/privileges.go` — `CanonicalisePrivilegeStatement`, `DerivePrivilegeRevoke`, `PrivilegeStatementMatchesRole` for diff-based reconciliation
- `internal/validation/user_validator.go`, `role_validator.go`, `rolebinding_validator.go` — controller-side validators (per CLAUDE.md rule #26)

**Reconciliation source-of-truth:**
- `Neo4jUser.spec` is authoritative for password (via Secret hash), `accountStatus`, `homeDatabase`, `roles`, `externalAuth`. Drift is reverted on every loop.
- `Neo4jRole.spec.privileges` is authoritative when `enforcePrivileges: true` (default). Manual `GRANT/REVOKE` outside the operator is reverted.
- Built-in roles (`PUBLIC`, `reader`, etc.) require `adoptBuiltin: true` to be managed; they are never dropped on CR delete.
- `PUBLIC` is auto-assigned by Neo4j and never granted/revoked by the operator.

**Watches:** the `Neo4jUser` controller watches `Neo4jRole` (in `SetupWithManager`) so users with missing custom roles re-reconcile when their roles land.

**Key Cypher commands** (all run against `system` DB):
- User: `CREATE USER`, `ALTER USER` (compound, REMOVE clauses before SET), `DROP USER IF EXISTS`, `SHOW USERS WITH AUTH`, `GRANT/REVOKE ROLE`
- Role: `CREATE ROLE [AS COPY OF]`, `DROP ROLE IF EXISTS`, `SHOW ROLES`, `SHOW ROLE <r> PRIVILEGES AS COMMANDS YIELD command, immutable`
- Privileges: `GRANT/DENY/REVOKE ... ON ... TO/FROM ...` — REVOKEs are derived textually, not user-supplied

## Key Implementation Patterns

**Resource Version Conflict**: Always wrap with `retry.RetryOnConflict(retry.DefaultRetry, ...)` — required for Neo4j 2025.01.0 cluster formation.

**Template Comparison**: Use `sts.UID != ""` to check if StatefulSet exists, NOT `sts.ResourceVersion != ""` (ResourceVersion is populated even for new resources).

**Split-Brain Detection**: `internal/controller/splitbrain_detector.go` — connects to each pod, compares cluster views, auto-restarts orphaned pods.
```bash
kubectl get events --field-selector reason=SplitBrainDetected -A
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager | grep -i splitbrain
```

**Edition field removed**: No `edition: enterprise` field in CRDs. Operator always assumes enterprise. Neo4j client still checks actual edition via `CALL dbms.components()`.

**Structured Events**: Use constants from `internal/controller/events.go`. Use `corev1.EventTypeNormal`/`corev1.EventTypeWarning` — never raw strings `"Normal"`/`"Warning"`.

## Regression Prevention Checklist

1. **Resource Conflicts**: `retry.RetryOnConflict` with `controllerutil.CreateOrUpdate`
2. **Template Comparison**: `UID != ""` not `ResourceVersion != ""`
3. **Test Timeouts**: 300-second for all integration tests
4. **Resource Requirements**: CPU ≤ 200m, memory ≥ 1.5Gi
5. **Cluster Formation**: Verify with `SHOW SERVERS`, not just status checks
6. **Server Architecture**: `servers` field for clusters; `primaries`/`secondaries` for databases
7. **Pod Naming**: `<cluster>-server-*` (never `primary-*` or `secondary-*`)
8. **Certificate DNS**: Include all server pod DNS names
9. **Discovery Port**: Always port **6000** (`tcp-tx`), never 5000
10. **CRD Separation**: Neo4jDatabase must not override cluster/server settings
11. **Enterprise Images**: `neo4j:X.Y-enterprise` only, never `neo4j:X.Y`
12. **Test Cleanup**: MANDATORY AfterEach with finalizer removal + `cleanupCustomResourcesInNamespace()`
13. **NEO4J_AUTH**: Standalone deployments need this env var
14. **Plugin Config Source**: APOC → StatefulSet env vars (cluster) / ConfigMap (standalone)
15. **Status Phase**: Check `status.phase="Ready"` before database ops
16. **TLS Scheme**: `bolt+s://` (TLS on) / `bolt://` (TLS off)
17. **Backup Path**: `--to-path` syntax for Neo4j 5.26+
18. **`envVarsEqual` Subset + Ownership-Tracked Removal**: subset check on desired side (tolerates foreign extras) + removal check via the `neo4j.com/cluster-controller-env-vars` annotation (`previously-owned ∖ desired` is enforced). Apply path uses `mergeEnvVars`, never wholesale-replace the env array. Never revert to strict equality, never drop the annotation
19. **`NEO4J_PLUGINS` Live-Patch**: Via `MergeNeo4jPluginList`, never in static StatefulSet template
20. **Fleet Two-Phase**: Plugin install phase ≠ token registration phase — never collapse
21. **Diagnostics Non-Fatal**: Never `return err` from `CollectDiagnostics`
22. **Diagnostic Conditions**: `SetNamedCondition` for `ServersHealthy`/`DatabasesHealthy`; `system` DB excluded
23. **Event Reasons**: Constants from `events.go`; `corev1.EventTypeNormal/Warning` not raw strings
24. **`SetNamedCondition`**: For non-Ready conditions; `SetReadyCondition` only for the `Ready` type
25. **Storage Expansion**: Orphan-delete STS (not regular delete); compare spec vs actual PVC sizes (not old vs new spec); `retry.RetryOnConflict` on PVC patches; validate `allowVolumeExpansion` before patching; never shrink PVCs
26. **No Webhooks**: All validation is controller-side in `internal/validation/`; never introduce `ValidatingWebhookConfiguration` or `_webhook.go` files
27. **TLS CA Auto-Discovery**: `buildTLSConfig()` in `internal/neo4j/client.go` loads CA from cert-manager Secret (`{name}-tls-secret`) automatically; `TrustedCASecret` is an override; `InsecureSkipVerify` is fallback only
28. **All Client Functions Must Handle TLS**: `NewClientForEnterprise`, `NewClientForEnterpriseStandalone`, AND `NewClientForPod` all call `buildTLSConfig()`; split-brain detector uses dynamic `bolt+s://` scheme
29. **ObservedGeneration**: Set `status.observedGeneration = latest.Generation` on every status update in both cluster and standalone controllers
30. **Name Length Validation**: Cluster names max 56 chars (DNS label 63 minus `-server` suffix); standalone max 63 chars; database names max 65 chars, must match `^[a-zA-Z][a-zA-Z0-9.\-]*$`
31. **serverRoles Validation**: Index must be in `[0, servers-1]`, no duplicates, cannot set ALL to SECONDARY
32. **Standalone Diagnostics**: `collectStandaloneDiagnostics()` runs `SHOW DATABASES` when monitoring enabled and phase Ready; non-fatal like cluster diagnostics
33. **Standalone UpgradeStrategy**: Pre-upgrade health check via `VerifyConnectivity`; `autoPauseOnFailure` blocks upgrade if health check fails; STS update strategy set from spec
34. **Standalone Health Probes**: Readiness/liveness/startup probes via `/conf/health.sh` (checks process + HTTP 7474); ConfigMap includes `health.sh` alongside `neo4j.conf` with `DefaultMode: 0755`
35. **Bolt TLS REQUIRED**: Both cluster and standalone set `server.bolt.tls_level=REQUIRED` when TLS enabled; plain `bolt://` rejected
36. **Deprecated Config Keys**: Validator warns on `dbms.logs.query.enabled` (use `db.logs.query.enabled`); always use `db.*` namespace for Neo4j 5.x+ settings
37. **Default Database Topology**: `neo4j` database gets 1 primary, 0 secondaries by default; `initial.dbms.default_primaries_count`/`default_secondaries_count` only work at first bootstrap; use `ALTER DATABASE` to change post-bootstrap
38. **Privileges live on `Neo4jRole`, not `Neo4jUser`**: never inline `GRANT/DENY` on a user. Users carry only `roles: []` bindings. Putting privileges on users re-implements RBAC inside-out and creates merge conflicts when two CRs touch the same role.
39. **PUBLIC role is implicit**: never include in role grants/revokes; the user controller filters it out of both desired and actual role sets. Listing PUBLIC in `Neo4jUser.spec.roles` produces a warning, not an error.
40. **Built-in role guard**: `Neo4jRole` validator rejects names in `{PUBLIC,reader,editor,publisher,architect,admin}` unless `spec.adoptBuiltin=true`. Adopted built-ins are NEVER dropped on CR delete (only the finalizer is released).
41. **Privilege drift via `SHOW ROLE PRIVILEGES AS COMMANDS`**: source of truth is `Neo4jRole.spec.privileges`. The controller canonicalises both sides (`CanonicalisePrivilegeStatement`), diffs as sets, derives REVOKEs textually via `DerivePrivilegeRevoke`. Immutable rows (immutable=true column) are excluded from the revoke set and surfaced via `status.privilegeDrift`. `enforcePrivileges: false` skips the revoke pass entirely.
42. **Privilege statement validation**: each entry in `Neo4jRole.spec.privileges` MUST start with GRANT or DENY (REVOKE is rejected — the operator derives REVOKEs) and end with `TO <spec.name>`. Otherwise the canonicaliser cannot derive the matching REVOKE when the privilege is removed from spec.
43. **`GetUserRoles` in `internal/neo4j/client.go` is buggy**: it queries `SHOW USER PRIVILEGES YIELD role`, returning one row per privilege instead of per role. Use `Client.ListUserRoles` or `Client.ShowUser` instead.
44. **Password rotation via Secret hash**: `Neo4jUser` controller stores SHA-256 of the password Secret value in `status.passwordSecretHash`; rotation is detected when the hash changes. The password is never persisted in CR fields. Skip `SET PASSWORD` when only `externalAuth` is configured.
45. **`ALTER USER` clause ordering**: REMOVE clauses MUST precede SET clauses on a single statement. The `AlterUserOptions` builder honours this — never hand-roll ALTER USER strings.
46. **`Neo4jUser.spec.roles` referencing missing custom roles**: do NOT fail the reconcile. Set the `PendingDependencies` condition and requeue; the user controller watches `Neo4jRole` and re-reconciles when the role lands.
47. **Same-namespace `clusterRef` for users/roles**: cross-namespace references are not supported in v1; both CRDs are namespace-scoped. If multi-tenant patterns become a real ask, design an opt-in via a `Neo4jClusterAccessGrant` CR — do not silently widen the lookup.
48. **Identifier quoting in Cypher**: all role/user names are wrapped with backticks via `escapeBackticks()` (which doubles embedded backticks). Never `fmt.Sprintf` user-controlled names directly into Cypher; password and provider IDs go through driver parameters.
49. **`Neo4jRoleBinding` never creates or drops users**: it only manages role grants for users provisioned externally (SSO/LDAP first-login). If the user is absent the binding sits in `UserNotFound` and waits — do not change this behaviour without a migration, since users may not exist until first authentication.
50. **`Neo4jRoleBinding` overlap with `Neo4jUser`**: validator rejects bindings whose `clusterRef`+`username` match an existing `Neo4jUser` CR in the same namespace. Two CRs racing on the same role grants is a footgun — pick one model.
51. **`Neo4jRoleBinding.spec.enforceExclusive`**: defaults to false. With false, the binding only manages roles in `.spec.roles` (and previously-recorded `status.grantedRoles` for revoke-on-removal). With true, any role on the user not in `.spec.roles` is revoked. Never flip the default — non-exclusive is what makes the CR safe to use alongside other tooling.
52. **Diagnostics `Users`/`Roles` lists are bounded**: `maxDiagnosticUsers`/`maxDiagnosticRoles` cap the slice length to keep CRD size reasonable; the full count is in `UserCount`/`RoleCount`. Never remove the cap without a corresponding pruning strategy — large user/role tables would otherwise blow up the CRD.
55. **Truststore init container seeds from JDK cacerts**: `BuildTrustStoreInitContainer` MUST start by copying `$JAVA_HOME/lib/security/cacerts` to `/truststore/truststore.jks` before importing user-supplied CAs. Skipping this seed step breaks trust in public CAs (Let's Encrypt, DigiCert, etc.) for any cluster that opts into a custom truststore. The seed step is what makes `spec.trustedCASecrets` purely additive.
56. **`spec.trustedCASecrets` Secret-name = keytool alias**: aliases must be unique inside the JKS, so the validator rejects duplicate Secret names. Don't change the alias derivation to anything not statically derivable from spec — the resource builder relies on Secret-name → mount-path → keytool alias.
57. **Legacy `spec.auth.trustStore` folds into `spec.trustedCASecrets`**: `CollectTrustedCASecrets` is the single source of truth for both paths. Never wire `spec.auth.trustStore` into resources directly, or the new and legacy paths will produce duplicate volumes/init containers and the truststore JKS build will fail with a duplicate-alias error.
58. **`spec.extraVolumeMounts` reserved paths**: the validator rejects mounts at `/data`, `/logs`, `/conf`, `/ssl`, `/plugins`, `/truststore`, `/truststore-ca`, `/var/lib/neo4j` and its standard subdirectories. Don't relax this list without a clear reason — overlaying any of these silently breaks operator-managed content or the truststore init flow.
59. **AUTH RULE Cypher requires `CYPHER 25` prefix**: every statement in `internal/neo4j/auth_rules.go` (SHOW/CREATE OR REPLACE/ALTER/DROP/GRANT/REVOKE) prepends `cypher25Prefix`. Neo4j 2026.x defaults the system DB to Cypher 5; without the prefix the syntax fails to parse with `42I06: Invalid input 'AUTH'`. Don't drop the prefix even after the database default flips to 25 — the prefix stays harmless.
60. **`oidc-`-prefixed provider name in ABAC config**: `dbms.security.abac.authorization_providers` must reference values that ALSO appear in `dbms.security.authorization_providers`. Neo4j uses the `oidc-<name>` form for OIDC providers in the authorization-providers list, so abac.authorization_providers must use the prefixed form too (e.g. `oidc-test-oidc`, NOT bare `test-oidc`).
61. **Authrule controller is in the `--controllers` default list**: `cmd/main.go`'s default for `--controllers` (dev mode) MUST include `authrule`, otherwise local dev deployments silently skip auth-rule reconciliation while accepting `Neo4jAuthRule` CRs. Production-mode (`setupProductionControllers`) wires the controller unconditionally, so this only bites in dev.
62. **Operator's outbound Bolt URI uses the routing scheme `neo4j://` / `neo4j+s://`**: `buildConnectionURIForEnterprise` and `buildConnectionURIForStandalone` (`internal/neo4j/client.go`) MUST emit `neo4j://` (or `neo4j+s://` with TLS), NOT `bolt://` / `bolt+s://`. Cluster admin commands (CREATE/DROP USER, GRANT/REVOKE, AUTH RULE, etc.) must execute on the cluster leader; the Go driver only honors `AccessMode: AccessModeWrite` and routes writes to the leader under the routing scheme. Under `bolt://` the access mode is silently ignored — connections land wherever K8s steered them via the `{cluster}-client` ClusterIP, producing `Neo.ClientError.Cluster.NotALeader` on roughly N-1 of every N reconciles and visible Ready ↔ Failed status flicker. The only legitimate `bolt://` consumer in the operator is `splitbrain_detector.go`, which intentionally bypasses routing to inspect each pod's RAFT view individually. There is also a unit test in `internal/neo4j/uri_test.go` that locks the scheme in.
63. **Bolt driver timeouts must stay tight enough that failure surfaces fast**: `NewClientForEnterprise` / `NewClientForEnterpriseStandalone` set `ConnectionAcquisitionTimeout=10s`, `SocketConnectTimeout=5s`, `MaxTransactionRetryTime=15s`. Under the routing scheme these gate routing-table fetch retries against an unreachable cluster; if you bump them back up to 30s+ the operator's reconcile queue stalls behind hung Bolt calls in any scenario where the cluster isn't responding (envtest, real outage, cluster bootstrap). The current values are still generous enough for healthy clusters (sub-second routing in practice).

## Generated artifacts

Several files in this repo are generated, not hand-written. Editing them directly is wasted work — the next sync overwrites your changes. Sources of truth:

| Generated file | Source | Regenerate via |
|---|---|---|
| `config/rbac/role.yaml` | `+kubebuilder:rbac:` markers in `internal/controller/*.go` | `make manifests` |
| `config/crd/bases/*.yaml` | Go types in `api/v1beta1/*` + kubebuilder markers | `make manifests` |
| `api/v1beta1/zz_generated.deepcopy.go` | Go types in `api/v1beta1/*` | `make generate` |
| `config/crd/kustomization.yaml` (resources list) | files in `config/crd/bases/` | `make sync-kustomize` |
| `config/samples/kustomization.yaml` (resources list) | `config/samples/neo4j_*.yaml` filenames | `make sync-kustomize` |
| `config/rbac/<crd>_{editor,viewer}_role.yaml` + RBAC kustomization | `spec.{group,names.plural,names.singular}` from each CRD base | `make sync-editor-viewer-roles` |
| `charts/neo4j-operator/crds/*.yaml` | `config/crd/bases/*.yaml` | `make helm-sync-crds` |
| `charts/neo4j-operator/templates/clusterrole.yaml` | `config/rbac/role.yaml` rules | `make helm-sync-rbac` |
| `charts/neo4j-operator/Chart.yaml` (`artifacthub.io/crds` annotation) | CRD bases + curated descriptions in `scripts/helm-sync-artifacthub-crds.sh` | `make helm-sync-artifacthub-crds` |
| `bundle/manifests/*` and `bundle/metadata/*` (OperatorHub) | `config/manifests/bases/*.csv.yaml` + everything above | `make bundle` |

Two umbrella targets:
- **`make sync-all`** — runs every regeneration step (no bundle).
- **`make ship-prep`** — `sync-all` + `bundle` + `helm-lint` + `check-csv-coverage`. Run before tagging a release.

CI gate:
- **`make check-drift`** — runs `sync-all` + `bundle`, then `git diff --exit-code` (ignoring the `createdAt:` timestamp the operator-sdk stamps). Fails if any committed file is stale. Use this in CI to enforce that whoever changes a source also commits the regenerated output.

53. **Never hand-edit generated files**: edit the source listed above instead. Each generated file carries a `# This file is GENERATED. DO NOT EDIT.` header; check-drift will revert tampering.
54. **`scripts/helm-sync-artifacthub-crds.sh` requires a description per CRD**: when adding a CRD, also add a `case "$kind" in ... esac` row to the script. The script exits non-zero if a CRD has no mapped description, so you can't forget.

## Reports

All reports in `/reports/` with format `YYYY-MM-DD-descriptive-name.md`.

**Key Reports:**
- `/reports/2025-08-19-server-based-architecture-implementation.md` — server-based architecture
- `/reports/2025-08-05-neo4j-2025.01.0-enterprise-cluster-analysis.md` — Neo4j 2025.x compatibility
- `/reports/2025-08-08-seed-uri-and-server-architecture-release-notes.md` — Seed URI feature

# important-instruction-reminders
Do what has been asked; nothing more, nothing less.
NEVER create files unless they're absolutely necessary for achieving your goal.
ALWAYS prefer editing an existing file to creating a new one.
NEVER proactively create documentation files (*.md) or README files. Only create documentation files if explicitly requested by the User.
