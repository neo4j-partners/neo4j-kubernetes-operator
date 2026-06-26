# `config`

## Client need

Operators need to tune Neo4j server behavior beyond chart defaults — memory sizing, query cache, strict validation, feature flags, and TLS reload — without forking the Helm chart. The `config` map is the primary escape hatch for arbitrary `neo4j.conf` key-value settings that apply to this release's single Neo4j instance.

## Neo4j documentation

- [Configuration settings](https://neo4j.com/docs/operations-manual/current/configuration/configuration-settings/) — full `neo4j.conf` reference
- [Validate configuration](https://neo4j.com/docs/operations-manual/current/configuration/validation/) — `server.config.strict_validation.enabled`
- [Dynamic SSL/TLS certificate reloading](https://neo4j.com/docs/operations-manual/current/security/ssl-framework/) — `dbms.security.tls_reload_enabled`

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-config.yaml` — user keys merged into `{release}-user-config` ConfigMap; rejects `server.jvm.additional` (redirects to `jvm.additionalJvmArguments`). `neo4j-env.yaml` reads `dbms.security.auth_enabled` from this map.
- **Go model**: `HelmValues.Config map[string]string` in `release_values.go`
- **K8s resources**: ConfigMaps `{release}-user-config`, `{release}-default-config` (chart-injected cluster/TLS/volume paths override or supplement user values)
- **Neo4j mechanism**: Keys rendered as `neo4j.conf` entries; mounted via `NEO4J_CONF=/config/`

## Category

config

## Semantic concerns

> From **helm-semantic-concern-mapper** — paths in the same concern may be far apart in values.yaml.

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TOPOLOGY | Chart injects cluster/discovery keys in `default-config` from topology flags; user `config` must not fight primaries count | `neo4j.minimumClusterSize`, `analytics.*`, `services.internals`, `clusterDomain` |
| CONCERN-TLS | Default includes `dbms.security.tls_reload_enabled`; SSL policy keys auto-set when `ssl.*` secrets present | `ssl.bolt`, `ssl.https`, `ssl.cluster` |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.config` (map[string]string)
- **Notes**: Operator merges user map → topology-derived defaults → K8s discovery settings (BDR-002). Open question: full passthrough vs allowlist (BDR-008).

## Aggregation

- **Group**: AGG-CONFIG-SURFACE
- **Must decide with**: `jvm.*`, operator config validation policy (BDR-008)

## Versioning

- **Classification**: breaking
- **Rationale**: BC-007 — passthrough map vs curated allowlist changes CRD contract and migration from Helm `config` keys.

## FR / AC

- FR: NEO-2-003, NEO-3-003-CFG-01, NEO-3-003-CFG-02, NEO-3-003-CFG-03
- AC: AC-NEO-CONFIG, AC-NEO-CONFIG-CHANGE

## Open questions

- V1 allowlist scope: which Helm-default keys (`tls_reload`, SPeeDy flag) become first-class CRD fields vs remain in `spec.config`?
- How does operator prevent user `config` from overriding topology-injected cluster keys?
