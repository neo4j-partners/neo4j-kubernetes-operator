# `Neo4j` — status model

**API**: `neo4j.com/v1beta1` · **Subresource**: `status`  
**Sources**: [BDR-002](../../decision-records/business/002-neo4j-crd-topology.md) · [ADR-001](../../decision-records/architecture/001-crd-validation-process.md) · [`20-operator-proposal.md`](../../20-operator-proposal.md) §3.1 · `OP-1-003` / `AC-OP-STATUS-*`

---

## Principles

| Rule | Detail |
|------|--------|
| **Observed state only** | `status` reflects what the operator measured — never user intent from `spec`. |
| **Conditions for automation** | Controllers and users gate on `Ready`, `Reconciling`, `Error` — not on `phase` alone. |
| **Topology warnings ≠ errors** | BDR-002 non-HA guidance surfaces as `TopologyWarning` — workload may still be `Ready`. |
| **Generation tracking** | `observedGeneration` must match `metadata.generation` before `Ready=True` after spec changes. |
| **Phase non-regression** | Established phases must **not** regress to earlier bootstrap phases (e.g. `Running` → `Bootstrapping`) unless the object is deleted and recreated. Sub-states (TLS pending, cluster formation, upgrade in progress) surface via **conditions**, `status.upgrade`, or `message` — not phase downgrade. Avoids UI / alert flicker. |
| **Long-running work in sub-status** | `status.phase` stays coarse. Upgrade, scale-down drain, and similar workflows use dedicated sub-blocks (`upgrade`, domain conditions) — not a generic `Reconciling` message alone. |
| **Diagnostics ≠ Ready path** | Bolt diagnostics (`SHOW SERVERS`, `SHOW DATABASES`, …) are optional and non-fatal. Collection failure sets `diagnostics.collectionError` — does **not** force `Ready=False`. |
| **Health decoupled from Ready** | `ServersHealthy` / `DatabasesHealthy` reflect live Neo4j observability when monitoring is on. `Ready=True` with `ServersHealthy=Unknown` is valid when diagnostics are disabled. |

---

## Top-level fields

| Field | Type | When populated | Description |
|-------|------|----------------|-------------|
| `phase` | string | Always | Coarse lifecycle phase (see below). |
| `conditions` | `[]Condition` | Always | Kubernetes-standard conditions — primary automation surface. |
| `observedGeneration` | int64 | Always | Last `metadata.generation` fully reconciled. |
| `version` | string | When known | **Effective** Neo4j version on the workload (image / DBMS). During upgrade: reflects version **already running** on members; see `upgrade.targetVersion` for intent. |
| `lastUpgradeTime` | `metav1.Time` | After successful upgrade | Timestamp when `upgrade.phase` last reached `Completed`. Audit / SRE. |
| `serverSummary` | `ReplicaSummary` | Always | Lightweight STS summary — cheap (no Bolt). Not `spec.topology.secondaries`. |
| `upgrade` | `UpgradeStatus` | During / after `spec.version` change | Rolling upgrade state machine (see below). |
| `members` | `[]MemberStatus` | Cluster + detail path; Standalone optional | Per-server summary (pool, plugins, K8s + Neo4j server state). |
| `diagnostics` | `DiagnosticsStatus` | When `spec.monitoring` enables deep collection and workload ready | Deep observability — separate from `members[]` summary. |
| `endpoints` | `EndpointsStatus` | When Services exist | Client URIs + connection examples. |
| `credentials` | `CredentialsStatus` | When auth Secret exists | Reference to auth Secret — never the password itself. |
| `clusterInfo` | `ClusterInfoStatus` | Cluster + Bolt reachable | Cluster ID, logical database states (summary). |
| `propertyShardingReady` | bool | When property sharding opt-in configured (V2) | Feature-scoped readiness — prerequisites met for sharding capability. |

### `status.version` semantics

| Situation | `status.version` | Also check |
|-----------|------------------|------------|
| Steady state | Matches `spec.version` on all ready members | — |
| Upgrade in progress | Highest version **already applied** on upgraded members (may lag `spec.version`) | `upgrade.targetVersion`, `upgrade.progress` |
| Per-member drift | Summary field may show majority; detail in `members[].version` | `upgrade.lastError` |

