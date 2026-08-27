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
| `feature-config` | [suites/feature-config.yaml](suites/feature-config.yaml) | `spec.config` passthrough (AC-NEO-CONFIG-001) + invalid-setting startup error (AC-NEO-CONFIG-002) + live config change via controlled restart, Standalone (RSTR-01) and 3-primary cluster rolled one-by-one with quorum held (RSTR-02) (NEO-2-010) |
| `feature-credentials` | [suites/feature-credentials.yaml](suites/feature-credentials.yaml) | Generated password vs `passwordSecretRef`, each verified with a real bolt query |
| `feature-tls` | [suites/feature-tls.yaml](suites/feature-tls.yaml) | TLS issued by cert-manager — operator issues one `Certificate` per policy against a self-signed CA Issuer, cluster forms and serves Bolt over TLS, plaintext Bolt refused |
| `feature-tls-byo` | [suites/feature-tls-byo.yaml](suites/feature-tls-byo.yaml) | TLS from Bring-Your-Own Secrets — Standalone bolt leaf supplied via labelled Secrets, operator mounts and verifies it (Ready is the SAN gate), Neo4j serves Bolt over TLS, plaintext Bolt refused |
| `feature-tls-byo-cluster` | [suites/feature-tls-byo-cluster.yaml](suites/feature-tls-byo-cluster.yaml) | Cluster BYO TLS — private CA signs shared bolt + cluster leaves with per-member SANs, members do mTLS (clientAuth Require), cluster forms and serves Bolt over TLS |
| `feature-storage` | [suites/feature-storage.yaml](suites/feature-storage.yaml) | `spec.storage` data modes, Share logs/metrics, additionalMounts, invalid-storage failures, growing a data volume (and the grow a StorageClass refuses), and the changes refused at admission because no StatefulSet could apply them |
| `feature-uninstall` | [suites/feature-uninstall.yaml](suites/feature-uninstall.yaml) | Deleting the CR preserves the data PVC by default (NEO-2-018) |
| `feature-plugins` | [suites/feature-plugins.yaml](suites/feature-plugins.yaml) | Plugin runtime — APOC/GDS procedures callable, generated allowlists effective, licence Secret mounting, per-plugin config, manual-import channel (BDR-004) |
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
- [x] TLS material via BYO Secret (`spec.trust` with `privateKey`/`publicCertificate`) — Standalone bolt (`feature-tls-byo`) and Cluster bolt+cluster mTLS (`feature-tls-byo-cluster`) — NEO-3-005-TLS-03 · AC-NEO-TLS
- [x] cert-manager issued certificates: operator creates one `Certificate` per policy, `TLSReady=SecretsPresent`, cluster forms and serves Bolt over TLS — NEO-2-005 · AC-NEO-TLS (see `feature-tls`)
- [x] Rolling restart of members one-by-one on config change: patching `spec.config` on a 3-primary cluster rolls the `<cr>-primary` pool with quorum held throughout (`readyReplicas` never below `members-1`), every member reports the new value via `SHOW SETTINGS`, no refusal reason appears on `ClusterFormed` during the roll and the cluster is formed again after it — NEO-3-010-RSTR-02 (asserted in `feature-config` case `config-change-restart-cluster`, which owns the per-case assert; `workload-cluster` runs a fixed assert list so it cannot)
- [x] Scale out then in after deploy (`topology.primaries.members` 3 → 5 → 3, one cluster) — NEO-2-011 / NEO-3-011-CSZ-01 · AC-NEO-SCALE
- [x] Added servers auto-enabled: operator runs `ENABLE SERVER` so every new ordinal is `Enabled` + `Available` in `SHOW SERVERS`, checked by pod name — NEO-3-011-SRV-01 · AC-NEO-SCALE
- [x] Scaling leaves the surviving members alone: identical pod UIDs, no container restarts and an unchanged pool config checksum on both halves, since the system bootstrap gate never follows `primaries.members` — derived when `minimumMembers` is unset, immutable when it is set
- [x] Scale-in drains Neo4j first: the StatefulSet is held until `status.drainOK` confirms the tail was deallocated and dropped — ADD-02
- [x] The tail is drained one member at a time, highest ordinal first (operator log order), and the drained ordinals are no longer `Enabled`
- [x] A database requesting more primaries than the scale-in target does not block the shrink; it is narrowed to the target count and stays online
- [x] The narrowing — the only topology rewrite the operator performs — is never silent: `DatabaseTopologyResized` Warning Event on the CR and an operator log entry, both naming the database and the counts before/after
- [x] A resize that would leave `defaultPrimariesCount` above `primaries.members` is refused at admission on update, and the running cluster is untouched
- [x] Scale a secondary (read) pool out then in (1 → 2 → 1) on a single-primary cluster — NEO-3-011-SRV-01 · AC-NEO-SCALE (case `scale-secondary`)
- [x] A departing secondary is released on what it still hosts (`SHOW SERVERS.hosting`), not on its state label: Neo4j can leave it `Deallocating` for good once only `system` is left, which used to hold the StatefulSet at its old size forever — ADR-007
- [x] Resizing a secondary pool never moves the primary: same pod UID, same restart count and an unchanged config checksum, because `initial.dbms.default_secondaries_count` is kept out of the checksum — on one primary a needless roll is a full outage
- [x] Growing `storage.volumes.data.dynamic.size` on a formed 3-primary cluster reaches **every** ordinal's claim from one CR patch, while the StatefulSet's `volumeClaimTemplate` keeps its original size — BDR-005 (case `storage-grow`; readiness read ordinal 0 alone before, so two members left behind went unnoticed)
- [x] A grow restarts no member at all — same pod UIDs and restart counts across the whole operation. Rolling one-by-one is the discipline for a PodTemplate change (RSTR-02); a size change never reaches the PodTemplate, so a roll would cost availability for nothing. This is the tripwire for an offline-resize path, which would need the bounce ordered and quorum-aware
- [ ] Scale-in to a single primary refused (`ServersPendingDrain`/`UnsupportedSinglePrimary`) while a multi-primary database exists — now reachable at admission (a 3 → 1 patch is accepted when `defaultPrimariesCount` is unset), so the operator-side refusal can finally be asserted

