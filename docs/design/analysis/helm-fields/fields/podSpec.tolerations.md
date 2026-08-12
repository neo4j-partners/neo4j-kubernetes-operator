# `podSpec.tolerations`

## Client need

Operators schedule Neo4j on tainted nodes (dedicated graph pools, spot with taints) by declaring tolerations on the pod.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl#neo4j.tolerations`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod spec tolerations
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | taint tolerance | nodeSelector, podSpec.nodeAffinity |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.scheduling.tolerations`
- **Notes**: Also applied to cleanup hook Job and operations Job in Helm.

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Standard K8s toleration list.

## FR / AC

- FR: NEO-2-008; NEO-3-008-SCH-03
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- None
