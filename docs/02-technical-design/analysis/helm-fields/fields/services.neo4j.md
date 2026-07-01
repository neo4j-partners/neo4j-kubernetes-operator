# `services.neo4j`

## Client need

External-facing service (LoadBalancer/NodePort) for Bolt, HTTP, and HTTPS from outside the cluster.

## Neo4j documentation

- [Networking](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Networking
- [Connectors](https://neo4j.com/docs/operations-manual/current/configuration/connectors/) — Connectors

## Helm implementation

- **Templates**: neo4j-loadbalancer.yaml; _loadbalancer.tpl
- **Go model**: release_values.go: Services.Neo4j
- **K8s resources**: Service `{neo4j.name}-lb-neo4j` (LoadBalancer/NodePort/ClusterIP)
- **Neo4j mechanism**: Port list from services.neo4j.ports; shared across cluster releases with helm.sh/resource-policy: keep.

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-EXPOSURE | external exposure root | services.neo4j.*, podSpec.loadbalancer |
| CONCERN-TOPOLOGY | shared LB across multi-release cluster | services.neo4j.multiCluster, neo4j.minimumClusterSize |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.connectivity.service`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-EXPOSURE
- **Must decide with**: AGG-EXPOSURE

## Versioning

- **Classification**: breaking
- **Rationale**: External access model is a primary client contract.

## FR / AC

- FR: NEO-2-007; NEO-3-007-SVC-03
- AC: AC-NEO-NETWORKING; AC-NEO-NETWORKING-LB

## Open questions

- Helm shares one LB per neo4j.name — operator topology may differ (BDR-007).
