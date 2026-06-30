# `startupProbe`

## Client need

Operators deploying Neo4j after backup restore or cluster formation need a long startup window before readiness/liveness take effect. Startup probe gates other probes until Bolt accepts connections, accommodating store recovery and cluster bootstrap.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/clustering/) — Cluster formation timing

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl#neo4j.startupProbe`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod spec startupProbe
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

health

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-HEALTH-PROBES | long bootstrap window | readinessProbe, livenessProbe, neo4j.minimumClusterSize |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.probes.startup`
- **Notes**: Default failureThreshold:1000 in Helm — operator must preserve long startup tolerance.

## Aggregation

- **Group**: AGG-HEALTH-PROBES
- **Must decide with**: fields sharing `AGG-HEALTH-PROBES` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Defaults are safe; custom values are passthrough.

## FR / AC

- FR: NEO-2-009; NEO-3-009-PROBE-01; NEO-3-009-PROBE-02
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Operator single-STS multi-replica: per-ordinal startup timing may differ during rolling upgrade.
