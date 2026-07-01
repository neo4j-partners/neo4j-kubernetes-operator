---
name: operator-architecture-orchestrator
description: >-
  Orchestrates phase 2 software architecture for the Neo4j Kubernetes operator.
  Sequences ADR backlog triage, BDR-vs-ADR classification, and ADR authoring.
  Use when designing reconcile pipeline, package layout, testing strategy, or
  starting implementation after CRD/BDR phase 1 is stable.
---

# Operator architecture orchestrator — phase 2

## Prerequisites

Phase 1 (API / BDR) should be **stable enough to implement**:

- Accepted BDRs for V1 scope: topology, storage, connectivity (MVP subset), config, TLS
- [`crd-spec/neo4j/spec.md`](../../../docs/02-technical-design/crd-spec/neo4j/spec.md) + [`validation.md`](../../../docs/02-technical-design/crd-spec/neo4j/validation.md)
- [`13-v1-scope-lock.md`](../../../docs/00-discovery/13-v1-scope-lock.md)

**Phase 2a (benchmark) should run before Track 2 ADRs are accepted:**

- [`operator-benchmark/readme.md`](../../../docs/02-technical-design/architecture/operator-benchmark/readme.md)
- At least 2× `operator-benchmark/operators/*.md` + `synthesis.md` draft

Draft architecture notes (not yet ADRs):

- [`architecture/layer.md`](../../../docs/02-technical-design/architecture/layer.md)
- [`architecture/file_structure.md`](../../../docs/02-technical-design/architecture/file_structure.md)
- [`architecture/flow.md`](../../../docs/02-technical-design/architecture/flow.md)

Accepted ADR: **ADR-001** (validation layering).

---

## Skills in this phase

| Step | Skill |
|------|-------|
| Study reference operators | **operator-benchmark-analyst** |
| Classify a topic | **decision-classifier-bdr-vs-adr** |
| Write ADR | **adr-author-neo4j-operator** |
| Review consistency | **design-consistency-reviewer** (extend to ADRs) |

---

## Pipeline

```
Phase 0: Benchmark (2a)  → operator-benchmark/operators/*.md + synthesis.md
Phase A: Backlog triage  → architecture-backlog.md (domains A–O)
Phase B: Classify        → decision-classifier-bdr-vs-adr per open row
Phase C: ADR draft       → Track 1 (011–020) then Track 2 (002–010)
Phase D: Promote drafts  → layer.md / flow.md → ADR or link
Phase E: Validate        → cross-links BDR ↔ ADR ↔ spec ↔ benchmark
```

### Phase gates

| Gate | Condition |
|------|-----------|
| G-0 | ≥2 Tier-1 operator sheets complete; `synthesis.md` has Adopt/Adapt/Avoid tables |
| G-A1 | Every V1-critical backlog row has ADR id or `deferred` + reason |
| G-A2 | No ADR defines CRD fields — cites BDR instead |
| G-A3 | ADR-011 **proposed** before ADR-002 **accepted** |
| G-A4 | ADR-013 (RBAC) cites BDR-003 + benchmark evidence |
| G-A5 | `decision-records/readme.md` architecture index matches files on disk |

---

## Phase 0 — Benchmark (2a)

Skill **operator-benchmark-analyst**:

1. CNPG + Strimzi first (layout, testing, scope)
2. ECK + MongoDB (RBAC, workload SA)
3. Update `operator-benchmark/synthesis.md`
4. Queue Track 1 ADRs from synthesis

**Do not accept ADR-002** until G-0 passes.

---

## Phase A — Backlog triage

1. Open [architecture-backlog.md](architecture-backlog.md)
2. For each `·` row in sections A–J, set: `candidate ADR-NNN` | `defer post-V1` | `fold into ADR-00x`
3. Write summary table to `docs/02-technical-design/architecture/readme.md`

---

## Phase B — Classify

For each open topic, run **decision-classifier-bdr-vs-adr**:

- If **BDR gap** found → stop architecture work; queue BDR via **bdr-author-neo4j-operator**
- If **split** → draft BDR amendment + ADR in parallel
- If **ADR only** → proceed to Phase C

---

## Phase C — ADR authoring

### Track 1 — Cross-cutting (benchmark-driven, start here after Phase 0)

| ADR | Title | Evidence |
|-----|-------|----------|
| ADR-011 | Reference architecture synthesis | `synthesis.md` |
| ADR-012 | Go & dependency policy | CNPG, Strimzi go.mod |
| ADR-013 | Operator & workload RBAC | ECK, MongoDB, BDR-003 |
| ADR-014 | Watch scope & cache | Strimzi, CNPG |
| ADR-015 | Pod security & platform profiles | ECK restricted, dependencies.md |
| ADR-016 | Cloud identity for workloads | CNPG, Percona backup |
| ADR-017 | Platform wiring strategy | dependencies.md |
| ADR-018 | CI quality gates | Tier-1 CI workflows |
| ADR-019 | Release & compatibility matrix | Tier-1 release docs |
| ADR-020 | Testing pyramid | CNPG e2e, envtest layout |

### Track 2 — Internal implementation (after ADR-011 proposed)

1. **ADR-002** — layering — promote `layer.md`
2. **ADR-003** — reconcile pipeline — promote `flow.md`
3. **ADR-004** … **ADR-010** — see backlog Track 2 table

One Task agent per ADR, skill **adr-author-neo4j-operator**.

---

## Phase D — Promote drafts

| Draft file | Action |
|------------|--------|
| `layer.md` | Accept content into ADR-002; leave stub linking to ADR |
| `file_structure.md` | Reference from ADR-002; update when packages land |
| `flow.md` | Merge into ADR-003 |
| `reconciliation.md` | Fill via ADR-003 + ADR-006 |

---

## Phase E — Validation

Extend **design-consistency-reviewer** checklist:

- [ ] Each ADR lists **Depends on** BDRs
- [ ] Each BDR with implementation complexity links **Triggers** ADR(s)
- [ ] Reconcile order in ADR-003 matches `domain/` packages in `file_structure.md`
- [ ] validation.md mechanism column matches ADR-001 (+ amendments)
- [ ] No `spec` field invented in ADR without BDR + spec.md update

---

## Launch prompt

```
@operator-architecture-orchestrator

Run phase 0 (benchmark CNPG + Strimzi), then phases A–E.
Draft ADR-011 synthesis before ADR-002 layering.
Respect gates G-0 and G-A1–G-A5.
```

---

## Relationship to phase 1 orchestrator

| Phase | Orchestrator | Output |
|-------|--------------|--------|
| 1 — API design | `neo4j-operator-design-orchestrator` | BDRs, CRD spec, helm-fields |
| 2 — Software arch | `operator-architecture-orchestrator` | ADRs, package map, test strategy |

Do not re-open accepted BDRs from phase 1 unless implementation proves API gap — use **decision-classifier** first.
