# E2E coverage

What each suite asserts today, mapped to functional requirements (`NEO-*` / `OP-*`) and AC groups.
See [design.md](design.md) for how suites are built and [contribute.md](contribute.md) to run them.

Suites are named by family — `workload-*` (topology), `feature-*` (topology-agnostic domains,
run on the cheapest topology), and `operator-*` (operator behavior, not the workload).

| Suite | File | Description |
|-------|------|-------------|
| `workload-standalone` | [suites/workload-standalone.yaml](suites/workload-standalone.yaml) | Positive Standalone (happy path / matrix) |
| `workload-cluster` | [suites/workload-cluster.yaml](suites/workload-cluster.yaml) | Cluster mode — members created, cluster forms, default database allocated on the declared primaries, routing works (1-primary lab + 3-primary HA) |
| `feature-connectivity` | [suites/feature-connectivity.yaml](suites/feature-connectivity.yaml) | Boots Neo4j (no TLS) and probes connectors from the pod and a client pod |
| `feature-config` | [suites/feature-config.yaml](suites/feature-config.yaml) | `spec.config` passthrough (AC-NEO-CONFIG-001) + invalid-setting startup error (AC-NEO-CONFIG-002) + live config change via controlled restart (NEO-2-010) |
| `feature-credentials` | [suites/feature-credentials.yaml](suites/feature-credentials.yaml) | Generated password vs `passwordSecretRef`, each verified with a real bolt query |
| `feature-storage` | [suites/feature-storage.yaml](suites/feature-storage.yaml) | `spec.storage` data modes, Share logs/metrics, additionalMounts, and invalid-storage failures |
| `feature-uninstall` | [suites/feature-uninstall.yaml](suites/feature-uninstall.yaml) | Deleting the CR preserves the data PVC by default (NEO-2-018) |
| `feature-plugins` | _(planned — no suite file yet)_ | Plugin runtime — APOC procedures, GDS, and Bloom available on assigned pools (BDR-004) |
| `operator-admission` | [suites/operator-admission.yaml](suites/operator-admission.yaml) | Admission rejections + one happy case |
| `operator-scope` | [suites/operator-scope.yaml](suites/operator-scope.yaml) | Namespace-scoped operator ignores CRs outside WATCH_NAMESPACE + namespaced RBAC |

## Coverage by suite

Legend: `[x]` implemented & asserted · `[ ]` not covered yet.

### `operator-admission` — validation (ADR-001)
- [x] Reject CR without accepted license — NEO-2-001-LIC-01 · AC-NEO-LICENSE
- [x] Reject Cluster on Community edition — Community is an accepted edition, confined to Standalone — NEO-2-001-EDT-01 · EDT-001 (CEL)
- [ ] Boot a Community Standalone and reach `Ready` — the edition is accepted at admission and unit-tested at render level (image tag, absent licensing env), but no suite starts a Community server: every fixture pins `enterprise` and `tests/config/neo4j/base.sh` sets the edition globally
- [x] Reject `connectivity.multiCluster.enabled` in every topology mode — NEO-3-007-MULTI-01 (CEL)
- [x] Reject a Cluster with no admin Bolt path — neither `trust.certificates.bolt` nor `trust.insecureAdminConnection` — NEO-2-005 · TLS-011 (CEL)
- [x] Reject `minimumMembers: 1` on a multi-primary cluster (a multi-primary `system` database cannot bootstrap on one server) — NEO-2-002-CSZ-01 · TOPO-008 (CEL)
- [ ] Reject `minimumMembers > primaries.members` at create — TOPO-009 (webhook only; the harness runs the operator without webhooks, so this case cannot be asserted here)
- [x] Accept a valid minimal Standalone CR (sanity)

