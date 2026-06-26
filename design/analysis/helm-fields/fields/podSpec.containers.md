# `podSpec.containers`

## Client need

Operators attach sidecar containers (metrics agents, log shippers) alongside the Neo4j process in the same pod.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-statefulset.yaml#L173-180`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod spec containers (additional)
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | sidecars | serviceMonitor, logging |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.podTemplate.sidecars`
- **Notes**: Renamed sidecars in CRD vs containers in Helm.

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Additive sidecar list.

## FR / AC

- FR: NEO-2-008; NEO-2-015
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Sidecar defaults neo4j image when image key omitted — operator behavior?
