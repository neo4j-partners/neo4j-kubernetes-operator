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
- `api/v1alpha1/` - CRD definitions
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
- Both include cert-manager v1.18.5 with `ca-cluster-issuer`

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
TLS-enabled → `bolt+s://` scheme; TLS-disabled → `bolt://`.

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

**`envVarsEqual` is an intentional subset check**: verifies desired vars present in current but tolerates extras. Never revert to strict length+value equality or controllers will oscillate.

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

## Neo4jDatabase CRD

Works with both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone`. `DatabaseValidator` tries cluster lookup first, then standalone automatically.

**CRD Separation of Concerns** (strict, never violate):
- Cluster/Standalone CRDs own: infrastructure, server config, auth, TLS, plugins, backup, images
- Neo4jDatabase CRD owns: database name, topology, Cypher version, CREATE DATABASE options only
- ❌ Neo4jDatabase MUST NOT override cluster/server-level settings

**Standalone deployments require `NEO4J_AUTH` env var** for automatic password setup (critical for Neo4jDatabase support).

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
18. **`envVarsEqual` Subset**: Never revert to strict equality check
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
