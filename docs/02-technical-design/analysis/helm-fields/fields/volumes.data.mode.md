# `volumes.data.mode`

## Client need

Choose how the data volume is provisioned or bound: dynamic PVC, existing PVC, selector, or shared volume.

## Neo4j documentation

- [Kubernetes deployment — storage](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment — storage

## Helm implementation

- **Templates**: _volumeTemplate.tpl (neo4j.volumeClaimTemplateSpec mode dispatch); neo4j.volumes.validation
- **Go model**: release_values.go: Data.Mode
- **K8s resources**: StatefulSet (volumeClaimTemplates, volumes, volumeMounts)
- **Neo4j mechanism**: Mode drives StatefulSet volumeClaimTemplates vs inline volumes; data always mounted at `/data`.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | provisioning mode selector | volumes.data.* |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.volumes.data.mode`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-STORAGE-DATA
- **Must decide with**: AGG-STORAGE-DATA

## Versioning

- **Classification**: breaking
- **Rationale**: Changing mode after install typically requires data migration.

## FR / AC

- FR: NEO-2-006; NEO-3-006-PVC-01; NEO-3-006-PVC-02; NEO-3-006-PVC-03; NEO-3-006-PVC-04; NEO-3-006-PVC-05
- AC: AC-NEO-STORAGE-DEFAULT; AC-NEO-STORAGE-DYNAMIC; AC-NEO-STORAGE-EXISTING; AC-NEO-STORAGE-SELECTOR; AC-NEO-STORAGE-VCT

## Open questions

- Map Helm modes (share|selector|defaultStorageClass|dynamic|volume|volumeClaimTemplate) to operator enum.
