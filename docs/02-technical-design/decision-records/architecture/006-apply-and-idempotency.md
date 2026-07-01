# ADR-006 — Apply strategy and idempotent reconcile

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-22 |
| **Depends on** | [ADR-002](002-package-layering.md) · [ADR-005](005-render-conventions.md) · [BDR-005](../business/neo4j/005-storage-volume-mode.md) · [BDR-008](../business/neo4j/008-neo4j-config-surface.md) |
| **Constraints** | controller-runtime `client.Client`; StatefulSet immutables |

---

## Context

`domain` packages must create or update child objects idempotently. Wrong apply strategy causes field manager conflicts (SSA), clobbered user merges on ConfigMaps, or unnecessary StatefulSet replacements.

**Forces:**

- [BDR-008](../business/neo4j/008-neo4j-config-surface.md) — operator-owned keys in ConfigMaps vs user `spec.config` passthrough.
- StatefulSet `volumeClaimTemplates` and `serviceName` are largely immutable.
- CNPG uses strategic merge / patch patterns extensively; Strimzi uses Fabric8 `resource().createOrReplace()` with model diff.

---

## Analysis

### Option A — Server-Side Apply (SSA) everywhere

`client.Patch(..., client.Apply, client.FieldOwner("neo4j-operator"))`.

| Advantages | Disadvantages |
|------------|---------------|
| Field ownership clarity | STS spec updates still limited; conflicts with other field managers |
| Kubernetes direction | Harder merge for ConfigMap multi-writer scenarios |

### Option B — CreateOrUpdate with semantic equality (chosen)

`controllerutil.CreateOrUpdate` + compare desired vs live using `equality.Semantic.DeepEqual` on relevant spec sections; strategic merge patch for updates.

| Advantages | Disadvantages |
|------------|---------------|
| Well-trodden in kubebuilder operators | Must carefully choose compared fields |
| Works with STS, Services, RBAC | Field ownership less explicit than SSA |

### Option C — Replace (delete + create) on drift

| Advantages | Disadvantages |
|------------|---------------|
| Simple mental model | Disruptive for STS/Pods — **unacceptable** |

### ConfigMaps — hybrid within Option B

| Surface | Strategy |
|---------|----------|
| Operator-owned keys (`server.config`, discovery, JVM block) | Full replace of data keys operator owns |
| User passthrough `spec.config` | Merge: operator sets defaults, user keys win on conflict only where BDR-008 allows |

**Improvement over blind SSA:** explicit **owned-key list** per ConfigMap in `domain/serverconfig` — avoids CNPG-style accidental full replace of user data.

---

## Comparison

| Criterion | A SSA | B CreateOrUpdate | C Replace |
|-----------|-------|------------------|-----------|
| STS safety | Medium | **Best** | Poor |
| Config merge control | Medium | **Best** (custom) | Poor |
| V1 fit | Possible | **Yes** | No |

---

## Decision

We will use **Option B** — shared helper in `internal/domain/shared/apply.go`:

```go
func CreateOrUpdate(ctx context.Context, c client.Client, obj client.Object, mutate func() error) (controllerutil.OperationResult, error) {
    return controllerutil.CreateOrUpdate(ctx, c, obj, mutate)
}
```

### Rules per kind

| Kind | Apply rule |
|------|------------|
| StatefulSet | Update `spec.template`, `spec.replicas`, `spec.volumeClaimTemplates` only on create; replicas via dedicated scale path; **no** wholesale replace |
| Service | Update ports, selectors, annotations |
| ConfigMap / Secret | See BDR-008 merge table; rotate Secrets via trust reload ([BDR-006](../business/neo4j/007-tls-trust-model.md)) |
| Role / RoleBinding / SA | Replace rules when `spec` changes; CNPG `IsServiceAccountAligned` pattern |
| PVC | Create only; shrink blocked ([ADR-001](001-crd-validation-process.md) STO-004) |

### Idempotency contract

- Same `spec` + same cluster state → **no-op** apply (OperationResultNone).
- Reconcile loops must tolerate `AlreadyExists`, `Conflict` — retry with `client.MergeFrom` on status only.

### Drift

- Operator **corrects** drift on operator-owned fields (labels, ownerRef, container image from spec).
- Does **not** adopt orphan resources without ownerRef — log event, optional metric.

---

## Consequences

### Positive

- Predictable upgrades; matches controller-runtime ecosystem.
- ConfigMap behaviour testable in unit tests without cluster.

### Negative

- Must maintain per-kind mutate functions — not one generic apply.

### Neutral

- May adopt SSA for select kinds (Services) in V1.1 if field conflicts appear — document in amend.

---

## References

- [BDR-005](../business/neo4j/005-storage-volume-mode.md) · [BDR-008](../business/neo4j/008-neo4j-config-surface.md)
- CNPG patch/align patterns — `pkg/specs/serviceaccount_test.go` ([cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md))
- controller-runtime `CreateOrUpdate` docs
- [ADR-002](002-package-layering.md) · [ADR-003](003-neo4j-reconcile-pipeline.md)
