# `image.customImage`

## Client need

Operators supply a complete image reference string (registry/repo:tag) for custom builds or internal image pipelines, bypassing chart version defaults.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Container image

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_image.tpl#L21-34`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet container image
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

packaging

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-IMAGE | full image override | image.registry, image.repository, image.tag |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.image (full override draft) | split parse`
- **Notes**: Helm fails if both customImage and separated fields set.

## Aggregation

- **Group**: AGG-IMAGE
- **Must decide with**: fields sharing `AGG-IMAGE` in aggregation-matrix.md

## Versioning

- **Classification**: breaking
- **Rationale**: CRD may not support opaque customImage string — must decompose for upgrades.

## FR / AC

- FR: NEO-2-012; NEO-3-012-UPG-01
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- PS: support customImage in CRD or require repository+version only?