---

## Phase (`status.phase`)

Coarse enum — **does not** encode upgrade step or scale sub-state.

| Phase | Meaning | Typical next phase |
|-------|---------|-------------------|
| `Pending` | CR accepted; reconciliation not started or waiting on prerequisites. | `Provisioning` |
| `Provisioning` | Creating RBAC, TLS material, ConfigMaps, Services, StatefulSet. | `Bootstrapping` |
| `Bootstrapping` | Pods exist; Neo4j starting or cluster forming. | `Running` |
| `Running` | Required members ready; cluster formed (if applicable). Upgrade may be in progress — see `status.upgrade`. | `Degraded` / `Maintenance` / `Failed` |
| `Degraded` | Partial availability — some members not ready or operational conditions false. | `Running` / `Failed` |
| `Failed` | Unrecoverable error — see `Error` condition. | manual fix |
| `Maintenance` | `spec.maintenance.offlineMode: true` or operator-led maintenance window. | `Running` |

**Not top-level phases:** `Upgrading`, `Scaling`, `Restoring` — tracked in `status.upgrade`, domain conditions, or day-2 CRD status (`Neo4jRestore`).

While `upgrade.phase != Completed` and `upgrade.phase != ""`, `status.phase` remains `Running` or `Degraded` (if members unhealthy) — never a dedicated `Upgrading` phase at top level.

---

## `status.upgrade`

Dedicated state machine for `spec.version` changes. Survives operator restart via `currentPartition` (StatefulSet rolling update partition).

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | `""` \| `Staging` \| `Rolling` \| `Stabilizing` \| `Verifying` \| `Completed` \| `Failed` |
| `targetVersion` | string | `spec.version` being rolled out. |
| `previousVersion` | string | Version before this upgrade started. |
| `currentPartition` | int32 | STS partition cursor — resume point after operator restart. |
| `stepStartTime` | `metav1.Time` | Start of current `phase` step. |
| `progress` | `UpgradeProgress` | `{ total, upgraded, pending }` server counts. |
| `lastError` | string | Last failure message; empty when healthy. |

### Upgrade phases

| Phase | Meaning |
|-------|---------|
| `Staging` | Preflight — image pull, plugin compatibility, PDB / maintenance checks. |
| `Rolling` | Partitioned rolling pod restarts (`currentPartition` advances). |
| `Stabilizing` | Waiting for Neo4j process + cluster membership after last restart. |
| `Verifying` | Post-upgrade checks — `SHOW SERVERS`, version alignment, optional smoke query. |
| `Completed` | All members on `targetVersion`; `lastUpgradeTime` updated. |
| `Failed` | Irrecoverable — `Error=True`, `lastError` set; manual intervention. |

```yaml
upgrade:
  phase: Rolling
  targetVersion: "2026.05.0"
  previousVersion: "5.26.0"
  currentPartition: 2
  stepStartTime: "2026-06-22T14:30:00Z"
  progress:
    total: 3
    upgraded: 1
    pending: 1
  lastError: ""
```

---

## `status.serverSummary`

Always updated from StatefulSet / pod list — **no Bolt required**. Distinct from `spec.topology.secondaries` (fixed pools `analytics`, `read`).

| Field | Type | Description |
|-------|------|-------------|
| `servers` | int32 | Desired server count (`1` Standalone; `primaries.members + analytics.members + read.members` Cluster). |
| `ready` | int32 | Pods passing readiness (K8s + operator gates). |

Use for `kubectl` columns, simple waits (`ready == servers`), HPA-style automation. Prefer over scanning `members[]` for counts.

---

## Conditions

Standard condition schema: `type`, `status` (`True` \| `False` \| `Unknown`), `reason`, `message`, `lastTransitionTime`, `observedGeneration`.

### Infrastructure conditions (V1)

