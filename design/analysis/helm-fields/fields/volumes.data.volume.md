# `volumes.data.volume`

## Client need

Attach an explicit pre-existing volume (PVC or other) for data instead of dynamic provisioning.

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _volumeTemplate.tpl (neo4j.volumeSpec); neo4j.initChmodContainer for setOwnerAndGroupWritableFilePermissions
- **Go model**: release_values.go: Data.Volume
- **K8s resources**: StatefulSet volumes (inline)
- **Neo4j mechanism**: Optional init container chown/chmod when setOwnerAndGroupWritableFilePermissions set.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | existing volume mode | volumes.data.mode |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.persistence.data.volume`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-STORAGE-DATA
- **Must decide with**: AGG-STORAGE-DATA

## Versioning

- **Classification**: breaking
- **Rationale**: Volume reference is immutable in practice.

## FR / AC

- FR: NEO-2-006; NEO-3-006-PVC-03
- AC: AC-NEO-STORAGE-EXISTING

## Open questions

- None identified.
