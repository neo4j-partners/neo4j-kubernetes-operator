# ADR-004 — `scale` subresource on `Neo4j`

| | |
|---|---|
| **Status** | accepted |
| **Date** | 2026-06-24 |
| **Reviewers** | Charles Boudry, Marouane Gazanayi |
| **Depends on** | [BDR-002](../business/002-neo4j-crd-topology.md) |

---

## Context

Kubernetes `scale` subresource enables `kubectl scale` and HPA-style integrations. [BDR-002](../business/002-neo4j-crd-topology.md) computes replica count from composable role fields: `primaries + secondaries + analytics`.

---

## Decision

We will implement the **`scale` subresource** on `Neo4j` V1.

| `topology.mode` | Total replicas |
|-----------------|----------------|
| `Standalone` | `1` (scale rejected) |
| `Cluster` | `primaries.members + secondaries.members + analytics.members` |

### Scale behaviour

- **`kubectl scale` up** — mutating webhook adds members to the **last non-zero role** or the role specified by annotation `neo4j.com/scale-role: secondaries|analytics` (default: `secondaries` if HA cluster without analytics growth intent, else `analytics` when GDS scale).
- **Primary count changes** — spec patch only, never via scale.
- **Scale down** — rejected ([`TOPO-008`](../../09-crd-spec/neo4j/validation.md)).

### License alignment

Scaling **`analytics`** up changes `analytics.members` — the declarative instance count. GDS validates the license file at Neo4j startup; the operator does not check commercial entitlements.

---

## Consequences

### Positive

- Scale secondaries and analytics servers independently of primaries.
- GDS instance growth maps to `analytics.members` — matches license upgrades.

### Negative

- Scale webhook needs role-aware translation.

---

## References

- [BDR-002](../business/002-neo4j-crd-topology.md)
- [BDR-004](../business/004-neo4j-plugin-topology.md)
