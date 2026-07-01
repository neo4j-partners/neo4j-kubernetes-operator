# Layering rule — `render` / `domain` / `controller`

How internal packages are split. Complements [`06-flow.md`](06-flow.md) (full pipeline including `validation/`) and [`08-file_structure.md`](08-file_structure.md) (directory tree).

**Golden rule**: a `Reconcile()` method is a **pipeline of named steps** — not a 500-line function. Target **~150 lines max** in the controller; logic lives downstream.

---

## The three layers

```
                    ┌──────────────────────────────────────────┐
                    │            controller/                   │
                    │   1 reconciler per CRD · thin pipeline   │
                    │   watch → steps → status                 │
                    └────────────────────┬─────────────────────┘
                                         │ calls
                    ┌────────────────────▼─────────────────────┐
                    │              domain/                     │
                    │   business logic · diff · apply · retry  │
                    │   uses client + status writer            │
                    └──────────────┬──────────────┬────────────┘
                                   │ calls        │ reads spec
                    ┌──────────────▼──────────────┘
                    │              render/                       │
                    │   pure builders — zero client, zero I/O    │
                    │   StatefulSet · Service · Job · ConfigMap  │
                    └──────────────────────────────────────────┘

         test: table-driven          test: fake client + envtest
         no cluster needed            integration per domain module
```

| Layer | Package | Answers | Must not |
|-------|---------|---------|----------|
| **render** | `internal/render/*` | *What* K8s objects should exist? | Import `client`, call API, read `status`, branch on live cluster state |
| **domain** | `internal/domain/*` | *How* to reconcile a concern? (merge env, apply SS, formation…) | Duplicate orchestration across CRDs; import `controller/*` |
| **controller** | `internal/controller/*` | *When* to run which step? | Domain logic, raw object building, Bolt/Cypher |

`validation/` sits **above** this stack (see `06-flow.md`) — it rejects bad specs before any render/domain work runs.

---

## Responsibilities in one line each

- **`render/`** — given `spec` + owner metadata → `[]client.Object`. Deterministic, side-effect free.
- **`domain/`** — given live state + desired objects → create/update/delete, patch status, requeue on error.
- **`controller/`** — given a CR event → ordered step list, finalizers, top-level conditions.

`internal/neo4j/` (Bolt driver, Cypher, health checks) is a **client library** — consumed by `domain/`, never by `render/` or `controller/` directly for object building.

---

## Import rules

```
api/v1beta1          ← types only, no internal imports

render/*             → api/v1beta1
domain/*             → api/v1beta1, render/*, neo4j/*
controller/*         → api/v1beta1, domain/*, status/*
```

| From → To | `render` | `domain` | `controller` | `api` |
|-----------|----------|----------|--------------|-------|
| `render` | — | ❌ | ❌ | ✅ |
| `domain` | ✅ | — | ❌ | ✅ |
| `controller` | ❌ | ✅ | — | ✅ |

---

## `Neo4jReconciler` example

Domain modules are shared; only `workload` branches on `spec.topology.mode`:

```go
func (r *Neo4jReconciler) Reconcile(ctx context.Context, obj *v1beta1.Neo4j) (ctrl.Result, error) {
    if err := r.validate(ctx, obj); err != nil { return ctrl.Result{}, err }

    if err := persistence.Reconcile(ctx, r.Client, obj); err != nil { return r.error(obj, err) }
    if err := trust.Reconcile(ctx, r.Client, obj); err != nil { return r.error(obj, err) }
    if err := serverconfig.Reconcile(ctx, r.Client, obj); err != nil { return r.error(obj, err) }
    if err := workload.Reconcile(ctx, r.Client, obj); err != nil { return r.error(obj, err) } // Standalone | Cluster
    if err := connectivity.Reconcile(ctx, r.Client, obj); err != nil { return r.error(obj, err) }

    return r.setReady(obj)
}
```

Each `domain/*/Reconcile` internally calls `render/*` builders, then applies the diff. The controller does not know about StatefulSet field semantics.

---

## Testing strategy per layer

| Layer | Test type | What you assert |
|-------|-----------|-----------------|
| `render/` | Unit, table-driven | Golden files: `spec` in → YAML/struct out |
| `domain/` | Integration (envtest / fake client) | Apply creates expected objects; idempotent re-apply; status patches |
| `controller/` | Integration + selective E2E | Step order, finalizers, requeue; full flow in kind for P0 scenarios |

Push complexity **down** the stack: most coverage in `render/` + `domain/`, thinnest slice in `controller/`.

---

## Anti-patterns

| Smell | Fix |
|-------|-----|
| `client.Create` inside `render/` | Move apply to `domain/` |
| StatefulSet merge logic in controller | Move to `domain/workload` |
| Duplicate Service builder in two domains | Single builder in `render/connectivity` |
| `Reconcile()` > 150 lines | Extract named steps; one step = one domain call |
| Domain package imports another domain's internals | Share via `render/` or small `internal/domain/shared` types |
