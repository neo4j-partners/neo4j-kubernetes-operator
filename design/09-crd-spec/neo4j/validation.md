# `Neo4j` — validation rules

**API**: `neo4j.com/v1beta1`  
**Sources**: [BDR-002](../../decision-records/business/002-neo4j-crd-topology.md) (overrides proposal on topology) · [BDR-004](../../decision-records/business/004-neo4j-plugin-topology.md) Option E · [ADR-001](../../decision-records/architecture/001-crd-validation-process.md) (CEL vs webhook ownership) · [`spec.md`](spec.md) · `01` / `03` variant matrix

**Mechanisms**:

| Mechanism | When |
|-----------|------|
| **CEL** (`x-kubernetes-validations`) | Structural rules, enum checks, cross-field guards — cheap, in CRD OpenAPI. |
| **Validating webhook** | Edition/license, storage class existence, scale-in policy, analytics config coherence. |
| **Reconciler** | Runtime cluster state, topology warnings → `status.conditions` (not admission). |

---

## Topology (BDR-002 — authoritative)

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
| TOPO-010 | Scale-in below formed cluster | Error | Webhook | Unsupported scale-in |
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

## Plugins (BDR-004 Option E)

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
| STO-001 | `persistence.data.size` required | Error | CEL | data volume size is required |
| STO-002 | `persistence.data.size` must be valid quantity | Error | CEL | invalid storage size |
| STO-003 | `storageClassName` must exist when set | Error | Webhook | StorageClass not found |
| STO-004 | Shrink `persistence.data.size` blocked | Error | Webhook | PVC expansion only — shrinking not supported |
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
| TLS-002 | Referenced TLS secrets must exist when `trust.enabled` and not using cert-manager | Error | Webhook | TLS secret not found |
| TLS-003 | `mode: Cluster` + `trust.enabled` → cluster TLS material required | Error | CEL | cluster TLS is required for clustered deployments |

---

## Connectivity

| ID | Rule | Severity | Mechanism | Message |
|----|------|----------|-----------|---------|
| NET-001 | `external.type` enum: `LoadBalancer`, `NodePort`, `ClusterIP`, `None` | Error | CEL | invalid external service type |
| NET-002 | `external.enabled: true` requires at least one port | Error | CEL | enable at least one external port |
| NET-003 | `https: true` requires TLS enabled | Error | CEL | HTTPS requires trust.enabled |
| NET-004 | `mode: Cluster` + `connectivity.multiCluster.enabled` | Error | CEL | multi-cluster networking not in V1 (NEO-3-007-MULTI-02) |

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
| `connectivity.internal.enabled` | `true` |
| `connectivity.external.enabled` | `false` |
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
| `NEO-2-005` TLS | TLS-001…003 |
| `NEO-2-006` Storage | STO-001…005 |
| `NEO-2-011` Scale | TOPO-009, TOPO-010 |
