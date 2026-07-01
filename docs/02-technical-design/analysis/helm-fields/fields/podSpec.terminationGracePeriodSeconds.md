# `podSpec.terminationGracePeriodSeconds`

## Client need

Operators need a long graceful shutdown window so Neo4j can flush in-memory data to disk before SIGKILL. Helm defaults to 3600s; offline maintenance forces 0.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/configuration/neo4j-conf/) — Graceful shutdown

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-statefulset.yaml#L67`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod spec terminationGracePeriodSeconds
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | graceful shutdown | neo4j.offlineMaintenanceModeEnabled |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.scheduling.terminationGracePeriodSeconds (draft)`
- **Notes**: Coupled to rolling restart strategy (NEO-3-010-RSTR-02).

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Numeric override with sensible default.

## FR / AC

- FR: NEO-2-008; NEO-2-010
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- CRD field name/section not yet in spec.md — add under scheduling?
