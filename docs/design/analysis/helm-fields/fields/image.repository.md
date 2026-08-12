# `image.repository`

## Client need

Operators set the image repository name (default neo4j) when using separated image fields instead of a full customImage string.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Container image

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_image.tpl#L37-39`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet container image
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

packaging

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-IMAGE | image name | image.tag, spec.version |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.image.repository`
- **Notes**: Required when using separated fields in Helm.

## Aggregation

- **Group**: AGG-IMAGE
- **Must decide with**: fields sharing `AGG-IMAGE` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Direct CRD field.

## FR / AC

- FR: NEO-2-012; NEO-3-012-UPG-01
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- None
