# CLAUDE.md

Guidance to Claude Code (claude.ai/code) when working in this repository.

## Project Overview

Neo4j Enterprise Operator for Kubernetes — manages Neo4j Enterprise deployments (v5.26+) using the Kubebuilder framework.

**Supported Neo4j versions**: 5.26.x (last semver LTS) and 2025.x.x+ (CalVer). Neo4j moved from semver to CalVer after 5.26 — no 5.27+ semver releases exist or will exist.

**Hard constraints (NEVER violate):**
- **KIND ONLY**: Kind (Kubernetes in Docker) is the only supported cluster for dev/test/CI. No minikube, k3s, etc.
- **ENTERPRISE IMAGES ONLY**: `neo4j:5.26-enterprise` / `neo4j:2025.01.0-enterprise`. Never community.
- **NO WEBHOOKS**: No `ValidatingWebhookConfiguration` / `MutatingWebhookConfiguration` / `_webhook.go` files. All validation lives in `internal/validation/` and is called inline from the reconciler.
- **Discovery**: V2_ONLY mode exclusively.

**Deployment types:**
- **Neo4jEnterpriseCluster**: HA clusters (min 2 servers; self-organize into primary/secondary).
- **Neo4jEnterpriseStandalone**: Single-node (dev/test).

## Architecture

- CRDs: `Neo4jEnterpriseCluster`, `Neo4jEnterpriseStandalone`, `Neo4jBackup`, `Neo4jRestore`, `Neo4jDatabase`, `Neo4jUser`, `Neo4jRole`, `Neo4jRoleBinding`, `Neo4jAuthRule`.
- Controllers: cluster & standalone controllers with controller-side validation.
- Neo4j client: Bolt protocol.

**Directories:** `api/v1beta1/` (CRD types), `internal/controller/`, `internal/resources/` (K8s builders), `test/` (unit/integration/e2e).

**Server-based architecture**: single `{cluster-name}-server` StatefulSet with `replicas: N`. Pods are `{cluster-name}-server-0…N-1`. Backup: `{cluster-name}-backup-0` (one centralized StatefulSet per cluster, ~70% fewer resources than sidecars). Never use `primary-*` / `secondary-*` pod names.

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

```bash
# Bootstrap / teardown
make dev-up                 # Create Kind cluster + deploy operator
make dev-down               # Tear down everything
make check-prereqs          # Verify tools

# Inner loop
make deploy-dev-local       # Rebuild + redeploy to Kind (~60s)
make dev-watch              # Auto-rebuild on file changes
tilt up                     # Live reload (~5s, needs Tilt)

# Build
make build / docker-build / manifests / generate

# Dev cluster (Kind: neo4j-operator-dev)
make dev-cluster / dev-cluster-reset / dev-cluster-delete / dev-destroy

# Deploy
make deploy-dev-local / deploy-prod-local
make operator-setup         # Deploy to whatever Kind cluster exists
make undeploy-dev / undeploy-prod

# Test
make test-unit              # No cluster
make test-one TEST="name"   # Single integration test
make test-integration       # Auto-creates cluster, deploys operator
make test-integration-ci    # Assumes cluster exists
make test-ci-local          # Emulate CI locally (logs → logs/ci-local-*.log)
make test / test-coverage
make smoke-test             # Standalone deploy + Ready check
go test ./internal/controller -run TestClusterReconciler
ginkgo run -focus "should create backup" ./test/integration

# Quality
make fmt / lint / lint-lenient / vet / security / tidy

# CRDs
make install / uninstall

# Generators (see ## Generated artifacts)
make sync-all               # Regenerate every artifact
make ship-prep              # sync-all + bundle + lint + CSV coverage
make bundle-release         # Stamp real createdAt: (release workflow)
make check-drift            # CI gate: fails on stale generated files

# Logs/status
make operator-logs / operator-status
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager
```

