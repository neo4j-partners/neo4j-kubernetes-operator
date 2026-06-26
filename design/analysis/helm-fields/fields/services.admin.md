# `services.admin`

## Client need

ClusterIP admin service reachable even when pods are not Ready — for backup and operational access.

## Neo4j documentation

- [Backup](https://neo4j.com/docs/operations-manual/current/backup/) — Backup
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: neo4j-svc.yaml (admin service); neo4j-statefulset.yaml (SERVICE_NEO4J_ADMIN env)
- **Go model**: release_values.go: Services.Admin
- **K8s resources**: Service `{release}-admin` ClusterIP, publishNotReadyAddresses: true
- **Neo4j mechanism**: Auto ports: backup, bolt, http/s, metrics based on config flags.

## Category

network

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-EXPOSURE | admin/ops access | services.default, services.internals |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.connectivity.internal.admin`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: AGG-EXPOSURE
- **Must decide with**: AGG-EXPOSURE

## Versioning

- **Classification**: safe
- **Rationale**: Internal ops surface.

## FR / AC

- FR: NEO-2-007; NEO-2-013
- AC: AC-NEO-NETWORKING; AC-NEO-BACKUP

## Open questions

- None identified.
