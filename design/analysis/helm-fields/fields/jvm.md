# `jvm`

## Client need

Operators need control over the Java virtual machine flags Neo4j runs with — retaining vendor-tuned defaults while appending heap dumps, metaspace limits, or GC tuning for production workloads. Helm groups JVM settings separately from the free-form `config` map because `server.jvm.additional` has special multi-line rendering rules.

## Neo4j documentation

- [JVM configuration](https://neo4j.com/docs/operations-manual/current/configuration/jvm-configuration/) — default and additional JVM arguments
- [Performance tuning](https://neo4j.com/docs/operations-manual/current/performance/) — heap and memory guidance

## Helm implementation

- **Templates**: `neo4j-config.yaml` — when `jvm.useNeo4jDefaultJvmArguments` or `jvm.additionalJvmArguments` set, emits `server.jvm.additional` block in user-config ConfigMap; parses defaults from bundled `neo4j-community.conf` / `neo4j-enterprise.conf` via `neo4j.configJvmAdditionalYaml`
- **Go model**: `HelmValues.Jvm` (`Jvm` struct) in `release_values.go`
- **K8s resources**: ConfigMap `{release}-user-config` (`server.jvm.additional` key)
- **Neo4j mechanism**: Multi-line `server.jvm.additional` in `neo4j.conf`

## Category

config

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| — | JVM is isolated from topology; no cross-path conditionals | `config` (explicitly rejects `server.jvm.additional`) |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.jvm` (parent object: `useDefaults`, `additionalArguments`)
- **Notes**: Renamed fields per CRD spec (`useNeo4jDefaultJvmArguments` → `useDefaults`).

## Aggregation

- **Group**: AGG-CONFIG-SURFACE
- **Must decide with**: `config`, BDR-008 config surface

## Versioning

- **Classification**: breaking
- **Rationale**: Part of AGG-CONFIG-SURFACE; field rename and structured object vs flat `neo4j.conf` key.

## FR / AC

- FR: NEO-2-003, NEO-3-003-JVM-01, NEO-3-003-JVM-02
- AC: AC-NEO-CONFIG

## Open questions

- Should operator expose GC profile presets vs raw `additionalArguments` only?
