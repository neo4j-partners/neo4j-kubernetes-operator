# `image.imagePullSecrets`

## Client need

Operators reference existing Kubernetes docker-registry Secrets so the cluster can pull private Neo4j images.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Container image

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_imagePullSecret.tpl#neo4j.imagePullSecrets`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod imagePullSecrets
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

packaging

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-IMAGE | registry auth | image.imageCredentials |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.image.pullSecrets`
- **Notes**: Maps to pullSecrets in CRD.

## Aggregation

- **Group**: AGG-IMAGE
- **Must decide with**: fields sharing `AGG-IMAGE` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: List of secret names; direct map.

## FR / AC

- FR: NEO-2-004; NEO-3-004-IMG-01
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- None
