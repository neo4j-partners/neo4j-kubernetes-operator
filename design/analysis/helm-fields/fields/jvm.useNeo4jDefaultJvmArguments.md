# `jvm.useNeo4jDefaultJvmArguments`

## Client need

Operators want Neo4j's vendor-recommended JVM flags applied automatically (heap sizing heuristics, GC defaults) unless they intentionally replace the entire JVM profile. Setting this to `false` means only `jvm.additionalJvmArguments` are used — for advanced users who manage JVM flags entirely themselves.

## Neo4j documentation

- [JVM configuration](https://neo4j.com/docs/operations-manual/current/configuration/jvm-configuration/) — Neo4j default JVM arguments shipped in edition-specific conf files

## Helm implementation

- **Templates**: `neo4j-config.yaml` L69–71 — when `true`, includes `neo4j.configJvmAdditionalYaml` output from edition conf file inside `server.jvm.additional`
- **Go model**: `Jvm.UseNeo4JDefaultJvmArguments bool` in `release_values.go`
- **K8s resources**: ConfigMap `{release}-user-config`
- **Neo4j mechanism**: Prepends default JVM lines before additional arguments in `server.jvm.additional`

## Category

config

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| — | Child of `jvm` object; no topology linkage | `jvm.additionalJvmArguments` |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.jvm.useDefaults`
- **Notes**: Default `true` matches Helm.

## Aggregation

- **Group**: AGG-CONFIG-SURFACE
- **Must decide with**: `jvm`, `jvm.additionalJvmArguments`

## Versioning

- **Classification**: safe
- **Rationale**: Boolean default with direct Helm equivalent; rename only at CRD boundary.

## FR / AC

- FR: NEO-3-003-JVM-01
- AC: AC-NEO-CONFIG

## Open questions

- None — straightforward bool mapping.
