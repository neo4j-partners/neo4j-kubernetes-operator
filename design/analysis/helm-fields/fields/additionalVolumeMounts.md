# `additionalVolumeMounts`

## Client need

Mount `additionalVolumes` into the Neo4j container at custom paths (config snippets, sidecar-less file injection, integration mounts).

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: `_volumeTemplate.tpl` (`neo4j.additionalVolumeMounts`); `neo4j-statefulset.yaml` — appended to Neo4j container `volumeMounts`
- **Go model**: sibling list on release values (not a separate top-level struct in all versions)
- **K8s resources**: StatefulSet `volumeMounts[]` passthrough
- **Neo4j mechanism**: Files visible under `mountPath`; Neo4j only uses them if referenced via `config` or admin tooling.

**Coupling**: Must reference `name` matching an entry in `additionalVolumes` **or** a chart-managed volume (`data`, `logs`, …). Chart does not validate referential integrity.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE-ESCAPE | raw container mounts | `additionalVolumes` |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.additionalMounts[].mountPath` (Option E) **or** `Neo4j.spec.volumes.additionalVolumeMounts` (Option F)
- **Notes**: Paired model preferred — avoids orphan mounts.

## Aggregation

- **Group**: AGG-STORAGE-ESCAPE
- **Must decide with**: `additionalVolumes`

## Versioning

- **Classification**: safe
- **Rationale**: Additive mount paths.

## FR / AC

- FR: NEO-2-006
- AC: AC-NEO-STORAGE

## Open questions

- Reject mounts under operator-reserved paths (`/data`, `/var/lib/neo4j/certificates/*`)?
