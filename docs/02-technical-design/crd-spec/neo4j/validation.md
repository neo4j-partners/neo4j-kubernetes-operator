# `Neo4j` — validation rules

**API**: `neo4j.com/v1beta1`  
**Sources**: [BDR-002](../../decision-records/business/neo4j/002-neo4j-crd-topology.md) · [BDR-004](../../decision-records/business/neo4j/004-neo4j-plugin-topology.md) (**Option E — accepted**) · [BDR-009](../../decision-records/business/neo4j/009-scale-pool-ordinal-semantics.md) (**Option B — accepted**) · [BDR-010](../../decision-records/business/neo4j/010-neo4j-features-catalog.md) (**Option C — accepted**) · [ADR-001](../../decision-records/architecture/001-crd-validation-process.md) · [`spec.md`](spec.md)

**Mechanisms**:

| Mechanism | When |
|-----------|------|
| **CEL** (`x-kubernetes-validations`) | Structural rules, enum checks, cross-field guards — cheap, in CRD OpenAPI. |
| **Validating webhook** | Edition/license, storage class existence, scale-in policy, analytics config coherence. |
| **Reconciler** | Runtime cluster state, topology warnings → `status.conditions` (not admission). |

---

## Topology (BDR-002 + BDR-009)

Per-pool StatefulSets ([BDR-009](../../decision-records/business/009-scale-pool-ordinal-semantics.md)): each `topology.*.members` scales independently at the pool's STS tail — no cross-pool ordinal reassignment.

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| TOPO-001 | `mode: Standalone` → `primaries`, `secondaries`, `minimumMembers` absent | Error | CEL | `members` fields not allowed when `mode` is `Standalone` |
| TOPO-002 | `mode: Cluster` → `primaries.members` required | Error | CEL | `primaries.members` is required when `mode` is `Cluster` |
| TOPO-003 | `secondaries` without `primaries` | Error | CEL | `primaries.members` must be set before secondaries |
| TOPO-004 | `secondaries` when `mode: Standalone` | Error | CEL | Secondaries require `mode: Cluster` |
| TOPO-005 | `gds` or `bloom` in `secondaries.read.plugins` | Error | CEL | GDS/Bloom must use secondaries.analytics pool |
| TOPO-006 | `primaries.members` even and > 0 | Error | CEL | Primary count must be odd for quorum |
| TOPO-007 | `secondaries.analytics` or `secondaries.read` present with `members < 1` | Error | CEL | pool members must be at least 1 when pool is configured |
| TOPO-008 | `minimumMembers` when `mode: Standalone` | Error | CEL | `minimumMembers` not allowed in Standalone |
| TOPO-009 | `minimumMembers > total members` | Error | Webhook | `minimumMembers` cannot exceed total member count |
| TOPO-010 | Primary scale-in below quorum / unsafe pool scale-in | Error | Webhook | Scale-in would break primary quorum or remove members before drain completes |
| TOPO-011 | `primaries.members: 1` + any secondary pool | Warning | Reconciler | Non-HA topology |
| TOPO-012 | `primaries.members < 3` | Warning | Reconciler | For HA production use `primaries.members ≥ 3` |
| TOPO-013 | `mode` immutable | Error | CEL | `topology.mode` cannot change |

### CEL sketches (topology)

```yaml
# TOPO-001 — Standalone forbids member blocks
- rule: |
    !(self.topology.mode == 'Standalone') ||
    !has(self.topology.primaries) && !has(self.topology.secondaries) &&
    !has(self.topology.minimumMembers)
  message: members fields are not allowed when mode is Standalone

# TOPO-002 — Cluster requires primaries.members
- rule: |
    self.topology.mode != 'Cluster' || (
      has(self.topology.primaries) && has(self.topology.primaries.members) &&
      self.topology.primaries.members >= 1
    )
  message: primaries.members is required when mode is Cluster

# TOPO-005 — GDS/Bloom only on analytics pool
- rule: |
    !has(self.topology.secondaries) || !has(self.topology.secondaries.read) ||
    !has(self.topology.secondaries.read.plugins) ||
    self.topology.secondaries.read.plugins.all(p, p != 'gds' && p != 'bloom')
  message: GDS and Bloom must be declared on secondaries.analytics, not secondaries.read

# TOPO-006 — odd primary count
- rule: |
    !has(self.topology.primaries) || self.topology.primaries.members == 0 ||
    self.topology.primaries.members % 2 == 1
  message: primary count must be odd for quorum
```

