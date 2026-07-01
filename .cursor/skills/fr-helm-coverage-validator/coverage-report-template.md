# FR coverage report — [YYYY-MM-DD]

**Validator**: fr-helm-coverage-validator  
**Helm source**: `helm-charts/neo4j/values.yaml`  
**FR source**: `design/01-functional_requirements.csv`

## Summary

| Metric | Count |
|--------|------:|
| Helm client needs (unique) | |
| Covered by existing FR | |
| Partially covered | |
| Missing from FR | |
| Out of scope (Helm-only) | |
| **Coverage %** (covered / in-scope) | |

**Gate**: Pass if missing = 0 for V1-scoped needs, or each missing has approved `out_of_scope` / deferred note.

## Coverage by domain

| Domain (_domains.yaml) | Needs | Covered | Partial | Missing |
|------------------------|------:|--------:|--------:|--------:|

## Detailed mapping

| helm_path / need_id | client_need (short) | fr_ids | status | notes |
|---------------------|---------------------|--------|--------|-------|
| | | | covered \| partial \| missing \| out_of_scope | |

## Missing requirements (proposed FR rows)

> **Do not apply without user review.** Copy proposals into `01-functional_requirements.csv`.

### Proposed level-3 rows

| Proposed ID | Parent ID | Config Group | Requirement | Description | V1 | Helm paths |
|-------------|-----------|--------------|-------------|-------------|-----|------------|
| | | | | | | |

### Proposed level-2 rows (if needed)

| Proposed ID | Parent ID | Requirement | Description | V1 |
|-------------|-----------|-------------|-------------|-----|
| | | | | |

## Requires IDs updates

| Level-1 ID | Add to Requires IDs |
|------------|---------------------|
| | |

## Variant matrix gaps

Rows to add in `03-variant_matrix.csv` for new level-3 FRs:

| Configuration FR ID | Variant | Notes |
|---------------------|---------|-------|
| | | |

## AC gaps

Missing acceptance criteria in `02-acceptance_criteria_library.csv`:

| Related FR | Suggested AC ID | Description |
|------------|-----------------|-------------|
| | | |

## Obsolete / redundant FR candidates

FR rows with no Helm path and no operator plan — flag for deprecation review only:

| FR ID | Reason |
|-------|--------|
| | |

## Actions

- [ ] User approved new FR rows
- [ ] Updated `validation-gaps.md`
- [ ] Updated `_index.csv` `fr_ids` column
- [ ] Updated `03-variant_matrix.csv`
- [ ] Updated `02-acceptance_criteria_library.csv`
