# `clusterDomain`

## Client need

Kubernetes cluster DNS suffix used in service FQDNs for discovery, routing, and client connection strings.

## Neo4j documentation

- [Clustering — K8s discovery](https://neo4j.com/docs/operations-manual/current/clustering/setup/k8s/) — Clustering — K8s discovery
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: neo4j-statefulset.yaml (SERVICE_* env); neo4j-config.yaml (dbms.routing.client_side.enforce_for_domains); NOTES.txt
- **Go model**: release_values.go: ClusterDomain
- **K8s resources**: Pod env vars (DNS FQDNs)
- **Neo4j mechanism**: Sets `dbms.routing.client_side.enforce_for_domains: *.{clusterDomain}` when clustering.

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-EXPOSURE | DNS domain for services | services.* |
| CONCERN-TOPOLOGY | K8s discovery FQDNs | services.internals, neo4j.minimumClusterSize |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.connectivity.clusterDomain`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-EXPOSURE
- **Must decide with**: AGG-EXPOSURE

## Versioning

- **Classification**: safe
- **Rationale**: Rarely changed; cluster-scoped default.

## FR / AC

- FR: NEO-2-007
- AC: AC-NEO-NETWORKING

## Open questions

- Default `cluster.local` — expose as optional override only.
