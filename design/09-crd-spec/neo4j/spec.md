# `Neo4j` — spec reference

**API group**: `neo4j.com` · **Version**: `v1beta1` · **Kind**: `Neo4j`  
**Scope**: Namespaced · **Short name**: `n4j`  
**Subresources**: `status`, `scale`

**Sources**: [BDR-001](../../decision-records/business/001-single-neo4j-crd.md) · [BDR-002](../../decision-records/business/002-neo4j-crd-topology.md) · [BDR-004](../../decision-records/business/004-neo4j-plugin-topology.md) · [BDR-005](../../decision-records/business/005-v1-full-crd-scope.md)  
**Related**: [`validation.md`](validation.md) · [`status.md`](status.md) · [`example.yaml`](example.yaml) · [`example-gds-cluster.yaml`](example-gds-cluster.yaml)

---

## Resource overview

One `Neo4j` resource → one StatefulSet → N pods. Infra concerns are **embedded `spec` sections** — not separate CRDs ([BDR-001](../../decision-records/business/001-single-neo4j-crd.md)).

```
spec
├── edition, version, license
├── topology                    # BDR-002 — Standalone | Cluster (roles compose)
├── plugins, pluginDefinitions  # BDR-004 — role refs + central config
├── image
├── auth
├── persistence                 # ADR-003 — all volume roles
├── resources, jvm
├── config
├── trust
├── connectivity
├── scheduling, podDisruptionBudget, probes
├── security
├── monitoring
├── maintenance
└── podTemplate
```

---

## `spec.edition`

| Field | Type | Required | Values |
|-------|------|----------|--------|
| `edition` | string | **yes** | `community`, `enterprise` |

---

## `spec.version`

| Field | Type | Required | Immutable | Description |
|-------|------|----------|-----------|-------------|
| `version` | string | **yes** | no | Neo4j image tag (e.g. `2026.05.0`). Drives rolling upgrade. |

---

## `spec.license`

| Field | Type | Required | Values |
|-------|------|----------|--------|
| `accept` | string | **yes** | `no`, `yes`, `eval` |

Enterprise deployments require `yes` or `eval`.

---

## `spec.topology` (BDR-002)

Two modes. In **`Cluster`**, three **fixed server roles** compose freely — sized to match HA needs and **licensed plugin instance counts** (e.g. GDS on one analytics server).

### `spec.topology.mode`

| Value | Meaning | Pods |
|-------|---------|------|
| `Standalone` | Single Neo4j instance | `1` |
| `Cluster` | Multi-role cluster | `primaries + secondaries + analytics` |

**Immutable** after create.

### Server roles (Cluster only)

| Role | Field | Purpose |
|------|-------|---------|
| **Primary** | `primaries.members` | Transactional writes / quorum |
| **Secondary** | `secondaries.members` | Read scaling (Neo4j v5+ Secondary) |
| **Analytics** | `analytics.members` | Optional dedicated analytics servers (e.g. 1 primary + 1 analytics with GDS) — **not** the only role that may run GDS |

### `Standalone`

```yaml
topology:
  mode: Standalone
```

Forbidden: `primaries`, `secondaries`, `analytics`, `minimumMembers`.

### `Cluster`

```yaml
topology:
  mode: Cluster
  primaries:
    members: 3
  secondaries:
    members: 1
  analytics:
    members: 1
  minimumMembers: 3
```

| Field | Required | Default | Notes |
|-------|----------|---------|-------|
| `primaries.members` | **yes** | — | odd, ≥ 1 |
| `secondaries.members` | no | `0` | Enterprise when > 0 |
| `analytics.members` | no | `0` | Enterprise when > 0; set to GDS/Bloom **contract instance count** |
| `minimumMembers` | no | `primaries.members` | formation gate |

All three role counters may be non-zero in the same cluster — e.g. 3 primaries + 1 secondary + 1 analytics server (GDS license for one instance).

### Decision guide

| Use case | Spec |
|----------|------|
| Dev / CI | `mode: Standalone` |
| Standalone + GDS | `mode: Standalone`, `plugins: [gds, …]` |
| Production HA | `mode: Cluster`, `primaries.members: 3` |
| Read scaling | `secondaries.members: N` |
| GDS on primaries | `plugins.primaries: [apoc, gds]` |
| 1 primary + 1 GDS server (1 license) | `primaries: 1`, `analytics: 1`, `plugins.analytics: [gds]` |
| HA + dedicated GDS server | `primaries: 3`, `analytics: 1`, `plugins.analytics: [gds]` |

### Ordinal → role

