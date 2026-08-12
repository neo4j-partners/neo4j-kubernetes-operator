# `neo4j.offlineMaintenanceModeEnabled`

## Client need

DBAs performing offline maintenance (`neo4j-admin dump`, filesystem operations) need to stop the Neo4j process while keeping the pod running. Enabling this mode replaces the database container command with a sleep loop and disables liveness/startup probes.

## Neo4j documentation

- [Backup and restore](https://neo4j.com/docs/operations-manual/current/backup-restore/) — offline admin tools
- [Neo4j Admin](https://neo4j.com/docs/operations-manual/current/tools/neo4j-admin/) — dump/load operations

## Helm implementation

- **Templates**: `helm-charts/neo4j/templates/neo4j-statefulset.yaml` L4, L88–95 (sleep loop command), L168 (probes disabled); `NOTES.txt` L11–13
- **Go model**: `release_values.go` — `Neo4J.OfflineMaintenanceModeEnabled`
- **K8s resources**: StatefulSet pod spec command override
- **Neo4j mechanism**: Neo4j process not started — data volume available for admin tools via `kubectl exec`

## Category

lifecycle

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| — | Maintenance lifecycle | `volumes.data` (data mount still attached) |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.maintenance.offlineMode` (`NEO-3-017-MNT-01` partial)
- **Notes**: Full maintenance Jobs (`NEO-2-017`) deferred V2

## Aggregation

- **Group**: none
- **Must decide with**: —

## Versioning

- **Classification**: safe
- **Rationale**: Operational toggle — reversible via spec update

## FR / AC

- FR: NEO-2-017
- AC: AC-NEO-MAINTENANCE-OFFLINE

## Open questions

- Operator status condition while `offlineMode: true`?
