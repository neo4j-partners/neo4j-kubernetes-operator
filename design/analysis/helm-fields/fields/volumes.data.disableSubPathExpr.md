# `volumes.data.disableSubPathExpr`

## Client need

Disable per-pod subPathExpr on the data mount when the backing volume layout does not support sub-path expressions.

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _volumeTemplate.tpl (neo4j.volumeMounts)
- **Go model**: release_values.go: Data.DisableSubPath
- **K8s resources**: StatefulSet volumeMounts
- **Neo4j mechanism**: When false (default), uses subPathExpr `data` for mount at `/data`.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | mount path control | volumes.data |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.persistence.data.disableSubPathExpr`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-STORAGE-DATA
- **Must decide with**: AGG-STORAGE-DATA

## Versioning

- **Classification**: breaking
- **Rationale**: Changing mount semantics can make existing data unreachable.

## FR / AC

- FR: NEO-2-006
- AC: AC-NEO-STORAGE

## Open questions

- None identified.
