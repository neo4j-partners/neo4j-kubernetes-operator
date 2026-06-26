# `jvm.additionalJvmArguments`

## Client need

Operators append custom JVM flags — heap dump on OOM, metaspace limits, diagnostic flags — on top of (or instead of, when defaults disabled) Neo4j's bundled JVM profile. Each argument is a separate list element, avoiding fragile multi-line YAML in the `config` map.

## Neo4j documentation

- [JVM configuration](https://neo4j.com/docs/operations-manual/current/configuration/jvm-configuration/) — `server.jvm.additional`
- [Heap dump configuration](https://neo4j.com/docs/operations-manual/current/monitoring/logging/) — common OOM diagnostics

## Helm implementation

- **Templates**: `neo4j-config.yaml` L72–74 — each list entry appended to `server.jvm.additional`; strips redundant `server.jvm.additional=` prefix if present
- **Go model**: `Jvm.AdditionalJvmArguments []string` in `release_values.go`
- **K8s resources**: ConfigMap `{release}-user-config`
- **Neo4j mechanism**: Additional lines in `server.jvm.additional` multiline value

## Category

config

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| — | Paired with `jvm.useNeo4jDefaultJvmArguments` | `config` (forbidden duplicate path) |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.jvm.additionalArguments`
- **Notes**: List of strings; Helm examples use `-XX:+HeapDumpOnOutOfMemoryError` style entries.

## Aggregation

- **Group**: AGG-CONFIG-SURFACE
- **Must decide with**: `jvm`, BDR-008

## Versioning

- **Classification**: safe
- **Rationale**: Additive list; no structural breaking change once `spec.jvm` exists.

## FR / AC

- FR: NEO-3-003-JVM-02
- AC: AC-NEO-CONFIG

## Open questions

- Validate JVM flag syntax at admission vs pass-through to Neo4j startup failure?