**NEVER `make dev-run`** — DNS resolution fails when the operator runs outside the cluster. Always deploy inside via `make operator-setup`.

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
kubectl patch -n neo4j-operator-dev deployment/neo4j-operator-controller-manager \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--mode=dev","--zap-log-level=debug"]}]}}}}'
kubectl describe pod <pod-name> | grep -E "(OOMKilled|Memory|Exit.*137)"
kubectl exec <pod-name> -c neo4j -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
```

## Testing

Ginkgo/Gomega, Kind only, **300-second timeouts** for all integration tests.

**Kind clusters:** dev = `neo4j-operator-dev`, test = `neo4j-operator-test`. Both ship cert-manager v1.20.0 with `ca-cluster-issuer`.

**Test resources:** CPU 50m–200m, memory ≥ 1.5Gi (Enterprise minimum), storage 500Mi–1Gi.

**Property sharding tests** (local only, skipped in CI): need Neo4j 2025.12+, 5+ servers, 4–8Gi/server, 2+ CPU/server, ~130s. `ginkgo run -focus "Property Sharding" ./test/integration`.

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

- Unit tests: every push/PR.
- Integration tests: on-demand — label `run-integration-tests`, `[run-integration]` in commit, or manual dispatch.
- E2E tests: manual dispatch only.
- Release: multi-arch builds on git tags.

Integration tests deploy to `neo4j-operator-system` in prod mode (100m–1000m CPU, 256Mi–1Gi, image `neo4j-operator:integration-test`). `waitForOperatorReady()` hardcodes this namespace. Dev mode for manual debugging: `make deploy-dev` → logs in `neo4j-operator-dev`.

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
Auto-generates SSL policies for `https`/`bolt`, certs at `/ssl/`. Cluster SSL defaults to strict (`spec.tls.strictPeerValidation: true`): `trust_all=false`, `client_auth=REQUIRE`, `verify_hostname=true`, with the cert-manager Secret's `ca.crt` projected to `/ssl/trusted/ca.crt`. Set `strictPeerValidation: false` to revert to legacy `trust_all=true` (Neo4j docs flag legacy as debugging-only). Validator refuses strict if the issuer doesn't populate `ca.crt`.

When TLS is enabled: `server.bolt.tls_level=REQUIRED`, scheme is `bolt+s://`. Plain `bolt://` connections are rejected.

## Neo4j Plugin Support

**APOC** (pre-bundled, no internet needed):
- `NEO4J_PLUGINS=["apoc"]` → Docker entrypoint copies `/var/lib/neo4j/labs/apoc-*-core.jar` to `/plugins/` EmptyDir at pod startup.
- APOC behavior → env vars (`NEO4J_APOC_EXPORT_FILE_ENABLED`, etc.), NOT `neo4j.conf`.
- Procedure allowlisting (`dbms.security.procedures.unrestricted`) → `neo4j.conf`.
- `apoc-extended` (not bundled) requires egress internet.

**Other plugins:**
- **GDS**: auto-adds `dbms.security.procedures.unrestricted=gds.*` + `allowlist=gds.*`.
- **Bloom**: auto-adds `dbms.bloom.*`, `server.unmanaged_extension_classes`, HTTP auth allowlist.
- **GenAI, N10s, GraphQL**: standard plugin config handling.

**Validate plugin config:** cluster → StatefulSet env vars (`NEO4J_PLUGINS`); standalone → ConfigMap content (Neo4j reads conf from there).

**`NEO4J_PLUGINS` live-patching**: never bake into the static StatefulSet template in `internal/resources/cluster.go`. Use `MergeNeo4jPluginList` so plugin/fleet/Aura controllers don't overwrite each other.

**`envVarsEqual` = subset check + ownership-tracked removal.** Verifies desired vars exist in current with the right value; tolerates extras so plugin/fleet/Aura controllers can live-patch their own env vars without triggering wholesale-replace oscillation. The cluster controller writes the set of names it owns to the `neo4j.com/cluster-controller-env-vars` annotation each reconcile; the next reconcile uses `previously-owned ∖ desired` to enforce removals without disturbing foreign vars (`current ∖ previously-owned ∖ desired`). Apply path uses `mergeEnvVars` — never wholesale-replace the env array, never revert to strict length+value equality, never drop the annotation.

## Aura Fleet Management

```yaml
spec:
  auraFleetManagement:
    enabled: true
    tokenSecretRef:
      name: aura-fleet-token
      key: token              # default: "token"
```

**Two-phase reconciliation** (never collapse):
1. Install plugin via `mergeFleetManagementPlugin` — every reconcile when enabled.
2. Register token via `CALL fleetManagement.registerToken($token)` — only when cluster `Ready` AND token not yet registered.

Plugin-only mode: omit `tokenSecretRef` to defer registration.

**Files:** `internal/controller/neo4jenterprisecluster_controller.go` (`reconcileAuraFleetManagement`, `mergeFleetManagementPlugin`); `neo4jenterprisestandalone_controller.go` (standalone equivalents); `plugin_controller.go` (`MergeNeo4jPluginList`); `internal/neo4j/client.go` (`RegisterFleetManagementToken`, `IsFleetManagementInstalled`); `internal/validation/fleet_validator.go`.

