# `Neo4j` — validation rules

**API**: `neo4j.com/v1beta1`  
**Sources**: [BDR-002](../../decision-records/business/002-neo4j-crd-topology.md) · [BDR-004](../../decision-records/business/004-neo4j-plugin-topology.md) · [BDR-005](../../decision-records/business/005-v1-full-crd-scope.md) · [ADR-001](../../decision-records/architecture/001-crd-validation-process.md)

**Mechanisms**: CEL · Validating webhook · Reconciler (warnings)

---

## Topology (BDR-002)

| ID | Rule | Severity | Mechanism |
|----|------|----------|-----------|
| TOPO-001 | `Standalone` → no `primaries`, `secondaries`, `analytics`, `minimumMembers` | Error | CEL |
| TOPO-002 | `Cluster` → `primaries.members` required | Error | CEL |
| TOPO-003 | `primaries.members` even and > 0 | Error | CEL |
| TOPO-004 | member fields when `mode: Standalone` | Error | CEL |
| TOPO-005 | `secondaries.members < 0` or `analytics.members < 0` | Error | CEL |
| TOPO-006 | `minimumMembers` forbidden in `Standalone` | Error | CEL |
| TOPO-007 | `minimumMembers > total members` | Error | Webhook |
| TOPO-008 | Scale-in below formed cluster | Error | Webhook |
| TOPO-009 | `primaries.members < 3` in `Cluster` | Warning | Reconciler → `TopologyWarning` |
| TOPO-010 | `mode` immutable | Error | CEL |
| TOPO-011 | Scale `Standalone` | Error | Webhook |

---

## Plugins (BDR-004)

GDS/Bloom **may run on any role** — no placement restriction by role type.

| ID | Rule | Severity | Mechanism |
|----|------|----------|-----------|
| PLG-001 | flat `spec.plugins` when `mode: Cluster` | Error | CEL |
| PLG-002 | `plugins.primaries`/`secondaries`/`analytics` when `mode: Standalone` | Error | CEL |
| PLG-003 | licensed plugin referenced without `licenseSecretRef` | Error | CEL |
| PLG-004 | duplicate id in same plugin list | Error | CEL |
| PLG-005 | plugin version major mismatch | Error | Webhook |
| PLG-006 | unknown catalog id | Error | CEL |
| PLG-007 | `gds`/`bloom` in `plugins.analytics` but `analytics.members < 1` | Error | CEL |
| PLG-008 | `gds`/`bloom` in `plugins.secondaries` but `secondaries.members < 1` | Error | CEL |
| PLG-009 | plugin in `plugins.primaries` but `primaries.members < 1` | Error | CEL |
| PLG-010 | unknown `pluginDefinitions` key | Error | CEL |
| PLG-011 | `licenseSecretRef` Secret must exist | Error | Webhook |
| PLG-012 | unused `pluginDefinitions` key | Warning | Reconciler |
| PLG-013 | license Secret changed | — | Reconciler → rolling restart |
| PLG-014 | `analytics.members > 0` but `plugins.analytics` empty | Warning | Reconciler |

```yaml
# PLG-007
- rule: |
    self.topology.mode != 'Cluster' ||
    !has(self.plugins) || !has(self.plugins.analytics) ||
    !self.plugins.analytics.exists(p, p == 'gds' || p == 'bloom') ||
    (has(self.topology.analytics) && self.topology.analytics.members >= 1)
  message: GDS/Bloom on plugins.analytics requires analytics.members >= 1
```

---

## Edition & license

| ID | Rule | Severity | Mechanism |
|----|------|----------|-----------|
| EDT-001 | `edition` ∈ `community`, `enterprise` | Error | CEL |
| EDT-002 | `license.accept` ∈ `no`, `yes`, `eval` | Error | CEL |
| EDT-003 | `secondaries.members > 0` requires Enterprise | Error | CEL |
| EDT-004 | `analytics.members > 0` requires Enterprise | Error | CEL |
| EDT-005 | `mode: Cluster` requires Enterprise | Error | CEL |
| EDT-006 | `gds`/`bloom` referenced requires Enterprise | Error | CEL |

---

## Identity, persistence, auth, TLS, connectivity, scheduling

| Domain | Key rules |
|--------|-----------|
| Version | `VER-001` required; `VER-002` downgrade blocked (webhook) |
| Persistence | `STO-001`…`STO-006` ([ADR-003](../../decision-records/architecture/003-persistence-model.md)) |
| Auth | `AUTH-001` LDAP requires secret; `AUTH-002` password secret exists |
| TLS | `TLS-001`…`TLS-003` |
| Connectivity | `NET-001`…`NET-004` |
| Scheduling | `SCH-001`, `SCH-002`, `RES-001` |

---

## Defaults (mutating webhook)

| Field | Default |
|-------|---------|
| `topology.secondaries.members` | `0` |
| `topology.analytics.members` | `0` |
| `topology.minimumMembers` | `primaries.members` |
| `plugins.*` | `[]` |
| auxiliary persistence roles | `shareWith: data` |

---

## Traceability

| Source | Rules |
|--------|-------|
| BDR-002 | TOPO-001…011 |
| BDR-004 | PLG-001…014 |
| ADR-001 | mechanism ownership |
