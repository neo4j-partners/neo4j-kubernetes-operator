# `apoc_config`

## Client need

Operators enable and tune APOC (and APOC Extended) behavior — triggers, file import, export settings — via `apoc.conf` properties without baking them into the main `neo4j.conf` user map. This separates plugin-specific configuration from core server settings.

## Neo4j documentation

- [APOC configuration](https://neo4j.com/docs/apoc/current/config/) — `apoc.conf` settings
- [APOC installation](https://neo4j.com/docs/apoc/current/installation/) — plugin prerequisites

## Helm implementation

- **Templates**: `neo4j-config.yaml` L197–218 — creates `{release}-apoc-config` ConfigMap with `apoc.conf` body from key=value pairs; `_volumeTemplate.tpl` `neo4j.apoc.volume` mounts projected ConfigMap at `/config/`; rejects `server.jvm.additional`
- **Go model**: No dedicated struct — dynamic map `apoc_config` in values (not typed in `release_values.go`)
- **K8s resources**: ConfigMap `{release}-apoc-config`; projected volume `apoc-conf` on Neo4j pod
- **Neo4j mechanism**: `apoc.conf` alongside `neo4j.conf` under `NEO4J_CONF`

## Category

plugins

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-PLUGINS-ON-TOPOLOGY | APOC config applies per release/pod; in operator model config follows pool plugin assignment | `analytics.*`, `apoc_credentials`, `env` (`NEO4J_PLUGINS`) |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.pluginDefinitions.apoc.config` (map[string]string per BDR-004 Option E)
- **Notes**: Assignment via `topology.*.plugins` or `spec.plugins`; config centralized in `pluginDefinitions`.

## Aggregation

- **Group**: AGG-TOPO-PLUGINS
- **Must decide with**: `apoc_credentials`, BDR-004, topology pool plugins

## Versioning

- **Classification**: breaking
- **Rationale**: BC-002 — moves from top-level Helm map to `pluginDefinitions` keyed by catalog id; pool-scoped in Cluster mode.

## FR / AC

- FR: NEO-2-003, NEO-3-003-APOC-01
- AC: AC-NEO-APOC

## Open questions

- APOC Extended vs Core: single `apoc` catalog id or split ids in operator?
- Per-pool `apoc.conf` diffs when primaries and secondaries both run APOC?
