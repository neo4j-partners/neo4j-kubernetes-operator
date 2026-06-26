# BDR-005 — Storage volume mode model for `Neo4j.spec.persistence.data`

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-26 |
| **Reviewers** | Charles Boudry |
| **Depends on** | [BDR-001](001-single-neo4j-crd.md) — single `Neo4j` CRD (accepted) |
| **Helm scope** | `volumes.data.*` rows (`mode`, `dynamic`, `defaultStorageClass`, `selector`, `volume`, `volumeClaimTemplate`, `share`, `labels`, `disableSubPathExpr`); group **AGG-STORAGE-DATA** |
| **Constraints** | `NEO-2-006`, `NEO-3-006-PVC-01..05`, `NEO-3-006-VOL-01`; AC `AC-NEO-STORAGE*`; Neo4j [Kubernetes storage](https://neo4j.com/docs/operations-manual/current/kubernetes/) |

---

## Context

The Neo4j database store lives at `/data` and must survive pod restart and reschedule. In the Helm chart, **how** that volume is provisioned or bound is selected through `volumes.data.mode`, which dispatches in `_volumeTemplate.tpl` to one of **six** mutually exclusive shapes:

| Helm mode | Mechanism | K8s result |
|-----------|-----------|------------|
| `defaultStorageClass` | dynamic provisioning, no `storageClassName` (cluster default) | per-pod PVC via `volumeClaimTemplates` |
| `dynamic` | dynamic provisioning on a **named** StorageClass + size + accessModes | per-pod PVC via `volumeClaimTemplates` |
| `selector` | PVC with a label `selector` (+ class) binding to **pre-provisioned** PVs; `selectorTemplate` rendered via Helm `tpl` | PVC with `selector` |
| `volume` | inline reference to an **existing** volume (PVC or other) directly in StatefulSet `volumes`; optional chown/chmod init container | inline `volumes` entry |
| `volumeClaimTemplate` | raw `volumeClaimTemplate` YAML passthrough | `volumeClaimTemplates` (verbatim) |
| `share` | reuse another volume's claim for `/data` | shared `volumes` entry |

Adjacent knobs: `volumes.data.labels` (PVC metadata, **safe**), `volumes.data.disableSubPathExpr` (mount `subPathExpr` control, **breaking**).

### Forces

- **Migration sensitivity.** StorageClass, size class, selector binding, and mount sub-path are effectively **locked at PVC create**. Changing them after install means data migration, not an in-place edit — every `volumes.data.*` row except `labels` is classified **breaking** in `_index.csv`.
- **Helm parity vs API minimalism.** Six modes is a large, partly overlapping surface (`defaultStorageClass` is just `dynamic` without a class; `volumeClaimTemplate` is a raw escape hatch). A CRD that copies all six inherits Helm's "which mode do I pick?" ambiguity and its `tpl`-based selector indirection, which has no operator equivalent.
- **Current `neo4j/spec.md` baseline.** The spec already sketches a **flat** `spec.persistence.data` (`size`, `storageClassName`, `accessMode`, `existingClaim`) with `existingClaim` marked **V1=No**. This BDR decides whether to keep that flat shape, adopt an explicit `mode` enum, or curate a subset.
- **Field-doc draft drift.** `fields/volumes.data*.md` draft the target as `Neo4j.spec.storage.data.*`, while `neo4j/spec.md` uses `spec.persistence.data`. This BDR standardizes on **`spec.persistence.data`** (the spec.md path) and the field-doc drafts should follow.

This BDR does **not** cover auxiliary volumes (logs, metrics, import, backups, licenses) — that is **AGG-STORAGE-AUX** (BDR-006).

---

## Cross-cutting rules

| Rule | Rationale |
|------|-----------|
| Provisioning shape is fixed at PVC creation | StorageClass / selector / size-class changes require migration, not edit |
| Capacity may only grow | PVC expansion is supported where the StorageClass allows it; shrink is blocked by Kubernetes |
| Exactly one provisioning source per `data` volume | dynamic, existing-claim, and raw-template are mutually exclusive — admission must reject combinations |
| `share` is not meaningful for the **data** root | Sharing applies to aux volumes reusing `data`; the data volume is the source, not a consumer |

---

## Options under review

### Option A — Full Helm parity: explicit `mode` enum with all six shapes

Mirror the Helm dispatch as a discriminated `mode` enum on the CR.

```yaml
spec:
  persistence:
    data:
      mode: dynamic          # defaultStorageClass | dynamic | selector | volume | volumeClaimTemplate | share
      dynamic:
        storageClassName: gp3
        size: 100Gi
        accessModes: [ReadWriteOnce]
      # selector:  { storageClassName: gp3, selector: {...} }
      # volume:    { persistentVolumeClaim: { claimName: my-pvc } }
      # volumeClaimTemplate: { ...raw VCT... }
      disableSubPathExpr: false
      labels: {}
```

| Advantages | Disadvantages |
|------------|---------------|
| **Highest Helm parity** — every `volumes.data.mode` value has a 1:1 target | Inherits Helm's mode ambiguity (`defaultStorageClass` vs `dynamic` differ only by a class field) |
| Lowest migration friction for Helm users keeping their current mode | Six mutually exclusive sub-objects → heavy CEL `oneOf` validation surface |
| Raw `volumeClaimTemplate` escape hatch preserved | `selector` relied on Helm `tpl` rendering — no operator equivalent; semantics must be reimplemented |
| | `share` is meaningless for the data root — dead enum value |
| | Largest, most error-prone V1 OpenAPI/CEL block |

**Helm parity**: strongest. **API minimalism**: weakest.

### Option B — Flat fields, no `mode` (current `spec.md` baseline)

Keep the flat shape already drafted in `neo4j/spec.md`; behaviour is inferred from which field is set.

```yaml
spec:
  persistence:
    data:
      size: 100Gi
      storageClassName: gp3      # omit → cluster default (= Helm defaultStorageClass)
      accessMode: ReadWriteOnce  # V1: ReadWriteOnce only
      existingClaim: ""          # set → bind existing PVC (V1 = deferred)
```

| Advantages | Disadvantages |
|------------|---------------|
| **Smallest API surface** — four well-understood fields | No first-class `selector` or raw `volumeClaimTemplate` escape hatch |
| Matches the common K8s StatefulSet PVC mental model | Mode is **implicit** — `storageClassName` set/unset overloads dynamic vs default |
| Already reflected in `neo4j/spec.md`; least churn | `existingClaim` + `storageClassName` combination needs validation to stay mutually exclusive |
| Cleanest CEL — few cross-field rules | Advanced users (selector binding, custom VCT) have no V1 path → must wait for V1.1 |

**Helm parity**: partial (covers `defaultStorageClass`, `dynamic`, `volume`-as-existing-claim). **API minimalism**: strongest.

### Option C — Curated `mode` enum (`Dynamic` | `Existing`) + raw `volumeClaimTemplate` escape hatch — **proposed**

A small, intent-named enum covering the two mainstream paths, plus a verbatim escape hatch for everything Kubernetes can express. Helm's six modes are **mapped into** this shape via `11-helm-mapping.md`, not copied as field names.

```yaml
spec:
  persistence:
    data:
      mode: Dynamic                 # Dynamic | Existing | VolumeClaimTemplate
      # --- mode: Dynamic ---
      size: 100Gi                   # required for Dynamic
      storageClassName: gp3         # omit → cluster default (Helm defaultStorageClass)
      accessMode: ReadWriteOnce     # V1: ReadWriteOnce only
      # --- mode: Existing ---
      # existingClaim: my-neo4j-pvc
      # --- mode: VolumeClaimTemplate (advanced escape hatch) ---
      # volumeClaimTemplate: { ...raw K8s VCT spec... }
      disableSubPathExpr: false     # breaking; defaults preserve /data layout
      labels: {}                    # safe PVC metadata
```

**Standalone / dev quick-start** — default class:

```yaml
spec:
  persistence:
    data:
      mode: Dynamic
      size: 50Gi
```

| Advantages | Disadvantages |
|------------|---------------|
| **Explicit intent** — `Dynamic` vs `Existing` removes Option B's implicit overloading | New vocabulary vs Helm (migration table required) |
| `defaultStorageClass`/`dynamic` collapse into one `Dynamic` mode (class optional) | One more field than Option B (`mode`) |
| **Escape hatch** (`VolumeClaimTemplate`) covers `selector` and advanced PVCs without bespoke `selector` reimplementation | `selector` has no dedicated field — power users route through raw VCT |
| Bounded CEL: 3-way `oneOf`, not 6-way | `mode` enum can grow later (additive, non-breaking) — but must be designed for it |
| Drops meaningless `share` for the data root | |
| Keeps `disableSubPathExpr` + `labels` from Helm | |

**Helm parity**: high via mapping table; **API minimalism**: high.

**Helm → operator mapping** (for `11-helm-mapping.md`):

| Helm `volumes.data.mode` | Operator `spec.persistence.data` |
|--------------------------|----------------------------------|
| `defaultStorageClass` | `mode: Dynamic`, `storageClassName` omitted |
| `dynamic` | `mode: Dynamic`, `storageClassName`, `size`, `accessMode` |
| `volume` | `mode: Existing`, `existingClaim` |
| `selector` | `mode: VolumeClaimTemplate` (raw VCT with `selector`) |
| `volumeClaimTemplate` | `mode: VolumeClaimTemplate` (verbatim passthrough) |
| `share` | not supported for data root (aux volumes only — BDR-006) |

---

## Comparison

| Criterion | A — full Helm enum | B — flat fields | C — curated enum + escape hatch |
|-----------|--------------------|-----------------|-------------------------------|
| Helm parity | ✅ all six modes | ⚠️ 3 of 6 implicit | ✅ all six via mapping |
| API minimalism | ❌ six sub-objects | ✅ four fields | ⚠️ enum + escape hatch |
| Intent clarity | ⚠️ Helm ambiguity inherited | ⚠️ implicit mode | ✅ explicit `mode` |
| Validation (CEL) cost | ❌ 6-way `oneOf` + `tpl` gap | ✅ few rules | ⚠️ 3-way `oneOf` |
| Advanced / selector support | ✅ first-class | ❌ none in V1 | ✅ via raw VCT |
| Migration safety (breaking fields fixed at create) | ✅ | ✅ | ✅ |
| Future extensibility | ⚠️ enum already full | ⚠️ add fields later | ✅ additive enum values |
| Churn vs current `spec.md` | high | none | low |

---

## Decision

**Not decided.** Pending review.

**Proposer direction:** Adopt **Option C** — a curated `mode` enum (`Dynamic` | `Existing` | `VolumeClaimTemplate`) on `spec.persistence.data`, with Helm's six modes mapped in through `11-helm-mapping.md`. `Dynamic` is the V1 default path; `Existing` and the raw `VolumeClaimTemplate` escape hatch cover pre-provisioned and advanced cases without reimplementing Helm's `tpl`-based `selector`. Drop `share` for the data root (aux-only, BDR-006). Keep `labels` (safe) and `disableSubPathExpr` (breaking, default preserves `/data`).

**Recommendation:** Ship V1 with **`mode: Dynamic` + `Existing`** fully supported and validated; gate **`VolumeClaimTemplate`** behind a clearly-labelled "advanced" path (documented, accepted, but minimal validation). Treat `mode`, `storageClassName`, `accessMode`, `existingClaim`, `volumeClaimTemplate`, and `disableSubPathExpr` as **immutable after create** (CEL `x-kubernetes-validations`), allowing only PVC **expansion** of `size`. This keeps the breaking surface honest while leaving the enum open to additive growth (e.g. a first-class `Selector` mode) in V1.1+ without an API break.

---

## Consequences

### Positive

- One intent-named field (`mode`) replaces Helm's overloaded six-way dispatch; the two mainstream paths are unambiguous.
- Raw `VolumeClaimTemplate` preserves full Kubernetes expressiveness (including `selector`) without bespoke operator logic.
- Migration-sensitive fields are pinned immutable in CEL, surfacing accidental edits at admission instead of as silent data loss.
- Enum is additively extensible — future modes do not break the API.

### Negative

- Diverges from Helm field names; a migration table in `11-helm-mapping.md` is required.
- Power users needing `selector` must hand-author a raw VCT in V1 until a first-class mode lands.
- `neo4j/spec.md` `spec.persistence.data` must be updated from the flat baseline (Option B) to the `mode`-based shape, and `fields/volumes.data*.md` drafts realigned from `spec.storage.data` to `spec.persistence.data`.

### Neutral

- `volumes.data.labels` remains a safe, additive passthrough.
- Auxiliary volume modes (`share` and friends) are decided separately in BDR-006 (AGG-STORAGE-AUX).
- `03-variant_matrix.csv` — add storage variants: dynamic-default, dynamic-named-class, existing-claim, raw-VCT.

---

## References

- `design/analysis/helm-fields/_index.csv` — rows `volumes.data.mode`, `volumes.data.dynamic`, `volumes.data.defaultStorageClass`, `volumes.data.selector`, `volumes.data.volume`, `volumes.data.volumeClaimTemplate`, `volumes.data.labels`, `volumes.data.disableSubPathExpr`
- `design/analysis/helm-fields/aggregation-matrix.md` — **AGG-STORAGE-DATA**
- `design/09-crd-spec/neo4j/spec.md` — `spec.persistence` section
- [Neo4j — Kubernetes deployment / storage](https://neo4j.com/docs/operations-manual/current/kubernetes/)
- [Neo4j — File locations](https://neo4j.com/docs/operations-manual/current/configuration/file-locations/)
- [BDR-001](001-single-neo4j-crd.md) — single `Neo4j` CRD
- [BDR-002](002-neo4j-crd-topology.md) — topology model (exemplar format)
