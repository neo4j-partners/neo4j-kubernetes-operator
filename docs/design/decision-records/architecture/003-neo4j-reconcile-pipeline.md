# ADR-003 — `Neo4j` reconcile pipeline order

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-22 |
| **Depends on** | [ADR-002](002-package-layering.md) · [BDR-002](../business/neo4j/002-neo4j-crd-topology.md) · [BDR-005](../business/neo4j/005-storage-volume-mode.md) · [BDR-006](../business/neo4j/007-tls-trust-model.md) · [BDR-008](../business/neo4j/008-neo4j-config-surface.md) |
| **Constraints** | V1 scope — no backup/restore controllers in pipeline |

---

## Context

The `Neo4j` reconciler must apply dependent resources in a safe order: PVCs before StatefulSets, TLS Secrets before pods mount them, ConfigMaps before workload rollout, Services after pods exist for endpoints, formation only when admin API is reachable.

**Forces:**

- [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md) — one StatefulSet per pool (`primary`, `analytics`, `read`).
- [BDR-007](../business/neo4j/006-service-exposure-connectivity.md) — derived `admin` / `internals` Services.
- Strimzi uses sub-reconcilers (`KafkaListenersReconciler`, `EntityOperatorReconciler`) inside `KafkaAssemblyOperator` — **order matters**.
- CNPG splits `cluster_create`, `cluster_scale`, `cluster_status` — single entry `Reconcile` orchestrates.

**What breaks if wrong:** pods start without certs; STS scale before PVC binding; formation against unreachable Bolt; status `Ready` before objects exist.

---

## Analysis

### Option A — Strict linear pipeline (chosen)

Fixed ordered steps; each `domain/*/Reconcile` is idempotent. Mode-specific steps noop or branch internally.

| Advantages | Disadvantages |
|------------|---------------|
| Predictable; easy to debug and document | Sub-optimal requeue when only one concern changes |
| Matches `layer.md` example | Adding a step requires explicit position decision |
| Testable step-by-step in envtest | |

### Option B — Graph / dependency-driven reconcile

Declare dependencies between resource kinds; topological sort each loop.

| Advantages | Disadvantages |
|------------|---------------|
| Flexible when CRD grows | Over-engineered for V1 |
| | Harder to reason about than Strimzi/CNPG linear flows |

### Option C — Per-pool isolated pipelines

Each pool StatefulSet has its own full pipeline in parallel.

| Advantages | Disadvantages |
|------------|---------------|
| Parallelism | TLS/Config must be shared — duplication or race |
| | Conflicts with shared client Service and trust material |

**Improvement over CNPG:** explicit **formation** step after workload is up — CNPG folds instance logic into one large loop; we separate for clarity and V1 testing.

---

## Comparison

| Criterion | A Linear | B Graph | C Per-pool |
|-----------|----------|---------|------------|
| Debuggability | **Best** | Poor | Medium |
| V1 fit | **Yes** | No | No |
| Strimzi/CNPG alignment | **Yes** | No | Partial |

---

## Decision

We will use **Option A** — global step order for `Neo4jReconciler`:

| Step | Domain package | Cluster only? | Purpose |
|------|----------------|---------------|---------|
| 1 | `domain/persistence` | No | PVCs / volume mode ([BDR-005](../business/neo4j/005-storage-volume-mode.md)) |
| 2 | `domain/trust` | No | TLS Secrets, cert-manager Certificates ([BDR-006](../business/neo4j/007-tls-trust-model.md)) |
| 3 | `domain/serverconfig` | No | ConfigMaps: `neo4j.conf`, `jvm`, `apoc.conf` ([BDR-008](../business/neo4j/008-neo4j-config-surface.md)) |
| 4 | `domain/workload/rbac` | Partial | Operand ServiceAccount + Role (backlog L-03; future ADR-013) |
| 5 | `domain/workload` | No | StatefulSets per pool ([BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md)) |
| 6 | `domain/connectivity` | No | Client, admin, internals Services ([BDR-007](../business/neo4j/006-service-exposure-connectivity.md)) |
| 7 | `domain/scheduling` | Yes | PDB, discovery RBAC if cluster |
| 8 | `domain/formation` | Yes | Bolt: enable servers, quorum gate ([ADR-007](007-formation-and-bolt.md)) |
| 9 | `status` writer | No | Conditions, phase, `serverSummary`, endpoints |

**Standalone** skips steps 7–8 (formation noop; scheduling minimal).

**Short-circuit:** if `spec.maintenance.offlineMode` or pause annotation → skip workload rollout and formation; patch status `Maintenance`.

**Requeue:** any step returns `RequeueAfter` or error → controller stops pipeline, patches `Reconciling=True`, returns.

### Implementation notes

```go
func (r *Neo4jReconciler) pipeline(n *v1beta1.Neo4j) []reconcileStep {
    steps := []reconcileStep{
        persistence.Reconcile,
        trust.Reconcile,
        serverconfig.Reconcile,
        workload.ReconcileRBAC,
        workload.Reconcile,
        connectivity.Reconcile,
    }
    if mode.IsCluster(n) {
        steps = append(steps, scheduling.Reconcile, formation.Reconcile)
    }
    return steps
}
```

Sub-reconciler pattern (Strimzi): `formation` may internally sequence enable → verify without expanding controller.

---

## Consequences

### Positive

- Documented order for support runbooks and tests.
- Each domain package owns one concern — matches file_structure.md.

### Negative

- Full pipeline runs on any spec change — mitigate with predicates ([ADR-009](009-watches-and-predicates.md)) and cheap early exits in domains.

### Neutral

- Upgrade orchestration (V2) inserts between steps 5 and 8 as `domain/maintenance` — not V1.

---

## References

- [layer.md](../../architecture/layer.md) · [flow.md](../../architecture/flow.md)
- [BDR-002](../business/neo4j/002-neo4j-crd-topology.md) · [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md)
- [operator-benchmark/operators/strimzi.md](../../architecture/operator-benchmark/operators/strimzi.md) — D2, D12 (`KafkaAssemblyOperator` sub-reconcilers)
- [operator-benchmark/operators/cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md) — `cluster_*.go` split
- [ADR-002](002-package-layering.md) · [ADR-007](007-formation-and-bolt.md)
