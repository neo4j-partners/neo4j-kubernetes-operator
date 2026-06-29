# `Neo4j` — spec reference

**API group**: `neo4j.com` · **Version**: `v1beta1` · **Kind**: `Neo4j`  
**Scope**: Namespaced · **Short name**: `n4j`  
**Subresources**: `status`, `scale` (optional — maps to total cluster member count)

**Sources**: [BDR-001](../../decision-records/business/001-single-neo4j-crd.md) · [BDR-002](../../decision-records/business/002-neo4j-crd-topology.md) · [BDR-004](../../decision-records/business/004-neo4j-plugin-topology.md) (**Option E — accepted**) · [`20-operator-proposal.md`](../../20-operator-proposal.md) §3.1  
**Related**: [`validation.md`](validation.md) · [`status.md`](status.md) · [`example.yaml`](example.yaml)

> **Conflict rule**: where this document disagrees with `20-operator-proposal.md`, **[BDR-002](../../decision-records/business/002-neo4j-crd-topology.md) wins** for topology, **[BDR-004](../../decision-records/business/004-neo4j-plugin-topology.md) Option E — accepted** wins for plugins, and **[BDR-005](../../decision-records/business/005-storage-volume-mode.md) wins** for storage naming (`spec.volumes`, not `persistence`).

**FR coverage**: `NEO-1-001`, `NEO-1-002`, `NEO-2-003`…`NEO-2-016`, `NEO-2-018`

---

## Resource overview

Primary workload CRD. One `Neo4j` resource → one StatefulSet → N pods. Infra concerns (persistence, connectivity, trust, config) are **embedded `spec` sections** — not separate CRDs ([BDR-001](../../decision-records/business/001-single-neo4j-crd.md)).

```
spec
├── edition, version, license          # identity
├── topology                           # BDR-002 — mode + role counts + plugin refs (Cluster)
├── pluginDefinitions                  # BDR-004 Option E — accepted — license, version, config per plugin id
├── plugins[]                          # Standalone only — catalog id refs at spec root
├── image
├── auth
├── volumes                            # BDR-005 — mirrors helm values.yaml `volumes:` block
├── additionalMounts[]                 # BDR-005 — paired helm additionalVolumes + additionalVolumeMounts
├── secretMounts                       # BDR-005 — top-level like helm values.yaml
├── resources, jvm
├── config
├── trust
├── connectivity
├── scheduling, podDisruptionBudget, probes
├── security
├── monitoring
├── maintenance
└── podTemplate                        # escape hatch (containers only)
```

---

## `spec.edition`

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `edition` | string | **yes** | — | `enterprise` only in V1 (`NEO-2-001-EDT-01`). `community` deferred. |

---

## `spec.version`

| Field | Type | Required | Default | Immutable | Description |
|-------|------|----------|---------|-----------|-------------|
| `version` | string | **yes** | — | no | Neo4j image tag (e.g. `2026.05.0`). Drives rolling upgrade. |

---

## `spec.license`

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `accept` | string | **yes** | — | `yes` — accepted Enterprise license (`NEO-2-001-LIC-01`). `eval` deferred (`NEO-2-001-LIC-02`, V1=No). |

---

## `spec.topology` (BDR-002)

> **Plugin attachment** follows [BDR-004](../../decision-records/business/004-neo4j-plugin-topology.md) **Option E — accepted**: pools declare **catalog id refs** (`plugins: [apoc, gds]`); configuration lives in `spec.pluginDefinitions`.

Deployment mode and Neo4j role composition.

### `spec.topology.mode`

| Value | Meaning |
|-------|---------|
| `Standalone` | Single Neo4j instance — no causal cluster formation. |
| `Cluster` | Multi-member cluster — requires `primaries.members`. |

**Neo4j terminology (V1 API):** **Primary** = quorum / writer members (`topology.primaries`). **Secondary** = non-primary servers (`topology.secondaries`).

