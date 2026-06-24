# BDR-002 — `Neo4j` CRD topology: license-driven server roles

| | |
|---|---|
| **Status** | accepted |
| **Date** | 2026-06-24 |
| **Amends** | BDR-002 (2026-06-24) — analytics role is optional capacity, not exclusive GDS placement |
| **Reviewers** | Charles Boudry, Marouane Gazanayi |
| **Depends on** | [BDR-001](001-single-neo4j-crd.md) |
| **Constraints** | `NEO-001`, `NEO-002`, `NEO-011`; [ADR-002](../architecture/002-helm-values-mapping.md) |

---

## Context

[BDR-001](001-single-neo4j-crd.md) chose one `Neo4j` CRD. The operator uses **one CR → one StatefulSet → N pods**.

Neo4j v5+ clusters combine **fixed server roles**, each with an independent member count:

| Role | Purpose |
|------|---------|
| **Primary** | Transactional writes / quorum |
| **Secondary** | Read scaling |
| **Analytics** | Optional **dedicated analytics-capacity servers** — used when GDS/graph analytics runs on a separate member (e.g. PS layout: 1 primary + 1 analytics server with GDS) |

**GDS is not restricted to the analytics role.** It may run on primaries, secondaries, or analytics servers — see [BDR-004](004-neo4j-plugin-topology.md). Which servers run GDS is declared in **`plugins.<role>`**, not implied by topology alone.

**Licensing** shapes common layouts: a contract for **one** GDS instance often yields **1 primary + 1 analytics server with GDS** — not GDS on every member.

---

## Decision

We will expose **`spec.topology.mode`**: **`Standalone`** | **`Cluster`**.

In **`Cluster`**, users set **`primaries.members`** (required) and optionally **`secondaries.members`** and **`analytics.members`**.

### `Standalone`

```yaml
topology:
  mode: Standalone
```

Forbidden: `primaries`, `secondaries`, `analytics`, `minimumMembers`. Replicas: **1**.

### `Cluster`

```yaml
topology:
  mode: Cluster
  primaries:
    members: 1
  secondaries:
    members: 0
  analytics:
    members: 1
  minimumMembers: 1
```

| Field | Required | Default | Rules |
|-------|----------|---------|-------|
| `primaries.members` | **yes** | — | odd, ≥ 1 |
| `secondaries.members` | no | `0` | ≥ 0; Enterprise when > 0 |
| `analytics.members` | no | `0` | ≥ 0; Enterprise when > 0 |
| `minimumMembers` | no | `primaries.members` | formation gate |

**Total replicas:** `primaries + secondaries + analytics`

### Ordinal → role

```
[0 .. primaries-1]                         → primary
[primaries .. primaries+secondaries-1]     → secondary
[primaries+secondaries .. total-1]         → analytics
```

### Common layouts

| Use case | Topology | Plugins (see BDR-004) |
|----------|----------|------------------------|
| Dev single server | `Standalone` | `plugins: [apoc, gds]` |
| GDS on all primaries | `primaries: 3` | `plugins.primaries: [apoc, gds]` |
| **1 primary + 1 GDS server (1 license)** | `primaries: 1`, `analytics: 1` | `plugins.analytics: [gds]` |
| HA + read scaling | `primaries: 3`, `secondaries: N` | `plugins.secondaries: [apoc]` |
| HA + dedicated GDS server | `primaries: 3`, `analytics: 1` | `plugins.analytics: [gds]` |

### Validation rules

| Rule | Severity |
|------|----------|
| `Standalone` + any member field | Error |
| `Cluster` without `primaries.members` | Error |
| `primaries.members` even and > 0 | Error |
| `secondaries` / `analytics` when `Standalone` | Error |
| `secondaries.members > 0` or `analytics.members > 0` on Community | Error |
| Plugin on `plugins.<role>` but that role `members < 1` | Error |
| `analytics.members > 0` but `plugins.analytics` empty | Warning |
| `primaries.members < 3` in `Cluster` | Warning → `TopologyWarning` |
| `mode` immutable | Error |
| Scale-in below formed cluster | Error |

---

## Consequences

### Positive

- **1 primary + 1 analytics (GDS)** is a first-class documented layout.
- GDS on primaries remains valid — no artificial API restriction.
- Fixed roles, no pools.

### Negative

- Users must align `plugins.<role>` with license instance count themselves.

---

## References

- [BDR-004](004-neo4j-plugin-topology.md)
- [`09-crd-spec/neo4j/spec.md`](../../09-crd-spec/neo4j/spec.md)
