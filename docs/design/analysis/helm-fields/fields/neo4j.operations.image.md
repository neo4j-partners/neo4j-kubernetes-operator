# `neo4j.operations.image`

## Client need

Platform teams pin the operations Job container image for supply-chain control and air-gapped registries. The image bundles the Helm operations binary that performs ENABLE SERVER against the cluster.

## Neo4j documentation

- [Neo4j on Kubernetes](https://neo4j.com/docs/operations-manual/current/kubernetes/) — chart operations sidecar image

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-operations.yaml` L32 (`image: {{ $.Values.neo4j.operations.image }}`)
- **Go model**: `release_values.go` — `Operations.Image`
- **K8s resources**: Job pod container image
- **Neo4j mechanism**: None directly — sidecar admin tool

## Category

lifecycle

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TOPOLOGY | Operations Job only exists in cluster topology path | `neo4j.operations.enableServer`, `neo4j.minimumClusterSize` |

## CRD mapping (draft)

- **Target**: N/A — operator-internal image pin (or bundled with operator version)
- **Notes**: Default `neo4j/helm-charts-operations:2026.04.0` in values.yaml

## Aggregation

- **Group**: AGG-TOPO-ROLES
- **Must decide with**: `neo4j.operations.enableServer`

## Versioning

- **Classification**: safe
- **Rationale**: Implementation detail; not user workload spec

## FR / AC

- FR: NEO-2-011
- AC: AC-NEO-SCALE

## Open questions

- Ship operations logic inside operator controller vs separate Job image?
