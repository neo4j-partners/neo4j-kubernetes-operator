# `analytics.type.name`

## Client need

When analytics mode is enabled, the deployer must declare whether **this Helm release** represents the primary writer (`primary`) or a GDS-capable secondary (`secondary`). This per-release role flag is how Helm distinguishes single-writer+analytics from dedicated secondary servers in a multi-release cluster.

## Neo4j documentation

- [GDS cluster deployment](https://neo4j.com/docs/graph-data-science/current/production-deployment/) — server roles
- [Configuration — mode constraint](https://neo4j.com/docs/operations-manual/current/configuration/) — `initial.server.mode_constraint`

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-config.yaml` L6–7, L104–107 (primary → `default_primaries_count: 1`), L137–147 (secondary → `SECONDARY` mode + GDS allowlist), L135–139 (label selector variants)
- **Go model**: `release_values.go` — `Analytics.Type.Name` (`primary` | `secondary`)
- **K8s resources**: ConfigMap `*-default-config`
- **Neo4j mechanism**: `primary` — forces one primary; `secondary` — `server.cluster.system_database_mode: SECONDARY`, unrestricted GDS procedures
- **Conditional links**: Mutually exclusive branches with `$clusterEnabled` in `neo4j-config.yaml`

## Category

topology

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TOPOLOGY | Per-release server role in analytics layout | `analytics.enabled`, `neo4j.minimumClusterSize`, `neo4j.operations.enableServer` |
| CONCERN-PLUGINS-ON-TOPOLOGY | `secondary` triggers GDS procedure allowlist | `pluginDefinitions` / `secondaries.analytics.plugins` in operator |

## CRD mapping (draft)

- **Target**: Operator pool assignment — `secondary` → `spec.topology.secondaries.analytics`; `primary` → primary pool sizing (`primaries.members`)
- **Notes**: No `analytics.type.name` field in CRD — intent encoded by fixed pool keys `secondaries.analytics` vs `secondaries.read` ([BDR-002](../../decision-records/business/002-neo4j-crd-topology.md))

## Aggregation

- **Group**: AGG-TOPO-ROLES, AGG-TOPO-PLUGINS
- **Must decide with**: `analytics.enabled`, `neo4j.minimumClusterSize`

## Versioning

- **Classification**: breaking
- **Rationale**: Defines Neo4j server role and plugin placement rules

## FR / AC

- FR: NEO-1-002, NEO-2-011
- AC: AC-NEO-CLUSTER

## Open questions

- Helm has no explicit `read` secondary type — operator `secondaries.read` is net-new vs Helm?