```
[0 .. primaries-1]                          → primary
[primaries .. primaries+secondaries-1]      → secondary
[primaries+secondaries .. total-1]          → analytics
```

Helm mapping → [ADR-002](../../decision-records/architecture/002-helm-values-mapping.md).

---

## Plugins (BDR-004)

**Topology sizes roles** (`analytics.members` = licensed instance count). **Plugins declare what runs on each role.** **Configuration** (Secret, contract cap) lives in `pluginDefinitions`.

### Assignment by mode

| Mode | Location |
|------|----------|
| `Standalone` | `spec.plugins: [apoc, gds, …]` |
| `Cluster` | `spec.plugins.primaries`, `spec.plugins.secondaries`, `spec.plugins.analytics` |

### Placement rules

| Rule | Detail |
|------|--------|
| GDS / Bloom on any role | Allowed on `plugins.primaries`, `plugins.secondaries`, `plugins.analytics`, or flat `spec.plugins` (Standalone) |
| Role consistency | Plugin listed on `plugins.<role>` ⇒ that role must have `members ≥ 1` |
| Licensed plugins | `pluginDefinitions.<id>.licenseSecretRef` required when referenced |

### `spec.pluginDefinitions`

```yaml
pluginDefinitions:
  apoc: {}
  gds:
    licenseSecretRef: gds-license
    config:
      gds.enterprise.license_file: /licenses/gds.key
  bloom:
    licenseSecretRef: bloom-license
  apoc-extended:
    credentials:
      - alias: jdbc
        secretRef: jdbc-credentials
        mountPath: /secrets/jdbc
        key: URL
```

| Field | Description |
|-------|-------------|
| `licenseSecretRef` | Required for `gds`, `bloom` when referenced |
| `version` | Default `spec.version` |
| `config` | Plugin settings map |
| `credentials[]` | APOC Extended JDBC/ES — `alias`, `secretRef`, `mountPath`, `key` |

### V1 catalog

`apoc`, `gds`, `bloom`, `apoc-extended`

---

## `spec.image`

| Field | Default | Description |
|-------|---------|-------------|
| `registry` | `""` | Optional private registry |
| `repository` | `neo4j` | Image repository |
| `customImage` | — | Full image ref override (exclusive with repository+version) |
| `pullPolicy` | `IfNotPresent` | |
| `pullSecrets` | `[]` | |

Effective image: `{registry/}{repository}:{version}` unless `customImage` set.

---

## `spec.auth`

| Field | Default | Description |
|-------|---------|-------------|
| `generatePassword` | `true` if no secret ref | Operator generates Secret |
| `passwordSecretRef.name` | — | Existing Secret; key `NEO4J_AUTH` |
| `ldap.enabled` | `false` | LDAP authentication |
| `ldap.host` | — | LDAP server |
| `ldap.bindDN` | — | Bind DN |
| `ldap.baseDN` | — | Search base |
| `ldap.passwordSecretRef` | — | Bind password Secret |

When `passwordSecretRef` is set, `generatePassword` defaults to `false`.

---

## `spec.persistence` (ADR-003)

### Roles

`data` (required), `logs`, `metrics`, `import`, `backups`, `licenses`

### `spec.persistence.data` (required)

| Field | Default | Immutable | Description |
|-------|---------|-----------|-------------|
| `size` | — | no† | PVC size |
| `storageClassName` | cluster default | no | |
| `accessMode` | `ReadWriteOnce` | yes | |
| `existingClaim` | — | yes | Bind existing PVC |
| `selector` | — | yes | Pre-provisioned PV selector |

†Expansion allowed; shrink blocked.

### Auxiliary roles

Default: `shareWith: data`. Or dedicated volume:

```yaml
persistence:
  logs:
    shareWith: data
  import:
    size: 50Gi
    storageClassName: gp3
  licenses:
    size: 1Gi
```

| Field | Description |
|-------|-------------|
| `shareWith` | `data` — subpath on data volume |
| `size`, `storageClassName`, `accessMode` | Dedicated PVC |
| `existingClaim` | Bind existing PVC |
| `subPathDisabled` | Disable subPath expression (Helm parity) |

---

## `spec.resources` · `spec.jvm`

Standard Kubernetes `resources` for the Neo4j container.

| `jvm.useDefaults` | Default `true` — Neo4j default JVM args |
| `jvm.additionalArguments` | Extra JVM flags |

---

## `spec.config`

`map[string]string` — Neo4j config keys in dot notation. Operator merges: defaults → user `config` → cluster discovery (operator-owned, not overridable).

---

## `spec.trust`

