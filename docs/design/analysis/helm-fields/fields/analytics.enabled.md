# `analytics.enabled`

## Client need

DBAs enabling GDS/Bloom on dedicated secondary servers need to turn on the analytics topology path without manually editing `neo4j.conf`. When enabled, the chart opens internal cluster ports and applies role-specific defaults for primary+secondary layouts.

## Neo4j documentation

- [GDS cluster deployment](https://neo4j.com/docs/graph-data-science/current/production-deployment/) — dedicated analytics members
- [Clustering](https://neo4j.com/docs/operations-manual/current/clustering/) — discovery and routing

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-config.yaml` L6–7 (`$primaryAnalyticsType`, `$secondaryAnalyticsType`), L149–157 (advertised addresses when `analytics.enabled`); `neo4j-svc.yaml` L171; `neo4j-statefulset.yaml` L59; `neo4j-service-account.yaml` L2
- **Go model**: `release_values.go` — `Analytics.Enabled`
- **K8s resources**: ConfigMap `*-default-config`, internals Service, ServiceAccount
- **Neo4j mechanism**: Enables cluster discovery/routing config branches; pairs with `analytics.type.name` for primary vs secondary behaviour
- **Conditional links**: Evaluated with `minimumClusterSize` and `edition` in same `neo4j-config.yaml` file

## Category

topology

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TOPOLOGY | Master switch for analytics topology branch | `analytics.type.name`, `neo4j.minimumClusterSize`, `neo4j.edition`, `services.internals` |
| CONCERN-PLUGINS-ON-TOPOLOGY | Secondary path enables GDS procedures | `apoc_config` |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.topology.secondaries.analytics.members` (>0 implies enabled)
- **Notes**: Operator encodes intent via fixed `analytics` pool key rather than boolean flag

## Aggregation

- **Group**: AGG-TOPO-ROLES
- **Must decide with**: `analytics.type.name`, `neo4j.minimumClusterSize`

## Versioning

- **Classification**: breaking
- **Rationale**: Toggles secondary pool and cluster service exposure

## FR / AC

- FR: NEO-1-002, NEO-2-011
- AC: AC-NEO-CLUSTER

## Open questions

- Standalone + analytics enabled (primary type) — map to `mode: Cluster` with `primaries.members: 1`?
