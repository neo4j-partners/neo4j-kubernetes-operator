# `apoc_credentials`

## Client need

Operators connect APOC Extended integrations (JDBC, Elasticsearch) to external systems without putting connection URLs in plain ConfigMap text. Secrets mount at configured paths; Helm generates `apoc.jdbc.<alias>.url` / `apoc.es.<alias>.url` entries in `apoc.conf` that read the `URL` key from the mounted Secret at runtime.

## Neo4j documentation

- [APOC Extended — JDBC](https://neo4j.com/labs/apoc/5/export/jdbc/) — `apoc.jdbc.*.url` configuration
- [APOC Extended — Elasticsearch](https://neo4j.com/labs/apoc/5/export/elasticsearch/) — `apoc.es.*.url` configuration

## Helm implementation

- **Templates**: `_apocCredentials.tpl` — validation (required fields, Secret lookup, `URL` key); `neo4j.apocCredentials.generateConfig` emits shell-expanded URLs; volume/volumeMount helpers; `neo4j-config.yaml` includes generated lines in `apoc.conf`
- **Go model**: `ApocCredentials` with `Jdbc` and `Elasticsearch` maps in `release_values.go`
- **K8s resources**: Secret volumes per credential type; projected `apoc-conf` volume; ConfigMap `{release}-apoc-config`
- **Neo4j mechanism**: `apoc.conf` properties reference files mounted from Kubernetes Secrets

## Category

plugins

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-PLUGINS-ON-TOPOLOGY | Credential-backed APOC features run only where APOC Extended is installed; ties to pool plugin assignment | `apoc_config`, `analytics.*`, `env` (`NEO4J_PLUGINS`) |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.pluginDefinitions.apoc.credentials[]` or secret refs under `pluginDefinitions.apoc` (TBD — BDR-004 extension)
- **Notes**: Helm models `jdbc` and `elasticsearch` as fixed sub-keys with `aliasName`, `secretName`, `secretMountPath`.

## Aggregation

- **Group**: AGG-TOPO-PLUGINS
- **Must decide with**: `apoc_config`, BDR-004, secrets model (NEO-3-004)

## Versioning

- **Classification**: breaking
- **Rationale**: BC-002 — structured credential refs in `pluginDefinitions` vs flat Helm map; must co-decide plugin placement.

## FR / AC

- FR: NEO-2-003, NEO-2-004, NEO-3-003-APOC-02
- AC: AC-NEO-APOC-CREDS, AC-NEO-SECRETS

## Open questions

- Generalize beyond jdbc/elasticsearch credential types in CRD?
- Share Secret contract (`URL` key) vs typed fields per integration?
