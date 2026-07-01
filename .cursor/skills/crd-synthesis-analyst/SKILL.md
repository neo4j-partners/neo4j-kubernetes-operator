---
name: crd-synthesis-analyst
description: >-
  Synthesizes Helm field analyses into Neo4j CRD spec sections using semantic concerns
  (not values.yaml layout). Discovers aggregation groups from template code + Neo4j docs.
  BDR-002/004 are exemplars that may be revised. Use after helm-semantic-concern-mapper.
---

# CRD synthesis analyst

## Discovery-first (not layout-first)

1. Read **`semantic-concerns.yaml`** + **`semantic-concern-report.md`** (Phase 2.5)
2. Aggregate by **`concern_id`** / **`AGG-*`** — paths may be far apart in values.yaml
3. **BDR-001/002/004** = exemplar hypotheses — align or propose amendments with evidence

## Rules (from BDR-001 exemplar)

| Condition | CRD shape |
|-----------|-----------|
| Same lifecycle as Neo4j workload | Embedded `Neo4j.spec.<section>` |
| Different lifecycle (backup job) | Separate CRD |
| Strong coupling (proven in templates) | **One BDR per concern group** |

## Topology exemplar — scattered Helm → one CRD section

| values.yaml region | helm_path | Joined via |
|--------------------|-----------|------------|
| `neo4j:` top | `minimumClusterSize`, `operations.*` | `$clusterEnabled`, enable-server Job |
| **File end** | `analytics.*` | `$primaryAnalyticsType`, `$secondaryAnalyticsType` |
| `services:` | `internals`, `multiCluster` | discovery / routing ports |
| Template only | STS `replicas: 1` | Helm gap vs operator 1-STS-N-pods |

**CRD hypothesis** (BDR-002 exemplar): unified `spec.topology` — validate against Helm semantics, not Helm file structure.

## Known aggregation patterns (validate, don't assume)

| Group | Helm signals (may be scattered) | CRD direction |
|-------|--------------------------------|---------------|
| `AGG-TOPO-ROLES` | minimumClusterSize, **analytics***, operations, services.internals | `spec.topology` — BDR-002 exemplar |
| `AGG-TOPO-PLUGINS` | analytics (GDS config), apoc_* | `pluginDefinitions` + pools — BDR-004 exemplar |
| `AGG-TLS-TRUST` | ssl.*, config tls_reload | `spec.trust` |
| `AGG-STORAGE-DATA` | volumes.data.* | `spec.persistence.data` |
| `AGG-EXPOSURE` | services.* | `spec.connectivity` |
| `AGG-CONFIG-SURFACE` | config, jvm | `spec.config` |

Add rows to `aggregation-matrix.md` when template analysis reveals new couplings.

## Workflow

1. Import concerns from Phase 2.5
2. Read all `fields/*.md` — prioritize **Semantic concerns** sections
3. Update `aggregation-matrix.md` — list **all** helm paths per group with `values.yaml` line region note
4. Update `crd-candidates.md`
5. Set `_index.csv`: `crd_target`, `aggregation_group`
6. If exemplar BDR insufficient → flag `new-BDR-0xx` in `breaking-change-register.md`

## CRD target notation

```
Neo4j.spec.topology.primaries.members
Neo4j.spec.topology.secondaries.analytics
Neo4j.spec.topology.secondaries.read
Neo4j.spec.persistence.data.mode
```

## Anti-patterns

- Group only by `_domains.yaml` batch id
- Treat `analytics` as plugins-only because GDS appears in secondary config
- Treat BDR-002 as locked — Helm `replicas:1` / multi-release may require explicit operator divergence note
- Split topology and plugins CRD fields without documenting `must_co_decide_with` from semantic-concerns.yaml

## Cross-check

`design/09-crd-spec/neo4j/spec.md` — flag drift; may need update if concern analysis revises exemplar.
