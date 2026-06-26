# `volumes.licenses`

## Client need

Mount `/licenses` for Enterprise license files when not using env-based acceptance only.

## Neo4j documentation

- [Licensing](https://neo4j.com/docs/operations-manual/current/installation/licensing/) — Licensing
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _volumeTemplate.tpl
- **Go model**: release_values.go: Volumes.Licenses
- **K8s resources**: StatefulSet (volumeClaimTemplates, volumes, volumeMounts)
- **Neo4j mechanism**: Mounted at `/licenses`; no automatic neo4j.conf key.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | auxiliary licenses volume | volumes.data, neo4j.acceptLicenseAgreement |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.persistence.licenses`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-STORAGE-AUX
- **Must decide with**: AGG-STORAGE-AUX

## Versioning

- **Classification**: safe
- **Rationale**: Optional aux volume.

## FR / AC

- FR: NEO-2-006; NEO-3-006-VOL-06
- AC: AC-NEO-LICENSE; AC-NEO-STORAGE

## Open questions

- None identified.
