# `podSpec.dnsPolicy`

## Client need

Operators set pod DNS resolution behavior (default ClusterFirst) for Neo4j discovery and sidecar integration.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-statefulset.yaml#L62`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod spec dnsPolicy
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | pod DNS | clusterDomain, services.* |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.podTemplate.dnsPolicy (draft)`
- **Notes**: Default ClusterFirst; surface via podTemplate if needed.

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: deferred
- **Rationale**: Rare override; not in V1 CRD scheduling section.

## FR / AC

- FR: NEO-2-008
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- V1 exposure via podTemplate escape hatch?