## Live Cluster Diagnostics

When `spec.monitoring.enabled=true` and cluster is `Ready`:
- `status.diagnostics.servers[]` from `SHOW SERVERS` (name, address, state, health).
- `status.diagnostics.databases[]` from `SHOW DATABASES` (`system` DB excluded from health checks).
- Conditions `ServersHealthy`, `DatabasesHealthy` via `SetNamedCondition` (NOT `SetReadyCondition` — that's only for the `Ready` type).
- Prometheus metric `neo4j_operator_server_health{cluster_name, namespace, server_name, server_address}`: 1=healthy, 0=degraded.

**`CollectDiagnostics` is non-fatal**: errors go to `status.diagnostics.collectionError` only — never `return err`.

Standalone has its own non-fatal `collectStandaloneDiagnostics()` running `SHOW DATABASES` under the same monitoring/Ready conditions.

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

**Default topology**: 1 primary, 0 secondaries — regardless of cluster size. On a 3-server cluster, `neo4j` only runs on 1 server.

**Control at bootstrap** (first bootstrap only, no effect after):
```yaml
spec:
  config:
    initial.dbms.default_primaries_count: "3"
    initial.dbms.default_secondaries_count: "1"
```

**Change post-bootstrap**: `ALTER DATABASE neo4j SET TOPOLOGY 3 PRIMARIES 1 SECONDARY`. Neo4j has no setting to skip creation.

**Operator interaction:**
- Diagnostics: included in `status.diagnostics.databases[]`, counts toward `DatabasesHealthy` (unlike `system` which is excluded).
- `Neo4jDatabase` CRD named `neo4j`: allowed with a warning ("will shadow the default database"); `IF NOT EXISTS` makes creation a no-op; deletion via CRD will drop it.
- `dbms.default_database` in `spec.config`: rejected by validator (deprecated — use `dbms.setDefaultDatabase()` procedure).

## Neo4jDatabase CRD

Works with both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone`. `DatabaseValidator` tries cluster lookup first, then standalone.

**Separation of concerns** (strict):
- Cluster/Standalone CRDs own: infrastructure, server config, auth, TLS, plugins, backup, images.
- Neo4jDatabase owns: database name, topology, Cypher version, CREATE DATABASE options ONLY.
- ❌ Neo4jDatabase MUST NOT override cluster/server-level settings.

**Standalone needs `NEO4J_AUTH`** env var for automatic password setup (required for Neo4jDatabase support).

## Neo4jUser, Neo4jRole & Neo4jRoleBinding CRDs

Three CRDs, one design rule: **privileges live on `Neo4jRole`, not `Neo4jUser` or `Neo4jRoleBinding`**. Users carry only `roles: []`; roles carry `privileges: []`; bindings carry only `roles: []`. Inlining `GRANT/DENY` on a user re-implements RBAC inside-out and creates merge conflicts when two CRs touch the same role. See `docs/user_guide/user_role_management.md`.

**Files:**
- `api/v1beta1/neo4juser_types.go`, `neo4jrole_types.go`, `neo4jrolebinding_types.go`.
- `internal/controller/neo4juser_controller.go`, `neo4jrole_controller.go`, `neo4jrolebinding_controller.go`.
- `internal/controller/cluster_resolver.go` (`ResolveClusterRef`), `diagnostics_users_roles.go` (`collectUsersAndRoles`).
- `internal/neo4j/users.go` — `ShowUser`, `AlterUser` (+ `AlterUserOptions`), `ShowRole`, `CreateRoleAdvanced`, `DropRoleIfExists`, `DropUserIfExists`, `ShowRolePrivileges`, `ListUserRoles` (replaces buggy `GetUserRoles`), `ListUsers`, `ListRoles`.
- `internal/neo4j/privileges.go` — `CanonicalisePrivilegeStatement`, `DerivePrivilegeRevoke`, `PrivilegeStatementMatchesRole`.
- `internal/validation/{user,role,rolebinding}_validator.go`.

**Source of truth:**
- `Neo4jUser.spec` is authoritative for password (via Secret hash), `accountStatus`, `homeDatabase`, `roles`, `externalAuth`. Drift reverted every loop.
- `Neo4jRole.spec.privileges` is authoritative when `enforcePrivileges: true` (default). Manual `GRANT/REVOKE` outside the operator is reverted; `enforcePrivileges: false` skips the revoke pass entirely.
- Built-in roles (`PUBLIC`, `reader`, `editor`, `publisher`, `architect`, `admin`) require `adoptBuiltin: true` to be managed; never dropped on CR delete (only finalizer released). Validator rejects unmanaged built-in names.
- `PUBLIC` is auto-assigned and never granted/revoked; user controller filters it from both sides. Listing PUBLIC in `Neo4jUser.spec.roles` produces a warning, not an error.

**Watches:** the `Neo4jUser` controller watches `Neo4jRole` in `SetupWithManager` so users with missing custom roles re-reconcile when the role lands.

**Key Cypher** (all against `system` DB):
- User: `CREATE USER`, `ALTER USER` (compound, REMOVE before SET), `DROP USER IF EXISTS`, `SHOW USERS WITH AUTH`, `GRANT/REVOKE ROLE`.
- Role: `CREATE ROLE [AS COPY OF]`, `DROP ROLE IF EXISTS`, `SHOW ROLES`, `SHOW ROLE <r> PRIVILEGES AS COMMANDS YIELD command, immutable`.
- Privileges: `GRANT/DENY/REVOKE ... ON ... TO/FROM ...`. REVOKEs are derived textually, not user-supplied.

## Key Implementation Patterns

- **Resource Version Conflict**: always wrap with `retry.RetryOnConflict(retry.DefaultRetry, ...)` — required for Neo4j 2025.01.0 cluster formation.
- **Template Comparison**: use `sts.UID != ""` to check if a StatefulSet exists, NOT `sts.ResourceVersion != ""` (ResourceVersion is populated even for new resources).
- **Split-Brain Detection**: `internal/controller/splitbrain_detector.go` connects to each pod, compares cluster views, auto-restarts orphans.
  ```bash
  kubectl get events --field-selector reason=SplitBrainDetected -A
  kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager | grep -i splitbrain
  ```
- **Edition field removed**: no `edition: enterprise` in CRDs. Operator always assumes enterprise; Neo4j client checks actual edition via `CALL dbms.components()`.
- **Structured Events**: constants from `internal/controller/events.go`. Use `corev1.EventTypeNormal` / `corev1.EventTypeWarning`, never raw strings.

## Regression Prevention Checklist

Rules already covered above in narrative sections are pointer-form here. Detailed rules below are NOT duplicated elsewhere — read them in full.

**Cross-references to narrative rules** (see linked section for the full rule):
- Enterprise images / No webhooks / Discovery V2_ONLY → *Project Overview*
- Pod naming `<cluster>-server-*` / `serverRoles` validation → *Architecture*
- Test cleanup / 300s timeouts / resource minimums → *Testing*
- TLS scheme `bolt+s://` (TLS on) / `bolt://` (off); Bolt TLS REQUIRED — *Deployment Configuration*
- CRD separation / `NEO4J_AUTH` for standalone → *Neo4jDatabase CRD*
- `NEO4J_PLUGINS` live-patch via `MergeNeo4jPluginList`; APOC env vars (cluster) vs ConfigMap (standalone); `envVarsEqual` subset + annotation-tracked removal → *Plugin Support*
- Fleet two-phase reconciliation → *Aura Fleet Management*
- Diagnostics non-fatal; `SetNamedCondition` for non-`Ready` conditions; `system` DB excluded → *Live Cluster Diagnostics*
- Default database topology / `initial.*` semantics → *Default Database Behavior*
- Privileges on `Neo4jRole` only / PUBLIC implicit / built-in role guard → *User/Role/RoleBinding CRDs*
- Resource version retry / `UID != ""` template check / event-reason constants → *Key Implementation Patterns*

**Standalone-specific:**
1. **`status.phase="Ready"`** required before database ops.
2. **Backup `--to-path`** syntax for Neo4j 5.26+.
3. **`ObservedGeneration`**: set `status.observedGeneration = latest.Generation` on every status update (both controllers).
4. **Name length validation**: cluster ≤ 56 chars (DNS label 63 minus `-server`); standalone ≤ 63; database ≤ 65 and must match `^[a-zA-Z][a-zA-Z0-9.\-]*$`.
5. **Standalone `UpgradeStrategy`**: pre-upgrade health check via `VerifyConnectivity`; `autoPauseOnFailure` blocks upgrade on failure; STS update strategy comes from spec.
6. **Standalone health probes**: readiness/liveness/startup via `/conf/health.sh` (process + HTTP 7474). ConfigMap includes `health.sh` alongside `neo4j.conf` with `DefaultMode: 0755`.
7. **Deprecated config keys**: validator warns on `dbms.logs.query.enabled` (use `db.logs.query.enabled`); always use `db.*` namespace for Neo4j 5.x+.
8. **Storage expansion**: orphan-delete STS (not regular delete); compare spec vs actual PVC sizes (not old vs new spec); `retry.RetryOnConflict` on PVC patches; validate `allowVolumeExpansion` before patching; never shrink PVCs.

**TLS / Bolt client:**
9. **TLS CA auto-discovery**: `buildTLSConfig()` in `internal/neo4j/client.go` loads CA from cert-manager Secret (`{name}-tls-secret`) automatically. `TrustedCASecret` is an override; `InsecureSkipVerify` is fallback only.
10. **All client functions must handle TLS**: `NewClientForEnterprise`, `NewClientForEnterpriseStandalone`, AND `NewClientForPod` all call `buildTLSConfig()`. Split-brain detector uses dynamic `bolt+s://` scheme.
11. **Operator's outbound Bolt URI uses the routing scheme**: `buildConnectionURIFor{Enterprise,Standalone}` MUST emit `neo4j://` / `neo4j+s://`, NOT `bolt://` / `bolt+s://`. Cluster admin commands run on the leader; the Go driver only honors `AccessMode: AccessModeWrite` under the routing scheme. Under `bolt://` the access mode is silently ignored — connections land wherever the `{cluster}-client` ClusterIP steered them, producing `Neo.ClientError.Cluster.NotALeader` on ~N-1/N reconciles. The only legitimate `bolt://` consumer is `splitbrain_detector.go`, which bypasses routing on purpose. `internal/neo4j/uri_test.go` locks the scheme.
12. **Tight Bolt driver timeouts**: `ConnectionAcquisitionTimeout=10s`, `SocketConnectTimeout=5s`, `MaxTransactionRetryTime=15s`. Under the routing scheme these gate routing-table fetch retries against an unreachable cluster; bumping to 30s+ stalls the reconcile queue behind hung calls.
13. **TLS Secret volume `DefaultMode=0440`** (owner+group read). Neo4j runs UID/GID 7474 with `FSGroup=7474`, so owner=group=Neo4j. `0400` would lock the JVM out (file owner is `root` in projected Secret volumes). Pinned by `TestBuildStatefulSet_TLSVolumeDefaultMode0440`.

**Users / Roles / Privileges:**
14. **`GetUserRoles` in `internal/neo4j/client.go` is buggy**: it queries `SHOW USER PRIVILEGES YIELD role`, returning one row per privilege. Use `Client.ListUserRoles` or `Client.ShowUser`.
15. **Password rotation via Secret hash**: `Neo4jUser` stores SHA-256 of the password Secret value in `status.passwordSecretHash`; rotation detected on hash change. Password is never persisted in CR fields. Skip `SET PASSWORD` when only `externalAuth` is configured.
16. **`ALTER USER` clause ordering**: REMOVE clauses MUST precede SET clauses on a single statement. `AlterUserOptions` builder honors this — never hand-roll ALTER USER strings.
17. **Missing custom roles**: do NOT fail reconcile. Set `PendingDependencies` condition and requeue; the user controller watches `Neo4jRole` and re-reconciles when the role lands.
18. **Same-namespace `clusterRef`** for users/roles: cross-namespace refs are not supported in v1; both CRDs namespace-scoped. Multi-tenant patterns must go through an opt-in `Neo4jClusterAccessGrant` CR — do not silently widen the lookup.
19. **Identifier quoting in Cypher**: all role/user names go through `escapeBackticks()` (doubles embedded backticks). Never `fmt.Sprintf` user-controlled names into Cypher; passwords and provider IDs go through driver parameters.
20. **Privilege drift via `SHOW ROLE PRIVILEGES AS COMMANDS`**: source of truth is `Neo4jRole.spec.privileges`. Controller canonicalises both sides (`CanonicalisePrivilegeStatement`), diffs as sets, derives REVOKEs via `DerivePrivilegeRevoke`. Immutable rows (`immutable=true`) are excluded from revokes and surfaced via `status.privilegeDrift`.
21. **Privilege statement validation**: each entry in `Neo4jRole.spec.privileges` MUST start with `GRANT` or `DENY` (`REVOKE` rejected — operator derives REVOKEs) and end with `TO <spec.name>`; otherwise the canonicaliser cannot derive the matching REVOKE on removal.
22. **`Neo4jRoleBinding` never creates or drops users**: it only manages role grants for users provisioned externally (SSO/LDAP first-login). Absent user → `UserNotFound` and waits.
23. **`Neo4jRoleBinding` overlap with `Neo4jUser`**: validator rejects bindings whose `clusterRef`+`username` match an existing `Neo4jUser` in the same namespace. Two CRs racing on role grants is a footgun.
24. **`Neo4jRoleBinding.spec.enforceExclusive`** defaults to false. Non-exclusive manages only `.spec.roles` (and `status.grantedRoles` for revoke-on-removal). Exclusive revokes any role on the user not in `.spec.roles`. Never flip the default.
25. **Diagnostics user/role lists bounded**: `maxDiagnosticUsers` / `maxDiagnosticRoles` cap slice length; full count in `UserCount` / `RoleCount`. Never remove caps without a pruning strategy.

**TrustStore / volumes:**
26. **Truststore init container seeds from JDK cacerts**: `BuildTrustStoreInitContainer` MUST start by copying `$JAVA_HOME/lib/security/cacerts` to `/truststore/truststore.jks` before importing user CAs. Skipping breaks trust in public CAs (Let's Encrypt, DigiCert) for any cluster opting into a custom truststore. The seed is what makes `spec.trustedCASecrets` purely additive.
27. **`spec.trustedCASecrets` Secret-name = keytool alias**: aliases must be unique in the JKS, so the validator rejects duplicate Secret names. Don't change alias derivation away from spec-statically-derivable.
28. **Legacy `spec.auth.trustStore` folds into `spec.trustedCASecrets`** via `CollectTrustedCASecrets`. Never wire legacy directly into resources, or both paths produce duplicate volumes/init containers and the JKS build fails with duplicate-alias.
29. **`spec.extraVolumeMounts` reserved paths**: validator rejects mounts at `/data`, `/logs`, `/conf`, `/ssl`, `/plugins`, `/truststore`, `/truststore-ca`, `/var/lib/neo4j` and its standard subdirectories.

**Auth / authrule / OIDC:**
30. **AUTH RULE Cypher requires `CYPHER 25` prefix**: every statement in `internal/neo4j/auth_rules.go` (SHOW/CREATE OR REPLACE/ALTER/DROP/GRANT/REVOKE) prepends `cypher25Prefix`. Neo4j 2026.x defaults system DB to Cypher 5; without the prefix the syntax fails with `42I06: Invalid input 'AUTH'`. Keep the prefix even when the default flips.
31. **`oidc-`-prefixed provider name in ABAC config**: `dbms.security.abac.authorization_providers` values must also appear in `dbms.security.authorization_providers`. Neo4j uses the `oidc-<name>` form there for OIDC providers — abac must use the prefixed form too (e.g. `oidc-test-oidc`, NOT `test-oidc`).
32. **Authrule controller in `--controllers` default list**: `cmd/main.go`'s dev-mode default for `--controllers` MUST include `authrule`. Production (`setupProductionControllers`) wires unconditionally — this only bites in dev.
33. **LDAP `useStartTLS` defaults to true for plain `ldap://` hosts**: when `ldap.UseStartTLS` is nil AND host starts with `ldap://`, emit `dbms.security.ldap.use_starttls=true`. `ldaps://` hosts skip (already TLS). Explicit `useStartTLS: false` is honored for dev/mock. Pinned across six cases in `TestBuildAuthConfig_LDAP_UseStartTLSDefault`.

**Network / metrics / audit:**
34. **NetworkPolicy peer-rule ports** must mirror cluster pod ContainerPorts: `BuildNetworkPolicyForEnterprise` peer rule covers `6000/7000/7688/7689`. Adding a new intra-cluster port to the StatefulSet without adding it here silently breaks pod-to-pod traffic on enforcing CNIs (Calico/Cilium/Antrea/Weave) — invisible on flannel. Pinned by `TestBuildNetworkPolicyForEnterprise_PeerPortsRestrictedToCluster`.
35. **NetworkPolicy public rule MUST include `2004` (Prometheus)**: K8s NetworkPolicy isolates the pod once any rule selects it. Port 2004 belongs in the public-ingress rule alongside 7474/7473/7687 so `networkPolicy.enabled=true` + `monitoring.enabled=true` doesn't silently break Prometheus scrape. Pinned by `TestBuildNetworkPolicyForEnterprise_PublicPortsOpen`.
36. **`BuildNetworkPolicy*` returns nil when disabled**: `if Spec.NetworkPolicy == nil || !Spec.NetworkPolicy.Enabled { return nil }`. Reconcilers short-circuit on nil. Standalone reconciler additionally uses `reflect.DeepEqual` on Spec to skip resourceVersion churn.
37. **Metrics JMX + CSV disabled unconditionally**: `server.metrics.jmx.enabled=false` and `server.metrics.csv.enabled=false` emitted in `BuildConfigMapForEnterprise` AND the standalone configLines builder regardless of `monitoring.enabled`. Neo4j defaults both ON — JMX is unauthenticated management surface, CSV writes pod-ephemeral files. Kill-switches MUST be outside the monitoring branch; pinned by `TestBuildConfigMapForEnterprise_MetricsHardening`. `BuildMonitoringConfig` itself no longer emits these keys (guarded by `TestBuildMonitoringConfig`).
38. **`spec.audit` emission order**: `BuildAuditConfig` runs AFTER `BuildMonitoringConfig` in rendered conf — both touch `db.logs.query.obfuscate_literals`, last-write-wins (Neo4j `strict_validation.enabled=false`) gives audit priority. User `spec.config` appends last and wins over both. Pinned by `TestBuildAuditConfig_PrecedenceOverMonitoring`. No `dbms.security.audit.*` keys are emitted — those are 4.x and removed; "audit logging" in 5.x/2025.x is `security.log` (`dbms.security.*`) + `query.log` (`db.logs.query.*`).
39. **`spec.audit.Enabled` is a hint, not a stomping default**: when `Enabled=true` AND `ObfuscateQueryLiterals` is nil, operator emits `obfuscate_literals=true`. Explicit values (true OR false) win. Exactly ONE `obfuscate_literals` line is emitted. Pinned by `TestBuildAuditConfig_ExplicitObfuscateFalseDespiteEnabled`.

**Backup / restore:**
40. **Backup per-run subfolder = Job name**: `--to-path` is `<base>/${BACKUP_RUN_ID}`. `BACKUP_RUN_ID` set via downward API (`metadata.labels['batch.kubernetes.io/job-name']`); operator records the same value in `BackupRun.BackupsPath`. One-shot Job name = `<backup>-backup`; CronJob child Job name = `<cronjob>-<unix-seconds>`. Pinned by `TestBackupRunIDEnvVar` + `TestJobToBackupRun`.
41. **CronJob backup defaults are load-bearing**: `ConcurrencyPolicy=Forbid`, `StartingDeadlineSeconds=60`, `TTLSecondsAfterFinished=1800`, `SuccessfulJobsHistoryLimit=10`, `FailedJobsHistoryLimit=3` — give `reconcileScheduledHistory` a 30-min window before K8s GCs the Jobs. Don't relax without cause.
42. **`source.type: backup` resolved upstream via `resolveRestoreSource`**: `createRestoreJob` calls it once, swaps `Spec.Source` onto a shallow restore copy, threads the resolved view through every builder. `buildRestoreCommand`'s `case "backup":` is dead-code with a defensive `internal:` error. The hardcoded `/backup/<backup-ref>` over EmptyDir is GONE — never reintroduce.
43. **`errBackupNotReady` → `StatusPending`, not `StatusFailed`**: when `Neo4jBackup.status.history` has no Succeeded run, `resolveBackupRef` wraps `errBackupNotReady` via `fmt.Errorf %w`. `startRestore` uses `errors.Is` and routes to Pending+requeue (Pending is NOT in the "previously failed, don't retry" guard set). Missing-CR errors stay terminal `Failed`. Pinned by `TestResolveRestoreSource_BackupRefNoSucceededRun_IsTransient` + `..._BackupRefMissingCR_IsPermanent`.
44. **Restore `--from-path` resolves to a FILE via shell substitution**: `buildLocalRestoreFilePath` emits `$(ls '/backup/<run>'/'<dbname>'-*.backup | head -1)` so neo4j-admin 5.26+ gets a file path (the only form it accepts). Operator doesn't know the timestamp at reconcile; the shell resolves at Pod start. **BOTH path AND database name MUST go through `shellQuote()`** — they come from user-controlled `spec.source.backupPath` and `spec.databaseName`; unquoted values like `foo; rm -rf /data #` let the shell escape the `ls` and execute arbitrary commands in the restore Pod (mounts `/data` RW, carries `NEO4J_ADMIN_PASSWORD`). Pinned by `TestResolveLocalPVCFromPath_BackupPathShellInjectionGuard` + `_NestedCommandSubstitutionGuard` + `_EmbeddedSingleQuoteGuard`. Cloud URIs (`s3://`, `gs://`, `azb://`) skip — native cloud readers handle per-file selection. Never pass the directory; never substitute the timestamp in Go; never drop quoting.
45. **Restore `--temp-path=/tmp/restore-tmp` is the default for PVC sources**: backup PVC is mounted ReadOnly (we never mutate user backups), so neo4j-admin can't extract in-place. Default emits `--temp-path=/tmp/restore-tmp` (Pod tmpfs) plus a `rm -rf && mkdir -p` prelude (neo4j-admin requires empty dir). Explicit `Options.TempStorage` / `Options.TempPath` win. Without the prelude/default the restore fails with `FileSystemException: Read-only file system`.
46. **`AlreadyExists` is non-fatal on restore Job creation; `startCluster` is idempotent on missing annotation**: two reconciles race during the stopCluster cycle (10s scale-down delay queues a fresh reconcile via watches before the original finishes). Job creation treats `AlreadyExists` as "another reconcile got there first" and re-fetches. `startCluster` treats a missing `neo4j.neo4j.com/original-replicas` annotation as "first reconcile already deleted it" and returns nil. Reverting either re-introduces the regression where successful Job/scale-up flips restore to `Failed` and the guard pins it terminal.
47. **Post-restore re-seed via `dbms.[cluster.]recreateDatabase`**: restore Job writes only to `data-{cluster}-server-0`'s PVC, so on multi-server clusters the post-restart primary placement is non-deterministic. `recreateRestoredDatabaseOnCluster` (in `neo4jrestore_controller.go`) calls the recreate procedure with **server-0 as the explicit seed** (resolved by matching `cluster.Name + "-server-0"` against `SHOW SERVERS YIELD address` — the `name` column is unreliable). Skipped for standalone / `Topology.Servers < 2` and for Neo4j versions lacking the procedure (pre-5.24 SemVer / pre-2025.02 CalVer). Procedure name from `version.RecreateDatabaseProcedure()`: `dbms.cluster.recreateDatabase` (5.24–2025.03) → `dbms.recreateDatabase` (2025.04+, since `cluster.*` form was deprecated in 2025.04). Non-fatal — restore completes if procedure call fails, but emits a Warning event `DatabaseCreateFailed`. Removing this step regresses the ~30% test flake where a stale-data server wins consensus and silently overwrites restored data.

## Generated artifacts

Several files in this repo are generated, not hand-written. Editing them directly is wasted work — the next sync overwrites your changes. **Never hand-edit** — each generated file carries a `# This file is GENERATED. DO NOT EDIT.` header and `check-drift` will revert tampering.

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
| `charts/neo4j-operator/Chart.yaml` (`artifacthub.io/crds` annotation) | CRD bases + curated descriptions in `scripts/helm-sync-artifacthub-crds.sh` | `make helm-sync-artifacthub-crds` |
| `bundle/manifests/*` and `bundle/metadata/*` (OperatorHub) | `config/manifests/bases/*.csv.yaml` + everything above | `make bundle` |

Umbrella targets:
- **`make sync-all`** — every regeneration step (no bundle).
- **`make ship-prep`** — `sync-all` + `bundle` + `helm-lint` + `check-csv-coverage`. Run before tagging a release.

CI gate:
- **`make check-drift`** — runs `sync-all` + `bundle`, then `git diff --exit-code`. Fails if any committed file is stale. `make bundle` pins the CSV's `createdAt:` to a stable placeholder so concurrent PRs don't conflict; release flow stamps the real value via `make bundle-release` before publishing.

**`scripts/helm-sync-artifacthub-crds.sh` requires a description per CRD**: when adding a CRD, also add a `case "$kind" in ... esac` row. The script exits non-zero if a CRD has no mapped description.

## Reports

All reports in `/reports/` named `YYYY-MM-DD-descriptive-name.md`.

**Key reports:**
- `/reports/2025-08-19-server-based-architecture-implementation.md` — server-based architecture
- `/reports/2025-08-05-neo4j-2025.01.0-enterprise-cluster-analysis.md` — Neo4j 2025.x compatibility
- `/reports/2025-08-08-seed-uri-and-server-architecture-release-notes.md` — Seed URI feature

# important-instruction-reminders
Do what has been asked; nothing more, nothing less.
NEVER create files unless they're absolutely necessary for achieving your goal.
ALWAYS prefer editing an existing file to creating a new one.
NEVER proactively create documentation files (*.md) or README files. Only create documentation files if explicitly requested by the User.
