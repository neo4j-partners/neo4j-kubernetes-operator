# `logInitialPassword`

## Client need

Developers doing first-time installs need to see the generated Neo4j password in `helm install` notes to connect immediately. Production users typically leave this `false` to avoid logging secrets to CI output.

## Neo4j documentation

- [Neo4j on Kubernetes](https://neo4j.com/docs/operations-manual/current/kubernetes/) — post-install notes

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/NOTES.txt` L6, L42–43 (`neo4j.logPassword` / password template when true)
- **Go model**: `release_values.go` — `HelmValues.LogInitialPassword`
- **K8s resources**: None — install notes only
- **Neo4j mechanism**: None

## Category

security

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| — | Install UX for generated password | `neo4j.password`, `neo4j.passwordFromSecret` |

## CRD mapping (draft)

- **Target**: N/A — operator should expose password retrieval via `kubectl`/status Secret reference, not install notes
- **Notes**: Security anti-pattern for GitOps pipelines

## Aggregation

- **Group**: AGG-AUTH
- **Must decide with**: `neo4j.password`

## Versioning

- **Classification**: deferred
- **Rationale**: Helm install-time only; not applicable to CRD lifecycle

## FR / AC

- FR: NEO-3-004-CRED-01
- AC: AC-NEO-SECRETS

## Open questions

- Operator status field pointing to generated Secret name instead of logging password?
