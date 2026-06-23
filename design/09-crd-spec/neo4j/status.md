# `Neo4j` — status model

**API**: `neo4j.com/v1beta1` · **Subresource**: `status`  
**Sources**: [BDR-002](../../adr/business/002-neo4j-crd-topology.md) · [`20-operator-proposal.md`](../../20-operator-proposal.md) §3.1 · `OP-1-003` / `AC-OP-STATUS-*`

---

## Principles

| Rule | Detail |
|------|--------|
| **Observed state only** | `status` reflects what the operator measured — never user intent from `spec`. |
| **Conditions for automation** | Controllers and users gate on `Ready`, `Reconciling`, `Error` — not on `phase` alone. |
| **Topology warnings ≠ errors** | BDR-002 non-HA guidance surfaces as `TopologyWarning` — workload may still be `Ready`. |
| **Generation tracking** | `observedGeneration` must match `metadata.generation` before `Ready=True` after spec changes. |

---

## Top-level fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | Coarse lifecycle phase (see below). V1 uses basic phases; detailed upgrade/backup phases deferred (`OP-2-003-STATUS-02`). |
| `conditions` | `[]Condition` | Kubernetes-standard conditions — primary automation surface. |
| `observedGeneration` | int64 | Last `metadata.generation` fully reconciled. |
| `version` | string | Neo4j version currently running (from image / cluster). |
| `members` | `[]MemberStatus` | Per-pod cluster view — empty when `mode: Standalone`. |
| `endpoints` | `EndpointsStatus` | Client connection URIs (Bolt, HTTP, HTTPS, internal). |
| `credentials` | `CredentialsStatus` | Reference to auth Secret — never the password itself. |
| `clusterInfo` | `ClusterInfoStatus` | Cluster ID, logical database states — populated when cluster API is reachable. |

---

## Phase (`status.phase`)

V1 enum:

| Phase | Meaning | Typical next phase |
|-------|---------|-------------------|
| `Pending` | CR accepted; reconciliation not started or waiting on prerequisites. | `Provisioning` |
| `Provisioning` | Creating RBAC, TLS, ConfigMaps, Services, StatefulSet. | `Bootstrapping` |
| `Bootstrapping` | Pods exist; Neo4j starting or cluster forming. | `Running` |
| `Running` | All required members ready; cluster formed (if applicable). | `Degraded` / `Maintenance` / `Failed` |
| `Degraded` | Partial availability — some members not ready or warning conditions set. | `Running` / `Failed` |
| `Failed` | Unrecoverable error — see `Error` condition. | manual fix |
| `Maintenance` | `spec.maintenance.offlineMode: true` or operator-led maintenance window. | `Running` |

**Not in V1** (deferred): `Upgrading`, `Scaling`, `Restoring` — track via conditions until `OP-2-003-STATUS-02`.

---

## Conditions

Standard condition schema: `type`, `status` (`True` \| `False` \| `Unknown`), `reason`, `message`, `lastTransitionTime`, `observedGeneration`.

### Required conditions (V1)

| Type | `True` when | `False` reason examples | Blocks `Ready`? |
|------|-------------|-------------------------|-----------------|
| `Ready` | Workload reachable; reconciliation complete for current generation. | `MembersNotReady`, `ClusterNotFormed`, `TLSNotReady` | — (this *is* Ready) |
| `Reconciling` | Active reconcile in progress. | — | Yes (Ready should be False) |
| `Installed` | Base K8s objects created (STS, Services, ConfigMaps). | `ProvisioningFailed` | Yes |
| `Error` | Last reconcile failed irrecoverably. | `ValidationFailed`, `StorageBindingFailed` | Yes |

### Domain conditions (V1)

| Type | `True` when | Notes |
|------|-------------|-------|
| `ClusterFormed` | Causal cluster quorum healthy (`mode: Cluster`). | Always `True` for `Standalone`. |
| `TLSReady` | Required TLS secrets exist and are mounted (`trust.enabled`). | `Unknown` when TLS disabled. |
| `LicenseValid` | Enterprise license accepted / eval valid. | |
| `StorageReady` | All member PVCs bound. | |
| `TopologyWarning` | Non-blocking topology guidance (BDR-002). | `reason: NonHA` when `cores.members < 3` with replicas |

### TopologyWarning (BDR-002)

Emitted by the reconciler (not admission) when spec is valid but risky:

| Reason | Trigger | Example message |
|--------|---------|-----------------|
| `NonHA` | `mode: Cluster` and `cores.members < 3` and `sum(replicaPools[].members) ≥ 1` | `cores.members < 3 — not suitable for production HA writes` |
| `LowCoreCount` | `mode: Cluster`, `cores.members: 1`, no replicas | Dev/single-writer cluster — informational |

`TopologyWarning=True` does **not** set `Ready=False` unless members are actually unhealthy.

---

## `status.members[]`

