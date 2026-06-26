# `neo4j.password`

## Client need

Application teams need an initial `neo4j` user password on first install. When unset, the chart generates a random password and stores it in a Kubernetes Secret; users may also supply a explicit password (with a security warning about Helm release storage).

## Neo4j documentation

- [Authentication](https://neo4j.com/docs/operations-manual/current/configuration/authentication/) — initial credentials
- [Neo4j on Kubernetes](https://neo4j.com/docs/operations-manual/current/kubernetes/) — auth Secret pattern

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl` (`neo4j.password` — generate or use value); `neo4j-auth.yaml` (Secret `{{ neo4j.name }}-auth` with `NEO4J_AUTH`); `NOTES.txt` (`neo4j.passwordWarning`)
- **Go model**: `release_values.go` — `Neo4J.Password`
- **K8s resources**: Secret `*-auth`
- **Neo4j mechanism**: `NEO4J_AUTH=neo4j/<password>` env via Secret mount

## Category

security

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| — | Initial auth | `neo4j.passwordFromSecret`, `logInitialPassword` |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.auth.generatePassword` (default true) or inline only at create — prefer Secret ref
- **Notes**: Operator should not store plaintext password in CR status

## Aggregation

- **Group**: AGG-AUTH
- **Must decide with**: `neo4j.passwordFromSecret`, `logInitialPassword`

## Versioning

- **Classification**: safe
- **Rationale**: Auth bootstrap — well-established pattern

## FR / AC

- FR: NEO-2-004, NEO-3-004-CRED-01
- AC: AC-NEO-SECRETS

## Open questions

- Rotate initial password via operator vs one-time bootstrap only?