### `workload-standalone` — NEO-1-001
- [x] Deploy single-member Standalone and reach `Ready` — NEO-2-001-MODE-01 · AC-NEO-STANDALONE / AC-NEO-INSTALL
- [x] Enterprise edition + accepted license (fixture) — NEO-2-001-EDT-01 / NEO-2-001-LIC-01 · AC-NEO-LICENSE
- [x] Continuous reconcile to desired state — OP-1-002 · AC-OP-RECONCILE
- [x] Basic condition catalog surfaced — `Ready`, `Reconciling`, `Error`, `Installed`, each with the reason the status writer documents — OP-2-003-STATUS-01 · AC-OP-STATUS
- [x] Reconcile-combination matrix (`E2E_PROFILE=matrix`, operator installed once)
- [x] Default startup/readiness/liveness probes rendered on the pod: `tcpSocket` on the container's Bolt port, `failureThreshold` 1000/20/40, period 5s, timeout 10s — NEO-2-009 / NEO-3-009-PROBE-01 · AC-NEO-PROBES
- [ ] Existing image pull secret honored: pod pulls the Neo4j image from a protected registry using a referenced pull Secret — NEO-3-004-IMG-01 · AC-NEO-IMAGE (install-time registry auth, not a Neo4j password)

### `workload-cluster` — NEO-1-002
- [x] Deploy 1-primary lab topology: Cluster mode renders, boots, forms — AC-NEO-CLUSTER-001
- [x] Deploy 3-primary HA topology: genuine quorum formation — NEO-2-002-MODE-01 · AC-NEO-CLUSTER-002
- [x] Members created as pool StatefulSets (`<cr>-primary-N`) — NEO-2-002-CSZ-01
- [x] Cluster forms (`SHOW SERVERS`: Enabled + Available) — AC-NEO-CLUSTER-002
- [x] Default database allocated on `topology.defaultPrimariesCount` primaries (`SHOW DATABASES`: requested = current)
- [x] `defaultPrimariesCount` omitted → the documented default of 1 primary, on a 3-primary cluster
- [x] `defaultPrimariesCount` is a creation default, not a constraint: a database created wider than the field keeps its topology across reconcile passes, with no `DatabaseTopologyResized` Event — TOPO-006
- [x] Default database reachable via `neo4j://` from members that do not host it (direct `bolt://` may be refused)
- [x] Routing works through the client Service (`neo4j://`) — AC-NEO-CLUSTER-003
- [ ] Cluster TLS material (`spec.trust`) — NEO-3-005-TLS-03 · AC-NEO-TLS (no TLS case yet)
- [ ] cert-manager issued certificates: operator creates one `Certificate` per policy, `TLSReady` moves `CertificatePending` → `SecretsPresent`, cluster forms over the issued material — NEO-2-005 · AC-NEO-TLS (unit-tested only)
- [ ] Rolling restart of members one-by-one on config change — NEO-3-010-RSTR-02
- [x] Scale out then in after deploy (`topology.primaries.members` 3 → 5 → 3, one cluster) — NEO-2-011 / NEO-3-011-CSZ-01 · AC-NEO-SCALE
- [x] Added servers auto-enabled: operator runs `ENABLE SERVER` so every new ordinal is `Enabled` + `Available` in `SHOW SERVERS`, checked by pod name — NEO-3-011-SRV-01 · AC-NEO-SCALE
- [x] Scaling leaves the surviving members alone: identical pod UIDs, no container restarts and an unchanged pool config checksum on both halves, since the system bootstrap gate never follows `primaries.members` — derived when `minimumMembers` is unset, immutable when it is set
- [x] Scale-in drains Neo4j first: the StatefulSet is held until `status.drainOK` confirms the tail was deallocated and dropped — ADD-02
- [x] The tail is drained one member at a time, highest ordinal first (operator log order), and the drained ordinals are no longer `Enabled`
- [x] A database requesting more primaries than the scale-in target does not block the shrink; it is narrowed to the target count and stays online
- [x] The narrowing — the only topology rewrite the operator performs — is never silent: `DatabaseTopologyResized` Warning Event on the CR and an operator log entry, both naming the database and the counts before/after
- [x] A resize that would leave `defaultPrimariesCount` above `primaries.members` is refused at admission on update, and the running cluster is untouched
- [ ] Scale-in to a single primary refused (`ServersPendingDrain`/`UnsupportedSinglePrimary`) while a multi-primary database exists — now reachable at admission (a 3 → 1 patch is accepted when `defaultPrimariesCount` is unset), so the operator-side refusal can finally be asserted