### `feature-connectivity` — NEO-2-007
- [x] Bolt (7687) reachable from the Neo4j pod — NEO-3-007-PRT-03 · AC-NEO-NETWORKING-PORTS-BOLT
- [x] HTTP (7474) reachable — NEO-3-007-PRT-01 · AC-NEO-NETWORKING-PORTS-HTTP
- [x] HTTP+Bolt exposed, HTTPS disabled — NEO-3-007-PCMB-03 · AC-NEO-NETWORKING-PORTS-HTTP-BOLT
- [x] Bolt only — `connectivity.service.expose: [bolt]` publishes only `tcp-bolt` on the client Service; HTTP/HTTPS are not exposed and Neo4j still reaches Ready over Bolt (exposure-only; Neo4j keeps listening on HTTP internally). `expose` must include `bolt` (CEL) since the operator manages Neo4j over the client-Service Bolt path — covered in `feature-ports` case `bolt-only` — NEO-3-007-PCMB-01 · AC-NEO-NETWORKING-PORTS-BOLT
- [x] Reachable via client ClusterIP Service from an external pod — NEO-3-007-SVC-01 · AC-NEO-NETWORKING-CLUSTERIP
- [x] Single-cluster only — `multiCluster.enabled` refused at admission, see `operator-admission` — NEO-3-007-MULTI-01

