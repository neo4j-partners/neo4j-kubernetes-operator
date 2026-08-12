# ADR-008 — Finalizers and deletion

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-22 |
| **Depends on** | [ADR-003](003-neo4j-reconcile-pipeline.md) · [ADR-007](007-formation-and-bolt.md) · [BDR-005](../business/neo4j/005-storage-volume-mode.md) |
| **Constraints** | Safe scale-in; PVC retention policy |

---

## Context

Deleting a `Neo4j` CR must decommission cluster members, remove Kubernetes children, and honour volume retention. Without finalizers, GC can orphan PVCs or remove pods before Neo4j deallocation completes.

**Forces:**

- [BDR-005](../business/neo4j/005-storage-volume-mode.md) — `Dynamic` vs `Existing` PVCs; retain policy.
- [ADR-007](007-formation-and-bolt.md) — scale-in requires Bolt decommission **before** pod deletion.
- CNPG: finalizer on Cluster; ordered instance deletion in `cluster_scale.go` / `finalizers_delete.go`.
- Strimzi: cascading deletion via owner refs + assembly cleanup.

---

## Analysis

### Option A — Single finalizer `neo4j.com/finalizer` with ordered teardown (chosen)

| Advantages | Disadvantages |
|------------|---------------|
| Explicit deletion path | Must handle stuck finalizer (support runbook) |
| Bolt decommission before STS delete | |
| Matches CNPG pattern | |

### Option B — Owner references only (no finalizer)

| Advantages | Disadvantages |
|------------|---------------|
| Simpler | No hook for Neo4j decommission or PVC retain |
| | Race: pods gone before `ENABLE SERVER` reverse |

### Option C — Per-pool finalizers

| Advantages | Disadvantages |
|------------|---------------|
| Fine-grained | UX complexity; overkill V1 |

---

## Comparison

| Criterion | A Finalizer | B Owner only | C Per-pool |
|-----------|-------------|--------------|------------|
| Data safety | **Best** | Poor | Good |
| Neo4j decommission | **Yes** | No | Yes |
| V1 fit | **Yes** | No | No |

---

## Decision

We will use **Option A**.

### Finalizer

```yaml
metadata:
  finalizers:
    - neo4j.com/finalizer
```

Added on first successful reconcile; removed when teardown complete.

### Deletion order (reverse of provision where applicable)

| Step | Action |
|------|--------|
| 1 | Set `phase=Terminating` (or keep `Deleting` condition), `Reconciling=True` |
| 2 | **Cluster:** Bolt decommission all members ([ADR-007](007-formation-and-bolt.md)) |
| 3 | Scale STS to 0 per pool (or delete STS) |
| 4 | Delete Services, ConfigMaps, RBAC (owner ref GC may assist) |
| 5 | PVCs per retention policy ([BDR-005](../business/neo4j/005-storage-volume-mode.md)) — default **Retain** data PVCs unless annotation `neo4j.com/delete-pvc: "true"` |
| 6 | Remove finalizer |

### PVC policy

| `spec.volumes.data` mode | On CR delete |
|--------------------------|--------------|
| `Dynamic` | Retain PVC by default (document in user guide); optional explicit wipe annotation |
| `Existing` | Never delete user-provisioned PVC |

**Improvement over Helm:** document single annotation — Helm pre-delete hooks are brittle in GitOps.

### Stuck deletion

- Timeout decommission → `Error` condition on CR, event, metric; finalizer remains until admin forces or fixes Bolt.
- `neo4j.com/force-delete: "true"` annotation (V1.1) — skip Bolt — **not V1**.

---

## Consequences

### Positive

- No orphaned Neo4j servers in cluster metadata.
- Clear support path for retained PVCs.

### Negative

- Delete latency = decommission time + pod termination.

### Neutral

- Backup CRDs (V2) may add cross-finalizer ordering — separate controller.

---

## References

- [BDR-005](../business/neo4j/005-storage-volume-mode.md)
- CNPG `finalizers_delete.go`, `scaleDownCluster` — [cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md)
- [ADR-003](003-neo4j-reconcile-pipeline.md) · [ADR-007](007-formation-and-bolt.md)
