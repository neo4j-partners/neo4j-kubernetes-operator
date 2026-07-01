# `image.registry`

## Client need

Operators pull Neo4j from a private container registry mirror instead of Docker Hub.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Container image

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_image.tpl#L36-39`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet container image URL prefix
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

packaging

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-IMAGE | private registry | image.repository, image.imagePullSecrets |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.image.repository (combined) | separate registry field (draft)`
- **Notes**: Mutually exclusive with customImage in Helm.

## Aggregation

- **Group**: AGG-IMAGE
- **Must decide with**: fields sharing `AGG-IMAGE` in aggregation-matrix.md

## Versioning

- **Classification**: breaking
- **Rationale**: CRD uses repository+version; registry may be embedded in repository string.

## FR / AC

- FR: NEO-2-004; NEO-3-004-IMG-01
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Add explicit spec.image.registry or parse from repository?
