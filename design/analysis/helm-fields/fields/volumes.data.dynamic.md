# `volumes.data.dynamic`

## Client need

Provision data storage on a named StorageClass with explicit size and access modes.

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _volumeTemplate.tpl (dynamic → volumeClaimTemplate)
- **Go model**: release_values.go: Data.Dynamic
- **K8s resources**: PersistentVolumeClaim
- **Neo4j mechanism**: Creates per-pod PVC via volumeClaimTemplates.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | named SC dynamic mode | volumes.data.mode |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.volumes.data.dynamic`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-STORAGE-DATA
- **Must decide with**: AGG-STORAGE-DATA

## Versioning

- **Classification**: breaking
- **Rationale**: Storage class and capacity changes need migration.

## FR / AC

- FR: NEO-2-006; NEO-3-006-PVC-02
- AC: AC-NEO-STORAGE-DYNAMIC

## Open questions

- None identified.
