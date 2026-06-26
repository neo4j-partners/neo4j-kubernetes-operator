# `services.neo4j.ports`

## Client need

Control which Neo4j connectors are published on the external service (http, https, bolt, backup).

## Neo4j documentation

- [Connectors](https://neo4j.com/docs/operations-manual/current/configuration/connectors/) — Connectors
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: neo4j-loadbalancer.yaml (per-port enabled flags)
- **Go model**: release_values.go: Neo4jService.Ports
- **K8s resources**: Service ports
- **Neo4j mechanism**: Disabling a port on the Service does not disable the Neo4j connector (config.* still governs process).

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-EXPOSURE | port publication | config server.http.enabled, ssl.https |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.connectivity.external.ports`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-EXPOSURE
- **Must decide with**: AGG-EXPOSURE

## Versioning

- **Classification**: safe
- **Rationale**: Port exposure is tunable day-2.

## FR / AC

- FR: NEO-2-007; NEO-3-007-PRT-01; NEO-3-007-PRT-02; NEO-3-007-PRT-03; NEO-3-007-PRT-04
- AC: AC-NEO-NETWORKING-PORTS-FULL; AC-NEO-NETWORKING-PORTS-BOLT; AC-NEO-NETWORKING-PORTS-HTTPS

## Open questions

- Align with config connector enablement in operator validation.
