---
name: design-consistency-reviewer
description: >-
  Reviews Neo4j operator design artifacts for consistency across helm-fields analysis,
  CRD spec, BDRs, and functional requirements. Use after helm pipeline phases or
  before locking V1 CRD OpenAPI.
---

# Design consistency reviewer

## Inputs

- `design/analysis/helm-fields/_index.csv` + `fields/*.md`
- `design/09-crd-spec/neo4j/spec.md`, `validation.md`, `status.md`
- `design/decision-records/business/*.md`
- `design/01-functional_requirements.csv`

## Checklist

### Terminology

- [ ] primaries / secondaries (`analytics`, `read`) — no cores, replicaPools, serverRole, no `secondaries[]` list
- [ ] pluginDefinitions Option E — consistent with BDR-004

### Traceability

- [ ] Every `breaking` CSV row has `bdr_id` or register entry
- [ ] Every `AGG-*` group has ≥2 helm paths
- [ ] No `draft` field doc with empty `client_need` (unless UNKNOWN)

### Spec alignment

- [ ] `crd_target` values exist or are marked `proposed` in spec.md
- [ ] Flag renames needed in `example-cluster.yaml` / `example.yaml`

### BDR hygiene

- [ ] readme.md index matches files on disk
- [ ] No duplicate decisions between BDRs
- [ ] Status proposed vs accepted accurate

### Gaps

- [ ] `validation-gaps.md` populated for orphan helm paths
- [ ] **fr-helm-coverage-validator** — `fr-coverage-report.md` gate G-FR-1 pass
- [ ] FR coverage for V1 scope (`13-v1-scope-lock.md` when exists)

## Output

Write summary to chat AND optional `design/analysis/helm-fields/review-YYYY-MM-DD.md`:

```markdown
# Design review — [date]

## Pass / fail
## Conflicts found (file:line or path)
## Recommended fixes (ordered)
## Ready for 11-helm-mapping.md export: yes/no
```

## Fixes

Apply **safe** doc fixes directly (typos, CSV status).  
Do **not** accept BDRs or change spec semantics without user confirmation.

## Export hook

When pass: suggest generating `design/11-helm-mapping.md` from `_index.csv` columns:
`helm_path`, `crd_target`, `aggregation_group`, `versioning`

## FR coverage (delegate)

Run skill **fr-helm-coverage-validator** before marking review complete.
Do not claim FR traceability pass without `fr-coverage-report.md` gate G-FR-1.