---

## Plugins (BDR-004 Option E — accepted)

**Placement rule:** Standalone → all plugins on `spec.plugins`. Cluster → plugins on `primaries`, `secondaries.analytics`, `secondaries.read`; `gds` / `bloom` only on `secondaries.analytics`.

Plugin **assignment** is `[]string` catalog ids on `spec.plugins` (Standalone), `topology.primaries.plugins`, `topology.secondaries.analytics.plugins`, or `topology.secondaries.read.plugins`. **Configuration** is `spec.pluginDefinitions.<id>`.

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| PLG-001 | `gds` or `bloom` in `topology.primaries.plugins` when `mode: Cluster` | Error | CEL | GDS/Bloom cannot be installed on primary members (allowed on Standalone via spec.plugins) |
| PLG-002 | `spec.plugins` when `mode: Cluster` | Error | CEL | use topology.primaries.plugins and secondaries.<pool>.plugins in cluster mode |
| PLG-003 | `topology.primaries.plugins` or `secondaries.*.plugins` when `mode: Standalone` | Error | CEL | use spec.plugins in standalone mode |
| PLG-004 | `gds` or `bloom` referenced but missing `pluginDefinitions.<id>.licenseSecretRef` | Error | CEL | licensed plugin requires licenseSecretRef in pluginDefinitions |
| PLG-005 | duplicate id in same `plugins[]` list | Error | CEL | duplicate plugin id |
| PLG-006 | `pluginDefinitions.<id>.version` major.minor ≠ `spec.version` | Error | Webhook | plugin version must match Neo4j |
| PLG-007 | unknown catalog id in any `plugins[]` list | Error | CEL | V1 catalog: apoc, gds |
| PLG-008 | `secondaries.analytics` references `gds` without analytics config | Error | Webhook | GDS on analytics pool requires analytics server configuration |
| PLG-009 | `pluginDefinitions` key not in catalog | Error | CEL | unknown pluginDefinitions key |
| PLG-010 | `licenseSecretRef` on licensed plugin must reference existing Secret | Error | Webhook | license secret not found |
| PLG-011 | unused `pluginDefinitions` key (not referenced anywhere) | Warning | Reconciler | pluginDefinitions entry is unused |
| PLG-012 | GDS license Secret changed | — | Reconciler | rolling restart required on pods running gds |
| PLG-013 | homogeneous `topology.primaries.plugins` across all primary ordinals | Error | Reconciler | primary plugin set must be identical on every primary member |

### CEL sketches (plugins)

```yaml
# PLG-001 — no GDS/Bloom on primaries (Cluster only; Standalone uses spec.plugins)
- rule: |
    self.topology.mode != 'Cluster' ||
    !has(self.topology.primaries) || !has(self.topology.primaries.plugins) ||
    self.topology.primaries.plugins.all(p, p != 'gds' && p != 'bloom')
  message: GDS and Bloom cannot be installed on primary members in Cluster mode

# PLG-002 — no spec.plugins in Cluster mode
- rule: |
    self.topology.mode != 'Cluster' || !has(self.plugins)
  message: spec.plugins is not allowed when mode is Cluster

# PLG-004 — gds referenced ⇒ licenseSecretRef in pluginDefinitions
- rule: |
    !(
      (has(self.topology.primaries) && has(self.topology.primaries.plugins) &&
       self.topology.primaries.plugins.exists(p, p == 'gds')) ||
      (has(self.topology.secondaries) && has(self.topology.secondaries.analytics) &&
       has(self.topology.secondaries.analytics.plugins) &&
       self.topology.secondaries.analytics.plugins.exists(p, p == 'gds')) ||
      (has(self.plugins) && self.plugins.exists(p, p == 'gds'))
    ) || (
      has(self.pluginDefinitions) && has(self.pluginDefinitions.gds) &&
      has(self.pluginDefinitions.gds.licenseSecretRef) &&
      self.pluginDefinitions.gds.licenseSecretRef != ''
    )
  message: gds requires pluginDefinitions.gds.licenseSecretRef when referenced
```

