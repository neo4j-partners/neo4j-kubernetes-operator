# `services.neo4j.multiCluster`

## Client need

Open internal cluster ports on the external service for multi-Kubernetes-zone/region Neo4j cluster scenarios.

## Neo4j documentation

- [Clustering](https://neo4j.com/docs/operations-manual/current/clustering/) — Clustering
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: neo4j-loadbalancer.yaml (extra ports 7688,5000,7000,6000; publishNotReadyAddresses)
- **Go model**: release_values.go: Neo4jService.MultiCluster
- **K8s resources**: Service ports + publishNotReadyAddresses
- **Neo4j mechanism**: Exposes discovery, raft, tx, routing ports on LB for cross-cluster formation.

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-EXPOSURE | multi-cluster port exposure | services.internals |
| CONCERN-TOPOLOGY | cross-K8s cluster formation | neo4j.minimumClusterSize, services.internals |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.connectivity.multiCluster`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-EXPOSURE
- **Must decide with**: AGG-EXPOSURE

## Versioning

- **Classification**: breaking
- **Rationale**: Security and topology implications of exposing cluster ports.

## FR / AC

- FR: NEO-2-007; NEO-3-007-MULTI-02
- AC: AC-NEO-MULTICLUSTER

## Open questions

- Operator multi-region model may differ from Helm multi-release pattern.
