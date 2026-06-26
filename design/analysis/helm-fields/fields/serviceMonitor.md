# `serviceMonitor`

## Client need

Operators integrate with Prometheus Operator by creating a ServiceMonitor CR to scrape Neo4j Prometheus metrics endpoint when server.metrics.prometheus.enabled is true in config.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/monitoring/metrics/) — Prometheus metrics

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-servicemonitor.yaml`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: ServiceMonitor (monitoring.coreos.com/v1)
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

observability

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-OBSERVABILITY | metrics scrape | config server.metrics.prometheus.enabled, services.admin |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.monitoring.serviceMonitor`
- **Notes**: Requires Prometheus Operator CRD in cluster.

## Aggregation

- **Group**: AGG-OBSERVABILITY
- **Must decide with**: fields sharing `AGG-OBSERVABILITY` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Optional monitoring integration; disabled by default.

## FR / AC

- FR: NEO-2-015; NEO-3-015-MON-01; NEO-3-015-MON-02
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Default selector/port/path when fields empty in Helm?
