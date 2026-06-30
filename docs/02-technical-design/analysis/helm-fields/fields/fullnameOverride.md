# `fullnameOverride`

## Client need

Operators integrating Neo4j with existing naming conventions (GitOps repos, multi-chart umbrellas) need to fully replace the computed Helm resource prefix instead of accepting `release-chartname` defaults. This avoids collisions when release names are long or when downstream automation expects a fixed object name stem.

## Neo4j documentation

- [Neo4j on Kubernetes](https://neo4j.com/docs/operations-manual/current/kubernetes/) — chart install context

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl` (`neo4j.fullname` — `fullnameOverride` takes precedence over `nameOverride` / release name)
- **Go model**: `helm-charts/internal/model/release_values.go` — `HelmValues.FullnameOverride`
- **K8s resources**: StatefulSet, Services, ConfigMaps, Secrets, operations Job, hooks — all `metadata.name` prefixes
- **Neo4j mechanism**: None

## Category

packaging

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| — | Chart packaging only | `nameOverride` |

## CRD mapping (draft)

- **Target**: N/A
- **Notes**: Operator derives child object names from `Neo4j.metadata.name`

## Aggregation

- **Group**: none
- **Must decide with**: —

## Versioning

- **Classification**: safe
- **Rationale**: Helm packaging only

## FR / AC

- FR: —
- AC: —

## Open questions

- None
