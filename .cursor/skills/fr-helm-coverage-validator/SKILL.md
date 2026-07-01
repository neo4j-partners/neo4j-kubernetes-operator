---
name: fr-helm-coverage-validator
description: >-
  Validates that design/01-functional_requirements.csv fully covers client needs
  discovered from Neo4j Helm chart analysis. Cross-checks helm-fields _index.csv,
  variant matrix, and acceptance criteria. Proposes missing FR rows with correct ID
  encoding. Use after helm field analysis or when auditing FR completeness against Helm.
---

# FR ↔ Helm coverage validator

Ensures **`design/01-functional_requirements.csv`** reflects **all in-scope client needs** found in Helm analysis — not a 1:1 helm_path ↔ FR mapping, but **no missing capability** the chart exposes to users.

## Inputs (read all)

| File | Use |
|------|-----|
| `design/analysis/helm-fields/_index.csv` | `helm_path`, `client_need`, `fr_ids`, `category`, `status` |
| `design/analysis/helm-fields/fields/*.md` | Full client need + open questions |
| `design/01-functional_requirements.csv` | **Source of truth** for requirements |
| `design/02-acceptance_criteria_library.csv` | AC coverage per FR |
| `design/03-variant_matrix.csv` | Level-3 variant ↔ FR linkage |
| `design/00-vision.md` | V1 / V2 scope boundaries |
| `design/13-v1-scope-lock.md` | If present — V1 exclusions |

ID encoding: [fr-id-encoding.md](fr-id-encoding.md)

## Outputs

1. **`design/analysis/helm-fields/fr-coverage-report.md`** — from [coverage-report-template.md](coverage-report-template.md)
2. **`design/analysis/helm-fields/validation-gaps.md`** — append missing / partial rows
3. **`_index.csv`** — fill `fr_ids` where coverage confirmed; flag `status=reviewed` only when mapped

**Do not edit `01-functional_requirements.csv` without explicit user approval** — propose rows in the report only.

## Workflow

### Step 1 — Extract client needs from Helm

Build **need catalog** (deduplicated). Prefer grouping by **`CONCERN-*`** from
`semantic-concerns.yaml` (e.g. one topology need spanning `minimumClusterSize` + `analytics.*`).

| need_id | source | client_need |
|---------|--------|-------------|
| `NEED-<domain>-<nn>` | helm_path(s) | text from field doc or `_index.csv` |

Rules:

- One need may map to **multiple** `helm_path` rows (e.g. all `volumes.data.*` modes → one storage need)
- Skip needs marked `UNKNOWN — PS input required` until resolved
- Merge duplicate phrasing across domains

### Step 2 — Match against FR catalog

For each need, search `01-functional_requirements.csv`:

1. Exact / semantic match on `Description` + `Requirement`
2. Match via existing `_index.csv` `fr_ids` if present
3. Match via `Config Group` + parent level-2 (e.g. storage → `NEO-2-006`)

Assign status:

| status | Meaning |
|--------|---------|
| `covered` | ≥1 FR fully describes the need; V1 flag aligned |
| `partial` | FR exists but missing variants (check `03-variant_matrix.csv`) |
| `missing` | No FR — **propose new row** |
| `out_of_scope` | Helm-only or operator N/A — document rationale |

### Step 3 — Depth check (tree integrity)

For each `covered` / `partial` need at level 3:

- [ ] Row exists in `03-variant_matrix.csv` if Helm exposes configuration variants
- [ ] Parent `NEO-1-00x` `Requires IDs` includes the level-2 parent
- [ ] At least one AC in `02` references the FR (level 3 or parent AC group)

### Step 4 — Reverse check (FR → Helm)

Scan all `NEO-*` FR rows:

- If V1=Yes and no Helm path maps to it → flag in **Obsolete / redundant FR candidates** (do not delete)
- Operator FRs (`OP-*`) — exclude from Helm coverage %

### Step 5 — Propose missing FRs

For each `missing` need:

1. Choose parent level-2 (existing or propose new)
2. Assign ID per [fr-id-encoding.md](fr-id-encoding.md)
3. Fill CSV columns: `Domain`, `Config Group`, `Requirement`, `Description`, `Primary AC Groups`, `V1`, `V1 Justification`
4. List `Requires IDs` updates on `NEO-1-001` / `NEO-1-002` if new level-2 capability

### Step 6 — Coverage score & gate

```
in_scope_needs = all needs except out_of_scope
coverage_pct = covered / in_scope_needs * 100
```

| Gate | Pass condition |
|------|----------------|
| **G-FR-1** | `missing` = 0 OR each missing has user waiver in report |
| **G-FR-2** | Every `partial` has variant matrix action item |
| **G-FR-3** | V1=Yes missing FRs for Helm features used in `examples/` — zero |

## Optional script

```bash
python design/analysis/helm-fields/scripts/check-fr-coverage.py
```

Exits non-zero if `_index.csv` rows with `client_need` lack `fr_ids` and are not `deferred`.

## Integration

| Pipeline phase | When |
|----------------|------|
| After Phase 2 | First pass — many `fr_ids` empty expected |
| Before Phase 6 | **Required** — design-consistency-reviewer calls this skill |
| Standalone | `@fr-helm-coverage-validator` anytime |

## Launch prompt

```
@fr-helm-coverage-validator

Audit FR completeness against helm-fields analysis.
Write design/analysis/helm-fields/fr-coverage-report.md.
Update validation-gaps.md. Propose missing FR rows — do not edit 01-functional_requirements.csv.
```

## Anti-patterns

- Do not create one FR per helm_path — aggregate by **client capability**
- Do not mark `covered` without citing specific `NEO-*` ID(s)
- Do not ignore `V1=No` FRs — note as deferred, not missing
