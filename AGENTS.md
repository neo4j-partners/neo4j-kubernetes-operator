# AGENTS.md

## Quick Mission
- Neo4j Enterprise Operator (Kubebuilder/controller-runtime 0.21, Go 1.24) automates Neo4j Enterprise 5.26+/2025.x on Kubernetes.
- Status: **alpha**; expect churn. Follow `docs/` plus historical reports in `reports/`.
- Platform assumptions: Kubernetes ≥1.21, cert-manager ≥1.18 for TLS, and **Kind-only** for any cluster work.

## Non-Negotiables (read with `CLAUDE.md`)
1. **Kind only**: use `make dev-cluster`, `test-cluster`, `operator-setup`; never minikube/k3s.
2. **Enterprise images only** (`neo4j:<version>-enterprise`), discovery V2_ONLY enforced via `internal/neo4j/version.go`.
3. **Operator must run in-cluster**: never `make dev-run`/`hack/dev-run.sh` or host-mode runs.
4. **Server-based design**: single `{cluster}-server` StatefulSet with `topology.servers`; preserve `topology.serverModeConstraint/serverRoles` hints and centralized `{cluster}-backup` StatefulSet.
5. **Conflict-safe writes**: wrap creates/updates in `retry.RetryOnConflict`; when checking existing resources use UID (not ResourceVersion) for template comparison.
6. **Edition removed**: API assumes Enterprise; do not reintroduce the field or allow community images.
7. **Safety hooks**: keep split-brain detector (`internal/controller/splitbrain_detector.go`) wiring and event reason `SplitBrainDetected`; status `.phase` drives readiness checks.
8. **Plugin rules**: APOC via env vars; Bloom/GDS/GenAI and similar via ConfigMap with automatic security defaults and dependency resolution; respect cluster vs standalone naming/labeling in `plugin_controller`.
9. **Property sharding**: `Neo4jShardedDatabase` + related tests stay opt-in; requires ≥5 servers with 4–8Gi each—guard resource gates.
10. **Database & TLS**: `Neo4jDatabase` must work for cluster and standalone (ensure `NEO4J_AUTH` for standalone), respect seedURI/seedConfig, TLS automation for `spec.tls.mode=cert-manager`, and keep discovery/TLS parameter mapping for 5.x vs 2025.x.
11. **CRD scope separation**: Cluster/Standalone manage infra/config; Database manages database lifecycle only—no cross-CRD overrides.

## Architecture Anchors
- **CRDs (`api/v1alpha1`)**: Neo4jEnterpriseCluster, Neo4jEnterpriseStandalone, Neo4jDatabase, Neo4jPlugin, Neo4jBackup, Neo4jRestore, Neo4jShardedDatabase.
- **Controllers (`internal/controller/`)**:
  - Cluster/Standalone reconcilers build ConfigMaps/Services/StatefulSets, manage status phases, TLS, auth, placement, and cache/configmap managers.
  - Database controller auto-detects cluster vs standalone, uses appropriate Bolt client, and supports wait/ifNotExists/topology/seed flows.
  - Plugin controller manages env-vs-config installs, dependencies, and pod readiness per deployment type.
  - Backup controller runs centralized `{cluster}-backup` pods with request file drop; Restore controller handles PIT restores.
  - Sharded database controller enforces property-sharding readiness; Topology scheduler adds AZ spread/anti-affinity; Rolling upgrade orchestrator handles leader-aware upgrades and metrics.
  - Split-brain detector restarts orphans after multi-pod view comparison.
- **Validation (`internal/validation/`)**: auth, backup, cluster, database (cluster+standalone), image, memory, plugin, resource, security, shardeddatabase, storage, TLS, topology, upgrade validators; edition validator stubbed out.
- **Resources/Clients**: `internal/resources` builds Kubernetes objects (TLS policies, discovery ports, memory sizing); `internal/neo4j` wraps Bolt, version parsing (5.x vs 2025.x), and upgrade safety checks.

## Repository Atlas
| Area | Purpose |
| --- | --- |
| `cmd/main.go` | Manager entrypoint wiring controllers/webhooks. |
| `api/v1alpha1/` | CRD schemas listed above. |
| `internal/controller/` | Reconcilers, split-brain detector, topology scheduler, rolling upgrade, cache/configmap managers. |
| `internal/resources/` | Builders for StatefulSets/Services/ConfigMaps, memory sizing, TLS/discovery helpers. |
| `internal/neo4j/` | Bolt client + version helpers used by controllers/tests. |
| `internal/validation/` | Validation/recommendation logic per CRD. |
| `charts/neo4j-operator/` | Helm chart (README quick start). |
| `config/` | Kubebuilder manifests (CRDs, RBAC, samples, overlays). |
| `docs/` | User/developer guides, deployment/seed-URI/split-brain guides, quick reference, API reference. |
| `examples/` | Standalone, clusters, backup/restore, plugins, property sharding, E2E scenarios. |
| `test/` | Ginkgo integration suites, fixtures, helpers. |
| `scripts/` | Automation (`test-env.sh`, setup/cleanup, demos, RBAC helpers, verification). |
| `reports/` | Design history and implementation analyses. |

