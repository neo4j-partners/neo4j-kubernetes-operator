# `podSpec.serviceAccountName`

## Client need

Operators bind Neo4j pods to a Kubernetes ServiceAccount for cloud workload identity (S3 backup, GKE WI) and for cluster-internal API access when clustering is enabled.

## Neo4j documentation

- [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/backup-restore/) — Cloud storage access

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/_helpers.tpl#neo4j.serviceAccountName`
- **Go model**: `helm-charts/internal/model/release_values.go`
- **K8s resources**: ServiceAccount (auto-created), StatefulSet pod serviceAccountName
- **Neo4j mechanism**: Probes check Bolt TCP 7687 by default; logging via Log4j2 XML mounted into `/config/`; image resolved by `neo4j.image` helper from edition + AppVersion.

## Category

scheduling

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-SCHEDULING | workload identity | secretMounts, volumes (cloud storage) |; | CONCERN-TOPOLOGY | auto SA for cluster | neo4j.minimumClusterSize, analytics.enabled |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.security.serviceAccount`
- **Notes**: Helm creates SA when cluster or analytics enabled and name empty.

## Aggregation

- **Group**: AGG-SCHEDULING
- **Must decide with**: fields sharing `AGG-SCHEDULING` in aggregation-matrix.md

## Versioning

- **Classification**: safe
- **Rationale**: Maps to spec.security.serviceAccount in CRD.

## FR / AC

- FR: NEO-2-008; NEO-3-008-SCH-06; NEO-3-006-CLD-01
- AC: see linked FR acceptance criteria in `design/01-functional_requirements.csv`

## Open questions

- Operator always create dedicated SA vs user-supplied only?