### `feature-connectivity` — NEO-2-007
- [x] Bolt (7687) reachable from the Neo4j pod — NEO-3-007-PRT-03 · AC-NEO-NETWORKING-PORTS-BOLT
- [x] HTTP (7474) reachable — NEO-3-007-PRT-01 · AC-NEO-NETWORKING-PORTS-HTTP
- [x] HTTP+Bolt exposed, HTTPS disabled — NEO-3-007-PCMB-03 · AC-NEO-NETWORKING-PORTS-HTTP-BOLT
- [x] Reachable via client ClusterIP Service from an external pod — NEO-3-007-SVC-01 · AC-NEO-NETWORKING-CLUSTERIP
- [x] Single-cluster only — `multiCluster.enabled` refused at admission, see `operator-admission` — NEO-3-007-MULTI-01

### `feature-config` — NEO-2-003 / NEO-2-010
- [x] Valid `spec.config.neo4j` effective at runtime (bolt `SHOW SETTINGS`) — NEO-3-003-CFG-01 · AC-NEO-CONFIG-001
- [x] Unknown setting admitted but rejected by Neo4j at startup — AC-NEO-CONFIG-002
- [x] JVM `additionalArguments` effective at runtime in `server.jvm.additional` (bolt `SHOW SETTINGS`) — NEO-3-003-JVM-02
- [x] APOC `apoc.*` config rendered into `<cr>-apoc-config` (`apoc.conf`) — NEO-3-003-APOC-01 · AC-NEO-APOC-001
- [x] Live `spec.config` change applied end-to-end — render (ConfigMap) + rollout (STS template bump = controlled restart) + runtime (bolt `SHOW SETTINGS` on the restarted server) — NEO-3-010-RSTR-01 · AC-NEO-CONFIG-CHANGE
- [x] JVM `useDefaults: true` renders the Neo4j default args into `server.jvm.additional` ahead of `additionalArguments` (ConfigMap) and they are effective at runtime (bolt `SHOW SETTINGS`); `useDefaults: false` leaves them out of the ConfigMap — NEO-3-003-JVM-01
- [x] JVM `additionalArguments` colliding with a default (same key) replace it in place — user value wins, one entry per key, position preserved — NEO-3-003-JVM-01
- [x] A dropped JVM argument is reported — `DuplicateEntry` Warning Event and operator log line naming the field, the value kept and the one dropped — NEO-3-003-JVM-01
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

### `feature-storage` — NEO-2-006
- [x] Dynamic data via existing StorageClass — NEO-3-006-PVC-02 · AC-NEO-STORAGE-DYNAMIC
- [x] Data volume role provisioned — NEO-3-006-VOL-01 · AC-NEO-STORAGE
- [x] Existing PVC by `claimName`
- [x] Raw `volumeClaimTemplate` provisioning
- [x] Ephemeral `emptyDir` (no PVC)
- [x] logs+metrics Share the data volume
- [x] `additionalMounts` mounted at their paths

A data PVC that cannot bind keeps the CR **Pending**: `StorageReady=False` with reason `PVCPending`
and a message naming the PVC and its `storageClassName`, never `phase=Failed` and never `Ready`. The
operator has no way to tell a misconfigured StorageClass from a slow provisioner, so it reports the
cause and waits instead of deciding the install has failed. The three cases below assert that
contract through `assert/storage-error` — the message lives on the condition only, as no Event is
emitted for it yet.

- [x] Non-existent StorageClass → stays Pending, `StorageReady=False/PVCPending`, message names the PVC — NEO-3-006-PVC-02
- [x] Missing `claimName` PVC → stays Pending, `StorageReady=False/PVCPending`
- [x] `volumeClaimTemplate` bad StorageClass → stays Pending, `StorageReady=False/PVCPending`

### `feature-uninstall` — NEO-2-018
- [x] CR delete preserves data PVC by default — OP-2-005-UNINST-01 · AC-NEO-UNINSTALL-PRESERVE
- [ ] Optional cleanup of services/jobs/PVCs on request — NEO-2-018 (optional path)
- [ ] Operator control-plane uninstall — OP-1-005 / OP-2-005-UNINST-02 (different scope)

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
