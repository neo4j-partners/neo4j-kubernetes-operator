# `neo4j.operations.protocol`

## Client need

Clusters with TLS-enabled Bolt need the operations Job to use the correct Neo4j driver URI scheme (`neo4j`, `neo4j+s`, `neo4j+ssc`) when connecting to enable a server. Misconfigured protocol causes the ENABLE SERVER Job to fail against secured endpoints.

## Neo4j documentation

- [Bolt URL schemes](https://neo4j.com/docs/operations-manual/current/configuration/ports/) — secure Bolt connectors
- [Clustering](https://neo4j.com/docs/operations-manual/current/clustering/) — admin connectivity

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-operations.yaml` L58–59 (`PROTOCOL` env, default `neo4j`)
- **Go model**: `release_values.go` — `Operations.Protocol`
- **K8s resources**: Job pod env
- **Neo4j mechanism**: Passed to operations binary for driver connection string (not `bolt://`)

## Category

lifecycle

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TOPOLOGY | Cluster admin connectivity | `neo4j.operations.ssl`, `ssl.bolt`, `neo4j.minimumClusterSize` |

## CRD mapping (draft)

- **Target**: N/A — derived from `spec.trust` / connectivity in operator
- **Notes**: Should align with workload TLS mode automatically

## Aggregation

- **Group**: AGG-TOPO-ROLES
- **Must decide with**: `neo4j.operations.ssl`, `ssl.bolt`

## Versioning

- **Classification**: safe
- **Rationale**: Operator can infer from trust settings

## FR / AC

- FR: NEO-2-011, NEO-2-005
- AC: AC-NEO-SCALE, AC-NEO-TLS

## Open questions

- Auto-derive protocol from `spec.trust` instead of explicit field?
