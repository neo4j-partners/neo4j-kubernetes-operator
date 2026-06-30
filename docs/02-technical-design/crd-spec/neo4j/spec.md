# `Neo4j` — spec reference

**API group**: `neo4j.com` · **Version**: `v1beta1` · **Kind**: `Neo4j`  
**Scope**: Namespaced · **Short name**: `n4j`  
**Subresources**: `status`, `scale` (optional — maps to `<name>-read` STS when read pool exists; [BDR-009](../../decision-records/business/009-scale-pool-ordinal-semantics.md))

**Sources**: [BDR-001](../../decision-records/business/001-single-neo4j-crd.md) · [BDR-002](../../decision-records/business/002-neo4j-crd-topology.md) · [BDR-004](../../decision-records/business/004-neo4j-plugin-topology.md) (**Option E — accepted**) · [`20-operator-proposal.md`](../../20-operator-proposal.md) §3.1  
**Related**: [`validation.md`](validation.md) · [`status.md`](status.md) · [`example.yaml`](example.yaml)

> **Conflict rule**: where this document disagrees with `20-operator-proposal.md`, **[BDR-002](../../decision-records/business/002-neo4j-crd-topology.md) wins** for topology, **[BDR-004](../../decision-records/business/004-neo4j-plugin-topology.md) Option E — accepted** wins for plugins, and **[BDR-005](../../decision-records/business/005-storage-volume-mode.md) wins** for storage naming (`spec.volumes`, not `persistence`).

**FR coverage**: `NEO-1-001`, `NEO-1-002`, `NEO-2-003`…`NEO-2-016`, `NEO-2-018`

---

## Resource overview

Primary workload CRD. One `Neo4j` resource → **one StatefulSet** (Standalone) or **up to three pool StatefulSets** (Cluster — [BDR-009](../../decision-records/business/009-scale-pool-ordinal-semantics.md) Option B). Infra concerns are **embedded `spec` sections** — not separate CRDs ([BDR-001](../../decision-records/business/001-single-neo4j-crd.md)).

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
├── features                           # BDR-007 — backup, monitoring (prometheus, serviceMonitor)
├── config
├── trust                              # TLS certs + mTLS — separate from connectivity
├── connectivity                       # BDR-007 — ports (listen) + services + ingress + clusterDomain + multiCluster
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

**Workloads** ([BDR-009](../../decision-records/business/009-scale-pool-ordinal-semantics.md) Option B):

| Mode | StatefulSet(s) | Replicas |
|------|----------------|----------|
| `Standalone` | `<name>` | `1` |
| `Cluster` | `<name>-primary` | `primaries.members` |
| `Cluster` | `<name>-analytics` | `secondaries.analytics.members` (omit pool or `0` → no STS) |
| `Cluster` | `<name>-read` | `secondaries.read.members` (omit pool or `0` → no STS) |

Ordinals are **per pool** (`0 .. members-1`). Example pod: `prod-read-2`.

**`scale` subresource (V1):** when `secondaries.read` is configured, maps to `<name>-read` STS replica count; otherwise scale via `spec.topology.*.members`.

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

Structured JVM flags → `server.jvm.additional` in `neo4j.conf` ([BDR-008](../../decision-records/business/008-neo4j-config-surface.md)).

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `useDefaults` | bool | `true` | Neo4j default JVM args (`NEO-3-003-JVM-01`). |
| `additionalArguments` | []string | `[]` | Extra JVM flags (`NEO-3-003-JVM-02`). |

---

## `spec.config`

