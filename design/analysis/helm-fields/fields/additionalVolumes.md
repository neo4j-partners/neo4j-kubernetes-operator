# `additionalVolumes`

## Client need

Inject arbitrary extra pod volumes (escape hatch) for integrations not covered by first-class volume roles.

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _volumeTemplate.tpl (neo4j.additionalVolumes); neo4j-statefulset.yaml
- **Go model**: release_values.go: AdditionalVolumes
- **K8s resources**: StatefulSet volumes (raw YAML passthrough)
- **Neo4j mechanism**: No neo4j.conf effect unless paired with mounts/config elsewhere.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | escape hatch volumes | additionalVolumeMounts |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.persistence.additionalVolumes`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: none
- **Must decide with**: standalone field

## Versioning

- **Classification**: safe
- **Rationale**: Passthrough K8s volume spec.

## FR / AC

- FR: NEO-2-006
- AC: AC-NEO-STORAGE

## Open questions

- Operator may restrict to allowlisted volume types in V1.
