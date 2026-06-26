# `logging.userLogsXml`

## Client need

Operators override user-logs.xml for neo4j.log and console appenders, tuning verbosity for application-level messages.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/monitoring/logging/) — User logs

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-config.yaml#L248-253`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: ConfigMap data user-logs.xml
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

observability

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-OBSERVABILITY | user log4j | logging.serverLogsXml |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.logging.userLogsXml (draft)`
- **Notes**: Mounted at /config/user-logs.xml.

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

- Same as serverLogsXml — inline vs external ConfigMap ref.
