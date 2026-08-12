# `neo4j.operations`

## Client need

Cluster administrators adding Neo4j members beyond the initial `minimumClusterSize` need automated post-install steps to register (“enable”) the new server in the causal cluster. The `operations` block configures the Helm operations Job that runs cluster admin commands against the release.

## Neo4j documentation

- [Clustering — add servers](https://neo4j.com/docs/operations-manual/current/clustering/) — ENABLE SERVER / cluster administration
- [Neo4j on Kubernetes](https://neo4j.com/docs/operations-manual/current/kubernetes/) — operations Job pattern

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-operations.yaml` (entire block gated by `operations.enableServer` + `$clusterEnabled`)
- **Go model**: `release_values.go` — `Neo4J.Operations` (`Operations` struct)
- **K8s resources**: Job, ServiceAccount, Role, RoleBinding (when enabled)
- **Neo4j mechanism**: Operations container connects via Bolt and executes ENABLE SERVER for servers added outside initial quorum
- **Conditional links**: Only rendered when `neo4j.isClusterEnabled` is true (`minimumClusterSize >= 3`)

## Category

lifecycle

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TOPOLOGY | Post-formation member enablement in clustered layout | `neo4j.minimumClusterSize`, `neo4j.operations.enableServer`, `analytics.*` |

## CRD mapping (draft)

- **Target**: Operator-managed scale workflow (`NEO-2-011`) — likely controller Job, not user-facing map
- **Notes**: V1 may fold into scale reconciliation; individual child fields (`image`, `protocol`, `ssl`) operator-internal

## Aggregation

- **Group**: AGG-TOPO-ROLES
- **Must decide with**: `neo4j.operations.enableServer`, `neo4j.minimumClusterSize`

## Versioning

- **Classification**: deferred
- **Rationale**: Day-2 cluster admin; `NEO-2-017` partial — operator may hide Helm Job surface

## FR / AC

- FR: NEO-2-011
- AC: AC-NEO-SCALE

## Open questions

- Expose operations Job image/protocol/ssl in CRD or keep operator-internal defaults?
