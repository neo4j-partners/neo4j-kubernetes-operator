---
name: bdr-author-neo4j-operator
description: >-
  Authors business decision records (BDR) for Neo4j operator breaking API choices.
  Produces options tables, comparison matrices, and recommendations from helm-fields
  analysis. Use when writing BDR-005+ for storage, TLS, connectivity, config surface.
---

# BDR author — Neo4j operator

## When to write

Only for entries in `breaking-change-register.md` with:

- `versioning: breaking`
- No existing accepted BDR covering the same `AGG-*` group

Skip if BDR-001/002/004 already decide — cross-link instead.

## Process

1. Read `AGG-*` group in `aggregation-matrix.md` and `semantic-concerns.yaml`
2. Read all related `fields/*.md` — **Semantic concerns** sections
3. Read exemplars: `002-neo4j-crd-topology.md`, `004-neo4j-plugin-topology.md` as **examples to validate or challenge**
4. Draft using [bdr-template.md](bdr-template.md)
5. Minimum **3 options** (A, B, C) unless genuinely only one viable
6. Include YAML sketches per option
7. Separate **Proposer direction** and **Recommendation**
8. If revising exemplar: status `proposed` + cross-link `amends: BDR-002` or `supersedes: BDR-002`
9. Update `decision-records/readme.md` index

## BDR exemplar policy

| Situation | Action |
|-----------|--------|
| Helm semantics match BDR-002 topology model | Reference as `aligns with BDR-002` |
| Operator must diverge from Helm (e.g. 1 STS N replicas) | `amend-BDR-002` with `helm_vs_operator_gap` evidence |
| Fundamentally different API | New BDR + `supersedes` note |

## BDR queue (initial)

| ID | Title | AGG group |
|----|-------|-----------|
| BDR-005 | Storage volume mode model | AGG-STORAGE-DATA |
| BDR-007 | Service exposure & connectivity | AGG-EXPOSURE |
| BDR-008 | Neo4j config surface | AGG-CONFIG-SURFACE |
| BDR-009 | Scale / pool ordinal semantics | AGG-TOPO-ROLES |

Renumber if index conflicts — check readme first.

## Quality bar

- Every option: advantages **and** disadvantages table
- Reference Helm paths explicitly
- Reference Neo4j official docs URLs
- State Helm parity column in comparison table

## Do not

- Set `Status: accepted` without user review — default `proposed`
- Create ADR in same file — link ADR separately if needed