| Field | Description |
|-------|-------------|
| `enabled` | Master TLS toggle |
| `certManager.enabled` | Provision via cert-manager |
| `certManager.issuerRef` | Issuer name + kind |
| `certificates.bolt\|https\|cluster` | `secretRef`, `trustedCerts[]`, `revokedCerts[]` |
| `reload.enabled` | Auto-reload on cert rotation |

`trustedCerts` / `revokedCerts` use projected volume source shape (Helm parity).

---

## `spec.connectivity`

### `spec.connectivity.internal`

| `enabled` | Default `true` — headless + ClusterIP services |

### `spec.connectivity.external`

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | External Service |
| `type` | `LoadBalancer` | `LoadBalancer`, `NodePort`, `ClusterIP`, `None` |
| `annotations` | `{}` | Cloud LB annotations |
| `loadBalancerSourceRanges` | `[]` | |
| `ports.bolt` | `true` | |
| `ports.http` | `true` | |
| `ports.https` | `false` | Requires `trust.enabled` |
| `ports.backup` | `false` | Backup port |

### `spec.connectivity.multiCluster`

| Field | Description |
|-------|-------------|
| `enabled` | Multi-zone / multi-region discovery |
| `advertisedAddresses` | Per-member advertised addresses |

---

## `spec.scheduling`

| Field | Description |
|-------|-------------|
| `nodeSelector` | Node labels |
| `tolerations` | Taints |
| `affinity.podAntiAffinity` | `soft` \| `hard` \| `custom` |
| `affinity.custom` | Full Affinity when `custom` |
| `topologySpreadConstraints` | Zone spread |
| `priorityClassName` | Pod priority |

---

## `spec.podDisruptionBudget`

| Field | Default |
|-------|---------|
| `enabled` | `true` when mode ≠ Standalone and total members ≥ 3 |
| `minAvailable` | `2` when enabled |

---

## `spec.probes`

Optional overrides for `startup`, `liveness`, `readiness`. Empty → Neo4j-tuned TCP defaults.

---

## `spec.security`

| Field | Description |
|-------|-------------|
| `podSecurityContext` | Default `runAsUser: 7474`, `fsGroup: 7474` |
| `containerSecurityContext` | `runAsNonRoot`, drop ALL |
| `serviceAccount.create` | Create dedicated SA |
| `serviceAccount.annotations` | IRSA / Workload Identity |
| `networkPolicy.enabled` | Opt-in NetworkPolicy |

---

## `spec.monitoring`

| Field | Default | Description |
|-------|---------|-------------|
| `prometheus.enabled` | `false` | Expose Prometheus metrics |
| `serviceMonitor.enabled` | `false` | Create ServiceMonitor |
| `serviceMonitor.interval` | `30s` | |
| `serviceMonitor.labels` | `{}` | |
| `serviceMonitor.port` | auto | |
| `serviceMonitor.path` | auto | |
| `serviceMonitor.namespaceSelector` | `{}` | |

---

## `spec.maintenance`

| Field | Description |
|-------|-------------|
| `offlineMode` | Replace Neo4j with sleep loop for admin tasks |
| `jobs[]` | `{ type: dump\|load\|report, schedule?, storageRef? }` — maps to neo4j-admin / operations |

---

## `spec.podTemplate`

| Field | Description |
|-------|-------------|
| `initContainers` | Additional init containers |
| `sidecars` | Sidecar containers |
| `additionalVolumes` | Extra volumes |
| `env` | Additional env vars (merged) |

Operator-owned keys cannot be overridden.

---

## Examples

### Production HA cluster

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: prod
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
  persistence:
    data:
      size: 100Gi
      storageClassName: gp3
  trust:
    enabled: true
  connectivity:
    external:
      enabled: true
      ports:
        bolt: true
        https: true
```

### 1 primary + 1 analytics with GDS

See [`example-gds-cluster.yaml`](example-gds-cluster.yaml).

---

## OpenAPI notes

- Kubebuilder markers on `api/v1beta1/neo4j_types.go` + `common_types.go`
- CEL from [`validation.md`](validation.md)
- Print columns: `Edition`, `Version`, `Mode`, `Ready`, `Age`
- Scale subresource → [ADR-004](../../decision-records/architecture/004-scale-subresource.md)

---

## Traceability

| Document | Role |
|----------|------|
| [`validation.md`](validation.md) | Admission rules |
| [`status.md`](status.md) | Status subresource |
| [ADR-002](../../decision-records/architecture/002-helm-values-mapping.md) | Helm → spec |
| [ADR-003](../../decision-records/architecture/003-persistence-model.md) | Persistence |
