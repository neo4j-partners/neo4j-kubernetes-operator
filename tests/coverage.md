# E2E coverage

What each suite asserts today, mapped to functional requirements (`NEO-*` / `OP-*`) and AC groups.
See [design.md](design.md) for how suites are built and [contribute.md](contribute.md) to run them.

Suites are named by family — `workload-*` (topology), `feature-*` (topology-agnostic domains,
run on the cheapest topology), and `operator-*` (operator behavior, not the workload).

| Suite | File | Description |
|-------|------|-------------|
| `workload-standalone` | [suites/workload-standalone.yaml](suites/workload-standalone.yaml) | Positive Standalone (happy path / matrix) |
| `workload-cluster` | [suites/workload-cluster.yaml](suites/workload-cluster.yaml) | Cluster mode — members created, cluster forms, routing works (1-primary lab + 3-primary HA) |
| `workload-scale` | [suites/workload-scale.yaml](suites/workload-scale.yaml) | Secondary pool scaled out and back in, with `ENABLE SERVER` / drain verified through Neo4j |
| `feature-connectivity` | [suites/feature-connectivity.yaml](suites/feature-connectivity.yaml) | Boots Neo4j (no TLS) and probes connectors from the pod and a client pod |
| `feature-config` | [suites/feature-config.yaml](suites/feature-config.yaml) | `spec.config` passthrough (AC-NEO-CONFIG-001) + invalid-setting startup error (AC-NEO-CONFIG-002) + live config change via controlled restart (NEO-2-010) |
| `feature-credentials` | [suites/feature-credentials.yaml](suites/feature-credentials.yaml) | Generated password vs `passwordSecretRef`, each verified with a real bolt query |
| `feature-storage` | [suites/feature-storage.yaml](suites/feature-storage.yaml) | `spec.storage` data modes, Share logs/metrics, additionalMounts, and invalid-storage failures |
| `feature-uninstall` | [suites/feature-uninstall.yaml](suites/feature-uninstall.yaml) | Deleting the CR preserves the data PVC by default (NEO-2-018) |
| `feature-plugins` | _(planned — no suite file yet)_ | Plugin runtime — APOC procedures, GDS, and Bloom available on assigned pools (BDR-004) |
| `operator-admission` | [suites/operator-admission.yaml](suites/operator-admission.yaml) | Admission rejections + one happy case |
| `operator-scope` | [suites/operator-scope.yaml](suites/operator-scope.yaml) | Namespace-scoped operator ignores CRs outside WATCH_NAMESPACE + namespaced RBAC |

## Coverage by suite

Legend: `[x]` implemented & asserted · `[ ]` not covered yet, or expected-fail pending a feature.

### `workload-standalone` — NEO-1-001
- [x] Deploy single-member Standalone and reach `Ready` — NEO-2-001-MODE-01 · AC-NEO-STANDALONE / AC-NEO-INSTALL
- [x] Enterprise edition + accepted license (fixture) — NEO-2-001-EDT-01 / NEO-2-001-LIC-01 · AC-NEO-LICENSE
- [x] Continuous reconcile to desired state — OP-1-002 · AC-OP-RECONCILE
- [x] Basic status condition `Ready` surfaced — OP-2-003-STATUS-01 · AC-OP-STATUS
- [x] Reconcile-combination matrix (`E2E_PROFILE=matrix`, operator installed once)
- [ ] Default startup/readiness/liveness probes present on the pod with expected config — NEO-2-009 / NEO-3-009-PROBE-01 · AC-NEO-PROBES (readiness is already validated implicitly by every `Ready` wait; only an explicit render check of the 3 probes is missing — one topology-agnostic check suffices)

### `workload-cluster` — NEO-1-002
- [x] Deploy 1-primary lab topology: Cluster mode renders, boots, forms — AC-NEO-CLUSTER-001
- [x] Deploy 3-primary HA topology: genuine quorum formation — NEO-2-002-MODE-01 · AC-NEO-CLUSTER-002
- [x] Members created as pool StatefulSets (`<cr>-primary-N`) — NEO-2-002-CSZ-01
- [x] Cluster forms (`SHOW SERVERS`: Enabled + Available) — AC-NEO-CLUSTER-002
- [x] Routing works through the client Service (`neo4j://`) — AC-NEO-CLUSTER-003
- [ ] Cluster TLS material (`spec.trust`) — NEO-3-005-TLS-03 · AC-NEO-TLS (no TLS case yet)
- [ ] Rolling restart of members one-by-one on config change — NEO-3-010-RSTR-02
- Scale out/in after deploy — NEO-2-011 · AC-NEO-SCALE: see `workload-scale`

