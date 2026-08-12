# `statefulset.metadata`

## Client need

Operators attach annotations to the StatefulSet object (not just pods) for controllers that watch workload-level metadata.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-statefulset.yaml#L31-34`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet metadata.annotations
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | STS annotations | statefulset |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.podTemplate.statefulSetMetadata.annotations (draft)`
- **Notes**: Child of statefulset map root.

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: deferred
- **Rationale**: Cosmetic/metadata; defer to podTemplate escape hatch.

## FR / AC

- FR: NEO-2-008
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Same as statefulset root.
