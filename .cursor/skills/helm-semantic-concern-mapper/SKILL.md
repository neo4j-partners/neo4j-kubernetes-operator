---
name: helm-semantic-concern-mapper
description: >-
  Discovers cross-cutting semantic concerns in Neo4j Helm charts by deep-diving templates
  and Go code plus Neo4j documentation — not by values.yaml section layout. Links scattered
  helm paths (e.g. analytics at file end) to topology, plugins, TLS. BDR-002/004 are
  exemplars that may be revised. Use in Phase 2.5 after per-field analysis or when
  aggregating Helm values into CRD concerns.
---

# Helm semantic concern mapper

## Core principle

> **YAML location ≠ semantic domain.**

`_domains.yaml` batches exist for **parallel work only**. A field at line 764 (`analytics`)
may belong to the same **client concern** as `neo4j.minimumClusterSize` at line 28 —
prove it via **template conditionals** + **Neo4j docs**, not file proximity.

**BDR-001 / BDR-002 / BDR-004** are **exemplars** (good topology/plugin split examples).
Treat as hypotheses: validate, extend, or propose amendments when Helm evidence differs.

## When to run

| Phase | Action |
|-------|--------|
| **2.5** (after Phase 2) | Required before Phase 3 aggregation |
| Per complex field | When `analytics`, `minimumClusterSize`, `operations`, `config` cluster keys |
| Phase 3 input | crd-synthesis-analyst consumes this output |

## Workflow

### 1 — Start from concern seeds

Read `design/analysis/helm-fields/semantic-concerns.yaml` — especially `CONCERN-TOPOLOGY`.

### 2 — Deep dive Helm code (mandatory)

For each concern, trace **all** template branches — not only direct `.Values.<path>`:

```bash
# Direct value refs
rg 'analytics|minimumClusterSize|isClusterEnabled|clusterEnabled' \
  helm-charts/neo4j/templates/ helm-charts/neo4j/templates/

# Shared conditionals
rg 'primaryAnalyticsType|secondaryAnalyticsType' helm-charts/neo4j/templates/
```

**Read these files completely** for topology:

| File | Why |
|------|-----|
| `neo4j-config.yaml` | Merges `minimumClusterSize`, `analytics.type`, cluster flags into `neo4j.conf` |
| `_helpers.tpl` | `neo4j.isClusterEnabled` (≥3 + enterprise) |
| `neo4j-statefulset.yaml` | `replicas: 1` — Helm ≠ operator STS model |
| `neo4j-svc.yaml` | Ports enabled when cluster OR analytics |
| `internal/unit_tests/`, `internal/resources/testData/` | Scenario YAMLs |

Document in concern entry:

```yaml
template_evidence:
  - file: ...
    symbols: [...]
    note: ...
```

### 3 — Neo4j documentation (mandatory)

Map concern to **Neo4j product concepts** (not Helm names):

| Helm signal | Neo4j concept |
|-------------|---------------|
| `minimumClusterSize >= 3` | Causal cluster / system primaries |
| `analytics.type: primary` | Single primary writer + analytics topology |
| `analytics.type: secondary` | Secondary server, GDS on secondary |
| `operations.enableServer` | `ENABLE SERVER` after scale |

Fetch/search Operations Manual + GDS cluster deployment docs. Cite URLs in concern notes.

### 4 — Scatter map (topology exemplar)

Produce table for `CONCERN-TOPOLOGY`:

| helm_path | values.yaml region | role in concern | template driver |
|-----------|-------------------|-----------------|-----------------|
| `neo4j.minimumClusterSize` | neo4j block top | HA quorum size | `$clusterEnabled`, primaries count |
| `analytics.*` | **end of file** | primary+secondary layout | `$primaryAnalyticsType`, `$secondaryAnalyticsType` |
| `neo4j.operations.enableServer` | neo4j block | post-install scale | operations Job |
| `services.internals` | services mid-file | cluster discovery | ports + labels |
| implicit `replicas: 1` | statefulset template | **Helm multi-release** | not in values |

### 5 — Challenge BDR exemplars

For each concern, answer:

| Question | Output |
|----------|--------|
| Does BDR exemplar CRD shape match Helm **semantics**? | yes / partial / no |
| Helm vs operator structural gap? | e.g. 1 STS N replicas vs 1 release 1 pod |
| Keep BDR, amend, or new BDR? | `keep` \| `amend-BDR-002` \| `new-BDR-0xx` |
| Evidence | template file:line + doc URL |

**Do not** reject BDR-002 solely because Helm layout differs — that may be **why** the operator aggregates differently.

### 6 — Update artifacts

1. **`semantic-concerns.yaml`** — extend `helm_paths`, `template_evidence`, `helm_vs_operator_gap`
2. **`fields/*.md`** — add section **Semantic concerns** (ids from this file)
3. **`_index.csv`** — set `aggregation_group` for all paths in concern
4. **`aggregation-matrix.md`** — ensure scattered paths listed under same `AGG-*`
5. **`semantic-concern-report.md`** — short narrative per concern (create if missing)

### 7 — Gate G-SC

| Gate | Pass |
|------|------|
| G-SC-1 | `CONCERN-TOPOLOGY` lists ≥4 helm_paths from ≥2 values.yaml regions |
| G-SC-2 | Each path has ≥1 `template_evidence` entry |
| G-SC-3 | `analytics.*` explicitly linked to topology with code citation |
| G-SC-4 | BDR exemplar challenged documented (keep/amend/new) |

## Output: field doc section

Add to every field in a multi-path concern:

```markdown
## Semantic concerns

| concern_id | role in concern | co-paths |
|------------|-----------------|----------|
| CONCERN-TOPOLOGY | secondary server + GDS config | neo4j.minimumClusterSize, analytics.*, … |
```

## Output: semantic-concern-report.md template

```markdown
# Semantic concern report — [date]

## CONCERN-TOPOLOGY
### Scattered helm paths
### Template proof chain
### Neo4j doc alignment
### BDR-002 exemplar verdict: keep | amend | supersede
### Operator CRD implication
```

## Anti-patterns

- Categorize `analytics` only under `plugins` because GDS appears in config
- Use `_domains.yaml` id as sole category
- Accept BDR-002 shape without noting Helm `replicas: 1` / multi-release model
- Skip `neo4j-config.yaml` — **primary** source for topology cross-links

## Launch

```
@helm-semantic-concern-mapper

Validate CONCERN-TOPOLOGY: prove analytics (end of values.yaml) belongs to topology
via neo4j-config.yaml + Neo4j docs. Update semantic-concerns.yaml and aggregation-matrix.
Challenge BDR-002 as exemplar — do not treat as final decision.
```