| Type | `True` when | `False` reason examples | Blocks `Ready`? |
|------|-------------|-------------------------|-----------------|
| `Ready` | Workload reachable; reconciliation complete for current generation. | `MembersNotReady`, `ClusterNotFormed`, `TLSNotReady` | — (this *is* Ready) |
| `Reconciling` | Active reconcile in progress (short-lived slices). | — | Yes (`Ready` should be `False`) |
| `Installed` | Base K8s objects created (STS, Services, ConfigMaps). | `ProvisioningFailed` | Yes |
| `Error` | Last reconcile failed irrecoverably. | `ValidationFailed`, `StorageBindingFailed`, `UpgradeFailed` | Yes |
| `ClusterFormed` | Cluster quorum / system DB healthy (`mode: Cluster`). | `QuorumLost`, `FormationTimeout` | Yes (Cluster) |
| `TLSReady` | Required TLS secrets exist and are mounted (`trust.enabled`). | `SecretMissing`, `MountFailed` | Yes when TLS on |
| `LicenseValid` | Enterprise license accepted. | `LicenseExpired` | Yes |
| `StorageReady` | All member PVCs bound. | `PVCPending` | Yes |
| `TopologyWarning` | Non-blocking topology guidance (BDR-002). | — | **No** |

### Domain conditions — operational (V1+)

Neo4j-specific workflows beyond infra. Populated when Bolt admin API is reachable (may be `Unknown` when monitoring off).

| Type | `True` when | Blocks `Ready`? | Notes |
|------|-------------|-----------------|-------|
| `ServersHealthy` | All servers `health: Available` per `SHOW SERVERS`. | No | `Unknown` when diagnostics disabled |
| `DatabasesHealthy` | User databases online per `SHOW DATABASES`. | No | |
| `ServersPendingDrain` | Scale-down: servers still registered in Neo4j but removed from spec. | Yes during scale-in | Cleared when deallocation complete |

Future (day-2 / V2): conditions for restore in progress, sharding migration, etc. — prefer domain conditions over new top-level phases.

### TopologyWarning (BDR-002)

| Reason | Trigger | Example message |
|--------|---------|-----------------|
| `NonHA` | `mode: Cluster`, `primaries.members < 3`, any secondary pool with `members ≥ 1` | `primaries.members < 3 — not suitable for production HA writes` |
| `LowPrimaryCount` | `mode: Cluster`, `primaries.members: 1`, no secondary pools | Dev/single-writer — informational |

`TopologyWarning=True` does **not** set `Ready=False` unless members are actually unhealthy.

---

## `status.members[]`

**Summary** view per server — populated for Cluster when pods exist; Standalone may use a **single** entry.

### When filled

| Mode | Default | Full Neo4j fields |
|------|---------|-------------------|
| `Standalone` | `serverSummary` only | Optional single `members[0]` with `pod` block |
| `Cluster` | `serverSummary` always | `members[]` when monitoring on or UI/detail requested |

Avoid mandatory Bolt on every reconcile — use `serverSummary` for counts.

### Member fields (Neo4j 5.26+ server model)

Prefer vocabulary from `SHOW SERVERS` over legacy causal roles (`LEADER` / `FOLLOWER` / `READ_REPLICA`).

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Pod / server name (`<metadata.name>-<ordinal>`). |
| `pool` | string | `primary`, `analytics`, or `read` from spec. |
| `address` | string | Bolt address for admin operations. |
| `plugins` | []string | Resolved catalog ids (pool refs + `pluginDefinitions`). |
| `neo4jState` | string | Server state from `SHOW SERVERS` — e.g. `Enabled`, `Cordoned`, `Deallocating`. |
| `neo4jHealth` | string | `Available`, `Unavailable`, … |
| `hostingDatabases` | int32 | Count of databases hosted on this server. |
| `version` | string | Running Neo4j version on this member. |
| `podReady` | bool | Kubernetes pod Ready bit. |
| `storageBound` | bool | Data PVC bound. |
| `pod` | `PodSummary` | K8s layer — see below. |

**Deprecated in docs / UI:** `role: LEADER` \| `FOLLOWER` \| `READ_REPLICA` — only expose if explicitly mapping causal cluster API for legacy tooling; not the primary V1 field.

