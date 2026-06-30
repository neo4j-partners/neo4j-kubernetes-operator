# `podSpec.loadbalancer`

## Client need

Include or exclude this pod from the shared external LoadBalancer via label `helm.neo4j.com/neo4j.loadbalancer`.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Services and exposure

## Helm implementation

- **Templates**: neo4j-statefulset.yaml (pod label); neo4j-loadbalancer.yaml (selector); _helpers.tpl (validation)
- **Go model**: release_values.go: PodSpec.Loadbalancer
- **K8s resources**: Pod label + Service selector
- **Neo4j mechanism**: Value `include`|`exclude` must match services.neo4j.selector loadbalancer label.

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-EXPOSURE | LB pod membership | services.neo4j.selector |
| CONCERN-TOPOLOGY | which members receive external traffic | neo4j.minimumClusterSize, analytics.* |

## CRD mapping (draft)

- **Target**: `N/A (operator-internal — BDR-007)`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-EXPOSURE
- **Must decide with**: AGG-EXPOSURE

## Versioning

- **Classification**: safe
- **Rationale**: Per-member traffic inclusion toggle.

## FR / AC

- FR: NEO-2-007
- AC: AC-NEO-NETWORKING

## Open questions

- Operator pool-level exposure may replace per-pod include/exclude.
