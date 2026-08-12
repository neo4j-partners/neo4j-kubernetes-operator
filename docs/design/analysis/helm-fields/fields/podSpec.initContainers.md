# `podSpec.initContainers`

## Client need

Operators inject init containers for pre-start setup (custom permissions, data seeding) while the chart may prepend its own chmod init container for volume permissions.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-statefulset.yaml#L68-83`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: StatefulSet pod spec initContainers
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | pre-start hooks | podSpec.containers, volumes |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.podTemplate.initContainers`
- **Notes**: Operator-owned init containers cannot be overridden per spec.md.

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Escape hatch list; operator merges with owned init containers.

## FR / AC

- FR: NEO-2-008
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Document merge order: operator chmod init + user inits.
