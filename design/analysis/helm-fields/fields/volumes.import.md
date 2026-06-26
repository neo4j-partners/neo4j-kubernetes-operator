# `volumes.import`

## Client need

Mount `/import` for bulk CSV import workflows.

## Neo4j documentation

- [Import](https://neo4j.com/docs/operations-manual/current/tools/neo4j-admin/neo4j-admin-import/) — Import
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _volumeTemplate.tpl; neo4j-config.yaml (server.directories.import)
- **Go model**: release_values.go: Volumes.Import
- **K8s resources**: StatefulSet (volumeClaimTemplates, volumes, volumeMounts)
- **Neo4j mechanism**: Sets `server.directories.import: /import` when volume configured.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | auxiliary import volume | volumes.data |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.persistence.import`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-STORAGE-AUX
- **Must decide with**: AGG-STORAGE-AUX

## Versioning

- **Classification**: safe
- **Rationale**: Optional aux volume.

## FR / AC

- FR: NEO-2-006; NEO-3-006-VOL-05
- AC: AC-NEO-STORAGE

## Open questions

- None identified.