Free-form `map[string]string` → **`neo4j.conf`** — **Option A** ([BDR-008](../../decision-records/business/008-neo4j-config-surface.md)): Helm `config` drop-in; any `neo4j.conf` key allowed except the **reserved denylist**. Do **not** put `apoc.*` keys here — use [`spec.apoc`](#specapoc).

| Field | Type | Description |
|-------|------|-------------|
| *(map)* | map[string]string | Neo4j config key-value pairs (`NEO-3-003-CFG-01`). Keys use Neo4j dot notation. |

Operator merges: defaults → user `config` → K8s-specific cluster discovery settings. Admission **rejects reserved keys** (topology, `connectivity.listeners`, `trust`, `server.jvm.additional`, `apoc.*`) — see [BDR-008](../../decision-records/business/008-neo4j-config-surface.md) and [CFG-LISTENER-*](validation.md). All other keys pass through; invalid values fail at Neo4j startup.

---

## `spec.apoc`

Free-form `map[string]string` → **`apoc.conf`** (separate file from `neo4j.conf`) — **Option A** ([BDR-008](../../decision-records/business/008-neo4j-config-surface.md)): Helm `apoc_config` drop-in.

| Field | Type | Description |
|-------|------|-------------|
| *(map)* | map[string]string | APOC config key-value pairs (`NEO-3-003-APOC-01`). |

Rendered **only** when APOC is assigned (`spec.plugins` or pool `plugins` includes `apoc`). Admission rejects `neo4j.conf` keys (`server.*`, `dbms.*`, `db.*`) and `server.jvm.additional` in this map. JDBC/ES credential URLs → `pluginDefinitions.apoc.credentials` (`NEO-3-003-APOC-02`).

---

## `spec.features`

Optional workload capabilities ([BDR-007](../../decision-records/business/006-service-exposure-connectivity.md) Option E — **accepted**; catalog [BDR-010](../../decision-records/business/010-neo4j-features-catalog.md) **Option C proposed**).

Each feature has three layers: **intent** (`enabled`), **mechanism** (`connectivity` / `trust` / `volumes`), **tuning** (fields below, mirroring `neo4j.conf`). The same tuning keys may also appear in `spec.config` for Helm migration — validation enforces coherence (CFG-FEAT-*).

### `features.backup`

Enterprise online backup ([BDR-010](../../decision-records/business/010-neo4j-features-catalog.md)).

| Field | Type | Default | `neo4j.conf` key | Notes |
|-------|------|---------|------------------|-------|
| `enabled` | bool | `false` | `server.backup.enabled` | Requires `connectivity.listeners.backup` set. Backup TLS → `spec.trust`. Storage → `spec.volumes.backups`. |

### `features.monitoring`

| Group | Field | Type | Default | `neo4j.conf` key |
|-------|-------|------|---------|------------------|
| `prometheus` | `enabled` | bool | `false` | `server.metrics.prometheus.enabled` |
| `prometheus` | `endpoint` | string | `localhost:2004` | `server.metrics.prometheus.endpoint` — port MUST match `connectivity.listeners.metrics` |
| `csv` | `enabled` | bool | `true` | `server.metrics.csv.enabled` |
| `csv` | `interval` | string | `30s` | `server.metrics.csv.interval` |
| `csv.rotation` | `keepNumber` | int | `7` | `server.metrics.csv.rotation.keep_number` |
| `csv.rotation` | `size` | string | `10MiB` | `server.metrics.csv.rotation.size` |
| `csv.rotation` | `compression` | string | `NONE` | `server.metrics.csv.rotation.compression` — `NONE`, `ZIP`, `GZ` |
| `jmx` | `enabled` | bool | `true` | `server.metrics.jmx.enabled` |
| `graphite` | `enabled` | bool | `false` | `server.metrics.graphite.enabled` |
| `graphite` | `server` | string | `localhost:2003` | `server.metrics.graphite.server` |
| `graphite` | `interval` | string | `30s` | `server.metrics.graphite.interval` |
| `graphite` | `prefix` | string | `neo4j` | `server.metrics.prefix` |
| `serviceMonitor` | `enabled` | bool | `false` | — (K8s CR) |
| `serviceMonitor` | `interval` | string | `30s` | — |
| `serviceMonitor` | `labels` | map | `{}` | — |
| `serviceMonitor` | `jobLabel` | string | `""` | — |
| `serviceMonitor` | `port` | string | `tcp-prometheus` | — |
| `serviceMonitor` | `path` | string | operator default | — |
| `serviceMonitor` | `namespaceSelector` | object | `{}` | — |
| `serviceMonitor` | `targetLabels` | []string | `[]` | — |
| `serviceMonitor` | `selector` | object | operator default | — |

`server.directories.metrics` is operator-injected when `spec.volumes.metrics` is set ([BDR-005](../../decision-records/business/005-storage-volume-mode.md)) — not a `features` field.

Replaces former top-level `spec.monitoring`.

---

## `spec.connectivity`

All reachability configuration — Neo4j listen ports and Kubernetes exposure ([BDR-007](../../decision-records/business/006-service-exposure-connectivity.md) Option E + Amendments B–F).

**Naming:** `connectivity.listeners` = Neo4j **listen** port per connector (integer); `connectivity.service.expose` = which connectors are on the client Service; `connectivity.service.ports` = optional Service **façade** ports (e.g. HTTP 80 → 7474).

**Derived (not in spec):** **admin** (backup OR prometheus OR Cluster), **internals** (Cluster).

### `spec.connectivity.listeners`

**Neo4j process** — each connector is an **integer port** (1–65535). Key present ⇒ enabled; key absent ⇒ operator default; `null` ⇒ explicitly disabled.

Operator sets `neo4j.conf`, `containerPort`, and every Service `targetPort` from `connectivity.listeners.{name}`.

| Connector | Default when absent | Port |
|-----------|---------------------|------|
| `bolt` | enabled | 7687 |
| `http` | enabled | 7474 |
| `https` | disabled | 7473 |
| `backup` | disabled | 6362 |
| `metrics` | disabled | 2004 |

**HTTPS / Bolt TLS:** certificate material and mTLS live in [`spec.trust`](#spectrust). `connectivity.listeners.https` set requires `trust` with https certs ([BDR-011](../../decision-records/business/011-https-connector-tls-coupling.md)).

Cluster ports (7688, 5000, 7000, 6000) on **internals** — operator-injected when `mode: Cluster`.

Do **not** duplicate connector settings in `spec.config` (`server.http.listen_address`, `server.*.enabled`, …) — see [CFG-LISTENER-*](validation.md).

### `spec.connectivity.service`

Client Service — merges Helm `services.default` + `services.neo4j`. **No `neo4j` key** — one client Service per CR.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | `ClusterIP` | `ClusterIP`, `LoadBalancer`, `NodePort`. |
| `annotations` | map | `{}` | Service metadata. |
| `loadBalancerSourceRanges` | []string | `[]` | When `type: LoadBalancer`. |
| `expose` | []string | `[bolt, http]` | Connector names published on this Service. |
| `ports.{name}` | int | = `listeners.{name}` | Optional Service port (LB façade); `targetPort` = listen port. |

CEL: each name in `expose` ⇒ `connectivity.listeners.{name}` is set (non-null). **`service.expose` ∩ `reverseProxy.expose` must be empty** (NET-005).

### `spec.connectivity.reverseProxy`

Optional HTTP/Bolt-ws front door ([BDR-007](../../decision-records/business/006-service-exposure-connectivity.md) Amendment F — **V1.1+**, deferred in MVP). Operator-managed Deployment + Service; upstream = client Service. Helm `neo4j-reverse-proxy` parity.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Create reverse-proxy workload. |
| `image` | string | catalog default | Proxy container image. |
| `expose` | []string | `[]` | Connectors via proxy (`http` typical). |
| `resources` | object | chart defaults | CPU/memory. |
| `service.type` | string | `ClusterIP` | Proxy front Service. |
| `service.ports.{name}` | int | `80` / `443` | Port Ingress uses when `backend: reverseProxy`. |

### `spec.connectivity.ingress`

Per-host rules with explicit **backend** (`service` | `reverseProxy`). Rule hosts feed cert-manager SANs when `trust.certManager.includeIngressHosts: true` ([BDR-006](../../decision-records/business/007-tls-trust-model.md)).

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Create Ingress (**V1.1+**). |
| `className` | string | — | `ingressClassName`. |
| `annotations` | map | `{}` | Controller metadata (cert-manager, ALB, …). |
| `tls` | []object | `[]` | `{ hosts, secretName }` per TLS block. |
| `rules` | []object | `[]` | Per-host routing. |
| `rules[].host` | string | — | HTTP host. |
| `rules[].paths` | []object | — | Path rules. |
| `rules[].paths[].path` | string | `/` | URL path. |
| `rules[].paths[].pathType` | string | `Prefix` | `Prefix`, `Exact`, `ImplementationSpecific`. |
| `rules[].paths[].backend` | string | — | `service` (client Service) or `reverseProxy`. |
| `rules[].paths[].port` | string | — | Connector name on chosen backend (`bolt`, `http`, …). |

CEL: `backend: reverseProxy` ⇒ `reverseProxy.enabled` (NET-006).

### `spec.connectivity.clusterDomain`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| *(scalar)* | string | `cluster.local` | Kubernetes DNS suffix for operator-built FQDNs. |

### `spec.connectivity.multiCluster`

| Field | V1 | Description |
|-------|-----|-------------|
| `enabled` | `false` only | Helm `services.neo4j.multiCluster` — expose cluster ports on client Service (`NEO-3-007-MULTI-02`, deferred). |

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
| `certManager.includeIngressHosts` | `false` |
| `reload.enabled` | `false` |

### `spec.trust` (top-level)

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | bool | Master TLS toggle. |
| `reload.enabled` | bool | Injects `dbms.security.tls_reload_enabled` (`NEO-3-005-TLS-04`). |
| `certManager.enabled` | bool | Provision certs via cert-manager (opt-in). |
| `certManager.issuerRef.name` | string | Issuer name (required when cert-manager on). |
| `certManager.issuerRef.kind` | string | `Issuer` or `ClusterIssuer` (default `ClusterIssuer`). |
| `certManager.includeIngressHosts` | bool | When `true`, merge hosts from `spec.connectivity.ingress.rules[].host` into bolt/https Certificate `dnsNames` (default `false`). |
| `certManager.dnsNames` | []string | Extra SANs for bolt/https Certificates (union with operator-derived and ingress hosts). |

**cert-manager `dnsNames` assembly** (operator): in-cluster Service/pod DNS (always) ∪ `certManager.dnsNames` ∪ union of `connectivity.ingress.rules[].host` (if `includeIngressHosts`) ∪ per-policy `certificates.{policy}.dnsNames`. Cluster policy excludes ingress hosts. See [BDR-006](../../decision-records/business/007-tls-trust-model.md) § cert-manager DNS derivation.

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
| `dnsNames` | []string | Optional extra SANs for this policy's Certificate (cert-manager mode). |

Mutually exclusive per policy: BYO (`privateKey` + `publicCertificate`) **or** cert-manager (`secretName` only).

`clientAuth` and `trustedCerts` apply in **both** BYO and cert-manager modes.

Mount paths (operator): `/var/lib/neo4j/certificates/{policy}/private.key`, `public.crt`, and `trusted/*` — same as Helm `_ssl.tpl`.

`revokedCerts` — deferred V1.1 ([BDR-006](../../decision-records/business/006-tls-trust-model.md)).

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

**Moved to** [`spec.features.monitoring`](#specfeatures) (Option E). Use `features.monitoring.prometheus` and `features.monitoring.serviceMonitor`.

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
      includeIngressHosts: true
    certificates:
      bolt:
        secretName: prod-neo4j-bolt-tls
      https:
        secretName: prod-neo4j-https-tls
      cluster:
        secretName: prod-neo4j-cluster-tls
  features:
    backup:
      enabled: true
    monitoring:
      prometheus:
        enabled: true
  connectivity:
    listeners:
      bolt: 7687
      http: 7474
      https: 7473
    service:
      type: LoadBalancer
      expose:
        - bolt
        - http
        - https
      ports:
        http: 80
        https: 443
    ingress:
      enabled: true
      className: nginx
      hosts:
        - neo4j.example.com
    clusterDomain: cluster.local
    multiCluster:
      enabled: false
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
- `scale` subresource: `specReplicasPath` → `spec.topology.secondaries.read.members` when read pool configured; `statusReplicasPath` → `<name>-read` STS `.status.replicas` ([BDR-009](../../decision-records/business/009-scale-pool-ordinal-semantics.md)).
- Print columns: `Edition`, `Version`, `Mode`, `Ready`, `Age`.

---

## Traceability

| Document | Role |
|----------|------|
| [`validation.md`](validation.md) | Admission rules |
| [`status.md`](status.md) | Status subresource |
| [`example.yaml`](example.yaml) | Minimal Standalone sample |
| [`example-cluster.yaml`](example-cluster.yaml) | Cluster + pluginDefinitions + volumes + connectivity |
| `11-helm-mapping.md` | Helm → spec field mapping (to author) |
| `10-status-model.md` | Cross-CRD status index (to author) |
