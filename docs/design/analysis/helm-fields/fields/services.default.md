# `services.default`

## Client need

In-cluster ClusterIP service for driver connections from workloads inside the Kubernetes cluster.

## Neo4j documentation

- [Networking](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Networking
- [Connectors](https://neo4j.com/docs/operations-manual/current/configuration/connectors/) — Connectors

## Helm implementation

- **Templates**: neo4j-svc.yaml (default ClusterIP service)
- **Go model**: release_values.go: Services.Default
- **K8s resources**: Service ClusterIP `{release}-neo4j`
- **Neo4j mechanism**: Exposes bolt (7687) and enabled http/https ports; selector matches release instance.

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-EXPOSURE | in-cluster client access | services.neo4j, services.admin, clusterDomain |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.connectivity.service` (`type: ClusterIP` — merged from Helm `services.default`)
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-EXPOSURE
- **Must decide with**: AGG-EXPOSURE

## Versioning

- **Classification**: breaking
- **Rationale**: Service naming and port surface are client contracts.

## FR / AC

- FR: NEO-2-007; NEO-3-007-SVC-01
- AC: AC-NEO-NETWORKING; AC-NEO-NETWORKING-CLUSTERIP

## Open questions

- Operator single CR may synthesize default service per pool.
