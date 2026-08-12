# `podSpec`

## Client need

Operators need a single values block for pod-level Kubernetes settings beyond the Neo4j container itself: affinity, tolerations, service account, grace period, sidecars, and load-balancer inclusion. This is the primary scheduling and pod-lifecycle surface in the Helm chart.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-statefulset.yaml`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod template spec
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | pod placement & lifecycle | nodeSelector, podDisruptionBudget |; | CONCERN-TOPOLOGY | loadbalancer inclusion | podSpec.loadbalancer, services.neo4j.selector |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.scheduling + spec.podTemplate + spec.security.serviceAccount`
- **Notes**: Map root — see child field docs for per-field CRD targets.

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: breaking
- **Rationale**: Helm flat podSpec splits across scheduling, security, podTemplate, and topology (loadbalancer) in CRD.

## FR / AC

- FR: NEO-2-008; NEO-3-008-SCH-01; NEO-3-008-SCH-02; NEO-3-008-SCH-03; NEO-3-008-SCH-04; NEO-3-008-SCH-05; NEO-3-008-SCH-06
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Aggregation must decide how loadbalancer flag maps vs services.exposure in CRD.
