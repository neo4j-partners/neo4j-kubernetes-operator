# `neo4j.resources`

## Client need

Platform teams must size CPU and memory for Neo4j pods to meet performance SLOs and pass chart validation (minimum 0.5 CPU, 2Gi memory). Helm supports shorthand (same requests/limits) or full Kubernetes resource format with independent limits.

## Neo4j documentation

- [Performance tuning](https://neo4j.com/docs/operations-manual/current/performance/) — memory planning
- [Configuration — memory](https://neo4j.com/docs/operations-manual/current/configuration/) — heap and page cache

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl` (`neo4j.resources.checkForEmptyResources`, `evaluateCPU`, `evaluateMemory` — validates and normalizes); `neo4j-statefulset.yaml` L134 (`resources` on neo4j container)
- **Go model**: `release_values.go` — `Neo4J.Resources` (`Resources` struct with cpu/memory or requests/limits)
- **K8s resources**: StatefulSet container `resources`
- **Neo4j mechanism**: JVM heap/pagecache derived from container memory via Neo4j docker entrypoint defaults

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| — | Workload sizing | `jvm.*`, `config` (memory-related keys) |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.resources` (requests/limits per spec.md)
- **Notes**: Operator may enforce `limits.memory == requests.memory` for stability

## Aggregation

- **Group**: none
- **Must decide with**: `jvm.additionalJvmArguments` (sizing coherence)

## Versioning

- **Classification**: safe
- **Rationale**: Standard K8s resources — mutable with rolling restart

## FR / AC

- FR: NEO-2-008
- AC: AC-NEO-SCHEDULING

## Open questions

- Per-pool resources in cluster mode (primaries vs secondaries) — operator V1 uses uniform `spec.resources`?
