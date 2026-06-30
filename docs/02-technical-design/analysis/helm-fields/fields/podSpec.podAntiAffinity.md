# `podSpec.podAntiAffinity`

## Client need

Operators running multiple Neo4j releases with the same neo4j.name must spread pods across nodes to survive host failure. Helm defaults to hard anti-affinity on hostname for pods sharing helm.neo4j.com/pod_category=neo4j-instance, or accepts a custom affinity object.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/clustering/) — HA placement

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl#neo4j.affinity`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod spec affinity.podAntiAffinity
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | spread Neo4j pods | podSpec.nodeAffinity, podSpec.topologySpreadConstraints, neo4j.name |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.scheduling.affinity.podAntiAffinity`
- **Notes**: CRD uses soft|hard|custom enum; Helm bool true = generated hard anti-affinity.

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Operator can map bool true → hard rule equivalent to Helm default.

## FR / AC

- FR: NEO-2-008; NEO-3-008-SCH-02
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Operator single-STS: anti-affinity across ordinals of same CR vs across separate CRs?