---

## Edition & license

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| EDT-001 | `edition` must be `enterprise` in V1 | Error | CEL | V1 supports Enterprise edition only |
| EDT-002 | `license.accept` must be `yes` or `eval` | Error | CEL | Enterprise license must be explicitly accepted |
| EDT-003 | any `secondaries` with `members > 0` requires `edition: enterprise` | Error | CEL | secondary pools require Enterprise edition |
| EDT-004 | any pool references `gds` in `plugins` requires `edition: enterprise` | Error | CEL | GDS requires Enterprise edition |
| EDT-005 | `secondaries.analytics` with `gds` in `plugins` requires analytics-capable server config | Error | Webhook | GDS on analytics pool requires analytics server configuration |
| EDT-006 | `mode: Cluster` requires `edition: enterprise` | Error | CEL | Cluster mode requires Enterprise edition |

---

## Identity & version

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| VER-001 | `version` required, semver-compatible Neo4j tag | Error | CEL | `spec.version` is required |
| VER-002 | Downgrade `version` blocked | Error | Webhook | Neo4j version downgrade is not supported |
| VER-003 | `version` change triggers upgrade preflight | — | Reconciler | (no admission block; preflight in domain) |

---

## Persistence

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| STO-001 | `volumes.data.dynamic.size` required when `mode: Dynamic` | Error | CEL | data volume size is required |
| STO-002 | `volumes.data.dynamic.size` must be valid quantity | Error | CEL | invalid storage size |
| STO-003 | `dynamic.storageClassName` must exist when set | Error | Webhook | StorageClass not found |
| STO-004 | Shrink `volumes.data.dynamic.size` blocked | Error | Webhook | PVC expansion only — shrinking not supported |
| STO-005 | `volumes.data.mode` must not be `Share` | Error | CEL | data volume cannot use Share mode |
| STO-006 | `Existing` requires exactly one of `claimName`, `volume`, `volumeClaimTemplate` | Error | CEL | invalid existing volume binding |
| STO-007 | `mode: Share` on aux requires `shareFrom: data` (V1) | Error | CEL | invalid shareFrom |
| STO-008 | `additionalMounts[].name` unique in pod | Error | CEL | duplicate additional mount name |
| STO-009 | `mountPath` must not overlap reserved paths (`/data`, `/var/lib/neo4j/certificates/`) | Error | Webhook | reserved mount path |
| STO-010 | `secretMounts.*.secretName` must exist | Error | Webhook | secretMounts secret not found |
| STO-005 | `accessMode` must be `ReadWriteOnce` for data (V1) | Error | CEL | V1 data volume supports ReadWriteOnce only |

---

## Authentication

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| AUTH-001 | `generatePassword: true` XOR valid `passwordSecretRef` | Error | CEL | provide generatePassword or passwordSecretRef, not both |
| AUTH-002 | `passwordSecretRef` must reference existing Secret | Error | Webhook | password secret not found |
| AUTH-003 | `ldap.enabled: true` requires `ldap.passwordSecretRef` | Error | CEL | LDAP requires password secret (V2 — NEO-3-004-SEC-02) |

---

