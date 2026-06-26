# `neo4j.edition`

## Client need

Deployers must declare Community vs Enterprise edition because clustering, analytics layouts, LDAP, and backup features are Enterprise-only. The chart fails fast if clustering is requested on Community.

## Neo4j documentation

- [Neo4j editions](https://neo4j.com/docs/operations-manual/current/introduction/#_neo4j_editions) — feature matrix
- [Clustering](https://neo4j.com/docs/operations-manual/current/clustering/) — Enterprise requirement

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl` (`neo4j.isClusterEnabled` fails if not enterprise); `_licensing.tpl`, `_image.tpl`, `neo4j-config.yaml`, `neo4j-statefulset.yaml`, `neo4j-svc.yaml`, `_ldap.tpl` — `$isEnterprise` checks
- **Go model**: `release_values.go` — `Neo4J.Edition`
- **K8s resources**: ConfigMap conf file selection (`neo4j-community.conf` vs `neo4j-enterprise.conf`); env `NEO4J_EDITION`
- **Neo4j mechanism**: Selects enterprise config defaults; gates cluster and LDAP features

## Category

topology

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-TOPOLOGY | Enterprise required for `minimumClusterSize >= 3` | `neo4j.minimumClusterSize`, `analytics.*`, `neo4j.acceptLicenseAgreement` |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.edition` (`enterprise` only in V1 per `NEO-2-001-EDT-01`)
- **Notes**: `community` deferred in operator V1

## Aggregation

- **Group**: AGG-TOPO-ROLES
- **Must decide with**: `neo4j.acceptLicenseAgreement`, `neo4j.minimumClusterSize`

## Versioning

- **Classification**: breaking
- **Rationale**: Edition change is a replace operation

## FR / AC

- FR: NEO-2-001-EDT-01, NEO-1-001, NEO-1-002
- AC: AC-NEO-LICENSE, AC-NEO-CLUSTER

## Open questions

- Community edition timeline for operator V2?