### `feature-config` — NEO-2-003 / NEO-2-010
- [x] Valid `spec.config.neo4j` effective at runtime (bolt `SHOW SETTINGS`) — NEO-3-003-CFG-01 · AC-NEO-CONFIG-001
- [x] Unknown setting admitted but rejected by Neo4j at startup — AC-NEO-CONFIG-002
- [x] Strict validation toggle toggles — with `server.config.strict_validation.enabled: "false"` the same unknown setting is downgraded to a warning, so the workload becomes Ready and `SHOW SETTINGS` reports the toggle value (mirror of the unknown-key case) — NEO-3-003-CFG-02 · AC-NEO-CONFIG-001
- [x] JVM `additionalArguments` effective at runtime in `server.jvm.additional` (bolt `SHOW SETTINGS`) — NEO-3-003-JVM-02
- [x] APOC `apoc.*` config rendered into `<cr>-apoc-config` (`apoc.conf`) — NEO-3-003-APOC-01 · AC-NEO-APOC-001
- [x] Live `spec.config` change applied end-to-end — render (ConfigMap) + rollout (STS template bump = controlled restart) + runtime (bolt `SHOW SETTINGS` on the restarted server) — NEO-3-010-RSTR-01 · AC-NEO-CONFIG-CHANGE
- [x] JVM `useDefaults: true` renders the Neo4j default args into `server.jvm.additional` ahead of `additionalArguments` (ConfigMap) and they are effective at runtime (bolt `SHOW SETTINGS`); `useDefaults: false` leaves them out of the ConfigMap — NEO-3-003-JVM-01
- [x] JVM `additionalArguments` colliding with a default (same key) replace it in place — user value wins, one entry per key, position preserved — NEO-3-003-JVM-01
- [x] A dropped JVM argument is reported — `DuplicateEntry` Warning Event and operator log line naming the field, the value kept and the one dropped — NEO-3-003-JVM-01
- [ ] APOC credentials mounted from secret — NEO-3-003-APOC-02 · AC-NEO-APOC-CREDS-001 (`pluginDefinitions.apoc.credentials`)
- [x] Rolling restart of cluster members one-by-one on config change — the same `neo4j.com/config-checksum` mechanism as Standalone, on a 3-replica pool: `updateRevision` bumps, quorum holds through the roll (`readyReplicas` >= `members-1`), all three primaries converge on the new `SHOW SETTINGS` value, and `ClusterFormed` returns to `True` without ever reporting a refusal (case `config-change-restart-cluster`) — NEO-3-010-RSTR-02

### `feature-plugins` — plugins (BDR-004)

Runtime plugin behavior (procedures actually callable), distinct from `feature-config` which
checks `apoc.*` config renders into `apoc.conf` (SHOW SETTINGS does not expose APOC keys).
Standalone only — every catalog plugin is legal in `spec.plugins`, so one topology covers all
three. The operator has no install logic: it sets `NEO4J_PLUGINS` and the image entrypoint
installs the JAR at container start. All three ship inside the Enterprise image (`apoc` in
`/var/lib/neo4j/labs`, `gds` and `bloom` in `/var/lib/neo4j/products`), so the suite is
hermetic — no outbound network required.
Licence Secrets take their content from the `LICENSE_GDS` / `LICENSE_BLOOM` repository secrets
when CI exports them, and a dummy otherwise; the acceptance checks are skipped in the dummy case
(local runs, fork PRs), the mount and config checks are not.

