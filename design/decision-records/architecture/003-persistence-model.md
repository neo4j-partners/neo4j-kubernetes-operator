# ADR-003 — Persistence model for `Neo4j` V1

| | |
|---|---|
| **Status** | accepted |
| **Date** | 2026-06-24 |
| **Reviewers** | Charles Boudry, Marouane Gazanayi |
| **Depends on** | [BDR-005](../business/005-v1-full-crd-scope.md) |
| **Constraints** | Helm `volumes.*`; `NEO-3-006-*` |

---

## Context

The Helm chart supports six volume roles (`data`, `logs`, `metrics`, `import`, `backups`, `licenses`) each with modes (`dynamic`, `selector`, `volume`, `volumeClaimTemplate`, `defaultStorageClass`, `share`). Earlier CRD drafts deferred auxiliary volumes to V2 and only exposed `persistence.data`.

[BDR-005](../business/005-v1-full-crd-scope.md) requires full Helm parity in V1 without replicating Helm’s mode enum complexity on the CR.

---

## Decision

We will model **`spec.persistence.<role>`** for six fixed roles. Each role uses a **simplified volume spec** — not Helm’s `mode` enum.

### Roles

| Role | Purpose |
|------|---------|
| `data` | Neo4j store — **required** |
| `logs` | Transaction / debug logs |
| `metrics` | Metrics files |
| `import` | Bulk import staging |
| `backups` | Backup target mount |
| `licenses` | GDS / Bloom license files |

### Per-role shape

```yaml
persistence:
  data:
    size: 100Gi
    storageClassName: gp3
    accessMode: ReadWriteOnce
    existingClaim: ""       # bind pre-provisioned PVC
  logs:
    shareWith: data         # default for auxiliary roles
  metrics:
    shareWith: data
  import:
    size: 50Gi
    storageClassName: gp3
  backups:
    shareWith: data
  licenses:
    size: 1Gi
    storageClassName: fast
```

| Field | Applies to | Description |
|-------|------------|-------------|
| `size` | dedicated volume | PVC size (required when not sharing) |
| `storageClassName` | dedicated volume | StorageClass |
| `accessMode` | dedicated volume | Default `ReadWriteOnce` |
| `existingClaim` | any | Bind existing PVC — immutable after bind |
| `shareWith: data` | auxiliary | Mount subpath of data volume (Helm `share` mode) |
| `selector` | dedicated volume | Label selector for pre-provisioned PVs (Helm `selector`) |

**Default:** auxiliary roles (`logs`, `metrics`, `import`, `backups`, `licenses`) default to `shareWith: data` when omitted — matches Helm default behaviour.

### Rules

| Rule | Mechanism |
|------|-----------|
| `persistence.data.size` required | CEL |
| PVC expansion allowed; shrink blocked | Webhook (compare live PVC) |
| `existingClaim` immutable after first bind | CEL + webhook |
| `accessMode` other than RWO for `data` | Error in V1 unless `existingClaim` binds RWX PV |
| Subpath layout on shared volume | operator-owned — not user-configurable |

### Helm mapping

| Helm `volumes.<role>.mode` | CRD |
|----------------------------|-----|
| `share` | `shareWith: data` |
| `defaultStorageClass` / `dynamic` | `size` + `storageClassName` |
| `selector` | `selector` + `size` |
| `volume` / `volumeClaimTemplate` | `existingClaim` or operator-generated PVC template |
| `disableSubPathExpr: true` | `subPathDisabled: true` on role (advanced) |

---

## Consequences

### Positive

- Full Helm volume surface without six × six mode matrix on the CR.
- `shareWith: data` keeps dev manifests minimal; production can split volumes explicitly.

### Negative

- `selector` and pre-provisioned PV flows need webhook validation — same as Helm lookups.

### Neutral

- Implementation lives in `internal/domain/persistence`; shared Go struct `VolumeRoleSpec` in `common_types.go`.

---

## References

- [BDR-005](../business/005-v1-full-crd-scope.md)
- [ADR-002](002-helm-values-mapping.md)
- [`09-crd-spec/neo4j/spec.md`](../../09-crd-spec/neo4j/spec.md)
