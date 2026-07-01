# `env`

## Client need

Operators inject extra container environment variables for Neo4j startup — commonly `NEO4J_PLUGINS` to install APOC, GDS, or Bloom JARs, or other image-supported env hooks — without patching the StatefulSet template. This is the Helm escape hatch for behaviors not modeled as first-class values.

## Neo4j documentation

- [Neo4j Docker configuration](https://neo4j.com/docs/operations-manual/current/docker/configuration/) — supported `NEO4J_*` environment variables
- [GDS cluster deployment](https://neo4j.com/docs/graph-data-science/current/production-deployment/neo4j-cluster/) — plugin placement constraints
- [APOC installation](https://neo4j.com/docs/apoc/current/installation/) — plugin enablement

## Helm implementation

- **Templates**: `neo4j-env.yaml` — merges user `env` map into `{release}-env` ConfigMap after chart-owned keys (`NEO4J_EDITION`, `NEO4J_CONF`, `NEO4J_AUTH_PATH`, license acceptance)
- **Go model**: `HelmValues.Env` (empty struct — dynamic map via YAML) in `release_values.go`
- **K8s resources**: ConfigMap `{release}-env`; referenced from StatefulSet via `envFrom`
- **Neo4j mechanism**: Official Neo4j image reads `NEO4J_PLUGINS` and other env vars at container start

## Category

config

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-PLUGINS-ON-TOPOLOGY | `NEO4J_PLUGINS` in `env` is the Helm path for plugin JAR install; must align with analytics secondary / pool assignment | `analytics.*`, `apoc_config`, `apoc_credentials` |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.podTemplate.env` (escape hatch) — plugin install should migrate to `spec.plugins` + `spec.pluginDefinitions` (BDR-004 Option E)
- **Notes**: Operator should discourage raw `NEO4J_PLUGINS` when structured plugin model is used.

## Aggregation

- **Group**: AGG-TOPO-PLUGINS (when used for plugins); none for generic env
- **Must decide with**: BDR-004 plugin catalog vs podTemplate escape hatch

## Versioning

- **Classification**: deferred
- **Rationale**: Plugin env vars superseded by `pluginDefinitions`; generic `podTemplate.env` retained as advanced escape hatch (V2).

## FR / AC

- FR: NEO-2-003
- AC: AC-NEO-CONFIG

## Open questions

- V1: reject `NEO4J_PLUGINS` in `podTemplate.env` when `spec.plugins` is set?
- Document migration from Helm `env.NEO4J_PLUGINS` to operator plugin model.
