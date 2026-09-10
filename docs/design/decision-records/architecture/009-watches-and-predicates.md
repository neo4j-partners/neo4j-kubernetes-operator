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
| `PersistentVolumeClaim` | `neo4j.com/component=storage` provenance label, else `Existing.claimName` match | `status.phase`, `status.capacity`, `status.conditions` |

Use `handler.EnqueueRequestsFromMapFunc` — map to `Neo4j` namespacedName via ownerRef or spec refs.

`Owns` cannot carry the claim watch: a Dynamic claim is created by the StatefulSet controller, which
leaves no ownerRef at all, and an `Existing.claimName` claim predates the CR. Both need the mapper,
and for two different reasons — the Dynamic claim carries operator provenance labels, the BYO claim
carries nothing the operator put there and is recognised by name only.

Without this watch, readiness is poll-only for anything the claim alone reports. The cost is
unequal across storage modes. A Dynamic bind is usually covered anyway, because the pod start that
follows it moves the StatefulSet, which *is* watched. Two cases have no such cover: an expansion
restarts no pod and leaves the immutable template untouched, so the claim is the only object that
moves; and a BYO claim that is still unbound keeps its pods `Pending`, so no StatefulSet event ever
arrives. Both would wait out the full 30 s requeue.

The predicate is not an optimisation detail. A bind stamps several annotations on the claim, and
each is an Update that would re-run a pipeline opening Bolt sessions in Cluster mode. Create and
Delete stay unfiltered: a claim a scale-out creates is born at the template's older size and has to
be grown on sight.

The PVC informer takes no label selector, since a selector would hide exactly the BYO claims the
second mapper path exists to catch. Scope stays bounded by the cache's `DefaultNamespaces`.

### Owned resources

`Owns(&appsv1.StatefulSet{})`, `Owns(&corev1.Service{})`, `Owns(&corev1.ConfigMap{})` — controller-runtime automatic enqueue.

### NOT watched in V1

- Cluster-wide `Node` events (CNPG watches nodes for zone — defer unless scheduling feature needs).
- `Ingress` (V1.1+ per [BDR-007](../business/neo4j/006-service-exposure-connectivity.md)).

### Max concurrent reconciles

`MaxConcurrentReconciles: 2` default — Neo4j reconcile is heavy; tune via `--max-concurrent-reconciles` or env `MAX_CONCURRENT_RECONCILES`. Maximum 16 (NEO-014); higher values are rejected at process start.

---

## Consequences

### Positive

- Cert rotation triggers reconcile without polling.
- Predicates avoid reconciling on status-only Pod updates unrelated to readiness.
- A completed volume expansion and a BYO claim binding report at once instead of waiting out the
  30 s requeue, which are the two storage transitions no other watched object reflects.

### Negative

- Map functions must stay in sync with spec ref fields — test in envtest.
- The claim informer caches every PVC in the watched namespaces, not only operator-created ones,
  because a label selector would filter out the BYO claims the name-match path is there to catch.

### Neutral

- Multi-namespace cache deferred — see future ADR-014 + [BDR-003](../business/operator/003-operator-install-scope.md).

---

## References

- [BDR-006](../business/neo4j/007-tls-trust-model.md) · [BDR-003](../business/operator/003-operator-install-scope.md)
- CNPG cluster predicates / watches — `cluster_predicates.go` ([cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md))
- Strimzi namespace watch docs — [strimzi.md](../../architecture/operator-benchmark/operators/strimzi.md) D8
- controller-runtime `Watches` / `Owns` documentation
- [ADR-003](003-neo4j-reconcile-pipeline.md)
