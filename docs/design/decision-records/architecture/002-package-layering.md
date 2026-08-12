# ADR-002 — Package layering (`render` / `domain` / `controller`)

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-22 |
| **Depends on** | [BDR-001](../business/neo4j/001-single-neo4j-crd.md) · [ADR-001](001-crd-validation-process.md) · [ADR-011](011-implementation-language.md) |
| **Constraints** | `EST-DEV-010` (operator core); kubebuilder scaffold |

---

## Context

The Neo4j operator must reconcile many Kubernetes object kinds (StatefulSets per pool, Services, ConfigMaps, Secrets, RBAC) and optional Bolt admin calls. Without a strict package split, reconcilers become monolithic — CloudNativePG's `cluster_controller.go` is **~1,400 lines** despite file splits; Strimzi's `KafkaAssemblyOperator` is **~1,000 lines** in Java.

**Forces:**

- [BDR-001](../business/neo4j/001-single-neo4j-crd.md) — one `Neo4j` CRD with mode-dependent behaviour.
- [ADR-001](001-crd-validation-process.md) — `internal/validation/` is separate from reconcile.
- Draft [`layer.md`](../../architecture/layer.md) and [`file_structure.md`](../../architecture/file_structure.md) already propose `render` / `domain` / `controller`.
- Benchmark: CNPG uses `pkg/specs` + `pkg/reconciler`; Strimzi uses `model/` + `assembly/` — same idea, different names.

**What breaks if wrong:** untestable reconcilers, import cycles, Bolt calls in object builders, duplicate Service/STS logic across domains.

---

## Analysis

### Option A — Flat kubebuilder layout (`controllers/` + `resources/`)

Single `controllers/neo4j_controller.go` builds and applies objects inline.

| Advantages | Disadvantages |
|------------|---------------|
| Default kubebuilder scaffold — fast start | Reconciler grows without bound (CNPG/Strimzi anti-pattern) |
| Few packages to navigate | Hard to unit-test builders without envtest |
| | `client` calls mixed with pure logic |

### Option B — Three layers: `render` / `domain` / `controller` (chosen)

| Advantages | Disadvantages |
|------------|---------------|
| Pure builders testable with golden files | More packages than scaffold default |
| Domain modules map to concerns (persistence, workload, formation) | Requires import discipline (enforced by linter) |
| Controller stays a pipeline (~150 lines target) | Slight indirection for new contributors |
| Matches CNPG/Strimzi separation of builders vs apply | |

### Option C — CNPG-style `pkg/reconciler` only (no `render` package)

Sub-reconcilers under `pkg/reconciler/*` embed builders inline.

| Advantages | Disadvantages |
|------------|---------------|
| Proven in CNPG | Builders less isolated — harder golden tests |
| Less package nesting | `pkg/` is public import path — prefer `internal/` |

**Improvement over CNPG:** enforce **thin** `Reconcile()` in controller; CNPG splits files but keeps a very large `ClusterReconciler` struct — we reject that as the norm.

---

## Comparison

| Criterion | A Flat | B render/domain/controller | C pkg/reconciler only |
|-----------|--------|----------------------------|------------------------|
| Testability | Poor | **Best** (render golden) | Good |
| Complexity | Low initial, high later | Medium | Medium |
| controller-runtime fit | Default | **Strong** | Strong |
| V1 fit | Risky | **Yes** | Yes |

---

## Decision

We will adopt **Option B** — four internal layers plus shared libraries:

| Package | Role |
|---------|------|
| `internal/render/*` | Pure K8s object builders from `spec` — no `client`, no Bolt |
| `internal/domain/*` | Reconcile one concern: render → diff → apply → partial status |
| `internal/controller/*` | Thin pipeline per CRD: finalizers, step order, top-level requeue |
| `internal/neo4j/*` | Bolt driver — **only** consumed by `domain/` (formation, health) |
| `internal/status/*` | Condition helpers and status writer (used by controller + domain) |
| `internal/validation/*` | Webhook validators ([ADR-001](001-crd-validation-process.md)) |

`domain` packages for V1: `persistence`, `trust`, `serverconfig`, `workload`, `connectivity`, `formation` (cluster only), `scheduling` (cluster only).

### Import rules

```
api/v1beta1  ←  render  ←  domain  ←  controller
                              ↑
                         internal/neo4j
```

- `render` MUST NOT import `client`, `controller`, or `neo4j`.
- `domain` MUST NOT import `controller`.
- Cross-domain sharing via `render/` or small types in `internal/domain/shared`.

### Implementation notes

```go
// internal/controller/neo4j/reconciler.go — target shape
func (r *Neo4jReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    neo4j, err := r.load(ctx, req)
    if err != nil || neo4j == nil { return r.handleDeleteOrNotFound(ctx, neo4j, err) }

    for _, step := range r.pipeline(neo4j) {
        if result, err := step(ctx, r.Client, neo4j); err != nil || result.Requeue {
            return r.failOrRequeue(ctx, neo4j, result, err)
        }
    }
    return r.status.SetReady(ctx, neo4j)
}
```

Enforce import boundaries with `go-arch-lint` or `golangci-lint` depguard in CI (future ADR-018).

---

## Consequences

### Positive

- Most tests live in `render/` (fast) and `domain/` (envtest).
- New concerns add a domain module without bloating controller.
- Aligns with Strimzi `model`/`assembly` and CNPG `specs`/`reconciler` — familiar to operator contributors.

### Negative

- More files than kubebuilder tutorial; onboarding doc required (`layer.md` stub → link here).
- Strict boundaries require review discipline.

### Neutral

- `flow.md` name `resources/` is renamed to `render/` in code — update architecture docs when ADR accepted.

---

## References

- [layer.md](../../architecture/layer.md) · [file_structure.md](../../architecture/file_structure.md)
- [operator-benchmark/operators/cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md) — D1, D2
- [operator-benchmark/operators/strimzi.md](../../architecture/operator-benchmark/operators/strimzi.md) — D1, D2
- [ADR-003](003-neo4j-reconcile-pipeline.md) — step order
- controller-runtime / kubebuilder project layout docs
