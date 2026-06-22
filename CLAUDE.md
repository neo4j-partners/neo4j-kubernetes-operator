# CLAUDE.md

Guidance to Claude Code (claude.ai/code) when working in this repository.

Last updated: 2026-06-13

This file is the **constitution + domain reference + index**. It holds the small set of
non-negotiable invariants, the domain-knowledge sections an agent needs to make a change,
and pointers to where the detailed, enforcement-tagged regression rules live. It is **not**
the rule dump — the 79-rule regression checklist now lives in `docs/knowledge/` (single home;
see [Regression rules](#regression-rules)). Do not re-add the inline numbered list here.

## INVARIANTS (NEVER violate)

These five are the hard project invariants. They are machine-checked by CI guard scripts and
mirrored verbatim in `docs/knowledge/invariants.md`. Any change that contradicts one of these
is wrong by definition — fix the change, not the invariant.

1. **NO WEBHOOKS**: no `ValidatingWebhookConfiguration` / `MutatingWebhookConfiguration` /
   `_webhook.go`. All validation lives in `internal/validation/`, called inline from the reconciler.
2. **KIND ONLY** for dev/test/CI. No minikube, k3s, etc.
3. **ENTERPRISE IMAGES ONLY**: `neo4j:5.26-enterprise` / `neo4j:2025.01.0-enterprise`. Never community.
4. **Discovery**: V2_ONLY mode exclusively.
5. **Server-based architecture**: single `{cluster-name}-server` StatefulSet with `replicas: N`;
   pods are `{cluster-name}-server-0…N-1`. Never use `primary-*` / `secondary-*` pod names.
   Backups are **Job-per-`Neo4jBackup`-CR ONLY**: no centralized `{cluster-name}-backup`
   StatefulSet, no `spec.backups` field, no `BuildBackupStatefulSet`, no standalone backup
   sidecar. The legacy backup pod/sidecar was **removed** — never reintroduce a long-running
   backup pod.

## Project Overview

Neo4j Enterprise Operator for Kubernetes — manages Neo4j Enterprise deployments (v5.26+) using the Kubebuilder framework. Built on controller-runtime, Go 1.26.

**Supported Neo4j versions**: 5.26.x (last semver LTS) and 2025.x.x+ (CalVer). No 5.27+ semver — Neo4j switched to CalVer after 5.26.

**Support policy** (see `docs/user_guide/version_support.md`): the operator supports **the current LTS line + the current CalVer feature line** (today: 5.26 + 2025.x/2026.x). A new LTS is *added* at its GA; the previous LTS is *dropped only at its Neo4j EOL* — never the moment a new LTS ships — so the operator is never stricter than the database. Steady state = 2 CI anchors; a vendor-overlap window may run 3. **CalVer = validated vs. best-effort**: each release pins ONE anchor CalVer in CI (e.g. 2026.04) and stands behind it; newer CalVers are *allowed* (forward-compatible via `IsCalver`) but best-effort — a CalVer shipped after an operator release can break it (operator emits strictly-validated config/Cypher), fixed in the next release. We don't claim to support every CalVer in a window; Neo4j itself supports only the latest.

**Deployment types:**
- **Neo4jEnterpriseCluster**: HA clusters (min 2 servers; self-organize into primary/secondary).
- **Neo4jEnterpriseStandalone**: Single-node (dev/test).

## Architecture

- CRDs: `Neo4jEnterpriseCluster`, `Neo4jEnterpriseStandalone`, `Neo4jDatabase`, `Neo4jShardedDatabase`, `Neo4jBackup`, `Neo4jRestore`, `Neo4jUser`, `Neo4jRole`, `Neo4jRoleBinding`, `Neo4jAuthRule`, `Neo4jPlugin`.
- Controllers: cluster & standalone controllers with controller-side validation. Neo4j client: Bolt protocol.
- **Directories:** `api/v1beta1/` (CRD types), `internal/controller/`, `internal/resources/` (K8s builders), `test/` (unit/integration/e2e).

**Server-based architecture**: single `{cluster-name}-server` StatefulSet with `replicas: N`. Pods are `{cluster-name}-server-0…N-1`. Never use `primary-*` / `secondary-*` pod names. Backups are Job-per-`Neo4jBackup`-CR exclusively (no persistent backup pod, no sidecars, no `spec.backups` field). The legacy `{cluster-name}-backup-0` StatefulSet and standalone backup sidecar were removed — never reintroduce a long-running backup pod. (Invariant 5.)

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

**Pre-release verification journey** (manual + LLM, complements the automated suites): `docs/developer_guide/release_verification.md` is the canonical matrix — what we verify, on standalone vs. cluster(3) vs. sharding(2026.04), at what size, and why. Run it from `main` before cutting a release via the `verify-journey` skill (`.claude/skills/verify-journey/`). Two durable rules: **one Enterprise deployment in the cluster at a time** (sequential phases, teardown between — concurrent JVMs wedge Bolt on a laptop) and **restore is walked on BOTH standalone (neo4j-admin path) and cluster (in-place Cypher path)**. When you add/change a capability, add its scenario to that doc in the same PR.

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

## Regression rules

Regression rules now live in `docs/knowledge/` as structured, enforcement-tagged entries — see
`docs/knowledge/invariants.md` (hard constraints), `docs/knowledge/backup-restore.md`, and
`docs/knowledge/operations.md`. **Do not re-add the inline numbered list here (single home).**

The previous CLAUDE.md carried these as an inline "Regression Prevention Checklist" (numbered
rules 1–79). They have been migrated; CLAUDE.md keeps only the invariants above and the domain
reference. When you discover a new invariant or regression, add it to the appropriate
`docs/knowledge/` file (with an enforcement tag), not back into this file.

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
| `charts/neo4j-operator/templates/metrics-rbac.yaml` | `config/rbac/metrics_{auth,reader}_role.yaml` | `make helm-sync-rbac` |
| `charts/neo4j-operator/Chart.yaml` (`artifacthub.io/crds`) | CRD bases + curated descriptions in `scripts/helm-sync-artifacthub-crds.sh` | `make helm-sync-artifacthub-crds` |
| `bundle/manifests/*` and `bundle/metadata/*` (OperatorHub) | `config/manifests/bases/*.csv.yaml` + everything above | `make bundle` |

Umbrella targets: **`make sync-all`** (every regeneration step, no bundle); **`make ship-prep`** (`sync-all` + `bundle` + `helm-lint` + `check-csv-coverage`, run before tagging a release).

CI gate: **`make check-drift`** runs `sync-all` + `bundle` then `git diff --exit-code`. `make bundle` pins the CSV's `createdAt:` to a stable placeholder so concurrent PRs don't conflict; release flow stamps the real value via `make bundle-release`.

**`scripts/helm-sync-artifacthub-crds.sh` requires a description per CRD**: when adding a CRD, add a `case "$kind" in ... esac` row or the script exits non-zero.
