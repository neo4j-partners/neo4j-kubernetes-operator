# Launch — Helm → CRD design pipeline

Copy into Cursor chat to start a full run (may take hours).

---

## Full pipeline

```
@neo4j-operator-design-orchestrator

Run Phases 1–6 for helm-charts/neo4j/values.yaml.

Phase 1: regenerate _index.csv with extract-helm-paths.py, verify ≥90 rows.
Phase 2: launch parallel Domain Analysts — one Task per domain in _domains.yaml (parallel batches ≠ semantic domains).
  Each analyst must use helm-field-categorizer, helm-template-tracer, neo4j-client-need-mapper.
  Write fields/*.md and update _index.csv (status=draft).
Phase 2.5: helm-semantic-concern-mapper — link analytics (end of values.yaml) to CONCERN-TOPOLOGY via neo4j-config.yaml + Neo4j docs; challenge BDR-002/004 exemplars.
Phase 3: crd-synthesis-analyst → aggregation-matrix.md, crd-candidates.md.
Phase 4: api-versioning-classifier → breaking-change-register.md.
Phase 4b: fr-helm-coverage-validator → fr-coverage-report.md (propose missing FRs, do not edit 01-functional_requirements.csv).
Phase 5: bdr-author-neo4j-operator for BDR-005, BDR-007, BDR-008, BDR-009 (parallel).
Phase 6: design-consistency-reviewer + fr-helm-coverage-validator gate G-FR-1.

Respect phase gates in orchestrator skill. Do not skip empty client_need without UNKNOWN tag.
```

---

## Single domain (incremental)

```
@helm-field-categorizer @helm-template-tracer @neo4j-client-need-mapper

Analyze domain "storage" from design/analysis/helm-fields/_domains.yaml.
Process all _index.csv rows where domain=storage.
```

---

## BDR only

```
@bdr-author-neo4j-operator

Draft BDR-005 (storage volume mode) from AGG-STORAGE-DATA in aggregation-matrix.md.
Status: proposed. Update decision-records/readme.md index.
```

---

## Semantic concerns only (topology scatter)

```
@helm-semantic-concern-mapper @helm-template-tracer

Prove CONCERN-TOPOLOGY: analytics (values.yaml ~L764) belongs with neo4j.minimumClusterSize (~L28)
via deep read of helm-charts/neo4j/templates/neo4j-config.yaml and Neo4j clustering/GDS docs.
Update semantic-concerns.yaml, semantic-concern-report.md, aggregation-matrix.md.
BDR-002 is an exemplar — document keep / amend / supersede.
```

---

## FR coverage audit (standalone)

```
@fr-helm-coverage-validator

Audit that design/01-functional_requirements.csv covers all client needs from helm-fields analysis.
Write fr-coverage-report.md. Update validation-gaps.md.
Propose missing FR rows with correct ID encoding — do not edit 01-functional_requirements.csv without approval.
```

---

## Skills index

| Skill | Role |
|-------|------|
| `neo4j-operator-design-orchestrator` | Master pipeline |
| `helm-values-inventory` | CSV skeleton |
| `helm-template-tracer` | Helm → K8s code path |
| `neo4j-client-need-mapper` | Docs + FR mapping |
| `helm-field-categorizer` | Field doc writer |
| `helm-semantic-concern-mapper` | Scattered values → semantic concerns |
| `crd-synthesis-analyst` | Aggregation + CRD |
| `api-versioning-classifier` | Breaking vs safe |
| `fr-helm-coverage-validator` | FR ↔ Helm completeness |
| `bdr-author-neo4j-operator` | BDR drafts |
| `design-consistency-reviewer` | Final gate |

Project rules: `.cursor/rules/neo4j-design-terminology.mdc`, `helm-analysis-output.mdc`, `bdr-format.mdc`
