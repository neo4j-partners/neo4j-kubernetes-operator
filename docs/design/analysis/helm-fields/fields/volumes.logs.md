# `volumes.logs`

## Client need

Persistent log storage at `/logs/$(POD_NAME)` so server logs survive pod restarts.

## Neo4j documentation

- [Logging](https://neo4j.com/docs/operations-manual/current/monitoring/logging/) — Logging
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _volumeTemplate.tpl; neo4j-config.yaml (server.directories.logs when volumes.logs set)
- **Go model**: release_values.go: Volumes.Logs
- **K8s resources**: StatefulSet (volumeClaimTemplates, volumes, volumeMounts)
- **Neo4j mechanism**: Sets `server.directories.logs: /logs` in neo4j.conf when volume enabled.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | auxiliary logs volume | volumes.data, logging.* |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.volumes.logs`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-STORAGE-AUX
- **Must decide with**: AGG-STORAGE-AUX

## Versioning

- **Classification**: safe
- **Rationale**: Optional; default shares data volume.

## FR / AC

- FR: NEO-2-006; NEO-2-016; NEO-3-006-VOL-03
- AC: AC-NEO-LOGGING; AC-NEO-STORAGE

## Open questions

- None identified.
