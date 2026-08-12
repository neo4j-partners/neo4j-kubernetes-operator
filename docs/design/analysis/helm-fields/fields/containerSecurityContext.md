# `containerSecurityContext`

## Client need

Operators constrain the Neo4j container process itself — non-root execution, dropped Linux capabilities — independent of pod-level fsGroup settings. This is the Kubernetes defense-in-depth layer ensuring the Java process cannot escalate privileges even if the pod spec allows broader permissions.

## Neo4j documentation

- [Neo4j Docker security](https://neo4j.com/docs/operations-manual/current/docker/introduction/docker-rationale/) — non-root execution
- [Kubernetes container security context](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/) — `capabilities`, `runAsNonRoot`

## Helm implementation

- **Templates**: `neo4j-statefulset.yaml` L143 — Neo4j container `securityContext`; `neo4j-operations.yaml` L34–35 — operations sidecar/container; defaults in `values.yaml` (`runAsNonRoot: true`, `runAsUser/Group: 7474`, `capabilities.drop: [ALL]`)
- **Go model**: `HelmValues.ContainerSecurityContext` (`SecurityContext` struct) in `release_values.go`
- **K8s resources**: StatefulSet container `securityContext`
- **Neo4j mechanism**: Container runtime enforces UID and capability set before JVM starts

## Category

security

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| — | Pairs with pod `securityContext` | `securityContext` |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.security.containerSecurityContext`
- **Notes**: Matches operator proposal secure-by-default table.

## Aggregation

- **Group**: none
- **Must decide with**: `securityContext`

## Versioning

- **Classification**: safe
- **Rationale**: Standard K8s field mapping; defaults align with Helm chart.

## FR / AC

- FR: NEO-2-004
- AC: AC-NEO-SECRETS (secure deployment baseline)

## Open questions

- Allow `readOnlyRootFilesystem` in V1 or defer?
- Webhook defaults vs required user acknowledgment for overrides?