## Trust / TLS

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| TLS-001 | `trust.certManager.enabled: true` requires `issuerRef` | Error | CEL | cert-manager issuerRef is required |
| TLS-002 | BYO: `privateKey.secretName` + `publicCertificate.secretName` must exist when policy enabled | Error | Webhook | TLS secret not found |
| TLS-002b | BYO: both key and cert `secretName` required per enabled policy | Error | CEL | missing TLS certificate pairing |
| TLS-002c | cert-manager: `certificates.{policy}.secretName` required when `certManager.enabled` | Error | CEL | cert-manager target secretName required |
| TLS-002d | Per policy: BYO shape XOR cert-manager `secretName` (not both) | Error | CEL | invalid trust certificate shape |
| TLS-003 | `mode: Cluster` + `trust.enabled` → cluster TLS material required | Error | CEL | cluster TLS is required for clustered deployments |
| TLS-004 | bolt/https: `clientAuth` `Optional` or `Require` → `trustedCerts.sources` non-empty | Error | CEL | mTLS requires trustedCerts sources |
| TLS-005 | `mode: Cluster` + cluster policy enabled → `clientAuth` cannot be `None` | Error | CEL | cluster mTLS requires clientAuth Require |
| TLS-006 | `clientAuth` set on a policy requires that policy's TLS material (key/cert or cert-manager secretName) | Error | CEL | clientAuth requires enabled TLS policy |
| TLS-007 | `certManager.includeIngressHosts: true` requires at least one `connectivity.ingress.rules[].host` | Error | CEL | includeIngressHosts requires ingress rule hosts |

---

## Connectivity

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| NET-001 | `connectivity.service.type` enum | Error | CEL | invalid service type |
| NET-002 | each `connectivity.service.expose` entry ⇒ `connectivity.listeners.{name}` set (non-null) | Error | CEL | cannot expose connector that is not listening |
| NET-005 | `service.expose` ∩ `reverseProxy.expose` = ∅ | Error | CEL | connector cannot be direct and proxied (north-south) |
| NET-006 | `ingress.rules[].paths[].backend: reverseProxy` ⇒ `reverseProxy.enabled`; `port` ∈ backend `expose` | Error | CEL | ingress backend/port mismatch |
| NET-007 | `reverseProxy.enabled` ⇒ `listeners.http` set | Error | CEL | reverse proxy requires HTTP listener |
| NET-004 | `mode: Cluster` + `connectivity.multiCluster.enabled` | Error | CEL | multi-cluster not in V1 |
| LISTENER-001 | `connectivity.listeners.backup` set ⇒ `features.backup.enabled` | Error | CEL | backup listener requires feature |
| LISTENER-002 | `connectivity.listeners.metrics` set ⇒ `features.monitoring.prometheus.enabled` | Error | CEL | metrics listener requires prometheus feature |
| LISTENER-003 | `connectivity.listeners.*` in 1–65535 when set | Error | CEL | invalid listen port |
| TLS-LISTENER-001 | `connectivity.listeners.https` set ⇒ `trust.enabled` + https cert material | Error | CEL | HTTPS listen port requires trust |
| TLS-LISTENER-002 | `https` ∈ `service.expose` ⇒ `connectivity.listeners.https` set | Error | CEL | cannot expose disabled HTTPS connector |
| TLS-LISTENER-003 | `connectivity.listeners.https` set ⇏ `https` ∈ `service.expose` | — | — | in-cluster HTTPS without LB port is valid |
| TLS-LISTENER-004 | mTLS only via `trust.certificates.https.clientAuth` + `trustedCerts` | — | — | not on `connectivity` |
| TLS-LISTENER-005 | `clientAuth: Require` on https ⇒ `trustedCerts.sources` non-empty | Error | CEL | HTTPS mTLS requires trustedCerts |
| TLS-LISTENER-006 | `http` and `https` independent on Service | — | — | `expose` may list both |

### Config vs ports (no contradictions)

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| CFG-LISTENER-001 | `spec.config` MUST NOT contain port-owned keys (denylist) | Error | CEL | `{key}` is owned by `connectivity.listeners.{name}` — remove from spec.config |
| CFG-LISTENER-002 | `server.*.listen_address` in config port ≠ `connectivity.listeners.{name}` | Error | Webhook | config `{key}={value}` contradicts `connectivity.listeners.{name}={port}` — align or remove config key |
| CFG-LISTENER-003 | `server.{bolt,http,https}.enabled` in config contradicts listener presence | Error | Webhook | config `{key}={value}` contradicts listener enablement — use connectivity.listeners only |
| CFG-LISTENER-004 | `server.backup.listen_address` in config port ≠ `connectivity.listeners.backup` | Error | Webhook | backup listen_address contradicts connectivity.listeners.backup |

