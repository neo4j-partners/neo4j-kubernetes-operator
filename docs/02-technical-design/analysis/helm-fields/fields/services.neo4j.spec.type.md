# `services.neo4j.spec.type`

## Client need

Choose Service type: LoadBalancer, NodePort, or ClusterIP for external access pattern.

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: neo4j-loadbalancer.yaml; _loadbalancer.tpl (neo4j.services.neo4j.defaultSpec)
- **Go model**: release_values.go: Neo4jService.Spec.Type
- **K8s resources**: Service spec.type
- **Neo4j mechanism**: Merges user spec with type-specific defaults (externalTrafficPolicy: Local for LB/NodePort).

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-EXPOSURE | service type | services.neo4j.ports |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.connectivity.service.type`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-EXPOSURE
- **Must decide with**: AGG-EXPOSURE

## Versioning

- **Classification**: breaking
- **Rationale**: Type change replaces Service semantics.

## FR / AC

- FR: NEO-2-007; NEO-3-007-SVC-01; NEO-3-007-SVC-02; NEO-3-007-SVC-03
- AC: AC-NEO-NETWORKING-LB; AC-NEO-NETWORKING-NODEPORT; AC-NEO-NETWORKING-CLUSTERIP

## Open questions

- None identified.
