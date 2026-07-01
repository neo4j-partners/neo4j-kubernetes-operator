---
name: adr-author-neo4j-operator
description: >-
  Authors architecture decision records (ADR) for Neo4j operator implementation.
  Covers layering, reconcile pipeline, status, testing, and deployment choices.
  Use when writing ADR-002+ after BDRs are accepted, or when promoting
  architecture/*.md drafts to decision records.
---

# ADR author — Neo4j operator

## When to write

Write an ADR when:

- The choice is **how to implement** (Go packages, reconcile order, clients, tests)
- Multiple viable patterns exist (not obvious kubebuilder default)
- The decision is **hard to reverse** or affects all controllers

Do **not** write an ADR for:

- CRD field naming or user-visible defaults → **BDR**
- Single obvious kubebuilder convention with no project fork → mention in ADR-002 appendix only

Use **decision-classifier-bdr-vs-adr** if unsure.

---

## Process

1. Read dependent **BDRs** — quote constraints, do not duplicate `spec` tables
2. Read **ADR-001** — align validation / webhook themes
3. Check [architecture-backlog.md](../operator-architecture-orchestrator/architecture-backlog.md) for row id
4. Draft with [adr-template.md](adr-template.md)
5. Minimum **2 options** (A, B); prefer 3 when trade-offs are real
6. Include **Go or pipeline sketch** — not full implementation
7. Status default: `proposed` — user accepts
8. Update `docs/02-technical-design/decision-records/readme.md` index
9. Cross-link from related BDR **References** (Triggers ADR-NNN)

---

## ADR queue

### Track 1 — Cross-cutting (benchmark-driven)

| ID | Title | Backlog / evidence |
|----|-------|-------------------|
| ADR-011 | Reference architecture synthesis | K-02, `synthesis.md` |
| ADR-012 | Go & dependency policy | N-01..N-04 |
| ADR-013 | Operator & workload RBAC | L-01..L-04, BDR-003 |
| ADR-014 | Watch scope & cache | L-05, H-02 |
| ADR-015 | Pod security & platform profiles | L-06..L-09 |
| ADR-016 | Cloud identity for workloads | M-02..M-06 |
| ADR-017 | Platform wiring strategy | M-01, M-08 |
| ADR-018 | CI quality gates | N-05..N-08 |
| ADR-019 | Release & compatibility matrix | N-09, J-02 |
| ADR-020 | Testing pyramid | I-01..I-07 |

### Track 2 — Internal implementation

| ID | Title | Backlog rows |
|----|-------|--------------|
| ADR-001 | CRD validation process | A-01 ✓ |
| ADR-002 | Package layering | A-02, A-03 — **after ADR-011 proposed** |
| ADR-003 | Reconcile pipeline order | B-01, B-02 |
| ADR-004 | Status & conditions | F-01..F-06 |
| ADR-005 | Render conventions | C-01..C-04 |
| ADR-006 | Apply & idempotency | D-01, D-03 |
| ADR-007 | Formation & Bolt | D-04, D-05, E-* |
| ADR-008 | Finalizers & deletion | B-06, B-07 |
| ADR-009 | Watches & predicates | B-06 |
| ADR-010 | Operator deployment & HA | H-01, J-01 |

Renumber if index conflicts — check readme first.

**Before ADR-002:** run `@operator-benchmark-analyst` on ≥2 Tier-1 operators.

---

## Quality bar

- **Depends on** lists BDRs that fix the API contract
- **Decision** section names packages under `internal/`
- Comparison table includes **testability** and **V1 fit**
- Link `architecture/layer.md` sections when promoting draft content
- No new `spec.*` fields without BDR + spec.md change

---

## BDR ↔ ADR linking

In ADR:

```markdown
| **Depends on** | [BDR-009](../business/neo4j/009-scale-pool-ordinal-semantics.md) — pool StatefulSets |
```

In BDR (add when ADR accepted):

```markdown
**Triggers:** [ADR-003](../architecture/003-neo4j-reconcile-pipeline.md)
```

---

## Do not

- Set `Status: accepted` without user review
- Encode product scope (V1 in/out) — cite `13-v1-scope-lock.md`
- Duplicate ADR-001 mechanism table — extend with new rule IDs only
