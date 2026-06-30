# `image.imagePullPolicy`

## Client need

Operators control when kubelet pulls the Neo4j image (IfNotPresent vs Always), important for pinned digests and air-gapped mirrors.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Container image

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-statefulset.yaml#L90`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet container imagePullPolicy
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

packaging

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-IMAGE | pull behavior | image.customImage, image.registry |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.image.pullPolicy`
- **Notes**: Default IfNotPresent per CRD spec.

## Aggregation

- **Group**: AGG-IMAGE
- **Must decide with**: fields sharing `AGG-IMAGE` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Direct field mapping.

## FR / AC

- FR: NEO-2-012; NEO-3-012-UPG-01
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- None