**Denylist keys (CFG-LISTENER-001)** — reject if present in `spec.config` (listen / connector enablement owned by `connectivity.listeners`):

`server.bolt.listen_address`, `server.bolt.enabled`, `server.http.listen_address`, `server.http.enabled`, `server.https.listen_address`, `server.https.enabled`, `server.backup.listen_address`

Feature-tuning keys (`server.backup.enabled`, `server.metrics.*`) — **not** on this denylist; governed by **CFG-FEAT-*** ([BDR-010](../../decision-records/business/010-neo4j-features-catalog.md)).

CEL sketch (key presence — http example; repeat per listen port name):

```yaml
- rule: |
    !has(self.config) || !('server.http.listen_address' in self.config)
  message: server.http.listen_address is owned by connectivity.listeners.http — remove from spec.config

- rule: |
    !has(self.config) || !('server.http.enabled' in self.config)
  message: server.http.enabled is owned by connectivity.listeners.http — remove from spec.config
```

Webhook (CFG-LISTENER-002): parse `listen_address` (`:7474`, `0.0.0.0:7474`, `[::]:7474`) → compare numeric port to `connectivity.listeners.{name}`; on mismatch return e.g. `spec.config server.http.listen_address=:8080 contradicts connectivity.listeners.http=7474`.

### Config vs features (coherence — [BDR-010](../../decision-records/business/neo4j/010-neo4j-features-catalog.md) Option C — accepted)

When the same `neo4j.conf` key is set in **both** `spec.features` and `spec.config`, values MUST match (string-normalized: `true`/`yes`/`1`).

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| CFG-FEAT-001 | Feature tuning key in both `features` and `config` with different values | Error | Webhook | `spec.config {key}={value}` contradicts `features.{path}={other}` — align or remove one |
| CFG-FEAT-002 | `features.backup.enabled` ≠ `server.backup.enabled` in config when both set | Error | Webhook | backup enablement mismatch between features and config |
| CFG-FEAT-003 | `features.monitoring.prometheus.enabled` ≠ `server.metrics.prometheus.enabled` in config when both set | Error | Webhook | prometheus enablement mismatch between features and config |
| CFG-FEAT-004 | Port in `features.monitoring.prometheus.endpoint` ≠ `connectivity.listeners.metrics` | Error | Webhook | prometheus endpoint port contradicts connectivity.listeners.metrics |
| CFG-FEAT-005 | `server.backup.listen_address` in config when `features.backup` set | Error | CEL | use connectivity.listeners.backup — listen_address is not a feature field |

**CFG-FEAT inventory (V1)** — keys eligible for dual-path coherence:

| `neo4j.conf` key | `features` path |
|------------------|-----------------|
| `server.backup.enabled` | `features.backup.enabled` |
| `server.metrics.prometheus.enabled` | `features.monitoring.prometheus.enabled` |
| `server.metrics.prometheus.endpoint` | `features.monitoring.prometheus.endpoint` |
| `server.metrics.csv.enabled` | `features.monitoring.csv.enabled` |
| `server.metrics.csv.interval` | `features.monitoring.csv.interval` |
| `server.metrics.csv.rotation.keep_number` | `features.monitoring.csv.rotation.keepNumber` |
| `server.metrics.csv.rotation.size` | `features.monitoring.csv.rotation.size` |
| `server.metrics.csv.rotation.compression` | `features.monitoring.csv.rotation.compression` |
| `server.metrics.jmx.enabled` | `features.monitoring.jmx.enabled` |
| `server.metrics.graphite.enabled` | `features.monitoring.graphite.enabled` |
| `server.metrics.graphite.server` | `features.monitoring.graphite.server` |
| `server.metrics.graphite.interval` | `features.monitoring.graphite.interval` |
| `server.metrics.prefix` | `features.monitoring.graphite.prefix` |

Keys only in `spec.config` → passthrough ([BDR-008](../../decision-records/business/008-neo4j-config-surface.md)). Keys only in `features` → operator writes to `neo4j.conf`.

