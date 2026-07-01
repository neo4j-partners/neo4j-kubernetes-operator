# Helm values → operator design analysis

Traceability workspace for mapping `helm-charts/neo4j/values.yaml` to Neo4j client needs, CRD `spec`, and BDR decisions.

**Source of truth**: [`_index.csv`](_index.csv) — one row per Helm path (or atomic group).

## Artifacts

| File | Purpose |
|------|---------|
| [`_index.csv`](_index.csv) | Master inventory — status drives pipeline progress |
| [`_domains.yaml`](_domains.yaml) | Parallel work batches for Domain Analyst subagents |
| [`fields/`](fields/) | Per-field analysis (`fields/<helm-path-dotted>.md`) |
| [`aggregation-matrix.md`](aggregation-matrix.md) | Cross-field groupings (topology↔plugins, TLS↔connectivity, …) |
| [`crd-candidates.md`](crd-candidates.md) | Draft CRD surface from Helm analysis |
| [`breaking-change-register.md`](breaking-change-register.md) | Breaking vs safe classification + BDR queue |
| [`validation-gaps.md`](validation-gaps.md) | Helm fields without FR/AC coverage |
| [`semantic-concerns.yaml`](semantic-concerns.yaml) | Cross-cutting concerns (topology scatter map) |
| [`semantic-concern-report.md`](semantic-concern-report.md) | Phase 2.5 output — BDR exemplar verdicts |

## Pipeline

Launch from Cursor chat:

```
@neo4j-operator-design-orchestrator

Phase 1: inventory helm-charts/neo4j/values.yaml using helm-values-inventory.
Write _index.csv and fields/*.md. Do not skip Phase 1 gate (0 rows with status=todo).
```

Skills live in [`.cursor/skills/`](../../.cursor/skills/).

## Status values (`_index.csv`)

| status | Meaning |
|--------|---------|
| `todo` | Not yet analyzed |
| `draft` | Field doc written, needs review |
| `reviewed` | Cross-validator approved |
| `deferred` | Out of V1 scope (document rationale) |

## Related design docs

- BDR-001 … BDR-004: [`design/decision-records/`](../../decision-records/)
- CRD spec: [`design/09-crd-spec/neo4j/`](../../09-crd-spec/neo4j/)
- Target mapping doc: [`design/11-helm-mapping.md`](../../11-helm-mapping.md) (to author from this analysis)
