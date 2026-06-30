# `logging.serverLogsXml`

## Client need

Operators override server-logs.xml to control debug, query, HTTP, and security log appenders and levels for operational and compliance needs.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/monitoring/logging/) — Server logs

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-config.yaml#L231-236`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: ConfigMap data server-logs.xml
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

observability

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-OBSERVABILITY | server log4j | logging.userLogsXml, config server.directories.logs |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.logging.serverLogsXml (draft)`
- **Notes**: Mounted at /config/server-logs.xml in pod.

## Aggregation

- **Group**: AGG-OBSERVABILITY
- **Must decide with**: fields sharing `AGG-OBSERVABILITY` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Optional string override.

## FR / AC

- FR: NEO-2-016; NEO-3-016-LOG-02
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Large XML in CRD vs ConfigMap reference?
