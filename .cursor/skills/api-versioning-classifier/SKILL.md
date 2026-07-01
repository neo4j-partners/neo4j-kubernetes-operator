---
name: api-versioning-classifier
description: >-
  Classifies Neo4j operator CRD design choices as breaking change vs safe iteration.
  Populates breaking-change-register.md and assigns BDR queue IDs. Use when
  finalizing helm-to-CRD mapping before writing BDRs.
---

# API versioning classifier

## Breaking change criteria

Mark `breaking` if changing post-V1 would require:

- CRD version bump (`v1beta1` → `v1`) or migration guide
- Renaming/removing spec field with stored user data
- New immutability on existing field
- Semantic change (enum values, validation error → different behavior)
- Splitting one Helm concern into multiple CRDs
- Ordinal/pool mapping change affecting running clusters

## Safe iteration criteria

Mark `safe` if:

- New **optional** spec field with default
- New status / condition fields
- New webhook **warning** (non-blocking)
- New additive CRD (does not alter Neo4j spec)
- Internal operator behavior fix with same spec contract

## Deferred

Mark `deferred` for V2+ features explicitly out of `13-v1-scope-lock.md`.

## Priority score

`priority = impact (1-5) × helm_frequency (1-5)`

| Score | Action |
|-------|--------|
| ≥ 20 | Dedicated BDR required before implementation |
| 12–19 | BDR or explicit note in spec.md |
| < 12 | Document in field doc only |

## Workflow

1. Read `aggregation-matrix.md` + `fields/*.md`
2. Update `breaking-change-register.md`
3. Set CSV column `versioning` and `bdr_id`
4. Queue BDR authoring in orchestrator Phase 5

## Reference decisions (already breaking — do not re-litigate)

- BDR-001: single Neo4j CRD
- BDR-002: topology primaries + `secondaries.analytics` / `secondaries.read`
- BDR-004: pluginDefinitions + pool refs

## Output table columns

See `breaking-change-register.md` template in repo.