Populated when `spec.topology.mode: Cluster` and pods exist.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Pod name (`<metadata.name>-<ordinal>`). |
| `pool` | string | `core` or `replicaPools[].name`. |
| `role` | string | Neo4j role: `LEADER`, `FOLLOWER`, `READ_REPLICA`, `ANALYTICS`. |
| `plugins` | []string | Resolved catalog ids installed on this member (from pool `plugins[]` + `pluginDefinitions`). |
| `address` | string | Bolt address for admin operations. |
| `ready` | bool | Pod Ready and Neo4j process accepting connections. |
| `storageBound` | bool | Data PVC bound. |
| `version` | string | Running Neo4j version on this member. |

**Standalone**: `members` omitted or single implicit entry without cluster roles.

**StatefulSet sizing** (operator internal, echoed in `Ready` message):

```
replicas = cores.members + sum(replicaPools[].members)   # Cluster
replicas = 1                                               # Standalone
```

Each `status.members[].pool` echoes the `replicaPools[].name` (or `core` / `server`).

---

## `status.endpoints`

| Field | Description |
|-------|-------------|
| `bolt` | Client Bolt URI (e.g. `neo4j+s://my-graph-lb.graph-prod.svc:7687`). |
| `http` | Browser HTTP URI when enabled. |
| `https` | Browser HTTPS URI when enabled. |
| `internal` | In-cluster headless / ClusterIP target for other pods. |
| `backup` | Backup port target when `connectivity.external.ports.backup: true`. |

URIs use `neo4j://` or `neo4j+s://` based on `trust.enabled`.

---

## `status.credentials`

| Field | Description |
|-------|-------------|
| `secretName` | Kubernetes Secret containing `NEO4J_AUTH` (or operator-managed equivalent). |
| `generated` | `true` if operator created the Secret (`auth.generatePassword: true`). |

---

## `status.clusterInfo`

| Field | Description |
|-------|-------------|
| `clusterId` | Neo4j causal cluster ID. |
| `databases` | `[]{ name, status }` — logical DB states (`online`, `offline`, …). |

---

## Ready semantics

`Ready=True` requires **all** of:

1. `observedGeneration == metadata.generation`
2. `Error=False`, `Reconciling=False`
3. `Installed=True`
4. All pods for current topology ready (1 for Standalone; sum of role counts for Cluster)
5. `ClusterFormed=True` when `mode: Cluster`
6. `TLSReady=True` when `trust.enabled: true`
7. `LicenseValid=True`

`TopologyWarning` may be `True` while `Ready=True`.

---

## Example

```yaml
status:
  phase: Running
  observedGeneration: 4
  version: "2026.05.0"
  conditions:
    - type: Ready
      status: "True"
      reason: AllMembersReady
      message: "3/3 members ready"
      observedGeneration: 4
    - type: Reconciling
      status: "False"
      reason: Complete
      observedGeneration: 4
    - type: Installed
      status: "True"
      reason: ResourcesCreated
      observedGeneration: 4
    - type: ClusterFormed
      status: "True"
      reason: QuorumHealthy
      observedGeneration: 4
    - type: TLSReady
      status: "True"
      reason: CertificatesMounted
      observedGeneration: 4
    - type: LicenseValid
      status: "True"
      reason: LicenseAccepted
      observedGeneration: 4
    - type: TopologyWarning
      status: "False"
      reason: None
      observedGeneration: 4
  members:
    - name: my-graph-0
      role: LEADER
      address: my-graph-0.my-graph.graph-prod.svc:7687
      ready: true
      storageBound: true
      version: "2026.05.0"
    - name: my-graph-1
      role: FOLLOWER
      address: my-graph-1.my-graph.graph-prod.svc:7687
      ready: true
      storageBound: true
      version: "2026.05.0"
    - name: my-graph-2
      role: FOLLOWER
      address: my-graph-2.my-graph.graph-prod.svc:7687
      ready: true
      storageBound: true
      version: "2026.05.0"
  endpoints:
    bolt: "neo4j+s://my-graph-lb.graph-prod.svc:7687"
    https: "https://my-graph-lb.graph-prod.svc:7473"
    internal: "my-graph.graph-prod.svc.cluster.local:7687"
  credentials:
    secretName: my-graph-auth
    generated: true
  clusterInfo:
    clusterId: "abc123"
    databases:
      - name: neo4j
        status: online
      - name: system
        status: online
```

---

## Traceability

| Requirement | Status coverage |
|-------------|-----------------|
| `OP-1-003` | conditions + phase |
| `OP-2-003-STATUS-01` | `Ready`, `Reconciling`, `Error`, `Installed` |
| `AC-NEO-CLUSTER` | `ClusterFormed`, `members[]` |
| `AC-NEO-STANDALONE` | `Ready` with single member |
| BDR-002 | `TopologyWarning` / `NonHA` |