**Fixed secondary pools (V1):** `topology.secondaries` is an object with optional keys **`analytics`** (GDS/Bloom / OLAP) and **`read`** (read scaling). No arbitrary pool names — intent is encoded by the key, not a free-form `name` field.

**No `serverRole` field.** Operator configures every secondary pool member as a Neo4j Secondary; pool-specific Neo4j config is derived from the fixed key (`analytics` vs `read`) and `plugins`.

**Immutable** after create (mode change requires replace).

### Standalone shape

Only `mode` — **no `members` fields**:

```yaml
topology:
  mode: Standalone
```

Forbidden when `Standalone`: `primaries`, `secondaries`, `minimumMembers`.

### Cluster shape

```yaml
topology:
  mode: Cluster
  primaries:
    members: 3
    plugins: [apoc]
  secondaries:
    analytics:
      members: 1
      plugins: [gds, bloom]
    read:
      members: 2
      plugins: [apoc]
  minimumMembers: 3
pluginDefinitions:
  apoc: {}
  gds:
    licenseSecretRef: gds-license
  bloom:
    licenseSecretRef: bloom-license
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `primaries.members` | int32 | **yes** (Cluster) | — | Primary members (writers / quorum). Odd when > 0. |
| `primaries.plugins` | []string | no | `[]` | Plugin ids for every primary pod. **Cluster only** — field absent in Standalone. |
| `secondaries.analytics` | SecondaryPool | no | — | Analytics / GDS secondary pool. Omit or `members: 0` to disable. |
| `secondaries.read` | SecondaryPool | no | — | Read-scaling secondary pool. Omit or `members: 0` to disable. |
| `secondaries.<pool>.members` | int32 | conditional | — | Required ≥ 1 when pool block is present and non-zero. |
| `secondaries.<pool>.plugins` | []string | no | `[]` | Plugin ids for every pod in that pool. |

**SecondaryPool** (`analytics` | `read` only): `{ members, plugins? }`.

**Removed (do not implement):** `secondaries[]` list with `name` field; `secondaries[].serverRole`.

| `minimumMembers` | int32 | no | `primaries.members` | Formation gate (`NEO-2-011`). |

Plugin ids in `primaries.plugins` / `secondaries.analytics.plugins` / `secondaries.read.plugins` are **references only** — resolved via `spec.pluginDefinitions` ([BDR-004](../../decision-records/business/004-neo4j-plugin-topology.md), **Option E — accepted**). Which plugins are allowed where depends on `topology.mode` — see [Plugin placement by mode](#plugin-placement-by-mode).

**StatefulSet replica count**:

| Mode | Replicas |
|------|----------|
| `Standalone` | `1` |
| `Cluster` | `primaries.members + analytics.members + read.members` (missing pool or `members: 0` → 0) |

**Ordinal → pool** (fixed order): primaries first, then **`analytics`**, then **`read`**.

### Topology decision guide

| Use case | Spec |
|----------|------|
| Dev / CI single server | `mode: Standalone` |
| Standalone + GDS | `mode: Standalone`, `spec.plugins: [apoc, gds]` + `pluginDefinitions` |
| Production HA writes | `mode: Cluster`, `primaries.members: 3`, omit `secondaries` or zero members |
| Primary + analytics / GDS | `mode: Cluster`, `primaries.members: 1`, `secondaries.analytics: { members: 1, plugins: [gds] }` |
| HA + read scaling | `mode: Cluster`, `primaries.members: 3`, `secondaries.read: { members: N, plugins: [apoc] }` |
| GDS + Bloom on analytics | `secondaries.analytics.plugins: [gds, bloom]` + `pluginDefinitions` |

### Helm mapping

| Helm | `spec.topology` |
|------|-----------------|
| `minimumClusterSize: 1`, no analytics | `mode: Standalone` |
| `minimumClusterSize: 3` | `mode: Cluster`, `primaries.members: 3`, `minimumMembers: 3` |
| analytics primary + N secondaries | `primaries.members: 1`, `secondaries.analytics.members: N` |
| Read replica scaling | `primaries.members: 3`, `secondaries.read.members: N` |
| `operations.enableServer: true` | scale / enable-server flow (`NEO-2-011`) |

---

## `spec.image`

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `repository` | string | no | `neo4j` | Container image repository. |
| `pullPolicy` | string | no | `IfNotPresent` | `Always`, `IfNotPresent`, `Never`. |
| `pullSecrets` | []string | no | `[]` | Image pull secret names (`NEO-3-004-IMG-01`). |

Effective image: `{repository}:{spec.version}`.

---

## `spec.auth`

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `generatePassword` | bool | no | `true`* | Operator generates password Secret. |
| `passwordSecretRef.name` | string | no | — | Existing Secret; key `NEO4J_AUTH` (`NEO-3-004-CRED-02`). |
| `ldap.enabled` | bool | no | `false` | LDAP auth (V2 — `NEO-3-004-SEC-02`). |
| `ldap.passwordSecretRef` | object | no | — | LDAP bind password Secret. |

\*Default `generatePassword: true` only when `passwordSecretRef` absent.

---

## `spec.volumes`

Mirrors Helm `values.yaml` → **`volumes:`** ([BDR-005](../../decision-records/business/005-storage-volume-mode.md), **Option D — accepted**). Not named `persistence` — that label came from the early operator proposal but does not match the chart surface operators migrate from.

### `spec.volumes.data` (required)

| Field | Type | Required | Default | Immutable | Description |
|-------|------|----------|---------|-----------|-------------|
| `mode` | string | **yes** | `Dynamic` | yes | `Dynamic` \| `Existing`. |
| `dynamic.size` | string | when `Dynamic` | — | no† | PVC size (e.g. `100Gi`). |
| `dynamic.storageClassName` | string | no | cluster default | yes | StorageClass (`NEO-3-006-PVC-01/02`). |
| `dynamic.accessMode` | string | no | `ReadWriteOnce` | yes | V1: `ReadWriteOnce` only. |
| `dynamic.labels` | map | no | `{}` | no | PVC metadata labels. |
| `existing.claimName` | string | oneOf | — | yes | Bind named PVC (`NEO-3-006-PVC-03`). |
| `existing.volume` | object | oneOf | — | yes | Inline StatefulSet `volume` source (incl. Helm `share`). |
| `existing.volumeClaimTemplate` | object | oneOf | — | yes | Raw K8s VCT — incl. `selector` (`NEO-3-006-PVC-04/05`). |
| `disableSubPathExpr` | bool | no | `false` | yes | Mount `subPathExpr` control for `/data`. |

†`dynamic.size` expansion allowed; shrink blocked.

**`Existing`:** exactly one of `claimName`, `volume`, or `volumeClaimTemplate` (CEL `oneOf`).

### Auxiliary volumes

Roles: `backups`, `logs`, `metrics`, `import`, `licenses`. Each supports:

| Field | Values | Default |
|-------|--------|---------|
| `mode` | `Share` \| `Dynamic` \| `Existing` | `Share` |
| `shareFrom` | `data` (V1 only) | `data` when `mode: Share` |
| `dynamic` | same shape as `data.dynamic` | — |
| `existing` | same oneOf as `data.existing` | — |

V1 Helm default: aux volumes **share** the data volume. `volumes.data.mode: Share` is **forbidden**.

---

## `spec.additionalMounts`

Top-level like Helm `additionalVolumes` + `additionalVolumeMounts` — **paired** in one list ([BDR-005](../../decision-records/business/005-storage-volume-mode.md), **Option E — accepted**).

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Volume name (unique in pod). |
| `volume` | object | Kubernetes `volume` source (`emptyDir`, `configMap`, `persistentVolumeClaim`, `csi`, …). |
| `mountPath` | string | Container mount path. |
| `subPath` | string | Optional subPath. |
| `readOnly` | bool | Default `false`. |

---

## `spec.secretMounts`

Top-level like Helm `secretMounts:` — map of mount id → Secret projection.

| Field | Type | Description |
|-------|------|-------------|
| `<id>.secretName` | string | Secret name. |
| `<id>.mountPath` | string | Mount directory in Neo4j container. |
| `<id>.items` | []KeyToPath | Optional key subset. |
| `<id>.defaultMode` | int | File mode (default `0644`). |

**Reserved paths:** `/data`, `/var/lib/neo4j/certificates/*` — webhook rejects user mounts (operator-owned).

**vs other secrets:** TLS → `spec.trust`; auth password → `spec.auth`; restore Job creds → `Neo4jRestore.spec.source.credentials`.

---

## `spec.resources`

Standard Kubernetes `resources` for Neo4j container (`NEO-2-004`).

| Field | Notes |
|-------|-------|
| `requests.cpu`, `requests.memory` | Recommended for production. |
| `limits.memory` | Should match `requests.memory` (Neo4j heap stability). |

---

## `spec.jvm`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `useDefaults` | bool | `true` | Neo4j default JVM args (`NEO-3-003-JVM-01`). |
| `additionalArguments` | []string | `[]` | Extra JVM flags (`NEO-3-003-JVM-02`). |

---

## `spec.config`

| Field | Type | Description |
|-------|------|-------------|
| *(map)* | map[string]string | Neo4j config key-value pairs (`NEO-3-003-CFG-01`). Keys use Neo4j dot notation. |

Operator merges: defaults → user `config` → K8s-specific cluster discovery settings.

---

## Plugin model (BDR-004 Option E — accepted)

Pools and Standalone declare **which** plugins run (catalog id strings). **How** to install them — license Secret, version, plugin config — lives in `spec.pluginDefinitions`.

### Plugin placement by mode

| `topology.mode` | Where to declare plugins | Which plugins are allowed |
|-------------------|--------------------------|---------------------------|
| **Standalone** | `spec.plugins` | **All** catalog plugins (`apoc`, `gds`, `bloom`, …) |
| **Cluster** | `topology.primaries.plugins`, `topology.secondaries.analytics.plugins`, `topology.secondaries.read.plugins` | **`gds` / `bloom` only on `secondaries.analytics`** · forbidden on `primaries` and `secondaries.read` |

Neo4j does not support GDS on **Primary** members. In Cluster mode, `gds` / `bloom` belong on **`secondaries.analytics`** only.

### Assignment vs configuration

| Mode | Assignment (where) | Configuration |
|------|-------------------|---------------|
| `Cluster` | `topology.primaries.plugins[]`, `secondaries.analytics.plugins[]`, `secondaries.read.plugins[]` | `spec.pluginDefinitions` |
| `Standalone` | `spec.plugins[]` | `spec.pluginDefinitions` |

**Operator resolution:** for each plugin id in a pool’s `plugins[]` (or `spec.plugins[]` in Standalone), look up `pluginDefinitions[id]`, merge with built-in catalog defaults, install on all pods in that pool.

### Standalone

```yaml
spec:
  topology:
    mode: Standalone
  plugins: [apoc, gds]
  pluginDefinitions:
    apoc: {}
    gds:
      licenseSecretRef: gds-license
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `plugins` | []string | no | `[]` | **Standalone only.** Any catalog plugin id. **Forbidden when `mode: Cluster`.** |

### `spec.pluginDefinitions`

Map of plugin id → configuration. Keys must be known catalog ids (`apoc`, `gds`, …).

```yaml
pluginDefinitions:
  apoc: {}
  gds:
    licenseSecretRef: gds-license
    config:
      gds.enterprise.license_file: /licenses/gds.key
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `<id>` | PluginDefinition | no | — | Per-plugin config. Key must match catalog id. |
| `<id>.licenseSecretRef` | string | conditional | — | Kubernetes Secret name. **Required** for `gds`, `bloom` when that id is referenced. Same Secret mounted on every pod running the plugin. |
| `<id>.version` | string | no | `spec.version` | Plugin JAR version; major.minor must match Neo4j. |
| `<id>.config` | map[string]string | no | `{}` | Plugin-specific settings (e.g. `gds.enterprise.license_file` path inside the container). |

Empty object `{}` means **catalog defaults only** (typical for `apoc`).

### Catalog (V1)

| Id | License when referenced |
|----|-------------------------|
| `apoc` | No (`pluginDefinitions.apoc: {}` or omit) |
| `gds` | **Yes** — `pluginDefinitions.gds.licenseSecretRef` |
| `bloom` | **Yes** (V2) |

Placement constraints are defined by **mode** in [Plugin placement by mode](#plugin-placement-by-mode) above — not per-plugin on Standalone.

### Invariants ([BDR-004](../../decision-records/business/004-neo4j-plugin-topology.md))

1. **Standalone** — any catalog plugin may appear in `spec.plugins`.
2. **Cluster** — `gds` / `bloom` must not appear in `topology.primaries.plugins` or `secondaries.read.plugins`; they may appear on `secondaries.analytics.plugins`.
3. **Shared license Secret** — one `licenseSecretRef` per plugin id in `pluginDefinitions`; operator mounts it on every pod that runs that plugin.
4. **License renewal** — update Secret content or `licenseSecretRef`, then rolling restart GDS pods (Neo4j validates license at startup only).

### Validation summary

See [`validation.md`](validation.md) rules PLG-001…013.

Deprecated: inline `PluginSpec` on pools (Option D); `spec.plugins.<poolName>` map (Option C); `enabledOn` map (Option F).

---

## `spec.trust`

TLS / certificate management — embedded, not a separate CRD ([BDR-006](../../decision-records/business/006-tls-trust-model.md), **Option B — accepted**).

Mirrors Helm `ssl.{policy}.privateKey` / `publicCertificate` field names (`secretName`, `subPath`). CRD section is **`trust`** (not Helm's `ssl` key).

### Defaults

| Field | Default |
|-------|---------|
| `enabled` | `false` |
| `certManager.enabled` | `false` |
| `reload.enabled` | `false` |

### `spec.trust` (top-level)

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | bool | Master TLS toggle. |
| `reload.enabled` | bool | Injects `dbms.security.tls_reload_enabled` (`NEO-3-005-TLS-04`). |
| `certManager.enabled` | bool | Provision certs via cert-manager (opt-in). |
| `certManager.issuerRef.name` | string | Issuer name (required when cert-manager on). |
| `certManager.issuerRef.kind` | string | `Issuer` or `ClusterIssuer` (default `ClusterIssuer`). |

### `spec.trust.certificates.{bolt,https,cluster}`

**BYO mode** (`certManager.enabled: false`) — same shape as Helm `values.yaml`:

| Field | Type | Description |
|-------|------|-------------|
| `privateKey.secretName` | string | Secret holding the private key (`NEO-3-005-TLS-0x`). |
| `privateKey.subPath` | string | Key within Secret (default `private.key`). |
| `publicCertificate.secretName` | string | Secret holding the certificate. |
| `publicCertificate.subPath` | string | Key within Secret (default `public.crt`). |
| `clientAuth` | string | `None` \| `Optional` \| `Require` — maps to `dbms.ssl.policy.{policy}.client_auth`. Default: `None` (bolt/https); `Require` (cluster when policy enabled). |
| `trustedCerts.sources` | []ProjectedVolumeSource | Projected volume sources (Helm shape) — client/peer CA certs mounted under `…/{policy}/trusted/`. Required when `clientAuth` is `Optional` or `Require` on bolt/https (TLS-004). |

Both `privateKey.secretName` and `publicCertificate.secretName` required when a policy is enabled (BYO).

**cert-manager mode** (`certManager.enabled: true`) — per policy:

| Field | Type | Description |
|-------|------|-------------|
| `secretName` | string | Target Secret for issued `tls.crt` / `tls.key` (operator creates `Certificate`). |

Mutually exclusive per policy: BYO (`privateKey` + `publicCertificate`) **or** cert-manager (`secretName` only).

`clientAuth` and `trustedCerts` apply in **both** BYO and cert-manager modes.

Mount paths (operator): `/var/lib/neo4j/certificates/{policy}/private.key`, `public.crt`, and `trusted/*` — same as Helm `_ssl.tpl`.

`revokedCerts` — deferred V1.1 ([BDR-006](../../decision-records/business/006-tls-trust-model.md)).

---

## `spec.connectivity`

Networking and service exposure (`NEO-2-007`).

### `spec.connectivity.internal`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `true` | Headless + ClusterIP services for in-cluster access. |

### `spec.connectivity.external`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | External-facing Service. |
| `type` | string | `LoadBalancer` | `LoadBalancer` (V1 P0), `NodePort`, `ClusterIP`, `None`. |
| `annotations` | map | `{}` | Cloud LB annotations. |
| `loadBalancerSourceRanges` | []string | `[]` | Source IP restrictions. |
| `ports.bolt` | bool | `true` | Bolt 7687 (`NEO-3-007-PRT-03`). |
| `ports.http` | bool | `true` | HTTP 7474 (`NEO-3-007-PRT-01`). |
| `ports.https` | bool | `false` | HTTPS 7473 (`NEO-3-007-PRT-02`). |
| `ports.backup` | bool | `false` | Backup port (`NEO-3-007-PRT-04`, V1=No). |

### `spec.connectivity.multiCluster`

| Field | V1 | Description |
|-------|-----|-------------|
| `enabled` | `false` only | Multi-zone / multi-region (`NEO-3-007-MULTI-02`, deferred). |

---

## `spec.scheduling`

Pod placement (`NEO-2-008`).

| Field | Type | Description |
|-------|------|-------------|
| `nodeSelector` | map | Node labels (`NEO-3-008-SCH-01`). |
| `tolerations` | []Toleration | Taints (`NEO-3-008-SCH-03`). |
| `affinity.podAntiAffinity` | string | `soft` \| `hard` \| `custom` (`NEO-3-008-SCH-02`). |
| `affinity.custom` | Affinity | Full affinity when `custom`. |
| `topologySpreadConstraints` | []TopologySpreadConstraint | Spread across zones (`NEO-3-008-SCH-04`). |
| `priorityClassName` | string | Pod priority (`NEO-3-008-SCH-05`, V1=No). |

---

## `spec.podDisruptionBudget`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `true`* | Create PDB (`NEO-2-008`). |
| `minAvailable` | int \| string | `2`* | Minimum available pods during disruption. |

\*Default enabled when Cluster with ≥ 3 total members.

---

## `spec.probes`

| Field | Description |
|-------|-------------|
| `startup` | Startup probe overrides (`NEO-3-009-PROBE-02`). |
| `liveness` | Liveness probe overrides. |
| `readiness` | Readiness probe overrides. |

Empty → operator applies Neo4j-tuned defaults (`NEO-3-009-PROBE-01`).

---

## `spec.security`

| Field | Description |
|-------|-------------|
| `podSecurityContext` | `runAsUser: 7474`, `fsGroup: 7474`, etc. |
| `containerSecurityContext` | `runAsNonRoot`, `capabilities.drop: [ALL]`. |
| `serviceAccount.create` | Create dedicated SA (`NEO-3-008-SCH-06`). |
| `serviceAccount.annotations` | IRSA / Workload Identity annotations. |
| `networkPolicy.enabled` | Opt-in NetworkPolicy (V1 default `false`). |

---

## `spec.monitoring`

| Field | Type | Default | V1 | Description |
|-------|------|---------|-----|-------------|
| `prometheus.enabled` | bool | `false` | Yes | Expose Prometheus metrics (`NEO-3-015-MON-01`). |
| `serviceMonitor.enabled` | bool | `false` | Yes | Create ServiceMonitor (`NEO-3-015-MON-02`). |
| `serviceMonitor.interval` | string | `30s` | Yes | Scrape interval. |
| `serviceMonitor.labels` | map | `{}` | Yes | Labels for Prometheus Operator selector. |

---

## `spec.maintenance`

| Field | Type | Default | V1 | Description |
|-------|------|---------|-----|-------------|
| `offlineMode` | bool | `false` | Yes | Replace Neo4j process with sleep loop for maintenance (`NEO-3-017-MNT-01` partial). |

Full maintenance jobs (`NEO-2-017`) deferred to V2.

---

## `spec.podTemplate`

Escape hatch for advanced customization.

| Field | Description |
|-------|-------------|
| `initContainers` | Additional init containers. |
| `sidecars` | Sidecar containers. |
| `env` | Additional environment variables (merged, not replaced). |

Prefer `spec.additionalMounts` and `spec.secretMounts` for volumes — see [BDR-005](../../decision-records/business/005-storage-volume-mode.md). Raw `podTemplate` patches deferred V1 unless needed for container overrides.

Operator-owned keys cannot be overridden.

---

## Additional manifest examples

### Production HA cluster

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: prod
  namespace: graph-prod
spec:
  edition: enterprise
  version: "2026.05.0"
  license:
    accept: "yes"
  topology:
    mode: Cluster
    primaries:
      members: 3
    minimumMembers: 3
  volumes:
    data:
      mode: Dynamic
      dynamic:
        size: 100Gi
        storageClassName: gp3
  trust:
    enabled: true
    reload:
      enabled: true
    certManager:
      enabled: true
      issuerRef:
        name: letsencrypt-prod
        kind: ClusterIssuer
    certificates:
      bolt:
        secretName: prod-neo4j-bolt-tls
      https:
        secretName: prod-neo4j-https-tls
      cluster:
        secretName: prod-neo4j-cluster-tls
  connectivity:
    external:
      enabled: true
      type: LoadBalancer
      ports:
        bolt: true
        https: true
        http: false
  podDisruptionBudget:
    enabled: true
    minAvailable: 2
```

### Primary + analytics (GDS on secondary pool)

```yaml
spec:
  topology:
    mode: Cluster
    primaries:
      members: 1
      plugins: [apoc]
    secondaries:
      analytics:
        members: 1
        plugins: [gds]
    minimumMembers: 1
  pluginDefinitions:
    apoc: {}
    gds:
      licenseSecretRef: gds-license
```

Expect `status.conditions` entry `TopologyWarning` / `NonHA` — valid for dev/analytics, not production HA writes.

---

## OpenAPI / CRD generation notes

- Use kubebuilder markers on `api/v1beta1/neo4j_types.go` + `common_types.go`.
- Embed CEL from [`validation.md`](validation.md) in CRD `x-kubernetes-validations`.
- `scale` subresource: `specReplicasPath` → computed member path or dedicated `spec.topology` aggregate (implementation choice).
- Print columns: `Edition`, `Version`, `Mode`, `Ready`, `Age`.

---

## Traceability

| Document | Role |
|----------|------|
| [`validation.md`](validation.md) | Admission rules |
| [`status.md`](status.md) | Status subresource |
| [`example.yaml`](example.yaml) | Minimal Standalone sample |
| [`example-cluster.yaml`](example-cluster.yaml) | Cluster + pluginDefinitions (Option E) |
| `11-helm-mapping.md` | Helm → spec field mapping (to author) |
| `10-status-model.md` | Cross-CRD status index (to author) |
