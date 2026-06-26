# `statefulset`

## Client need

Operators may need to annotate the Neo4j StatefulSet for integration with backup tools, GitOps, or cloud-specific controllers. The Helm chart exposes only metadata annotations on the StatefulSet resource.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-statefulset.yaml`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet metadata
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | workload metadata | statefulset.metadata.annotations |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.podTemplate.statefulSetMetadata (draft) | N/A for V1`
- **Notes**: V1: operator generates STS; only annotations may surface via podTemplate or metadata section.

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: deferred
- **Rationale**: STS metadata escape hatch low priority for V1; operator owns STS.

## FR / AC

- FR: NEO-2-008
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- PS input: expose STS annotations in CRD or rely on operator labels only?