### `workload-scale` — NEO-2-011

Scales a **secondary** pool on a live cluster (3 primaries + `read`), `1 → 2 → 1`, in a single
case. Secondary pools are the supported scale unit per BDR-009.

- [x] Scale out a pool after deploy (`topology.secondaries.read.members`) — NEO-3-011-CSZ-01 · AC-NEO-SCALE-001
- [x] Added server auto-enabled: Neo4j reports the new member `Enabled`+`Available` in `SHOW SERVERS`, proving the operator ran `ENABLE SERVER` rather than only resizing the StatefulSet — NEO-3-011-SRV-01 · AC-NEO-SCALE-002
- [x] Scale in: the removed member is drained and the cluster stays formed — AC-NEO-SCALE-003
- [x] `ClusterFormed` stays `True` across both topology changes
- [ ] Scale the **primary** pool (1 → N) — NEO-3-011-CSZ-01 (primary half). Not covered: on
  `main@ebb9fc0` this neither succeeds nor is cleanly refused. The StatefulSet grows, all
  servers are enabled and the *system* database reaches 3 primaries
  (`requestedPrimariesCount=3, currentPrimariesCount=3`), but the `neo4j` database never comes
  back online — stuck `starting`/`unknown`, `statusMessage="Server is unavailable"`, still
  broken after 12+ min. `ClusterFormed=UnsupportedSystemScaleUp` appears only **transiently**
  (~30s) before the operator moves on to `EnablingServer`, so a naive assert on that reason
  passes while the cluster is actually broken. Add a case once the behaviour is settled.
- [ ] Scale a primary pool **in** — same reason as above

### `feature-connectivity` — NEO-2-007
- [x] Bolt (7687) reachable from the Neo4j pod — NEO-3-007-PRT-03 · AC-NEO-NETWORKING-PORTS-BOLT
- [x] HTTP (7474) reachable — NEO-3-007-PRT-01 · AC-NEO-NETWORKING-PORTS-HTTP
- [x] HTTP+Bolt exposed, HTTPS disabled — NEO-3-007-PCMB-03 · AC-NEO-NETWORKING-PORTS-HTTP-BOLT
- [x] Reachable via client ClusterIP Service from an external pod — NEO-3-007-SVC-01 · AC-NEO-NETWORKING-CLUSTERIP
- [x] Single-cluster only (multiCluster disabled) — NEO-3-007-MULTI-01

### `feature-config` — NEO-2-003 / NEO-2-010
- [x] Valid `spec.config.neo4j` effective at runtime (bolt `SHOW SETTINGS`) — NEO-3-003-CFG-01 · AC-NEO-CONFIG-001
- [x] Unknown setting admitted but rejected by Neo4j at startup — AC-NEO-CONFIG-002
- [x] JVM `additionalArguments` effective at runtime in `server.jvm.additional` (bolt `SHOW SETTINGS`) — NEO-3-003-JVM-02
- [x] APOC `apoc.*` config rendered into `<cr>-apoc-config` (`apoc.conf`) — NEO-3-003-APOC-01 · AC-NEO-APOC-001
- [x] Live `spec.config` change applied end-to-end — render (ConfigMap) + rollout (STS template bump = controlled restart) + runtime (bolt `SHOW SETTINGS` on the restarted server) — NEO-3-010-RSTR-01 · AC-NEO-CONFIG-CHANGE
- [ ] JVM `useDefaults: true` prepends Neo4j default JVM args into `server.jvm.additional` — NEO-3-003-JVM-01 (test **postponed**: render ignores `useDefaults` today, and the assert depends on how defaults are sourced — vendored `.conf` vs hardcoded list vs image; see the jvm.useDefaults implementation issue)
- [ ] APOC credentials mounted from secret — NEO-3-003-APOC-02 · AC-NEO-APOC-CREDS-001 (`pluginDefinitions.apoc.credentials`)
- [ ] Rolling restart of cluster members one-by-one on config change — NEO-3-010-RSTR-02 (cluster-specific, see `workload-cluster`)

### `feature-plugins` — plugins (BDR-004)

