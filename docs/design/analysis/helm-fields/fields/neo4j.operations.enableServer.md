# `neo4j.operations.enableServer`

## Client need

When scaling a Neo4j cluster by installing an additional Helm release (one pod per release), operators need the chart to automatically run ENABLE SERVER so the new member joins the causal cluster without manual `cypher-shell` administration.

## Neo4j documentation

- [Clustering — enable servers](https://neo4j.com/docs/operations-manual/current/clustering/) — adding members to a cluster
- [Neo4j on Kubernetes](https://neo4j.com/docs/operations-manual/current/kubernetes/) — operations Job

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-operations.yaml` L3 (gate: `enableServer && $clusterEnabled`)
- **Go model**: `release_values.go` — `Operations.EnableServer`
- **K8s resources**: Job `*-operations`, ServiceAccount, Role, RoleBinding
- **Neo4j mechanism**: Job pod uses Go driver + cluster Service to run ENABLE SERVER query
- **Conditional links**: Requires `$clusterEnabled` from `minimumClusterSize >= 3`

## Category

lifecycle

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TOPOLOGY | Enables servers added outside initial `minimumClusterSize` | `neo4j.minimumClusterSize`, `neo4j.operations.*`, `analytics.*` |

## CRD mapping (draft)

- **Target**: Implicit in operator scale reconciliation (`NEO-3-011-SRV-01`) — not a direct spec field
- **Notes**: Operator single-STS model may enable servers via ordinal rollout instead of per-release Job

## Aggregation

- **Group**: AGG-TOPO-ROLES
- **Must decide with**: `neo4j.minimumClusterSize`, `neo4j.operations.image`

## Versioning

- **Classification**: deferred
- **Rationale**: Lifecycle automation detail; operator may implement differently

## FR / AC

- FR: NEO-2-011
- AC: AC-NEO-SCALE

## Open questions

- Operator scale subresource vs automatic enable on replica increase?
