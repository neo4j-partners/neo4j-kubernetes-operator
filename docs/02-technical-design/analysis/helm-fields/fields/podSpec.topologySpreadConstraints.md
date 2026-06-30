# `podSpec.topologySpreadConstraints`

## Client need

Operators distribute Neo4j pods across failure domains (zones, hosts) with skew limits, especially when running multiple members or co-located workloads.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl#neo4j.topologySpreadConstraints`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod spec topologySpreadConstraints
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | domain spread | podSpec.podAntiAffinity, podSpec.nodeAffinity |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.scheduling.topologySpreadConstraints`
- **Notes**: Direct mapping per CRD spec.md.

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Passthrough K8s API type.

## FR / AC

- FR: NEO-2-008; NEO-3-008-SCH-04
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Operator multi-replica STS: constraints should reference pool labels automatically?
