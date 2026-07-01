# ADR-005 — Render conventions (naming, labels, owner references)

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-22 |
| **Depends on** | [ADR-002](002-package-layering.md) · [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md) · [BDR-001](../business/neo4j/001-single-neo4j-crd.md) |
| **Constraints** | Stable selectors across upgrades; GitOps-friendly names |

---

## Context

Every `internal/render/*` builder must produce deterministic object names, labels, and owner references so `domain` can idempotently apply and Kubernetes GC works. [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md) fixes pool StatefulSet naming; Helm today uses `neo4j.name` / release name — operator must be equally predictable.

**Forces:**

- One `Neo4j` CR per deployment ([BDR-001](../business/neo4j/001-single-neo4j-crd.md)).
- Pools: `primary`, `analytics`, `read` — STS name `{cr-name}-{pool}` (e.g. `prod-primary`).
- Strimzi uses consistent `Labels` / `ModelUtils` in `model/`; CNPG uses `pkg/specs` with cluster name prefix.

---

## Analysis

### Option A — CR name as sole prefix (chosen)

All child objects: `{neo4j.metadata.name}` or `{neo4j.metadata.name}-{suffix}` where suffix encodes role (`-primary`, `-client`, `-admin`).

| Advantages | Disadvantages |
|------------|---------------|
| Simple; matches BDR-009 | Long names if CR name is long |
| One label selector set per CR | Collision if two operators in NS (standard K8s assumption) |

### Option B — Helm-style `fullnameOverride` in spec

User-configurable name prefix on every object.

| Advantages | Disadvantages |
|------------|---------------|
| Helm migration parity | Extra spec surface; selector churn on rename |
| | Violates minimal V1 API goal |

### Option C — Hash-suffixed names for every object

Content-hash in ConfigMap/Secret names (deployment-style).

| Advantages | Disadvantages |
|------------|---------------|
| Immutable config rotations | Orphaned objects; harder operations |
| | Not how CNPG/Strimzi name STS/Services |

---

## Comparison

| Criterion | A CR prefix | B Override | C Hash |
|-----------|-------------|------------|--------|
| Helm parity | Good | **Best** | Poor |
| Operational clarity | **Best** | Medium | Poor |
| V1 fit | **Yes** | Defer | No |

---

## Decision

We will adopt **Option A** with a shared `render.Context`:

### Naming

| Object | Pattern | Example (`name: prod`) |
|--------|---------|------------------------|
| STS per pool | `{name}-{pool}` | `prod-primary`, `prod-read` |
| Client Service | `{name}` | `prod` |
| Admin Service | `{name}-admin` | `prod-admin` |
| Internals Service | `{name}-internals` | `prod-internals` |
| ConfigMap (neo4j.conf) | `{name}-config` | `prod-config` |
| Secret (auth) | `{name}-auth` or user `passwordSecretRef` | |
| Operand SA | `{name}` | `prod` |
| Operand Role | `{name}` | `prod` |

Standalone: single STS `{name}-server` or `{name}` — **pick `{name}-server`** to reserve bare `{name}` for client Service only.

### Labels (required on every rendered object)

```yaml
app.kubernetes.io/name: neo4j
app.kubernetes.io/instance: <neo4j.metadata.name>
app.kubernetes.io/managed-by: neo4j-operator
neo4j.com/pool: <primary|analytics|read|server>   # workload only
neo4j.com/component: <workload|connectivity|config|trust>
```

Selector labels on STS/Services: `app.kubernetes.io/instance` + `neo4j.com/pool` (workload).

### Owner references

- Every namespaced child: `ownerReference` → `Neo4j` CR (`controller: true`, `blockOwnerDeletion: true`).
- Cluster-scoped children (none in V1 MVP) — N/A.

### RenderContext

```go
type Context struct {
    Neo4j     *v1beta1.Neo4j
    Pool      PoolID          // primary | analytics | read | server
    Namespace string
}
func (c Context) STSName() string { ... }
func (c Context) CommonLabels() map[string]string { ... }
```

---

## Consequences

### Positive

- Golden tests: one `spec` → expected names/labels.
- Support can `kubectl get all -l app.kubernetes.io/instance=prod`.

### Negative

- Helm migrations must rename releases to CR name convention (migration CLI).

### Neutral

- User `metadata.name` choice becomes operational contract — document in install guide.

---

## References

- [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md) · [file_structure.md](../../architecture/file_structure.md)
- CNPG `pkg/specs` — cluster-named SA/Role ([cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md))
- Strimzi `Labels` / `ModelUtils` ([strimzi.md](../../architecture/operator-benchmark/operators/strimzi.md))
- [ADR-002](002-package-layering.md) · [ADR-006](006-apply-and-idempotency.md)
