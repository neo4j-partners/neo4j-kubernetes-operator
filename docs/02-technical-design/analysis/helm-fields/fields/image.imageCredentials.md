# `image.imageCredentials`

## Client need

Operators provide registry username/password inline so Helm creates docker-registry Secrets automatically, avoiding manual Secret creation.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Container image

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_imagePullSecret.tpl`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: Secret type kubernetes.io/dockerconfigjson
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

packaging

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-IMAGE | auto secret creation | image.imagePullSecrets |

## CRD mapping (draft)

- **Target**: `N/A — operator expects pre-created pullSecrets`
- **Notes**: Helm-only convenience; operator should reference existing secrets.

## Aggregation

- **Group**: AGG-IMAGE
- **Must decide with**: fields sharing `AGG-IMAGE` in aggregation-matrix.md

## Versioning

- **Classification**: deferred
- **Rationale**: Inline credentials not in V1 CRD; use external Secret + pullSecrets.

## FR / AC

- FR: NEO-2-004; NEO-3-004-IMG-02
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Defer NEO-3-004-IMG-02 to V2 or document external Secret workflow.
