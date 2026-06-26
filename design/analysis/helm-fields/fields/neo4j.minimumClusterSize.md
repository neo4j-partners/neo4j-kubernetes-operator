# `neo4j.minimumClusterSize`

## Client need

Production teams deploying Neo4j Enterprise need a causal cluster with a defined quorum of primary members before the database accepts writes. This value sets how many system primaries must form initially and gates cluster discovery configuration; values ≥3 enable full HA clustering.

## Neo4j documentation

- [Clustering](https://neo4j.com/docs/operations-manual/current/clustering/) — causal cluster formation
- [Configuration — cluster](https://neo4j.com/docs/operations-manual/current/configuration/) — `initial.dbms.default_primaries_count`, `dbms.cluster.minimum_initial_system_primaries_count`

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl` (`neo4j.isClusterEnabled` — true when `minimumClusterSize >= 3` and `edition: enterprise`); `neo4j-config.yaml` L94–102 (injects primaries count and raft binding timeout); `neo4j-svc.yaml`, `neo4j-statefulset.yaml`, `neo4j-loadbalancer.yaml`, `neo4j-operations.yaml` (cluster branches)
- **Go model**: `release_values.go` — `Neo4J.MinimumClusterSize`
- **K8s resources**: ConfigMap `*-default-config`, internals Service, StatefulSet (`replicas: 1` — one pod per release)
- **Neo4j mechanism**: Sets `initial.dbms.default_primaries_count` and `dbms.cluster.minimum_initial_system_primaries_count`; enables K8S discovery resolver when clustered
- **Conditional links**: Coupled with `analytics.*` in `neo4j-config.yaml` — primary analytics forces `default_primaries_count: 1` (L104–107)

## Category

topology

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TOPOLOGY | HA quorum threshold; enables cluster template branch | `analytics.enabled`, `analytics.type.name`, `neo4j.edition`, `neo4j.operations.enableServer`, `services.internals`, `services.neo4j.multiCluster`, `config` (cluster keys) |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.topology.mode` (`Cluster` when ≥3), `spec.topology.primaries.members`, `spec.topology.minimumMembers`
- **Notes**: Helm multi-release HA (STS `replicas: 1` per install) diverges from operator single-STS multi-replica model ([BDR-002](../../decision-records/business/002-neo4j-crd-topology.md))

## Aggregation

- **Group**: AGG-TOPO-ROLES
- **Must decide with**: `analytics.*`, `neo4j.operations.enableServer`, `neo4j.edition`

## Versioning

- **Classification**: breaking
- **Rationale**: Defines cluster vs standalone mode and primary count — core topology contract

## FR / AC

- FR: NEO-1-002, NEO-2-002-CSZ-01, NEO-2-002-MODE-01, NEO-2-011
- AC: AC-NEO-CLUSTER, AC-NEO-SCALE

## Open questions

- Map Helm `minimumClusterSize: 1` + `analytics.type: primary` → operator `primaries.members: 1` + `secondaries.analytics`?
