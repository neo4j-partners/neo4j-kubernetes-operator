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

### Where `status.phase` comes from (F-07)

A second, independent choice inside the same writer. `phase` is named as a lifecycle stage, but it is
computed from the current observation alone, so any not-ready re-reports a bootstrap phase.

**Option P1 — pure observation (today).** Stateless and idempotent, and it cannot go stale. But
`Running` → `Bootstrapping` on any blip, `Degraded` is never assigned, and a scale or a roll reports
`Bootstrapping` for its whole duration — minutes, while nothing is wrong.

**Option P2 — gate on the prior phase.** Anything not ready after `Running` becomes `Degraded`. One
short change, and it does kill the regression. But it calls every planned roll, scale and upgrade a
degradation, which contradicts [`status.md`](../../crd-spec/neo4j/status.md): during an upgrade the
phase must stay `Running`.

**Option P3 — discriminate wanted from unwanted (chosen).** The question is not *was it `Running`
before* but *did we ask for this not-ready state*. Costs the writer its purity — it has to read the
prior phase — and leaves one situation without a good value (see the open point below).

| Criterion | P1 observation | P2 prior phase | P3 intent |
|-----------|----------------|----------------|-----------|
| Truthful during a planned roll or scale | No | No | **Yes** |
| `Degraded` ever assigned | No | Yes | **Yes** |
| Testability | Best — pure function | Good | **Good** — table-driven over five inputs |
| V1 fit | No — `status.md` rules stay unenforced | No | **Yes** |

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

### Computing `status.phase`

Option **P3**, and not to be read as the write moments above: this is the published `phase` value.
`phase` answers *where the object is in its life and what the operator is doing*; health is carried by
`Ready` and the domain conditions, never by a phase downgrade. Two rules follow. The first is already
contract in [`status.md`](../../crd-spec/neo4j/status.md), only unenforced: `phase` never regresses to
`Provisioning` or `Bootstrapping` once the object has been `Ready`. The second generalises a rule
`status.md` states for upgrades alone — phase stays `Running`, no dedicated `Upgrading` — to **any**
change the user asked for, a roll and a scale included, on the grounds that the user cannot see why
a member is restarting and does not care: what they asked for is in flight either way.

Every signal needed is already read by `ObserveAndWrite`:

| Signal | Source | What it says |
|--------|--------|--------------|
| `allReady` | computed | everything the CR asked for is serving |
| `status.version` | the object the reconciler `Get`s | whether the CR was ever established |
| `Generation != Status.ObservedGeneration` | the CR | the spec changed and the change is not absorbed yet |
| `UpdateRevision != CurrentRevision` | pool StatefulSet — already fetched, only `ReadyReplicas` read today | a roll is still in flight |
| `ServersPendingDrain=True` | conditions — already consulted for `allReady` | a scale-in is waiting on Neo4j ([ADR-007](007-formation-and-bolt.md)) |

A change is **in flight** when any of the last three holds. That single predicate is what separates a
roll, a scale or an upgrade from an incident:

```go
switch {
case offlineMode(neo4j):
    phase = Maintenance
case allReady:
    phase = Running
case !anySTSFound:
    phase = Provisioning
case !established(neo4j.Status.Phase):
    phase = Bootstrapping // never been Ready — a genuine first install
case changeInFlight(neo4j, rolling, drainPending):
    phase = Running // a roll, a scale or an upgrade we asked for; conditions carry the detail
default:
    phase = Degraded // unplanned loss after the object was established
}
```

`established` reads `status.version`, not the prior phase. The writer sets that field only under
`allReady`, so a non-empty value means the workload served at least once — and unlike the phase it
survives the `Failed` that a transient pipeline error leaves behind. Keying establishment on the
phase would let one failed reconcile hand a served CR back to `Bootstrapping`, which is the very
regression this decision removes.

One deliberate simplification: the `changeInFlight` branch wins even when a member is unhealthy for a
reason unrelated to the change, which narrows `status.md`'s "`Running` **or `Degraded`** (if members
unhealthy)" during an upgrade to `Running` alone. A rolling update makes members not-ready by design,
one at a time, so no cheap signal separates *not ready because we are rolling it* from *not ready for
its own reasons*. Rather than guess, the phase reports the change and `Ready` plus its reason report
the health — which is the principle above, applied where it costs something.

One contract consequence, settled in [`status.md`](../../crd-spec/neo4j/status.md) rather than here:
`Degraded` used to read *partial availability*, but the `default` branch above also catches total loss
— a Standalone whose only pod is gone is not partially available. The wording widened to *reduced or
lost availability after the CR had served* rather than a new enum value being added, since a published
value would cost API surface for a situation `Ready=False` already describes. `Pending` stays
unassigned; its fate is a separate call.

There is one ordering wrinkle worth naming rather than discovering later: `!anySTSFound` is tested
before `!established`, so an established CR whose StatefulSet was deleted by hand reports
`Provisioning` — a regression the first rule forbids. It is left as is because the pipeline re-applies
the StatefulSet before the writer runs, making the state reachable only by hand.

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
- A roll, a scale or an upgrade stops reporting a bootstrap phase. Today a healthy cluster reads
  `Bootstrapping` for the whole length of a secondary drain, which can run to the drain budget.
- `Degraded` starts occurring, so the published enum stops advertising a value that never happens.

### Negative

- Domains must return structured facts instead of patching — small API contract (`StepStatus`).
- `phase` becomes path-dependent: the writer reads the prior phase. Recovery is unconditional
  (`allReady` → `Running`), so nothing can strand in `Degraded`, but phase is no longer a pure
  function of the observation and its tests need the prior value as an input.

### Neutral

- `upgrade` sub-status deferred V2 — writer reserves hook.
- `Pending` remains unassigned — untouched by this decision.

---

## References

- [`crd-spec/neo4j/status.md`](../../crd-spec/neo4j/status.md) — **contract** (do not duplicate field tables here)
- [BDR-002](../business/neo4j/002-neo4j-crd-topology.md) · [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md)
- Strimzi `KafkaNodePool` status — [strimzi.md](../../architecture/operator-benchmark/operators/strimzi.md) D11
- CNPG `ConditionClusterReady` — [cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md) D11
- [ADR-003](003-neo4j-reconcile-pipeline.md) · [ADR-007](007-formation-and-bolt.md)