### Config denylist ([BDR-008](../../decision-records/business/008-neo4j-config-surface.md) Option A)

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| CFG-001 | `server.jvm.additional` in `spec.config` | Error | CEL | use spec.jvm.additionalArguments |
| CFG-002 | topology-owned keys in `spec.config` | Error | CEL | key owned by spec.topology — see BDR-008 denylist |
| CFG-003 | TLS-owned keys in `spec.config` when `trust` set | Error | CEL | key owned by spec.trust — see BDR-008 denylist |

Port-owned keys: **CFG-LISTENER-001..004** above. Feature coherence: **CFG-FEAT-001..005**. Full denylist maintained in BDR-008 and operator code.

---

## Scheduling & resilience

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| SCH-001 | `podDisruptionBudget.minAvailable` ≤ total replicas | Error | Webhook | PDB minAvailable exceeds member count |
| SCH-002 | `podDisruptionBudget.enabled: true` requires `mode: Cluster` with ≥2 members | Warning | Webhook | PDB has limited effect on single-member topology |

---

## Resources & JVM

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| RES-001 | `resources.limits.memory` should equal `requests.memory` (Neo4j best practice) | Warning | Webhook | set memory limit equal to request to avoid OOM variance |
| JVM-001 | `jvm.additionalArguments` entries non-empty | Error | CEL | JVM argument cannot be empty string |

---

## Monitoring

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| MON-001 | `serviceMonitor.enabled: true` when Prometheus Operator CRD absent | Warning | Reconciler | ServiceMonitor CRD not installed — skipping |

---

## Maintenance

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| MNT-001 | `maintenance.offlineMode: true` on `mode: Cluster` | Warning | Webhook | offline mode causes full cluster outage |

---

## Defaults applied (mutating webhook / reconciler)

| Field | Default when omitted |
|-------|---------------------|
| `topology.mode` | — (required) |
| `topology.primaries.plugins` | `[]` (Cluster) |
| `secondaries.analytics.plugins` | `[]` |
| `secondaries.read.plugins` | `[]` |
| `spec.plugins` | `[]` (Standalone) |
| `topology.minimumMembers` | `primaries.members` (Cluster) |
| `image.pullPolicy` | `IfNotPresent` |
| `auth.generatePassword` | `true` if no `passwordSecretRef` |
| `trust.enabled` | `false` |
| `connectivity.service.type` | `ClusterIP` |
| `connectivity.service.expose` | `[bolt, http]` |
| `features.backup.enabled` | `false` |
| `features.monitoring.prometheus.enabled` | `false` |
| `connectivity.listeners.bolt` | `7687` |
| `connectivity.listeners.http` | `7474` |
| `monitoring.prometheus.enabled` | `false` |
| `monitoring.serviceMonitor.enabled` | `false` |
| `podDisruptionBudget.enabled` | `true` when Cluster and total members ≥ 3 |
| `maintenance.offlineMode` | `false` |

**Standalone**: mutating webhook must **not** inject `primaries` / `secondaries` / `minimumMembers`.

**Cluster**: mutating webhook may inject empty `pluginDefinitions` entries for referenced `apoc` ids only when `pluginDefinitions` is present but key missing (optional convenience — prefer explicit `{}`).

---

## Validation ownership

```
                    ┌─────────────────────┐
                    │  Admission (CEL +   │
                    │  validating webhook)│
                    │  Reject bad spec    │
                    └──────────┬──────────┘
                               │
                    ┌──────────▼──────────┐
                    │  Reconciler         │
                    │  TopologyWarning,   │
                    │  runtime preflight  │
                    └─────────────────────┘
```

---

## Traceability

| Source | Rules |
|--------|-------|
| ADR-001 | Mechanism choice (CEL / webhook / reconciler) |
| BDR-004 Option E | TOPO-001…013, PLG-001…013 |
| `03-variant_matrix` Edition | EDT-001…006 |
| `NEO-2-005` TLS | TLS-001…007 |
| `NEO-2-006` Storage | STO-001…005 |
| `NEO-2-011` Scale | TOPO-009, TOPO-010 |
