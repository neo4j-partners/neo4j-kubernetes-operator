# `nameOverride`

## Client need

Platform teams deploying multiple Helm releases in one namespace need predictable Kubernetes resource name prefixes without renaming the Helm release itself. `nameOverride` shortens or customizes the chart-derived name segment used in labels and object names derived from `neo4j.fullname`, while keeping the release name as the install identifier.

## Neo4j documentation

- [Neo4j on Kubernetes](https://neo4j.com/docs/operations-manual/current/kubernetes/) — deployment overview (chart-level naming is Helm convention, not Neo4j server config)

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl` (`neo4j.fullname` — combines `Release.Name` with `nameOverride` when set)
- **Go model**: `helm-charts/internal/model/release_values.go` — `HelmValues.NameOverride`
- **K8s resources**: All chart objects using `include "neo4j.fullname"` (StatefulSet, Services, ConfigMaps, Secrets, Jobs)
- **Neo4j mechanism**: None — affects K8s metadata naming only; does not change `neo4j.name` cluster identity

## Category

packaging

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| — | Chart packaging only | `fullnameOverride` |

## CRD mapping (draft)

- **Target**: N/A — operator uses `Neo4j.metadata.name` as workload identity; no Helm `nameOverride` equivalent
- **Notes**: Document Helm→operator naming divergence in `11-helm-mapping.md`

## Aggregation

- **Group**: none
- **Must decide with**: —

## Versioning

- **Classification**: safe
- **Rationale**: Helm-only packaging knob; absent from operator CRD surface

## FR / AC

- FR: —
- AC: —

## Open questions

- Should operator document recommended `metadata.name` patterns mirroring Helm `nameOverride` behaviour?
