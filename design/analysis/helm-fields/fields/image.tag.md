# `image.tag`

## Client need

Operators pin or override the Neo4j image tag; when empty Helm derives from chart AppVersion and edition (-enterprise suffix).

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Container image

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_image.tpl#L38-39`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet container image tag
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

packaging

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-IMAGE | version pin | neo4j.edition, image.repository |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.version`
- **Notes**: Effective image {repository}:{spec.version} per CRD spec.

## Aggregation

- **Group**: AGG-IMAGE
- **Must decide with**: fields sharing `AGG-IMAGE` in aggregation-matrix.md

## Versioning

- **Classification**: breaking
- **Rationale**: Tag promoted to top-level spec.version for upgrade semantics.

## FR / AC

- FR: NEO-2-012; NEO-3-012-UPG-01; NEO-3-012-UPG-02
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Enterprise suffix: operator appends or user supplies full tag?
