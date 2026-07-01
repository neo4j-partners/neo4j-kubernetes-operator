# ADR-004 — Status and conditions writer

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-22 |
| **Depends on** | [ADR-003](003-neo4j-reconcile-pipeline.md) · [BDR-002](../business/neo4j/002-neo4j-crd-topology.md) · [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md) · [ADR-001](001-crd-validation-process.md) |
| **Constraints** | [`status.md`](../../crd-spec/neo4j/status.md) contract; `OP-1-003` / `AC-OP-STATUS-*` |

---

## Context

Users and automation gate on `.status` — not logs. The status model is fully specified in [`crd-spec/neo4j/status.md`](../../crd-spec/neo4j/status.md). This ADR decides **how** the operator writes status (writer placement, patch strategy, condition precedence), not the API shape.

**Forces:**

- [BDR-002](../business/neo4j/002-neo4j-crd-topology.md) — `TopologyWarning` must not block `Ready`.
- [ADR-001](001-crd-validation-process.md) — some rules are reconciler-only warnings.
- Strimzi: `KafkaNodePool` per-pool status — analogue to pool-level member summary.
- CNPG: `Ready` condition + `readyInstances` column — cheap counts without SQL.

**What breaks if wrong:** status patch conflicts, flapping `Ready`, expensive Bolt on every loop blocking reconcile.

---

## Analysis

### Option A — Central `internal/status` writer (chosen)

Single `status.Writer` used by controller and domains; domains return **facts**, writer merges conditions.

| Advantages | Disadvantages |
|------------|---------------|
| One place for Ready semantics | Writer can become large — split by file (`conditions.go`, `members.go`) |
| Avoids conflicting patches from domains | Domains must not patch status directly |
| Matches `status.md` rules | |

### Option B — Each domain patches status independently

| Advantages | Disadvantages |
|------------|---------------|
| Locality | Patch conflicts; inconsistent `observedGeneration` |
| | Hard to enforce Ready gate |

### Option C — Strimzi-style status diff in assembly only

| Advantages | Disadvantages |
|------------|---------------|
| Proven in Java | Less idiomatic in controller-runtime |
| | Still need condition merge logic |

---

## Comparison

| Criterion | A Central writer | B Per-domain | C Assembly diff |
|-----------|------------------|--------------|-----------------|
| Consistency | **Best** | Poor | Good |
| Testability | **Good** | Medium | Good |
| V1 fit | **Yes** | No | Yes |

---

## Decision

We will use **Option A** — `internal/status/writer.go` with:

### Write phases

| Phase | When | Updates |
|-------|------|---------|
| **Start** | Controller entry | `Reconciling=True`, bump if generation changed |
| **Per-step** | Domain returns `StepStatus` | Intermediate conditions (`Installed`, `StorageReady`, `TLSReady`) |
| **End** | Pipeline success | Compute `Ready`, `phase`, `serverSummary`, `endpoints` |
| **Diagnostics** | Optional async / throttled | `diagnostics.*` — **never** blocks Ready ([status.md](../../crd-spec/neo4j/status.md)) |

### Condition merge rules

1. **Single writer** sets `conditions[]` — use `meta.SetStatusCondition` pattern.
2. `observedGeneration` updated only when pipeline completes without requeue for current generation.
3. `TopologyWarning` — set by topology check from spec ([BDR-002](../business/neo4j/002-neo4j-crd-topology.md)); never blocks Ready.
4. `ClusterFormed` — set by `domain/formation` via `StepStatus` fact.
5. Domain conditions (`ServersHealthy`, …) — `Unknown` when Bolt diagnostics disabled.

### Pool / member status

- **`serverSummary`** — always from K8s list pods / STS (no Bolt) — CNPG `readyInstances` pattern.
- **`members[]`** — populated when Cluster + (monitoring on OR detail needed); Strimzi pool analogue at summary level per [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md).

### Patch strategy

```go
func (w *Writer) Patch(ctx context.Context, neo4j *v1beta1.Neo4j, mutate func(*v1beta1.Neo4jStatus)) error {
    return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
        latest := &v1beta1.Neo4j{}
        if err := w.client.Get(ctx, key, latest); err != nil { return err }
        base := latest.Status.DeepCopy()
        mutate(&latest.Status)
        if equality.Semantic.DeepEqual(base, &latest.Status) { return nil }
        return w.client.Status().Patch(ctx, latest, client.MergeFrom(latest))
    })
}
```

### Events

- `Recorder.Eventf` for user-visible transitions (`Ready`, `ScaleDown`, formation failures) — CNPG `Recorder.Event` pattern.

---

## Consequences

### Positive

- `status.md` rules enforced in one module — OpenAPI and writer stay aligned.
- Cheap summary for `kubectl wait` without Bolt.

### Negative

- Domains must return structured facts instead of patching — small API contract (`StepStatus`).

### Neutral

- `upgrade` sub-status deferred V2 — writer reserves hook.

---

## References

- [`crd-spec/neo4j/status.md`](../../crd-spec/neo4j/status.md) — **contract** (do not duplicate field tables here)
- [BDR-002](../business/neo4j/002-neo4j-crd-topology.md) · [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md)
- Strimzi `KafkaNodePool` status — [strimzi.md](../../architecture/operator-benchmark/operators/strimzi.md) D11
- CNPG `ConditionClusterReady` — [cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md) D11
- [ADR-003](003-neo4j-reconcile-pipeline.md) · [ADR-007](007-formation-and-bolt.md)
