# `volumes.data`

## Client need

Operators need durable persistent storage for the Neo4j database so data survives pod restarts and rescheduling.

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment
- [File locations](https://neo4j.com/docs/operations-manual/current/configuration/file-locations/) — File locations

## Helm implementation

- **Templates**: _volumeTemplate.tpl (neo4j.volumeClaimTemplates, neo4j.volumes, neo4j.volumeMounts); neo4j-statefulset.yaml; neo4j.volumes.validation
- **Go model**: release_values.go: Volumes.Data
- **K8s resources**: StatefulSet (volumeClaimTemplates, volumes, volumeMounts)
- **Neo4j mechanism**: Mounts `/data` via volumeMounts; data directory holds the graph store.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | primary data volume root | volumes.backups, volumes.logs, volumes.metrics, volumes.import, volumes.licenses |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.persistence.data`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-STORAGE-DATA
- **Must decide with**: AGG-STORAGE-DATA

## Versioning

- **Classification**: breaking
- **Rationale**: PVC provisioning mode and size are migration-sensitive.

## FR / AC

- FR: NEO-2-006; NEO-3-006-VOL-01
- AC: AC-NEO-STORAGE

## Open questions

- Operator may unify data + aux volume schema (BDR-006 draft).
