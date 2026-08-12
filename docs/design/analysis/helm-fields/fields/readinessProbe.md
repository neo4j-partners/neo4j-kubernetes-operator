# `readinessProbe`

## Client need

Operators need Kubernetes to know when a Neo4j pod can accept traffic so Services and load balancers do not route clients to a JVM still starting or recovering from GC pauses. The chart defaults to a Bolt TCP probe with generous thresholds tuned for Java workloads.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment — health checks

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl#neo4j.readinessProbe`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod spec readinessProbe
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

health

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-HEALTH-PROBES | primary probe config | livenessProbe, startupProbe, neo4j.offlineMaintenanceModeEnabled |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.probes.readiness`
- **Notes**: Empty spec.probes.readiness → operator applies chart-equivalent defaults (tcpSocket:7687, failureThreshold:20).

## Aggregation

- **Group**: AGG-HEALTH-PROBES
- **Must decide with**: fields sharing `AGG-HEALTH-PROBES` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Probe overrides are additive; operator can ship same Neo4j-tuned defaults with optional overrides.

## FR / AC

- FR: NEO-2-009; NEO-3-009-PROBE-01; NEO-3-009-PROBE-02
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Should operator expose probe type (tcp vs http) as enum or raw Probe struct passthrough?
