# `image`

## Client need

Operators must specify which Neo4j container image to run, including private registry mirrors, pull policy, and credentials. Image choice drives version, edition suffix, and upgrade path.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Container image

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_image.tpl#neo4j.image`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet container image; Secret (imagePullSecret); Job images
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

packaging

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-IMAGE | container identity | image.*, neo4j.edition, neo4j.version |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.image + Neo4j.spec.version`
- **Notes**: Map root — see child fields.

## Aggregation

- **Group**: AGG-IMAGE
- **Must decide with**: fields sharing `AGG-IMAGE` in aggregation-matrix.md

## Versioning

- **Classification**: breaking
- **Rationale**: Helm customImage vs registry/repository/tag splits to spec.version + spec.image in CRD.

## FR / AC

- FR: NEO-2-004; NEO-2-012; NEO-3-004-IMG-01; NEO-3-004-IMG-02; NEO-3-012-UPG-01
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- customImage full string vs repository+version split for operator upgrades.
