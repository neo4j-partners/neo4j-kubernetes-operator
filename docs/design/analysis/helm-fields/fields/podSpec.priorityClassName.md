# `podSpec.priorityClassName`

## Client need

Operators assign pod priority for preemption and scheduling order in congested clusters. Helm optionally validates the PriorityClass exists via lookup.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl#neo4j.priorityClassName`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod spec priorityClassName
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | scheduling priority | podSpec |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.scheduling.priorityClassName`
- **Notes**: Deferred in V1 CRD per spec.md.

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: deferred
- **Rationale**: CRD spec marks V1=No for priorityClassName.

## FR / AC

- FR: NEO-2-008; NEO-3-008-SCH-05
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Include in V1.1 or podTemplate escape hatch only?