## Development Workflow
1. Prereqs: Go 1.24+, Docker, kubectl, Kind, make, git; verify `kind version`.
2. Dev cluster: `make dev-cluster` (Kind `neo4j-operator-dev`, installs cert-manager issuer). Test cluster via `make test-cluster`.
3. Codegen/hygiene: `make manifests generate`; then `make fmt vet lint` (use `rg`/`goimports`; ASCII only).
4. Build & load image: `make docker-build IMG=neo4j-operator:dev` then `kind load docker-image neo4j-operator:dev --name neo4j-operator-dev` (or test cluster). Never run operator out-of-cluster.
5. Deploy: `make deploy-dev`/`deploy-prod` (or `*-registry` for registry images); Helm chart under `charts/` if needed.
6. Utilities: `make operator-setup`, `operator-status`, `operator-logs`; cleanup via `make dev-cluster-clean`, `dev-cluster-reset`, `dev-destroy` (similar `test-*` targets). Demos via `make demo`, `demo-fast`, `demo-setup`.

## Testing Strategy
- **Unit**: `make test-unit` (envtest; skips integration dirs).
- **Integration** (`test/integration`, Ginkgo v2): `make test-integration` builds local image, loads into Kind `neo4j-operator-test`, deploys operator, runs suite (timeouts ~5m, CI extends to 10–20m). CI-friendly subsets: `make test-integration-ci`, heavy `*-ci-full`, full workflow `make test-ci-local` (logs in `logs/`).
- **Cleanup rule**: every spec must `AfterEach` delete CRs, drop finalizers, and call `cleanupCustomResourcesInNamespace()`—do not rely on suite cleanup.
- **Property sharding**: opt-in only (`test/integration/property_sharding_test.go`), requires ≥5 servers and 4–8Gi memory each.
- **Plugin tests**: clusters expect env var config, standalone expects ConfigMap content; keep sources consistent.
- **Scripts**: `scripts/test-env.sh` manages Kind clusters; `scripts/run-tests-clean.sh` wraps go test.

## Documentation & Examples
- User docs: `docs/user_guide` (install/config, external access, topology placement, property sharding, backup/restore, security, performance, monitoring, upgrades, troubleshooting).
- Developer docs: `docs/developer_guide/` (architecture, development setup, testing instructions, Makefile reference).
- References: `docs/quick-reference/operator-modes-cheat-sheet.md`, `docs/api_reference/` for CRDs, `docs/deployment-guide.md`, `docs/seed-uri-feature-guide.md`, `docs/split-brain-troubleshooting.md`.
- Examples: `examples/` for standalone/cluster mins, plugins, backup/restore, property sharding, E2E blueprints.

## Observability & Troubleshooting
- Logs: `kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager -f` or `make operator-logs`.
- Events: monitor split-brain via `kubectl get events --field-selector reason=SplitBrainDetected -A`.
- Inspect: `kubectl explain neo4jenterprisecluster.spec`, `kubectl describe neo4jenterprisecluster/<name>`; use `cypher-shell` in pods for cluster state.
- Debug aids: see `CLAUDE.md` for enabling debug logging, OOM checks (`kubectl describe pod`, `kubectl top pod`), and port-forward/seed URI guidance.

## Contribution Expectations
- Follow `CONTRIBUTING.md`; run `make fmt lint test-unit` (plus integration when relevant) before PRs; Kind required.
- Keep CRDs regenerated with `make manifests`; run `make generate` if API types change.
- Prefer Makefile/scripts over ad-hoc commands; do not reintroduce edition field or community images.
- Maintain docs/examples/API references when behavior changes; add design reports under `reports/` for architectural shifts.
- Preserve AfterEach cleanup in integration tests; respect usage of `retry.RetryOnConflict` and server-based topology helpers.
- Default to ASCII text; prefer `rg` for search. Avoid creating new docs unless explicitly requested.

## Where to Start
1. `README.md` for requirements and quick start.
2. `docs/developer_guide/architecture.md` for design (server architecture, centralized backup, controllers).
3. `internal/controller/neo4jenterprisecluster_controller.go`, `splitbrain_detector.go`, `rolling_upgrade.go`, `topology_scheduler.go` for reconciliation/safety flows.
4. `test/integration/integration_suite_test.go` for harness assumptions and cleanup expectations.
5. `CLAUDE.md` (and this file) for agent-specific guardrails before making changes.
