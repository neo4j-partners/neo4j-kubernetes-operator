# `services.neo4j.enabled`

## Client need

Toggle creation of the external LoadBalancer (or NodePort) service.

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: neo4j-loadbalancer.yaml (guarded by enabled); delete-loadbalancer-hook.yaml
- **Go model**: release_values.go: Neo4jService.Enabled
- **K8s resources**: Service (conditional)
- **Neo4j mechanism**: When false, only in-cluster services remain.

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-EXPOSURE | external service toggle | services.neo4j |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.connectivity.service.type` (`LoadBalancer` when external LB enabled)
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-EXPOSURE
- **Must decide with**: AGG-EXPOSURE

## Versioning

- **Classification**: safe
- **Rationale**: Enabling/disabling external service is operational.

## FR / AC

- FR: NEO-2-007
- AC: AC-NEO-NETWORKING

## Open questions

- None identified.
