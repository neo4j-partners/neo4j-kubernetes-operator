# `volumes.data.defaultStorageClass`

## Client need

Quick-start dynamic provisioning using the cluster default StorageClass (dev/test).

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _volumeTemplate.tpl (defaultStorageClass → dynamic rewrite)
- **Go model**: release_values.go: Data.DefaultStorageClass
- **K8s resources**: PersistentVolumeClaim (default StorageClass)
- **Neo4j mechanism**: No storageClassName set; cluster default used.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | default SC mode | volumes.data.mode |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.persistence.data.defaultStorageClass`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-STORAGE-DATA
- **Must decide with**: AGG-STORAGE-DATA

## Versioning

- **Classification**: breaking
- **Rationale**: Initial PVC size/class locked at create.

## FR / AC

- FR: NEO-2-006; NEO-3-006-PVC-01
- AC: AC-NEO-STORAGE-DEFAULT

## Open questions

- None identified.
