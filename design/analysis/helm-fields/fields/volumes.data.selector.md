# `volumes.data.selector`

## Client need

Bind the data volume to existing PVs matching a label selector and storage class (pre-provisioned storage).

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _volumeTemplate.tpl (selector mode → dynamic; selectorTemplate tpl)
- **Go model**: release_values.go: Data.Selector
- **K8s resources**: PersistentVolumeClaim with selector
- **Neo4j mechanism**: selectorTemplate rendered with Helm tpl; converted to dynamic PVC spec.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | selector provisioning mode | volumes.data.mode |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.persistence.data.selector`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-STORAGE-DATA
- **Must decide with**: AGG-STORAGE-DATA

## Versioning

- **Classification**: breaking
- **Rationale**: Selector binding is install-time commitment.

## FR / AC

- FR: NEO-2-006; NEO-3-006-PVC-04
- AC: AC-NEO-STORAGE-SELECTOR

## Open questions

- None identified.
