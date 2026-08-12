# `nodeSelector`

## Client need

Operators must pin Neo4j pods to nodes with suitable hardware (SSD, memory class) or tenancy labels. Helm validates at install time that at least one node matches all selector labels unless disableLookups is set.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Resource planning

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_labels.tpl#neo4j.nodeSelector`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod spec nodeSelector
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | node placement | podSpec.nodeAffinity, podSpec.tolerations, podSpec.topologySpreadConstraints |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.scheduling.nodeSelector`
- **Notes**: Direct map to spec.scheduling.nodeSelector.

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Standard K8s map passthrough.

## FR / AC

- FR: NEO-2-008; NEO-3-008-SCH-01
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Should operator validate nodeSelector at admission (requires live node list) like Helm?
