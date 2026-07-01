---
name: helm-field-categorizer
description: >-
  Categorizes Neo4j Helm values by semantic concern (topology, storage, etc.) using
  template deep dive — not values.yaml position. Writes field docs with Semantic concerns
  section linking scattered paths (e.g. analytics at file end to topology). Use with
  helm-template-tracer and helm-semantic-concern-mapper.
---

# Helm field categorizer

## Two axes (do not conflate)

| Axis | Source | Example |
|------|--------|---------|
| **`category`** | Primary design bucket for CRD | `topology` |
| **`semantic_concerns`** | Cross-cutting concern ids | `CONCERN-TOPOLOGY`, `CONCERN-PLUGINS-ON-TOPOLOGY` |

A field's **YAML neighbourhood** does not determine `category` or concerns alone — use **template conditionals**.

## Categories (one primary `category`)

| Category | Helm examples (possibly scattered in values.yaml) |
|----------|-----------------------------------------------------|
| `topology` | minimumClusterSize, **analytics.*** (end of file), operations.enableServer, services.internals |
| `storage` | volumes.* |
| `network` | services.*, clusterDomain, ssl.* |
| `config` | config, jvm, env |
| `security` | password, ldap, securityContext |
| `scheduling` | nodeSelector, podSpec, statefulset |
| `health` | probes |
| `observability` | logging, serviceMonitor |
| `plugins` | apoc_* (assignment may still be topology-linked) |
| `lifecycle` | operations, offlineMaintenanceMode |
| `packaging` | image, nameOverride |

## Workflow

1. Read helm_path row in `_index.csv`
2. **helm-template-tracer** — direct + conditional links
3. **neo4j-client-need-mapper** — client need + Neo4j docs
4. **helm-semantic-concern-mapper** — assign `semantic_concerns[]` from `semantic-concerns.yaml`
5. Write field doc using [field-template.md](field-template.md)
6. Update CSV: `category`, `aggregation_group`, `status=draft`

## Topology — mandatory cross-links

When analyzing **any** of these paths, document links to **all** co-paths in `CONCERN-TOPOLOGY`:

| helm_path | values.yaml location |
|-----------|---------------------|
| `neo4j.minimumClusterSize` | `neo4j:` block (~L28) |
| `neo4j.operations.*` | `neo4j:` block |
| `analytics.*` | **end of file (~L764)** |
| `services.internals` | `services:` block |
| `services.neo4j.multiCluster` | `services.neo4j` |

Proof: `neo4j-config.yaml` variables `$clusterEnabled`, `$primaryAnalyticsType`, `$secondaryAnalyticsType`.

## Special cases

| helm_path | category | semantic_concerns | note |
|-----------|----------|-------------------|------|
| `analytics.*` | `topology` | CONCERN-TOPOLOGY, CONCERN-PLUGINS-ON-TOPOLOGY | Not plugins-only — secondary sets GDS server mode |
| `config` | `config` | may include CONCERN-TOPOLOGY | cluster keys injected in default-config CM |
| `apoc_*` | `plugins` | CONCERN-PLUGINS-ON-TOPOLOGY | Pool assignment → BDR-004 exemplar |

## Filename

`fields/<helm-path-with-dots>.md`
