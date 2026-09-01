# `Neo4j` — status model

**API**: `neo4j.com/v1beta1` · **Subresource**: `status`  
**Sources**: [BDR-002](../../decision-records/business/neo4j/002-neo4j-crd-topology.md) · [ADR-001](../../decision-records/architecture/001-crd-validation-process.md) · [ADR-004](../../decision-records/architecture/004-status-and-conditions.md) · [ADR-014](../../decision-records/architecture/014-operator-observability.md) · `OP-1-003` / `AC-OP-STATUS-*`

---

## Principles

| Rule | Detail |
|------|--------|
| **Observed state only** | `status` reflects what the operator measured — never user intent from `spec`. |
| **Conditions for automation** | Controllers and users gate on `Ready`, `Reconciling`, `Error` — not on `phase` alone. |
| **Topology warnings ≠ errors** | BDR-002 non-HA guidance must never block `Ready`. The `TopologyWarning` condition that carries it is [planned, not written](#planned-for-a-later-version). |
| **Generation tracking** | `observedGeneration` must match `metadata.generation` before `Ready=True` after spec changes. |
| **Phase non-regression** | Once a CR has been `Ready`, `phase` does **not** regress to `Provisioning` or `Bootstrapping` — those two mean *this workload has never served*. Sub-states surface via **conditions** or `message`, never a phase downgrade. |
| **Phase says intent, conditions say health** | A not-ready state the user asked for — a roll after a config change, a scale, an upgrade — keeps `phase: Running`; `Ready=False` and its reason carry the health. `Degraded` is for an unplanned loss only. See [phase](#phase-statusphase) and ADR-004. |
| **Long-running work in sub-status** | `status.phase` stays coarse. Upgrade, scale-down drain, and similar workflows use dedicated sub-blocks (`upgrade`, domain conditions) — not a generic `Reconciling` message alone. |
| **Diagnostics ≠ Ready path** | Bolt diagnostics (`SHOW SERVERS`, `SHOW DATABASES`, …) are optional and non-fatal. Collection failure sets `diagnostics.collectionError` — does **not** force `Ready=False`. |
| **Health decoupled from Ready** | Live Neo4j health must not gate `Ready`: a workload that serves clients is `Ready` even when diagnostics are off. The `ServersHealthy` / `DatabasesHealthy` conditions that would report it are [planned, not written](#planned-for-a-later-version). |

---

## Top-level fields

| Field | Type | When populated | Description |
|-------|------|----------------|-------------|
| `phase` | string | Always | Coarse lifecycle phase (see below). |
| `conditions` | `[]Condition` | Always | Kubernetes-standard conditions — primary automation surface. |
| `observedGeneration` | int64 | Always | Last `metadata.generation` fully reconciled. |
| `version` | string | When known | **Effective** Neo4j version on the workload (image / DBMS). During upgrade: reflects version **already running** on members; see `upgrade.targetVersion` for intent. |
| `lastUpgradeTime` | `metav1.Time` | **Not written yet** | Timestamp when `upgrade.phase` last reached `Completed`. Audit / SRE. |
| `serverSummary` | `ReplicaSummary` | Always | Lightweight STS summary — cheap (no Bolt). Not `spec.topology.secondaries`. |
| `upgrade` | `UpgradeStatus` | **Not written yet** | Rolling upgrade state machine (see below) — the schema is settled, the writer is not implemented. |
| `members` | `[]MemberStatus` | **Not written yet** | Per-server summary (pool, plugins, K8s + Neo4j server state). |
| `diagnostics` | `DiagnosticsStatus` | **Not written yet** | Deep observability — needs the Bolt collector, which does not exist. |
| `endpoints` | `EndpointsStatus` | When Services exist | Client URIs + connection examples. |
| `credentials` | `CredentialsStatus` | When auth Secret exists | Reference to auth Secret — never the password itself. |
| `clusterInfo` | `ClusterInfoStatus` | **Not written yet** | Cluster ID, logical database states (summary). |
| `propertyShardingReady` | bool | **Not written yet** (V2) | Feature-scoped readiness — prerequisites met for sharding capability. |

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
| `Pending` | CR accepted; reconciliation not started or waiting on prerequisites. **Never assigned today** — the first pass already reports `Provisioning`. | `Provisioning` |
| `Provisioning` | No StatefulSet observed yet. | `Bootstrapping` |
| `Bootstrapping` | A StatefulSet exists, `Ready` is not met, and the CR has **never** been `Ready` — pods starting, PVCs binding, or the cluster still forming for the first time. | `Running` |
| `Running` | `Ready=True`, **or** a change the user asked for is in flight (a roll, a scale, an upgrade) on a CR that has already served. | `Degraded` / `Maintenance` / `Failed` |
| `Degraded` | Availability reduced or lost after the CR had served, with nothing in flight that would explain it. Covers a member gone from a cluster and a Standalone whose only pod is down — `Ready=False` and its reason say which. | `Running` / `Failed` |
| `Failed` | A pipeline step returned an error — see the `Error` condition. | manual fix |
| `Maintenance` | `spec.maintenance.offlineMode: true`. | `Running` |

How the writer picks between them is decided in ADR-004: offline maintenance first, then `Ready`,
then a workload with no StatefulSet, then one that has never been `Ready`, then a change in
flight — anything left is `Degraded`. "Has ever been `Ready`" is read from `status.version`, which is
written only when everything was serving, so it survives the `Failed` a transient reconcile error
leaves behind.

`Pending` is published in the CRD enum, so automation may legitimately match on it, but it never
occurs — the first pass already reports `Provisioning`. Either the writer starts assigning it or it
leaves the enum; still open.

**Not top-level phases:** `Upgrading`, `Scaling`, `Restoring` — tracked in `status.upgrade`, domain conditions, or day-2 CRD status (`Neo4jRestore`).

While `upgrade.phase != Completed` and `upgrade.phase != ""`, `status.phase` remains `Running` — never a dedicated `Upgrading` phase at top level. It stays `Running` even while members are not ready, because a rolling update makes them not ready one at a time by design and no cheap signal separates that from a member failing on its own; `Ready=False` and its reason report the health throughout.

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

## `status.drainOK` / `status.drainOKGeneration` (ADD-02)

Operator-owned scale-in gate. After every departing member is `DEALLOCATE`d/`DROP`ped, the formation reconciler writes:

| Field | Type | Description |
|-------|------|-------------|
| `drainOK` | map[string]int32 | Pool id (`primary`, `analytics`, `read`) → replica floor safe to shrink to. |
| `drainOKGeneration` | int64 | `metadata.generation` when `drainOK` was written. Shrink is allowed only when this equals the current generation. |

The workload reconciler reads these fields (never CR annotations). Setting `neo4j.com/drain-ok` on the Neo4j object is ignored.

## `status.primaryReplicasCap` (ADD-02)

Optional primary STS ceiling while `system` still has a single primary (blocks unsupported 1→N system scale-out). Operator-owned; formerly a forgeable annotation.

---

## Conditions

Standard condition schema: `type`, `status` (`True` \| `False` \| `Unknown`), `reason`, `message`, `lastTransitionTime`, `observedGeneration`.

### What the operator writes

The table below is **generated** from the condition catalog in `src/internal/oracle` by
`make errors`, and `make test` fails if it is stale ([ADR-014](../../decision-records/architecture/014-operator-observability.md)). Two consequences worth stating: a
condition the operator does not write cannot appear here, and a condition renamed in Go cannot
stay right in this page and wrong in the next release. Do not hand-edit between the markers.

The reasons each condition can carry, with their severity and what they mean, are the other
projection of the same catalog: the [error reference](../../../user-guide/05-reference/errors.md).

<!-- BEGIN GENERATED oracle:conditions -->
| Type | `True` when | Blocks `Ready`? |
|------|-------------|-----------------|
| `Ready` | Every desired server is ready, the data claims are bound, trust material is in place, and in Cluster mode the cluster is formed with no drain outstanding | — this *is* `Ready` |
| `Reconciling` | A reconcile pass is in flight; the writer clears it at the end of every pass, so it narrates progress rather than gating anything | No |
| `Installed` | At least one StatefulSet exists for the active pools | Yes — `False` holds `Ready` back |
| `Error` | The last pipeline pass returned an error; the same reason is recorded as a Warning Event | Yes — `True` clears `Ready` |
| `StorageReady` | Every data PVC the operator manages is Bound | Yes — `False` holds `Ready` back |
| `TLSReady` | Trust is disabled, or every required TLS Secret and key is present | Yes — `False` holds `Ready` back |
| `ClusterFormed` | Every desired server is enabled in the Neo4j cluster | Cluster mode — `False` holds `Ready` back |
| `ServersPendingDrain` | A server dropped from the spec is still registered in Neo4j and waiting to be drained | Cluster mode — `True` holds `Ready` back |
<!-- END GENERATED oracle:conditions -->

### Planned for a later version

Not written by any code path today. They are kept here because the need is real and the names are
reserved — but nothing may gate on them: no e2e assert, no runbook, no alert. A reader should treat
their absence from a live CR as normal, not as a defect. Promoting one means declaring it in the
catalog, which puts it in the generated table above automatically.

| Type | Intended meaning | Why it is not written yet |
|------|------------------|---------------------------|
| `LicenseValid` | Enterprise licence accepted and not expired. | Admission already refuses `edition: enterprise` without `license.accept` (CEL, [ADR-001](../../decision-records/architecture/001-crd-validation-process.md)), so the only case left is runtime expiry — which needs a licence probe the operator does not perform. |
| `TopologyWarning` | Non-HA topology guidance ([BDR-002](../../decision-records/business/neo4j/002-neo4j-crd-topology.md)) — surfaced without blocking `Ready`. | The case is live: admission requires `primaries.members >= 1` and an odd count, so a single-primary Cluster is accepted. Nothing computes the guidance yet. |
| `ServersHealthy` | All servers `health: Available` per `SHOW SERVERS`. | Needs the Bolt diagnostics collector behind `status.diagnostics`, which is not implemented. |
| `DatabasesHealthy` | User databases online per `SHOW DATABASES`. | Same collector. |

Later day-2 needs — restore in progress, sharding migration — should follow the same route: a new
condition in the catalog, never a new top-level phase.

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
| `name` | string | Pod / server name (`<sts-name>-<ordinal>`, e.g. `prod-read-2` — [BDR-009](../../decision-records/business/009-scale-pool-ordinal-semantics.md)). |
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
    address: dev-0.dev.default.svc:7687
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

`Ready=True` requires **all** of, as `internal/status.Writer` computes it:

1. `Installed=True` — at least one StatefulSet observed for the active pools
2. `serverSummary.ready == serverSummary.servers`, with `servers > 0`
3. `StorageReady=True` — every data PVC Bound
4. `TLSReady=True` — trust disabled, or all required Secrets and keys present
5. `ClusterFormed=True` **and** `ServersPendingDrain != True`, when `mode: Cluster`

Two things sit outside that list rather than in it. A failed pass takes the other route: it sets
`Error=True` and clears `Ready` directly, so `Error=True` always means `Ready=False` even though
`Ready` is not computed from it. And `spec.maintenance.offlineMode: true` overrides the whole
calculation — `Ready=False` with reason `OfflineMaintenance`, phase `Maintenance` — because the
pods run a sleep loop and no client can connect.

`Reconciling` does **not** gate `Ready`: the writer clears it at the end of every pass, before
computing `Ready` in the same pass.

**Read `observedGeneration` before trusting `Ready`.** The writer sets it in the same pass, so a
`Ready=True` observed while `observedGeneration < metadata.generation` describes the *previous*
spec — the usual Kubernetes caveat, not an operator quirk.

Deliberately not part of `Ready`: the diagnostics collection error, and every condition in
[Planned for a later version](#planned-for-a-later-version).

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

## Example (Cluster, steady state)

What a healthy three-primary Cluster reports today, with trust enabled. The fields marked
**Not written yet** above are absent, so this is what `kubectl get neo4j -o yaml` actually shows —
not what the schema allows.

```yaml
status:
  phase: Running
  observedGeneration: 7
  version: "2026.05.0"
  serverSummary:
    servers: 3
    ready: 3
  conditions:
    - type: Ready
      status: "True"
      reason: AllMembersReady
      message: "3/3 servers ready"
    - type: Reconciling
      status: "False"
      reason: Completed
    - type: Installed
      status: "True"
      reason: ObjectsCreated
    - type: Error
      status: "False"
      reason: NoError
    - type: StorageReady
      status: "True"
      reason: PVCBound
    - type: TLSReady
      status: "True"
      reason: SecretsPresent
    - type: ClusterFormed
      status: "True"
      reason: Formed
    - type: ServersPendingDrain
      status: "False"
      reason: NoDrain
  endpoints:
    bolt: "neo4j+s://my-graph.graph-prod.svc:7687"
    https: "https://my-graph.graph-prod.svc:7473"
    internal: "my-graph-server.graph-prod.svc:7687"
    connectionExamples:
      boltURI: "neo4j+s://my-graph.graph-prod.svc:7687"
      neo4jURI: "neo4j+s://my-graph.graph-prod.svc:7687"
      portForward: "kubectl port-forward -n graph-prod svc/my-graph 7687:7687 # then bolt+s://127.0.0.1:7687 (use bolt+s, not neo4j+s, over port-forward)"
  credentials:
    secretName: my-graph-auth
    generated: true
  drainOK: true
  drainOKGeneration: 7
```

---

## Traceability

| Requirement | Status coverage |
|-------------|-----------------|
| `OP-1-003` | conditions + phase + upgrade |
| `OP-2-003-STATUS-01` | `Ready`, `Reconciling`, `Error`, `Installed` |
| `OP-2-003-STATUS-02` | `upgrade` sub-status and the operational conditions — both planned, neither written |
| `AC-NEO-CLUSTER` | `ClusterFormed`, `serverSummary` (`members[]` planned) |
| `AC-NEO-STANDALONE` | `Ready`, `serverSummary` |
| BDR-002 | `TopologyWarning` — planned, not written |