### `PodSummary` (Standalone and Cluster)

| Field | Description |
|-------|-------------|
| `podName` | Kubernetes pod name. |
| `podIP` | Pod IP. |
| `nodeName` | Scheduled node. |
| `restartCount` | Container restart count. |
| `phase` | Pod phase (`Running`, `Pending`, …). |

Standalone example — one member, no invented cluster role:

```yaml
members:
  - name: dev-server-0
    pool: server
    address: dev-0.dev.graph-dev.svc:7687
    neo4jState: Enabled
    neo4jHealth: Available
    hostingDatabases: 2
    podReady: true
    version: "2026.05.0"
    pod:
      podName: dev-0
      podIP: 10.42.1.15
      nodeName: worker-2
      restartCount: 0
      phase: Running
```

---

## `status.diagnostics`

Deep observability — **not** on the critical path for `Ready`.

### Collection policy

| Rule | Detail |
|------|--------|
| Collect only when | `spec.monitoring` (or explicit diagnostics flag) enabled **and** workload past bootstrap |
| On Bolt failure | Set `diagnostics.collectionError`; leave `Ready` unchanged |
| Staleness | `diagnostics.lastCollectedTime` — consumers treat data as best-effort |

| Field | Type | Description |
|-------|------|-------------|
| `lastCollectedTime` | `metav1.Time` | Last successful collection. |
| `collectionError` | string | Last Bolt / Cypher error; empty when OK. |
| `servers` | `[]ServerDiagnostic` | Raw-aligned `SHOW SERVERS` rows (optional mirror of `members` detail). |
| `databases` | `[]DatabaseDiagnostic` | `SHOW DATABASES` snapshot. |
| `users` | `[]UserDiagnostic` | V2 — when auth CRDs enabled (`Neo4jUser`). |
| `userCount` | int32 | Total users when `users` truncated. |
| `roles` | `[]RoleDiagnostic` | V2 — when auth CRDs enabled (`Neo4jRole`). |
| `roleCount` | int32 | Total roles when `roles` truncated. |

`members[]` = operator summary for GitOps / `kubectl`; `diagnostics` = support / UI / runbooks.

---

## `status.endpoints`

| Field | Description |
|-------|-------------|
| `bolt` | Primary Bolt URI (`neo4j://` or `neo4j+s://`). |
| `neo4j` | Routing URI when applicable (`neo4j+s://…`). |
| `http` | Browser HTTP when enabled. |
| `https` | Browser HTTPS when enabled. |
| `internal` | In-cluster headless / ClusterIP target. |
| `backup` | Backup port when exposed. |
| `connectionExamples` | Onboarding helpers (below). |

### `connectionExamples`

| Field | Example |
|-------|---------|
| `boltURI` | `neo4j+s://my-graph-lb.graph-prod.svc:7687` |
| `neo4jURI` | `neo4j+s://my-graph-lb.graph-prod.svc:7687` |
| `portForward` | `kubectl port-forward -n graph-prod svc/my-graph-client 7687:7687` |
| `python` | `GraphDatabase.driver("neo4j+s://…", auth=(…))` |
| `java` | `GraphDatabase.driver("neo4j+s://…", AuthTokens.basic(…))` |

URIs follow `trust.enabled` (`neo4j://` vs `neo4j+s://`).

---

## `status.credentials`

| Field | Description |
|-------|-------------|
| `secretName` | Kubernetes Secret containing `NEO4J_AUTH`. |
| `generated` | `true` if operator created the Secret. |

---

## `status.clusterInfo`

Lightweight summary — detail in `diagnostics.databases` when collected.

| Field | Description |
|-------|-------------|
| `clusterId` | Neo4j cluster / DBMS identifier. |
| `databases` | `[]{ name, status }` — `online`, `offline`, … |

---

## Feature-scoped readiness (V2 pattern)

| Field | When | Meaning |
|-------|------|---------|
| `propertyShardingReady` | Property sharding enabled in spec | `true` when CalVer, config, and `Ready` prerequisites for sharding are met |

