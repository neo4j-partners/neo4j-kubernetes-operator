# `logging`

## Client need

Operators customize Neo4j Log4j2 configuration for server logs (debug, query, http, security) and user logs (neo4j.log) without rebuilding the image. Defaults ship as chart-bundled XML files.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/monitoring/logging/) — Logging configuration

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-config.yaml`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: ConfigMap server-logs-config, user-logs-config; volume mounts
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

observability

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-OBSERVABILITY | log4j config | logging.serverLogsXml, logging.userLogsXml, volumes.logs |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.logging (draft) | config map refs`
- **Notes**: Map root for logging XML blobs.

## Aggregation

- **Group**: AGG-OBSERVABILITY
- **Must decide with**: fields sharing `AGG-OBSERVABILITY` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Optional XML overrides; defaults unchanged if empty.

## FR / AC

- FR: NEO-2-016; NEO-3-016-LOG-01; NEO-3-016-LOG-02
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- CRD section for logging not fully specified in spec.md — add spec.logging?
