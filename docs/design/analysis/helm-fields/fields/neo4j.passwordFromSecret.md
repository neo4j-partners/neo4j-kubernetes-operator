# `neo4j.passwordFromSecret`

## Client need

Security-conscious teams want to supply the initial Neo4j password via an existing Kubernetes Secret (GitOps-friendly, no plaintext in Helm values) rather than chart-generated or inline passwords. The Secret must contain `NEO4J_AUTH` in `neo4j/<password>` format.

## Neo4j documentation

- [Authentication](https://neo4j.com/docs/operations-manual/current/configuration/authentication/) — credential setup
- [Neo4j on Kubernetes](https://neo4j.com/docs/operations-manual/current/kubernetes/) — `passwordFromSecret`

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl` (`neo4j.password`, `neo4j.secretName` — lookup/validate Secret); `neo4j-auth.yaml` (skips creating auth Secret when set); `neo4j-operations.yaml` L49–54 (`SECRETNAME` env)
- **Go model**: `release_values.go` — `Neo4J.PasswordFromSecret`
- **K8s resources**: References existing Secret; operations Job reads same Secret
- **Neo4j mechanism**: Pod mounts referenced Secret for `NEO4J_AUTH`

## Category

security

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| — | Auth bootstrap | `neo4j.password`, `disableLookups` (Secret validation) |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.auth.passwordSecretRef.name` (`NEO-3-004-CRED-02`)
- **Notes**: Key fixed as `NEO4J_AUTH` in V1

## Aggregation

- **Group**: AGG-AUTH
- **Must decide with**: `neo4j.password`

## Versioning

- **Classification**: safe
- **Rationale**: Standard Secret reference pattern

## FR / AC

- FR: NEO-2-004, NEO-3-004-CRED-02
- AC: AC-NEO-SECRETS

## Open questions

- Support secret key override in V2?
