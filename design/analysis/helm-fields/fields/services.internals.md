# `services.internals`

## Client need

Cluster-internal service for discovery, raft, routing, and inter-member traffic.

## Neo4j documentation

- [Clustering](https://neo4j.com/docs/operations-manual/current/clustering/) — Clustering
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: neo4j-svc.yaml (internals; cluster ports when clusterEnabled or analytics); neo4j-config.yaml (K8S discovery)
- **Go model**: release_values.go: Services.Internals
- **K8s resources**: Service `{release}-internals` ClusterIP
- **Neo4j mechanism**: Created when cluster enabled OR internals.enabled; drives dbms.kubernetes.label_selector.

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-EXPOSURE | cluster internal networking | services.default, services.admin |
| CONCERN-TOPOLOGY | cluster discovery endpoints | neo4j.minimumClusterSize, analytics.*, clusterDomain |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.connectivity.internal.discovery`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-EXPOSURE
- **Must decide with**: AGG-EXPOSURE

## Versioning

- **Classification**: breaking
- **Rationale**: Cluster formation depends on internals service shape.

## FR / AC

- FR: NEO-2-002; NEO-2-007
- AC: AC-NEO-CLUSTER; AC-NEO-NETWORKING

## Open questions

- BDR-002: operator may use headless vs ClusterIP for member DNS.
