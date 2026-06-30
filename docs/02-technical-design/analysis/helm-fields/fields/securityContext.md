# `securityContext`

## Client need

Operators run Neo4j pods as non-root with consistent UID/GID (7474) and fsGroup so data volumes are accessible without running the database as root. Pod-level security context also drives init-container volume permission fixes (`chown`/`chmod`) when volumes request group-writable permissions.

## Neo4j documentation

- [Neo4j Docker — file permissions](https://neo4j.com/docs/operations-manual/current/docker/mounting-volumes/) — recommended UID 7474
- [Kubernetes Pod security standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/) — restricted profile alignment

## Helm implementation

- **Templates**: `neo4j-statefulset.yaml` L63 — pod `securityContext`; `neo4j-operations.yaml` — operations Job pod context; `_volumeTemplate.tpl` `neo4j.initChmodScript` uses `runAsUser`/`runAsGroup` for volume ownership init
- **Go model**: `HelmValues.SecurityContext` in `release_values.go` (`runAsNonRoot`, `runAsUser`, `runAsGroup`, `fsGroup`, `fsGroupChangePolicy`)
- **K8s resources**: StatefulSet `spec.template.spec.securityContext`; init container overrides to root for chmod only
- **Neo4j mechanism**: Process runs as neo4j user inside container matching volume ownership

## Category

security

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE | fsGroup and init chmod interact with `volumes.*.setOwnerAndGroupWritableFilePermissions` | `volumes.data`, `volumes.logs`, etc. |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.security.podSecurityContext`
- **Notes**: Operator proposal defaults `runAsUser: 7474`, `fsGroup: 7474`; may be enforced with limited override.

## Aggregation

- **Group**: none
- **Must decide with**: `containerSecurityContext`, volume permission model

## Versioning

- **Classification**: safe
- **Rationale**: Standard Kubernetes API surface; secure defaults with optional override.

## FR / AC

- FR: NEO-2-004
- AC: AC-NEO-SECRETS (indirect — pod hardening supports secure deployment)

## Open questions

- Lock to 7474 or allow override with validation warning?
- Enforce restricted PSS profile without user input?
