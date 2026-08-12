# `ldapPasswordMountPath`

## Client need

When LDAP system account password comes from a Kubernetes Secret, operators must specify where that Secret is mounted inside the Neo4j container so the generated `neo4j.conf` entry can read `LDAP_PASS` from disk. This path pairs mandatorily with `ldapPasswordFromSecret`.

## Neo4j documentation

- [LDAP integration](https://neo4j.com/docs/operations-manual/current/authentication-authorization/ldap-integration/) — system account credentials

## Helm implementation

- **Templates**: `_ldap.tpl` — `neo4j.ldapPasswordMountPath` enforces Enterprise edition and mutual requirement with `ldapPasswordFromSecret`; `neo4j.ldapVolumeMount` mounts Secret at this path; `neo4j-config.yaml` references path in `dbms.security.ldap.authorization.system_password`
- **Go model**: `HelmValues.LdapPasswordMountPath string` in `release_values.go`
- **K8s resources**: VolumeMount on Neo4j container; Secret volume from `ldapPasswordFromSecret`
- **Neo4j mechanism**: Shell expansion reads `{{ ldapPasswordMountPath }}/LDAP_PASS` at config load

## Category

security

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| — | Tightly coupled pair with `ldapPasswordFromSecret` | `neo4j.edition` |

## CRD mapping (draft)

- **Target**: Operator-managed mount path (fixed) + `Neo4j.spec.auth.ldap.systemPasswordSecretRef` — user may not need explicit path in CRD
- **Notes**: Helm exposes path because user chooses mount location; operator can internalize.

## Aggregation

- **Group**: AGG-AUTH
- **Must decide with**: `ldapPasswordFromSecret`

## Versioning

- **Classification**: safe
- **Rationale**: Likely hidden in operator implementation; no user-facing breaking field if SecretRef-only API.

## FR / AC

- FR: NEO-2-004, NEO-3-004-SEC-02
- AC: AC-NEO-LDAP

## Open questions

- Expose mount path in CRD for custom layouts or always use `/config/ldap/`?
