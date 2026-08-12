# `additionalVolumes`

## Client need

Inject arbitrary extra pod volumes (escape hatch) for integrations not covered by first-class volume roles (`data`, `logs`, `backups`, …).

## Neo4j documentation

- [Kubernetes deployment](https://neo4j.com/docs/operations-manual/current/kubernetes/) — Kubernetes deployment

## Helm implementation

- **Templates**: `_volumeTemplate.tpl` (`neo4j.additionalVolumes`); `neo4j-statefulset.yaml` — appended to StatefulSet `spec.template.spec.volumes`
- **Go model**: `release_values.go` — `AdditionalVolumes []Volume`
- **K8s resources**: StatefulSet `volumes[]` (raw YAML passthrough per list item)
- **Neo4j mechanism**: No `neo4j.conf` effect unless paired with `additionalVolumeMounts` and/or user `config`.

**Coupling**: Helm splits volume definition (`additionalVolumes`) and container mount (`additionalVolumeMounts`) into **two independent lists** linked only by `name`. No chart validation that every mount references a defined volume.

## Category

storage

## Semantic concerns

| concern_id | role in this concern | co-paths (scattered) |
|------------|----------------------|----------------------|
| CONCERN-STORAGE-ESCAPE | raw pod volumes | `additionalVolumeMounts`, `secretMounts` |

## CRD mapping (draft)

- **Target**: `Neo4j.spec.additionalMounts[].volume` (Option E — paired) **or** `Neo4j.spec.volumes.additionalVolumes` (Option F — Helm split)
- **Notes**: See [BDR-005](../../../decision-records/business/005-storage-volume-mode.md) § Escape hatches.

## Aggregation

- **Group**: AGG-STORAGE-ESCAPE
- **Must decide with**: `additionalVolumeMounts`, `secretMounts`

## Versioning

- **Classification**: safe
- **Rationale**: Additive passthrough; does not change data PVC binding.

## FR / AC

- FR: NEO-2-006
- AC: AC-NEO-STORAGE

## Open questions

- V1 allowlist on volume types (`emptyDir`, `configMap`, `secret`, `csi`, `persistentVolumeClaim`) vs full passthrough?