Runtime plugin behavior (procedures actually callable), distinct from `feature-config` which
checks `apoc.*` config renders into `apoc.conf` (SHOW SETTINGS does not expose APOC keys).
Needs Neo4j Ready + a bolt query.

- [ ] APOC assigned: `apoc.*` procedures callable at runtime (e.g. `RETURN apoc.version()`) — NEO-3-003-APOC-01
- [ ] GDS assigned: `gds.*` procedures available (e.g. `RETURN gds.version()`) — BDR-004 (no dedicated FR)
- [ ] Bloom assigned: Bloom server/license available on the workload — BDR-004 (no dedicated FR)
- [ ] Procedure allowlists injected into neo4j.conf (`dbms.security.procedures.unrestricted`/`allowlist`) for assigned plugins — BDR-004

### `feature-credentials` — NEO-2-004
- [x] Operator-generated password authenticates over bolt — NEO-3-004-CRED-01 · AC-NEO-SECRETS
- [x] Password from referenced Secret authenticates over bolt — NEO-3-004-CRED-02 · AC-NEO-SECRETS
- [ ] Image pull secret honored — NEO-3-004-IMG-01

### `feature-storage` — NEO-2-006
- [x] Dynamic data via existing StorageClass — NEO-3-006-PVC-02 · AC-NEO-STORAGE-DYNAMIC
- [x] Data volume role provisioned — NEO-3-006-VOL-01 · AC-NEO-STORAGE
- [x] Existing PVC by `claimName`
- [x] Raw `volumeClaimTemplate` provisioning
- [x] Ephemeral `emptyDir` (no PVC)
- [x] logs+metrics Share the data volume
- [x] `additionalMounts` mounted at their paths
- [ ] Non-existent StorageClass → time out and mark CR `Failed` (message mentions PVC) — expected-fail, pending storage-timeout feature
- [ ] Missing `claimName` PVC → time out and mark CR `Failed` — expected-fail
- [ ] `volumeClaimTemplate` bad StorageClass → time out and mark CR `Failed` — expected-fail

### `feature-uninstall` — NEO-2-018
- [x] CR delete preserves data PVC by default — OP-2-005-UNINST-01 · AC-NEO-UNINSTALL-PRESERVE
- [ ] Optional cleanup of services/jobs/PVCs on request — NEO-2-018 (optional path)
- [ ] Operator control-plane uninstall — OP-1-005 / OP-2-005-UNINST-02 (different scope)

### `operator-admission` — validation (ADR-001)
- [x] Reject CR without accepted license — NEO-2-001-LIC-01 · AC-NEO-LICENSE
- [x] Reject Cluster on Community edition — NEO-2-001-EDT-01 · AC-NEO-LICENSE (edition guard)
- [x] Accept a valid minimal Standalone CR (sanity)

### `operator-scope` — OP-1-001
- [x] RBAC is namespaced, least-privilege, no cluster-wide grant — OP-1-006 · AC-OP-RBAC / AC-OP-SCOPE-SINGLE-004
- [x] CR reconciled inside WATCH_NAMESPACE — OP-2-001-SCOPE-01 · AC-OP-SCOPE-SINGLE-002
- [x] CR outside WATCH_NAMESPACE ignored — AC-OP-SCOPE-SINGLE-003

## Cross-cutting (every suite, `setup` / `teardown`)

Asserted once per run, not per topology — so not duplicated into individual suites.

- [x] Operator installed via YAML manifests: CRD registered, Deployment rolled out, ≥1 ready replica — OP-1-001 / OP-2-001-PKG-01 · AC-OP-INSTALL (`operator/install/verify.sh`, run in every suite's `setup`)
- [ ] Operator installed via Helm chart, then deploys a **Standalone** workload to `Ready` — OP-2-001-PKG-02 · AC-OP-INSTALL / AC-PACKAGING-HELM (`charts/neo4j-operator`, `make helm-install`)
- [ ] Operator installed via Helm chart, then deploys a **Cluster** workload that forms — OP-2-001-PKG-02 · AC-OP-INSTALL / AC-PACKAGING-HELM (`charts/neo4j-operator`, `make helm-install`)
- [ ] Operator uninstall asserted (control-plane removed cleanly) — OP-1-005 / OP-2-005-UNINST-02 (`cleanup/operator` is best-effort teardown today, not asserted; see also `feature-uninstall`)
