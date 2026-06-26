# `volumes.data.labels`

## Client need

Apply Kubernetes labels to the data PVC when dynamically provisioned for cost allocation or policy selection.

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _volumeTemplate.tpl (neo4j.volumeClaimMetadata)
- **Go model**: release_values.go: Data.Labels
- **K8s resources**: PersistentVolumeClaim metadata.labels
- **Neo4j mechanism**: Labels applied at PVC creation only; no neo4j.conf effect.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | PVC metadata | volumes.data |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.persistence.data.labels`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-STORAGE-DATA
- **Must decide with**: AGG-STORAGE-DATA

## Versioning

- **Classification**: safe
- **Rationale**: Label changes on existing PVCs are additive metadata.

## FR / AC

- FR: NEO-2-006
- AC: AC-NEO-STORAGE

## Open questions

- None identified.
