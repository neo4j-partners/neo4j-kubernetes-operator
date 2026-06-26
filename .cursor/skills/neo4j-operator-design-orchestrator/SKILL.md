---
name: neo4j-operator-design-orchestrator
description: >-
  Orchestrates the Helm-to-CRD design pipeline for the Neo4j Kubernetes operator.
  Sequences inventory, parallel domain analysis, aggregation, versioning, and BDR
  authoring. Use when mapping helm-charts/neo4j/values.yaml to CRD spec, running
  design analysis, or launching the helm-fields pipeline.
---

# Neo4j operator design orchestrator

## Prerequisites (exemplars — not final decisions)

Read as **reference examples**; validate and challenge via **helm-semantic-concern-mapper**:

- `design/decision-records/business/001-single-neo4j-crd.md` — exemplar
- `design/decision-records/business/002-neo4j-crd-topology.md` — exemplar (topology is complex; Helm scatters it)
- `design/decision-records/business/004-neo4j-plugin-topology.md` — exemplar (plugins ↔ topology)
- `design/analysis/helm-fields/semantic-concerns.yaml` — cross-cutting concern seeds
- `design/analysis/helm-fields/readme.md`
Helm source: `helm-charts/neo4j/values.yaml`  
Go model: `helm-charts/internal/model/release_values.go`

## Pipeline phases

```
Phase 1: Inventory     → _index.csv (0 rows status=todo)
Phase 2: Domain analyze → fields/*.md (parallel, 1 subagent per _domains.yaml domain)
Phase 2.5: Semantic concerns → semantic-concerns.yaml, semantic-concern-report.md (helm-semantic-concern-mapper)
Phase 3: Aggregate     → aggregation-matrix.md, crd-candidates.mdPhase 4: Version       → breaking-change-register.md
Phase 4b: FR coverage  → fr-coverage-report.md (fr-helm-coverage-validator)
Phase 5: BDR draft     → decision-records/business/005+.md (breaking only)
Phase 6: Validate      → design-consistency-reviewer + fr-helm-coverage-validator gate
```

### Phase gates (do not skip)

| Gate | Condition |
|------|-----------|
| G1 | `_index.csv` has ≥90 rows, all `helm_path` from extract script |
| G2 | Every `todo` row has matching `fields/*.md` or explicit `deferred` |
| G2.5 | **helm-semantic-concern-mapper** gates G-SC-1…G-SC-4 pass (topology scatter proven) |
| G3 | Every `AGG-*` in aggregation-matrix has ≥2 helm paths **possibly from distant values.yaml regions** |
| G4 | Every `breaking` row has `bdr_id` or entry in breaking-change-register |
| G5 | BDR exemplars challenged in `semantic-concern-report.md` — amend/supersede documented if diverging || G6 | **fr-helm-coverage-validator** pass — `missing` = 0 or waived in `fr-coverage-report.md` |

## Phase 1 — Inventory

1. Run: `python design/analysis/helm-fields/scripts/extract-helm-paths.py --output design/analysis/helm-fields/_index.csv`
2. Apply skill **helm-values-inventory** — merge any missing paths
3. Commit skeleton only if user asks; default: write files

## Phase 2 — Parallel domain analysis

Launch **one Task subagent per domain** in `design/analysis/helm-fields/_domains.yaml`:

- `subagent_type`: `generalPurpose`
- Each agent uses skills: **helm-template-tracer**, **neo4j-client-need-mapper**, **helm-field-categorizer**
- Input: domain `prefixes` + rows from `_index.csv`
- Output: `fields/<path>.md` + update CSV columns
- **Note**: `_domains.yaml` is for parallelism — semantic concerns may span domains (e.g. `analytics` end of file ↔ topology)

Suggested parallelism: all domains in one message (batch Task calls).

## Phase 2.5 — Semantic concern mapping (required)

Skill **helm-semantic-concern-mapper**:

1. Deep dive `helm-charts/neo4j/templates/` (especially `neo4j-config.yaml`, `_helpers.tpl`)
2. Link scattered values (e.g. `analytics.*` L764 + `minimumClusterSize` L28) to `CONCERN-TOPOLOGY`
3. Read Neo4j clustering / GDS docs — align product concepts
4. Challenge BDR-002/004 exemplars — document keep / amend / new BDR
5. Update `semantic-concerns.yaml`, `semantic-concern-report.md`, field docs **Semantic concerns** section

**Do not start Phase 3** until gate G-SC passes.

## Phase 3 — Aggregation
Single agent, skills **helm-semantic-concern-mapper** + **crd-synthesis-analyst**:

- Read `semantic-concerns.yaml` + `semantic-concern-report.md` first
- Read all `fields/*.md`- Update `aggregation-matrix.md`, `crd-candidates.md`
- Set `aggregation_group` on CSV rows

## Phase 4 — Versioning

Skill **api-versioning-classifier** → `breaking-change-register.md`

## Phase 4b — FR coverage

Skill **fr-helm-coverage-validator**:

- Requires Phase 2 complete (`client_need` filled in `_index.csv` / `fields/*.md`)
- Writes `design/analysis/helm-fields/fr-coverage-report.md`
- Updates `validation-gaps.md`
- Proposes missing FR rows — **does not** edit `01-functional_requirements.csv` without user approval

```bash
python design/analysis/helm-fields/scripts/check-fr-coverage.py
```

## Phase 5 — BDR authoring

For each BC entry with priority TBD resolved as high:

- One Task agent per BDR, skill **bdr-author-neo4j-operator**
- Model: prefer `claude-opus-4-8-thinking-high` for BDR writers

## Phase 6 — Validation

Skills **design-consistency-reviewer** + **fr-helm-coverage-validator** (gate G6) — fix conflicts, set CSV `status=reviewed`

## Launch prompt (copy-paste)

```
@neo4j-operator-design-orchestrator

Run the full pipeline Phases 1–6 for helm-charts/neo4j/.
Respect all phase gates. Use parallel Domain Analysts per _domains.yaml.
```

## Deferred (phase 2 of program)

- `helm-charts/neo4j-admin`
- `helm-charts/neo4j-loadbalancer`
- `helm-charts/neo4j-reverse-proxy`