Pattern: `status.<feature>Ready` for opt-in capabilities — avoid overloading `ClusterFormed`.

---

## Ready semantics

`Ready=True` requires **all** of:

1. `observedGeneration == metadata.generation`
2. `Error=False`, `Reconciling=False`
3. `Installed=True`
4. `serverSummary.ready == serverSummary.servers` (or equivalent pod gate)
5. `ClusterFormed=True` when `mode: Cluster`
6. `TLSReady=True` when `trust.enabled: true`
7. `LicenseValid=True`
8. `ServersPendingDrain=False` when scale-down in progress
9. `upgrade.phase` is `""` or `Completed` (upgrade failure sets `Error=True`)

**Explicitly not required for `Ready`:**

- `diagnostics.collectionError` empty
- `ServersHealthy` / `DatabasesHealthy` (informational)
- `TopologyWarning` (guidance only)

---

## Observability contract

Each key status signal should have a Prometheus equivalent for SRE dashboards and alerts.

| Status signal | Metric (illustrative) | Labels |
|---------------|----------------------|--------|
| `phase` | `neo4j_operator_neo4j_phase` (gauge enum) | `namespace`, `name` |
| `replicas` | `neo4j_operator_neo4j_replicas_desired`, `_ready` | Counts from `serverSummary` |
| `upgrade.phase` | `neo4j_operator_neo4j_upgrade_phase` | `target_version` |
| `conditions.Ready` | `neo4j_operator_neo4j_ready` | |
| `members[].neo4jHealth` | `neo4j_operator_server_health` | `server`, `pool` |
| Upgrade progress | `neo4j_operator_upgrade_members_upgraded` | |

Phase / condition transitions should increment event counters or structured log fields for audit.

---

## Example (Cluster, post-upgrade)

```yaml
status:
  phase: Running
  observedGeneration: 7
  version: "2026.05.0"
  lastUpgradeTime: "2026-06-22T15:00:00Z"
  serverSummary:
    servers: 3
    ready: 3
  upgrade:
    phase: Completed
    targetVersion: "2026.05.0"
    previousVersion: "5.26.0"
    currentPartition: 0
    progress: { total: 3, upgraded: 3, pending: 0 }
    lastError: ""
  conditions:
    - type: Ready
      status: "True"
      reason: AllMembersReady
      message: "3/3 servers ready"
    - type: ServersHealthy
      status: "True"
      reason: AllAvailable
    - type: DatabasesHealthy
      status: "True"
      reason: AllOnline
    - type: ClusterFormed
      status: "True"
      reason: QuorumHealthy
    - type: TopologyWarning
      status: "False"
  members:
    - name: my-graph-0
      pool: primary
      address: my-graph-0.my-graph.graph-prod.svc:7687
      neo4jState: Enabled
      neo4jHealth: Available
      hostingDatabases: 2
      podReady: true
      version: "2026.05.0"
  diagnostics:
    lastCollectedTime: "2026-06-22T15:05:00Z"
    collectionError: ""
  endpoints:
    bolt: "neo4j+s://my-graph-lb.graph-prod.svc:7687"
    https: "https://my-graph-lb.graph-prod.svc:7473"
    connectionExamples:
      boltURI: "neo4j+s://my-graph-lb.graph-prod.svc:7687"
      portForward: "kubectl port-forward -n graph-prod svc/my-graph-client 7687:7687"
  credentials:
    secretName: my-graph-auth
    generated: true
```

---

## Traceability

| Requirement | Status coverage |
|-------------|-----------------|
| `OP-1-003` | conditions + phase + upgrade |
| `OP-2-003-STATUS-01` | `Ready`, `Reconciling`, `Error`, `Installed` |
| `OP-2-003-STATUS-02` | `upgrade` sub-status (not deferred); domain conditions |
| `AC-NEO-CLUSTER` | `ClusterFormed`, `members[]`, `serverSummary` |
| `AC-NEO-STANDALONE` | `Ready`, `serverSummary`, optional single `members[]` |
| BDR-002 | `TopologyWarning` / `NonHA` |
