# `neo4j.acceptLicenseAgreement`

## Client need

Enterprise deployers must explicitly acknowledge Neo4j license terms before the chart installs Enterprise edition. Without `yes` or `eval`, template rendering fails with licensing guidance.

## Neo4j documentation

- [Neo4j licensing](https://neo4j.com/licensing/) — Enterprise agreement
- [Neo4j on Kubernetes](https://neo4j.com/docs/operations-manual/current/kubernetes/) — `acceptLicenseAgreement`

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_licensing.tpl` (`neo4j.checkLicenseAgreement`); `neo4j-env.yaml` L13–14 (`NEO4J_ACCEPT_LICENSE_AGREEMENT` env); `neo4j-config.yaml` L9 (included at top)
- **Go model**: `release_values.go` — `Neo4J.AcceptLicenseAgreement`
- **K8s resources**: ConfigMap env source via `neo4j-env` Secret/ConfigMap pattern
- **Neo4j mechanism**: `NEO4J_ACCEPT_LICENSE_AGREEMENT` container env (`yes` | `eval`)

## Category

security

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TOPOLOGY | Required for Enterprise cluster paths | `neo4j.edition`, `neo4j.minimumClusterSize` |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.license.accept` (`yes` in V1; `eval` deferred per `NEO-2-001-LIC-02`)
- **Notes**: Required field on Enterprise installs

## Aggregation

- **Group**: AGG-TOPO-ROLES
- **Must decide with**: `neo4j.edition`

## Versioning

- **Classification**: breaking
- **Rationale**: Legal/install gate — required at create

## FR / AC

- FR: NEO-2-001-LIC-01, NEO-2-001-LIC-02
- AC: AC-NEO-LICENSE

## Open questions

- Eval mode support timeline in operator?