- [x] APOC assigned: `apoc.*` procedures callable at runtime — NEO-3-003-APOC-01
- [x] GDS assigned: `gds.*` procedures available, with no `licenseSecretRef` (Community runs licence-free) — BDR-004 (no dedicated FR)
- [x] GDS and Bloom assigned with a licence Secret each: both mounted at `/licenses/<id>`, and `pluginDefinitions.<id>.config` makes `gds.enterprise.license_file` / `dbms.bloom.license_file` effective on the running server — BDR-004
- [x] Both plugins accept the mounted licence at runtime (`gds.isLicensed()`, `bloom.checkLicenseCompliance()`) — asserted only when CI supplies real licence material, skipped on the dummy
- [ ] Licensed plugins with no `spec.config.neo4j` settings of their own — the image writes a plugin's defaults into `$NEO4J_HOME/conf/neo4j.conf` while the operator runs the server with `NEO4J_CONF=/config`, so none of them land: strict validation then rejects the plugin-namespaced `*.license_file` keys (the server refuses to start), `dbms.security.procedures.unrestricted=gds.*,bloom.*` is lost (plugin procedures touching internals stay sandboxed) and `server.unmanaged_extension_classes` is lost (Bloom is not served). The fixture and `docs/user-guide/03-neo4j/07-plugins.md` now set all three by hand, matching what Neo4j's own Helm docs tell users to do; closing this needs the operator to render the defaults itself
- [x] Procedure allowlist injected into neo4j.conf (`dbms.security.procedures.allowlist`) for assigned plugins, verified effective on the running server — BDR-004
- [x] `dbms.security.procedures.unrestricted` left empty — the operator allowlists plugin procedures but never removes them from the security sandbox; that needs an explicit `spec.config.neo4j` opt-in — NEO-024
- [x] Licence Secret without `neo4j.com/mountable-by-operator` refused at reconcile with `Error/SecretNotMountable`, a matching Warning Event, and no operands — NEO-005
- [x] `pluginDefinitions.<id>.config` merges into `neo4j.conf` (not a per-plugin file) and is effective at runtime — BDR-004
- [x] Manual import channel: an existing PVC mounted at `/plugins` with no `spec.plugins`, `NEO4J_PLUGINS` left unset, content required under the `plugins` subPath — BDR-004
- [x] Bloom server extension mounted and served — `GET /bloom/` answers `401` (extension mounted, request unauthenticated) rather than `404`, with `server.unmanaged_extension_classes` declared in `pluginDefinitions.bloom.config`; needs no licence material, so it runs on every platform — BDR-004
- [x] Imported JAR's procedures callable — the assert copies the image's APOC core jar onto the Existing claim at `/plugins`, restarts pod-0, and requires `apoc.*` procedures to be registered on the server that comes back — BDR-004

### `feature-credentials` — NEO-2-004
- [x] Operator-generated password authenticates over bolt — NEO-3-004-CRED-01 · AC-NEO-SECRETS
- [x] Password from referenced Secret authenticates over bolt — NEO-3-004-CRED-02 · AC-NEO-SECRETS

### `feature-tls` — NEO-2-005

Cluster TLS with certificates issued by cert-manager. Runs on the smallest real HA topology
(3 primaries) so cluster mTLS is genuinely exercised, not just a single-server handshake.
Installs cert-manager before the operator (dedicated `neo4j-tls-suite` pipeline) so the
operator watches `Certificate`s at boot. The fixture ships a self-signed CA `Issuer`, so the
suite is self-contained.

