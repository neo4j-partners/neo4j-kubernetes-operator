# `additionalVolumeMounts`

## Client need

Mount additionalVolumes into the Neo4j container at custom paths.

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _volumeTemplate.tpl (neo4j.additionalVolumeMounts); neo4j-statefulset.yaml
- **Go model**: release_values.go (via AdditionalVolumes sibling; mounts not separate struct field in index)
- **K8s resources**: StatefulSet volumeMounts
- **Neo4j mechanism**: Raw mount YAML passthrough.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | escape hatch mounts | additionalVolumes |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.persistence.additionalVolumeMounts`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: none
- **Must decide with**: standalone field

## Versioning

- **Classification**: safe
- **Rationale**: Passthrough mount spec.

## FR / AC

- FR: NEO-2-006
- AC: AC-NEO-STORAGE

## Open questions

- None identified.
