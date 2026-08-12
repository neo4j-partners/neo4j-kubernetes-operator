# `podSpec.nodeAffinity`

## Client need

Operators express required or preferred node placement rules richer than simple nodeSelector (zones, instance types). Passed through to the pod spec affinity.nodeAffinity block.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl#neo4j.affinity`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod spec affinity.nodeAffinity
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | advanced placement | nodeSelector, podSpec.topologySpreadConstraints |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.scheduling.affinity.custom (nodeAffinity portion)`
- **Notes**: When affinity.podAntiAffinity=custom, full affinity blob may include nodeAffinity.

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Passthrough affinity rules.

## FR / AC

- FR: NEO-2-008; NEO-3-008-SCH-02
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Merge nodeSelector with nodeAffinity in operator or keep separate CRD fields?
