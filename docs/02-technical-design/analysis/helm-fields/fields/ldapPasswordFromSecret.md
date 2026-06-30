# `ldapPasswordFromSecret`

## Client need

Enterprise deployments using LDAP authorization need the LDAP system account password available to Neo4j without storing it in the Helm values file or ConfigMap in cleartext. Operators reference an existing Kubernetes Secret; the chart mounts it and injects a `dbms.security.ldap.authorization.system_password` config value that reads the file at runtime.

## Neo4j documentation

- [LDAP integration](https://neo4j.com/docs/operations-manual/current/authentication-authorization/ldap-integration/) — LDAP system account and `dbms.security.ldap.authorization.system_password`
- [Security configuration](https://neo4j.com/docs/operations-manual/current/configuration/configuration-settings/#_security_configuration) — auth settings

## Helm implementation

- **Templates**: `_ldap.tpl` — `neo4j.ldapPasswordFromSecretExistsOrNot` validates Secret exists (unless `disableLookups`) and contains `LDAP_PASS` key; `neo4j.ldapVolume` adds Secret volume; `neo4j-config.yaml` L49–52 injects password via shell `cat` from mount path
- **Go model**: `HelmValues.LdapPasswordFromSecret string` in `release_values.go`
- **K8s resources**: Secret volume `neo4j-ldap-password`; ConfigMap `{release}-user-config`
- **Neo4j mechanism**: LDAP system password read from mounted file at container start

## Category

security

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| — | Paired with `ldapPasswordMountPath`; Enterprise-only | `neo4j.edition`, `config` (LDAP-related settings) |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.auth.ldap.systemPasswordSecretRef` (name + key `LDAP_PASS`) — exact path TBD in auth section
- **Notes**: Must be set together with mount path equivalent or use operator-managed fixed mount.

## Aggregation

- **Group**: AGG-AUTH
- **Must decide with**: `ldapPasswordMountPath`, LDAP config keys in `config`

## Versioning

- **Classification**: safe
- **Rationale**: Standard SecretRef pattern; V2 feature (NEO-3-004-SEC-02 V1=No) but non-breaking shape.

## FR / AC

- FR: NEO-2-004, NEO-3-004-SEC-02
- AC: AC-NEO-LDAP

## Open questions

- Collapse secret name + mount path into single `secretRef` with operator-chosen mount?
- V1 scope: defer LDAP to V2 per FR priority?
