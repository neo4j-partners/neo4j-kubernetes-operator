# BDR-005 — V1 `Neo4j` CRD: full scope, no deferred fields

| | |
|---|---|
| **Status** | accepted |
| **Date** | 2026-06-24 |
| **Reviewers** | Charles Boudry, Marouane Gazanayi |
| **Depends on** | [BDR-001](001-single-neo4j-crd.md), [BDR-002](002-neo4j-crd-topology.md), [BDR-004](004-neo4j-plugin-topology.md) |
| **Constraints** | `01-functional_requirements.csv`, Helm chart parity |

---

## Context

Earlier drafts marked many CRD capabilities as “V2” or “partial V1” — Community edition, eval licenses, LDAP, multi-volume persistence, Bloom, backup ports, multi-cluster networking, maintenance jobs, and more. That split created two problems:

1. **Helm parity gaps** — Helm chart users expect these knobs; deferral forces `spec.config` or `podTemplate` escape hatches for mainstream scenarios.
2. **Documentation drift** — spec, validation, and BDRs disagreed on what was in scope.

We will ship a **complete, simple `Neo4j` CRD in V1** — one manifest covers the full Helm chart surface for a single release, expressed in domain language ([BDR-002](002-neo4j-crd-topology.md)), not Helm field names.

---

## Decision

All items below are **in V1**. We will not label them deferred in `spec.md` or validation.

### Identity & edition

| Field | V1 |
|-------|-----|
| `edition: community \| enterprise` | Yes |
| `version` | Yes |
| `license.accept: no \| yes \| eval` | Yes |

### Topology ([BDR-002](002-neo4j-crd-topology.md))

| Field | V1 |
|-------|-----|
| `topology.mode: Standalone \| Cluster` with `primaries`, `secondaries`, `analytics` | Yes |
| `primaries`, `secondaries`, `analytics`, `minimumMembers` | Yes — mode-scoped |
| `scale` subresource | Yes ([ADR-004](../architecture/004-scale-subresource.md)) |

### Plugins ([BDR-004](004-neo4j-plugin-topology.md))

| Field | V1 |
|-------|-----|
| `plugins` / `plugins.primaries` / `plugins.secondaries` / `plugins.analytics` | Yes |
| `pluginDefinitions` — CAB, gds, bloom, apoc-extended | Yes |
| `pluginDefinitions.*.credentials` (APOC JDBC/ES) | Yes |

### Persistence ([ADR-003](../architecture/003-persistence-model.md))

| Field | V1 |
|-------|-----|
| `persistence.data` — size, storageClass, accessMode, existingClaim | Yes |
| `persistence.logs`, `metrics`, `import`, `backups`, `licenses` | Yes — each with size or `shareWith: data` |
| PVC expansion; shrink blocked | Yes |

### Auth

| Field | V1 |
|-------|-----|
| `auth.generatePassword`, `passwordSecretRef` | Yes |
| `auth.ldap` | Yes |

### Trust / TLS

| Field | V1 |
|-------|-----|
| `trust.enabled`, cert-manager, BYO secrets | Yes |
| `trust.certificates.*.trustedCerts`, `revokedCerts` | Yes — projected volume sources |
| `trust.reload.enabled` | Yes |

### Connectivity

| Field | V1 |
|-------|-----|
| `connectivity.internal`, `connectivity.external` | Yes |
| External ports: bolt, http, https, **backup** | Yes |
| `connectivity.multiCluster` | Yes |

### Operations & resilience

| Field | V1 |
|-------|-----|
| `resources`, `jvm`, `config` | Yes |
| `scheduling` — nodeSelector, tolerations, affinity, topologySpread, **priorityClassName** | Yes |
| `podDisruptionBudget`, `probes` | Yes |
| `security` — pod/container context, serviceAccount, **networkPolicy** | Yes |
| `monitoring.prometheus`, `monitoring.serviceMonitor` (full fields) | Yes |
| `maintenance.offlineMode` | Yes |
| `maintenance.jobs` — dump, load, report (maps to Helm neo4j-admin / operations) | Yes |
| `podTemplate` escape hatch | Yes |

### Explicitly out of V1 (separate CRDs, not deferred Neo4j spec fields)

| Item | Reason |
|------|--------|
| `Neo4jDatabase`, `Neo4jBackup`, `Neo4jRestore` | Day-2 CRDs — separate reconcilers |
| `Neo4jProfile` / topology presets | Removed — no FR backing |
| `topology.profile` enum shortcuts | Optional V1.1 convenience only |

---

## Consequences

### Positive

- One **`Neo4j` manifest = full Helm release`** for PS and customers.
- No “check V2 doc” gaps in support or technical writing.
- Validation catalog ([`validation.md`](../../09-crd-spec/neo4j/validation.md)) covers the full surface.

### Negative

- Larger OpenAPI schema and implementation effort — mitigated by Helm mapping ([ADR-002](../architecture/002-helm-values-mapping.md)) and phased reconciler delivery inside V1 timeline.
- Community + Cluster rules add validation branches — required for edition parity.

### Neutral

- `13-v1-scope-lock.md` must be regenerated from this BDR.
- Day-2 CRDs unchanged — they reference `Neo4j` via `neo4jRef`.

---

## References

- [BDR-002](002-neo4j-crd-topology.md)
- [BDR-004](004-neo4j-plugin-topology.md)
- [ADR-002](../architecture/002-helm-values-mapping.md)
- [ADR-003](../architecture/003-persistence-model.md)
- [`09-crd-spec/neo4j/spec.md`](../../09-crd-spec/neo4j/spec.md)
