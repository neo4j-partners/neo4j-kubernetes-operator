# ADR-007 — Formation and Bolt client usage

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-22 |
| **Depends on** | [ADR-003](003-neo4j-reconcile-pipeline.md) · [BDR-002](../business/neo4j/002-neo4j-crd-topology.md) · [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md) |
| **Constraints** | V1 — replace Helm `neo4j-operations` Job; no sidecar requirement for MVP |

---

## Context

Cluster mode requires enabling new servers in Neo4j (`ENABLE SERVER` / equivalent), waiting for quorum, and decommissioning on scale-in. Helm uses a separate operations Job; the operator proposal replaces that with reconciler-driven admin API calls.

**Forces:**

- [BDR-002](../business/neo4j/002-neo4j-crd-topology.md) — Cluster formation and `minimumMembers`.
- [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md) — scale per pool STS.
- CNPG: instance manager **inside pod** + operator SQL for failover/scale — **no external Job**.
- Strimzi: Kafka Admin API from operator JVM for broker lifecycle.

**What breaks if wrong:** split-brain, servers stuck `Free`, scale-in leaves ghost members, Bolt creds leaked into render layer.

---

## Analysis

### Option A — Operator-only Bolt client (chosen for V1)

`internal/neo4j/` driver connects to **existing** ready pod (primary or `:7687` via headless Service). `domain/formation` runs Cypher/admin procedures from operator pod.

| Advantages | Disadvantages |
|------------|---------------|
| No operand sidecar — simpler pod spec | Operator needs network path to Neo4j Service |
| Clear separation ([ADR-002](002-package-layering.md)) | Bolt creds in operator memory — secure SA/RBAC |
| Matches "replace operations Job" goal | Must handle "no leader yet" with requeue |

### Option B — CNPG-style instance manager sidecar

Each pod runs management binary with local API.

| Advantages | Disadvantages |
|------------|---------------|
| Proven CNPG pattern | Extra container; image coupling |
| Local admin without cluster networking | More moving parts for V1 |

### Option C — Kubernetes Job per scale/formation action

Helm parity.

| Advantages | Disadvantages |
|------------|---------------|
| Familiar to Helm users | Not continuous reconcile; Job RBAC sprawl |
| | **Explicitly rejected** in operator proposal |

**Improvement over Helm:** idempotent formation in reconcile loop with backoff — CNPG/Strimzi style.

---

## Comparison

| Criterion | A Operator Bolt | B Sidecar | C Job |
|-----------|-----------------|-----------|-------|
| V1 simplicity | **Best** | Medium | Poor |
| Reconcile idempotency | **Best** | Good | Poor |
| CNPG alignment | Partial | **Best** | No |

---

## Decision

We will implement **Option A** for V1; revisit Option B if in-pod bootstrap proves necessary.

### Bolt client (`internal/neo4j/`)

| Rule | Detail |
|------|--------|
| Library | Official `neo4j-go-driver/v5` |
| Routing | `neo4j://` to client Service; direct bolt to pod for admin when needed |
| Auth | From auth Secret ([BDR-001](../business/neo4j/001-single-neo4j-crd.md)) |
| TLS | From `spec.trust` material ([BDR-006](../business/neo4j/007-tls-trust-model.md)) |
| Timeouts | Connect 10s; transaction 30s; formation overall bounded by requeue |
| Allowlist | Documented procedures/Cypher only — no arbitrary user queries |

### Formation sequence (`domain/formation`)

**Scale out** (new ordinal ready):

1. Wait pod Ready + Bolt port open on new member.
2. `ENABLE SERVER` / equivalent for server name derived from [ADR-005](005-render-conventions.md) pod name.
3. Poll `SHOW SERVERS` until `Enabled` + `Available`.
4. Set `ClusterFormed` / clear `Reconciling` when `minimumMembers` satisfied.

**Scale in** ([BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md)):

1. Set `ServersPendingDrain=True`.
2. Deallocate / remove server via admin API.
3. After Neo4j confirms removed, allow STS scale-down ([ADR-003](003-neo4j-reconcile-pipeline.md) order — formation before replica drop).

**Standalone:** formation package noops; `ClusterFormed` not required.

### Error handling

| Error | Action |
|-------|--------|
| Transient network / leader election | `RequeueAfter: 15s` |
| Auth failure | `Error=True`, terminal until Secret fixed |
| Quorum lost | `ClusterFormed=False`, `Degraded` phase |

```go
// domain/formation/reconcile.go (sketch)
func Reconcile(ctx context.Context, c client.Client, n *v1beta1.Neo4j) (Result, error) {
    if !mode.IsCluster(n) { return Result{}, nil }
    driver, err := neo4j.Connect(ctx, c, n) // internal/neo4j
    if err != nil { return Requeue(15 * time.Second), nil }
    defer driver.Close(ctx)
    return ensureMembersEnabled(ctx, driver, n)
}
```

---

## Consequences

### Positive

- Eliminates Helm operations chart dependency.
- Formation testable with testcontainers Neo4j in integration tier.

### Negative

- Operator pod must reach Neo4j network — document NetworkPolicy allowances.
- Bolt driver version must track supported Neo4j versions (release matrix — future ADR-019).

### Neutral

- Sidecar (Option B) remains documented alternative if bootstrap hooks require local process.

---

## References

- [`20-operator-proposal.md`](../../../00-discovery/20-operator-proposal.md) — cluster membership manager
- [BDR-002](../business/neo4j/002-neo4j-crd-topology.md) · [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md)
- CNPG instance manager + scale — [cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md) D12
- Strimzi `KafkaAssemblyOperator` — [strimzi.md](../../architecture/operator-benchmark/operators/strimzi.md) D12
- [ADR-002](002-package-layering.md) · [ADR-003](003-neo4j-reconcile-pipeline.md) · [ADR-004](004-status-and-conditions.md)
