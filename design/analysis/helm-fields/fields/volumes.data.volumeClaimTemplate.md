# `volumes.data.volumeClaimTemplate`

## Client need

Supply a full volumeClaimTemplate spec for advanced PVC configuration.

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _volumeTemplate.tpl (volumeClaimTemplate mode passthrough)
- **Go model**: release_values.go: Data.VolumeClaimTemplate
- **K8s resources**: StatefulSet volumeClaimTemplates
- **Neo4j mechanism**: Raw VCT YAML embedded in StatefulSet.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | raw VCT mode | volumes.data.mode |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.persistence.data.volumeClaimTemplate`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-STORAGE-DATA
- **Must decide with**: AGG-STORAGE-DATA

## Versioning

- **Classification**: breaking
- **Rationale**: VCT fields largely immutable after creation.

## FR / AC

- FR: NEO-2-006; NEO-3-006-PVC-05
- AC: AC-NEO-STORAGE-VCT

## Open questions

- None identified.