- [x] Operator issues one `Certificate` per policy (bolt, cluster) against the referenced Issuer; each reaches `Ready=True` — NEO-2-005 · AC-NEO-TLS
- [x] `TLSReady=True/SecretsPresent` — operator read usable key material from the Secrets cert-manager wrote (`observeTLSReady`) — NEO-2-005
- [x] Cluster forms over the issued material (`ClusterFormed=True` — the operator's admin dial speaks TLS to members) — NEO-2-005 · AC-NEO-CLUSTER-002
- [x] Neo4j serves Bolt over TLS: `SHOW SERVERS` via `bolt+ssc` returns every member Enabled+Available — NEO-2-005 · AC-NEO-TLS
- [x] TLS is enforced, not merely offered: a plaintext `bolt://` session is refused — NEO-2-005
- [x] Leaf renewal picked up: deleting the leaf Secret makes cert-manager reissue, the operator re-stamps `neo4j.com/tls-checksum` and rolls the StatefulSet, and the pod serves a new certificate serial (subPath mounts never update in place — the roll is the mechanism, BDR-006) — NEO-2-005
- [ ] `includeIngressHosts` SANs — requires an ingress case, not covered

### `feature-tls-byo` — NEO-2-005 (BYO)

Bring-Your-Own TLS: the user supplies the key/cert Secrets instead of cert-manager. Runs on
Standalone so a single self-signed bolt leaf proves the path without cluster mTLS. `trust/provision-byo`
generates the leaf (SAN = `<cr>.<ns>.svc`, the operator's dial target) and creates the labelled
Secrets before the CR; `trust/cleanup-byo` removes them on teardown (the CR does not own them).

- [x] Operator mounts user-supplied `privateKey`/`publicCertificate` Secrets (NEO-005 mountable label enforced) — NEO-2-005 · AC-NEO-TLS
- [x] `TLSReady=True/SecretsPresent` from BYO material — NEO-2-005
- [x] Operator dials its own admin Bolt over verified `bolt+s` and reaches Ready — the SAN/trust gate (a cert without `<cr>.<ns>.svc` keeps Ready False) — NEO-2-005 · NEO-004
- [x] Neo4j serves Bolt over TLS (`bolt+ssc` query succeeds) and plaintext `bolt://` is refused — NEO-2-005
- [x] Cluster BYO (per-member SANs + cluster mTLS with `clientAuth: Require`) — see `feature-tls-byo-cluster`
- [ ] BYO HTTPS listener material — unit-tested only
- [ ] BYO renewal (user updates the Secret → operator re-stamps checksum → roll) — cert-manager renewal is covered in `feature-tls`; the BYO update path is not

### `feature-tls-byo-cluster` — NEO-2-005 (BYO, Cluster)

The hardest BYO shape: a 3-primary cluster where the user supplies both the bolt and the cluster
mTLS material. `trust/provision-byo-cluster` runs a private CA that signs one shared bolt leaf and
one shared cluster leaf (the operator mounts the same Secret on every member), each with every
member's service FQDN as a SAN. Reuses `assert/tls-ready` with `TLS_EXPECT_CERTIFICATES=false`.

- [x] Operator mounts user-supplied cluster + bolt material (private.key/public.crt/ca.crt), NEO-005 label enforced — NEO-2-005 · AC-NEO-TLS
- [x] `TLSReady=True/SecretsPresent` from BYO material — NEO-2-005
- [x] Members do cluster mTLS (`clientAuth: Require`) against the CA and the cluster forms (`ClusterFormed=True`) — NEO-2-005 · AC-NEO-CLUSTER-002
- [x] Cluster serves Bolt over TLS (`SHOW SERVERS` via `bolt+ssc`, all members Enabled+Available) and plaintext `bolt://` refused — NEO-2-005

### `feature-storage` — NEO-2-006
- [x] Dynamic data via existing StorageClass — NEO-3-006-PVC-02 · AC-NEO-STORAGE-DYNAMIC
- [x] Data volume role provisioned — NEO-3-006-VOL-01 · AC-NEO-STORAGE
- [x] Existing PVC by `claimName`
- [x] Raw `volumeClaimTemplate` provisioning
- [x] Ephemeral `emptyDir` (no PVC)
- [x] logs+metrics Share the data volume
- [x] `additionalMounts` mounted at their paths

A size change used to be accepted at admission, dropped on the way to the StatefulSet, and still
reported `Ready`/`StorageReady=True`. The `volumeClaimTemplate` is immutable once a StatefulSet
exists, so a grow can only reach the claims themselves — and everything that would need the
template rewritten has to be refused before it is accepted. The cases below assert both halves
(BDR-005, ADR-001 amended).

- [x] Grow the data volume: every claim requests the new size, capacity follows, `StorageReady` reports `StorageResizing` while it does and `Ready` holds back under `StorageNotReady`, then a single `StorageResizeCompleted` Event — the template never moves
- [x] Grow refused by the StorageClass (`allowVolumeExpansion: false`): `StorageReady=False/StorageResizeFailed` carrying the API server's own message as a Warning Event, claims untouched, and the server keeps serving — the operator asks and surfaces the refusal rather than reading the class first
- [x] Every storage change that could not be applied is refused at admission (CEL transition rules): shrink — including one hidden by units, `10Gi` → `9000Mi`, which is why the supported Kubernetes floor is 1.35 and its quantity library — plus `storageClassName`, data `mode`, `disableSubPathExpr`, adding or removing an auxiliary volume, changing an auxiliary `mode`, and dropping `spec.storage`; a grow is attempted last as the positive control, since a rule set that refused everything would pass every rejection check
- [ ] `accessMode` change — unreachable today: the field is an enum of one value, so the API server refuses any other before CEL runs. The immutability rule behind it is the backstop for the day that enum widens

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
