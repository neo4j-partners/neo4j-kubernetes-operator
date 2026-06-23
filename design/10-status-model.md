# Status model — cross-CRD index

Per-CRD status design lives in **`09-crd-spec/<crd>/status.md`**. This file indexes them and captures shared conventions.

**Status**: `[~]` partial — `Neo4j` documented (upgrade sub-status, diagnostics, replicas summary); day-2 CRDs pending.

---

## Shared conventions

| Convention | Detail |
|------------|--------|
| Condition schema | Kubernetes `metav1.Condition` — `type`, `status`, `reason`, `message`, `lastTransitionTime`, `observedGeneration` |
| Operator conditions | All managed CRDs expose at minimum `Ready`, `Reconciling`, `Error` (`OP-2-003-STATUS-01`) |
| Generation gate | `Ready=True` only when `status.observedGeneration == metadata.generation` |
| Warnings vs errors | Topology / best-practice guidance → warning conditions; admission rejects invalid specs |
| Phase non-regression | Coarse `phase` does not regress after bootstrap; long-running work uses sub-status (`upgrade`, domain conditions) |
| Diagnostics optional | Deep Bolt collection failures do not block `Ready`; use `collectionError` + operational health conditions |
| Observability contract | Key phases/conditions expose Prometheus metrics (per-CRD detail in `09-crd-spec`) |

---

## Per-CRD status references

| CRD | Document | Phase model | Notes |
|-----|----------|-------------|-------|
| **`Neo4j`** | [`09-crd-spec/neo4j/status.md`](09-crd-spec/neo4j/status.md) | `Pending` → `Running` / `Failed` | `upgrade` sub-status; `replicas` summary; `diagnostics`; `TopologyWarning` (BDR-002); Neo4j 5.26+ `SHOW SERVERS` fields |
| `Neo4jDatabase` | *(pending)* | `Creating` → `Online` | Logical DB inside `neo4jRef` |
| `Neo4jBackup` / `Neo4jBackupSchedule` | *(pending)* | Job-driven | `lastBackup`, schedule timestamps |
| `Neo4jRestore` | *(pending)* | Job-driven | Restore progress |

---

## Traceability

- `OP-1-003` — operator and workload status
- `OP-2-003-STATUS-01` — basic conditions (V1)
- `OP-2-003-STATUS-02` — upgrade sub-status + domain conditions (V1 — not deferred for `Neo4j`)
