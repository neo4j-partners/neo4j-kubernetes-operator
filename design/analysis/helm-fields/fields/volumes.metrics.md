# `volumes.metrics`

## Client need

Storage for CSV metrics at `/metrics/$(POD_NAME)` (Enterprise).

## Neo4j documentation

- [Metrics](https://neo4j.com/docs/operations-manual/current/monitoring/metrics/) — Metrics
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _volumeTemplate.tpl; neo4j-config.yaml (server.directories.metrics when enterprise + volumes.metrics)
- **Go model**: release_values.go: Volumes.Metrics
- **K8s resources**: StatefulSet (volumeClaimTemplates, volumes, volumeMounts)
- **Neo4j mechanism**: Sets `server.directories.metrics: /metrics` for Enterprise editions.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | auxiliary metrics volume | volumes.data, serviceMonitor |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.persistence.metrics`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-STORAGE-AUX
- **Must decide with**: AGG-STORAGE-AUX

## Versioning

- **Classification**: safe
- **Rationale**: Optional Enterprise aux volume.

## FR / AC

- FR: NEO-2-006; NEO-2-015; NEO-3-006-VOL-04
- AC: AC-NEO-MONITORING; AC-NEO-STORAGE

## Open questions

- None identified.
