# ADR-009 — Watches and predicates

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-22 |
| **Depends on** | [ADR-003](003-neo4j-reconcile-pipeline.md) · [BDR-006](../business/neo4j/007-tls-trust-model.md) · [BDR-007](../business/neo4j/006-service-exposure-connectivity.md) |
| **Constraints** | controller-runtime `Watches`; V1 single-namespace scope ([BDR-003](../business/operator/003-operator-install-scope.md)) |

---

## Context

The `Neo4j` reconciler must react to changes beyond the `Neo4j` CR itself — TLS Secrets, auth password, referenced ConfigMaps, and (cluster) member Pods. Wrong watches cause missed reconciles or excessive queue depth.

**Forces:**

- [BDR-006](../business/neo4j/007-tls-trust-model.md) — cert rotation and `trust.reload`.
- [ADR-003](003-neo4j-reconcile-pipeline.md) — full pipeline is expensive; avoid blind global reconcile storms.
- CNPG: watches on Secrets, Pods, PVCs with `EnqueueRequestForOwner` and custom predicates.
- Strimzi: informers on multiple types; warns multi-namespace watch overhead ([BDR-003](../business/operator/003-operator-install-scope.md)).

---

## Analysis

### Option A — Primary watch + mapped watches with predicates (chosen)

`For(Neo4j)` + `Watches` for owned types and selected secondary types with `EnqueueRequestsFromMapFunc`.

| Advantages | Disadvantages |
|------------|---------------|
| Standard controller-runtime pattern | Must map secondary objects → Neo4j CR |
| Predicates limit noise | More setup code in `watches.go` |

### Option B — Watch everything in namespace

| Advantages | Disadvantages |
|------------|---------------|
| Simple | High churn — Strimzi-documented overhead |

### Option C — Reconcile only on Neo4j generation changes

| Advantages | Disadvantages |
|------------|---------------|
| Minimal watches | **Misses** Secret rotation — unacceptable |

---

## Comparison

| Criterion | A Mapped watches | B NS-wide | C CR only |
|-----------|------------------|-----------|-----------|
| Secret rotation | **Yes** | Yes | No |
| Queue efficiency | **Good** | Poor | Best but incomplete |
| V1 fit | **Yes** | No | No |

---

## Decision

We will implement **Option A** in `internal/controller/neo4j/watches.go`.

### Primary

```go
ctrl.NewControllerManagedBy(mgr).
    For(&v1beta1.Neo4j{}).
    WithEventFilter(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.AnnotationChangedPredicate{})).
    Complete(r)
```

Pause annotation: `neo4j.com/paused: "true"` → skip reconcile (log, no requeue).

### Secondary watches (enqueue owning Neo4j)

| Type | Map function | Predicate |
|------|--------------|-----------|
| `Secret` | TLS + auth secrets referenced in spec | label or name match via index |
| `ConfigMap` | Only if referenced (rare BYO) | Update |
| `Pod` | Owned pods (pool STS) | Ready / phase change for formation |
| `StatefulSet` | Owned STS | Generation / replicas |

Use `handler.EnqueueRequestsFromMapFunc` — map to `Neo4j` namespacedName via ownerRef or spec refs.

### Owned resources

`Owns(&appsv1.StatefulSet{})`, `Owns(&corev1.Service{})`, `Owns(&corev1.ConfigMap{})` — controller-runtime automatic enqueue.

### NOT watched in V1

- Cluster-wide `Node` events (CNPG watches nodes for zone — defer unless scheduling feature needs).
- `Ingress` (V1.1+ per [BDR-007](../business/neo4j/006-service-exposure-connectivity.md)).

### Max concurrent reconciles

`MaxConcurrentReconciles: 2` default — Neo4j reconcile is heavy; tune via env `MAX_CONCURRENT_RECONCILES`.

---

## Consequences

### Positive

- Cert rotation triggers reconcile without polling.
- Predicates avoid reconciling on status-only Pod updates unrelated to readiness.

### Negative

- Map functions must stay in sync with spec ref fields — test in envtest.

### Neutral

- Multi-namespace cache deferred — see future ADR-014 + [BDR-003](../business/operator/003-operator-install-scope.md).

---

## References

- [BDR-006](../business/neo4j/007-tls-trust-model.md) · [BDR-003](../business/operator/003-operator-install-scope.md)
- CNPG cluster predicates / watches — `cluster_predicates.go` ([cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md))
- Strimzi namespace watch docs — [strimzi.md](../../architecture/operator-benchmark/operators/strimzi.md) D8
- controller-runtime `Watches` / `Owns` documentation
- [ADR-003](003-neo4j-reconcile-pipeline.md)
