# `livenessProbe`

## Client need

Operators need Kubernetes to restart Neo4j pods that are truly dead while tolerating long JVM GC pauses. The chart uses Bolt TCP checks with a higher failure threshold than readiness so transient pauses do not cause unnecessary restarts.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Pod lifecycle

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl#neo4j.livenessProbe`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod spec livenessProbe
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

health

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-HEALTH-PROBES | primary probe config | readinessProbe, startupProbe, neo4j.offlineMaintenanceModeEnabled |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.probes.liveness`
- **Notes**: Disabled when offline maintenance mode is active in Helm; operator should mirror.

## Aggregation

- **Group**: AGG-HEALTH-PROBES
- **Must decide with**: fields sharing `AGG-HEALTH-PROBES` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Optional override of operator defaults; no CRD shape break.

## FR / AC

- FR: NEO-2-009; NEO-3-009-PROBE-01; NEO-3-009-PROBE-02
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Cluster rolling restart: should liveness failure on one member trigger operator-level remediation beyond kubelet restart?
