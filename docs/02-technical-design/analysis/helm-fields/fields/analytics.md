# `analytics`

## Client need

Teams running Graph Data Science or Bloom alongside a smaller primary footprint need a Helm-level switch for the “one primary + N secondary” analytics layout. The root `analytics` map groups `enabled` and `type.name` controls that sit at the **end** of `values.yaml` but drive the same topology concern as `neo4j.minimumClusterSize` at the top.

## Neo4j documentation

- [GDS cluster deployment](https://neo4j.com/docs/graph-data-science/current/production-deployment/) — primary/secondary server layout
- [Clustering — secondary servers](https://neo4j.com/docs/operations-manual/current/clustering/) — Secondary role

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-config.yaml` L6–7, L104–147, L149–157; `neo4j-svc.yaml` L171; `neo4j-statefulset.yaml` L59; `neo4j-service-account.yaml` L2
- **Go model**: `release_values.go` — `HelmValues.Analytics` (`Analytics` struct with `Enabled`, `Type.Name`)
- **K8s resources**: ConfigMap `*-default-config`, internals Service, ServiceAccount (when enabled)
- **Neo4j mechanism**: Primary analytics → single primary count; secondary analytics → `SECONDARY` mode_constraint + GDS procedure allowlists
- **Conditional links**: `$primaryAnalyticsType` / `$secondaryAnalyticsType` variables at top of `neo4j-config.yaml`

## Category

topology

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TOPOLOGY | Analytics layout (primary vs secondary server) | `neo4j.minimumClusterSize`, `neo4j.edition`, `neo4j.operations.enableServer`, `services.internals` |
| CONCERN-PLUGINS-ON-TOPOLOGY | Secondary analytics injects GDS allowlist | `apoc_config`, `config` (implicit GDS keys) |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.topology.secondaries.analytics` (fixed key — not free-form pool name)
- **Notes**: Operator uses `secondaries.analytics` and `secondaries.read` instead of Helm per-release `analytics.type` ([BDR-002](../../decision-records/business/002-neo4j-crd-topology.md))

## Aggregation

- **Group**: AGG-TOPO-ROLES, AGG-TOPO-PLUGINS
- **Must decide with**: `analytics.enabled`, `analytics.type.name`, `neo4j.minimumClusterSize`

## Versioning

- **Classification**: breaking
- **Rationale**: Determines secondary pool presence and Neo4j server role

## FR / AC

- FR: NEO-1-002, NEO-2-002-MODE-01, NEO-2-011
- AC: AC-NEO-CLUSTER

## Open questions

- How does Helm `analytics.type: primary` map when operator also has `secondaries.read`?
