# `volumes.backups`

## Client need

Dedicated or shared storage for backup files written under `/backups` for neo4j-admin backup operations.

## Neo4j documentation

- [Backup](https://neo4j.com/docs/operations-manual/current/backup/) — Backup
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _volumeTemplate.tpl; neo4j-statefulset.yaml
- **Go model**: release_values.go: Volumes.Backups
- **K8s resources**: StatefulSet (volumeClaimTemplates, volumes, volumeMounts)
- **Neo4j mechanism**: Mounted at `/backups`; default mode `share` reuses data volume.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | auxiliary backup volume | volumes.data |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.volumes.backups`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-STORAGE-AUX
- **Must decide with**: AGG-STORAGE-AUX

## Versioning

- **Classification**: safe
- **Rationale**: Optional aux volume; defaults to sharing data.

## FR / AC

- FR: NEO-2-006; NEO-2-013; NEO-3-006-VOL-02
- AC: AC-NEO-BACKUP; AC-NEO-STORAGE

## Open questions

- Backup CRD may own backup storage separately from workload aux volume.
