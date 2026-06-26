# `secretMounts`

## Client need

Mount arbitrary Kubernetes Secrets into the pod (e.g. S3 credentials for seedURI restore from private buckets).

## Neo4j documentation

- [Restore](https://neo4j.com/docs/operations-manual/current/backup-restore/restore-backup/) — Restore
- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: _secretMounts.tpl; neo4j-statefulset.yaml
- **Go model**: release_values.go: SecretMounts (map)
- **K8s resources**: StatefulSet volumes + volumeMounts (Secret)
- **Neo4j mechanism**: Files available at mountPath for neo4j-admin seed/restore or custom config.

## Category

security

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | pod secret file mounts | neo4j.passwordFromSecret, apoc_credentials |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.security.secretMounts`
- **Notes**: Draft mapping from Helm analysis.

## Aggregation

- **Group**: none
- **Must decide with**: standalone field

## Versioning

- **Classification**: safe
- **Rationale**: Additive secret mounts.

## FR / AC

- FR: NEO-2-004; NEO-2-014; NEO-3-004-SEC-01
- AC: AC-NEO-SECRETS; AC-NEO-RESTORE

## Open questions

- Distinguish secretMounts from TLS cert secrets (ssl.*) in CRD.
