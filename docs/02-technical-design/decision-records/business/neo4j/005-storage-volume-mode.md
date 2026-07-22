# BDR-005 — Storage volume mode model for `Neo4j.spec.volumes`

| | |
|---|---|
| **Status** | accepted |
| **Date** | 2026-06-26 |
| **Reviewers** | Charles Boudry |
| **Depends on** | [BDR-001](001-single-neo4j-crd.md) — single `Neo4j` CRD (accepted) |
| **Helm scope** | `volumes.data.*`, `volumes.{backups,logs,metrics,import,licenses}`, `additionalVolumes`, `additionalVolumeMounts`, `secretMounts`; groups **AGG-STORAGE-DATA**, **AGG-STORAGE-AUX**, **AGG-STORAGE-ESCAPE** |
| **Operator extension** | `volumes.plugins` (Share/Dynamic/Existing → `/plugins`) so `NEO4J_PLUGINS` downloads persist on disk — not in Helm |
| **Constraints** | `NEO-2-006`, `NEO-3-006-PVC-01..05`, `NEO-3-006-VOL-01..06`; AC `AC-NEO-STORAGE*`; Neo4j [Kubernetes storage](https://neo4j.com/docs/operations-manual/current/kubernetes/) |

---

## Context

The Neo4j database store lives at `/data` and must survive pod restart and reschedule. Auxiliary paths (`/logs`, `/backups`, `/metrics`, `/import`, `/licenses`) may use dedicated PVCs or **share** the data volume — Helm's default for most aux roles.

In the Helm chart, provisioning is selected through `volumes.<role>.mode` (for `data`, six modes; for aux volumes, typically `share` or `dynamic`):

| Helm `volumes.data.mode` | Mechanism | K8s result |
|--------------------------|-----------|------------|
| `defaultStorageClass` | dynamic provisioning, no `storageClassName` | per-pod PVC via `volumeClaimTemplates` |
| `dynamic` | dynamic provisioning on a named StorageClass + size | per-pod PVC via `volumeClaimTemplates` |
| `selector` | PVC with label `selector` binding to pre-provisioned PVs | PVC with `selector` in VCT |
| `volume` | inline reference to an existing volume in StatefulSet `volumes` | inline `volumes` entry |
| `volumeClaimTemplate` | raw `volumeClaimTemplate` YAML passthrough | `volumeClaimTemplates` (verbatim) |
| `share` | reuse another volume's claim for `/data` | shared `volumes` entry |

Adjacent knobs on **data**: `volumes.data.labels` (**safe**), `volumes.data.disableSubPathExpr` (**breaking**).

### CRD layout mirrors Helm `values.yaml`

| Helm `values.yaml` (top-level) | `Neo4j.spec` |
|-------------------------------|--------------|
| `volumes.data` / `volumes.logs` / … | `volumes.data` / `volumes.logs` / … |
| `additionalVolumes` + `additionalVolumeMounts` | `additionalMounts[]` (paired — Option E) |
| `secretMounts` | `secretMounts` |

`persistence` was used in early operator drafts (`20-operator-proposal.md`) as a generic infra label. **Rejected** for the public API — operators migrating from Helm expect **`volumes:`**.

---

### Forces

- **Migration sensitivity.** StorageClass, selector binding, and mount sub-path are locked at PVC create — classified **breaking** in `_index.csv`.
- **Helm parity vs API minimalism.** Six Helm data modes collapse to two user intents: **provision new PVCs** vs **bring your own binding** (existing claim, inline volume, or raw VCT including `selector`).
- **`volumeClaimTemplate` is not a third top-level mode.** It is an advanced form of **Existing** — the user supplies the Kubernetes VCT spec (standard API, including `selector`).
- **`share` must be first-class** for aux volumes (Helm default). Data is the **source** volume; aux volumes default to `Share` from `data`.
- **Field-doc / spec alignment.** CRD section is **`spec.volumes`** — same key as Helm `values.yaml` `volumes:`. The name `persistence` in early drafts (`20-operator-proposal.md`) is **rejected** for the public API; it does not match what Helm users configure.

---

## Cross-cutting rules

| Rule | Rationale |
|------|-----------|
| Provisioning shape is fixed at PVC creation | StorageClass / selector / size-class changes require migration |
| Capacity may only grow | PVC expansion where StorageClass allows; shrink blocked |
| `data.mode` is `Dynamic` or `Existing` only | `Share` on `data` is forbidden — data is the anchor volume |
| `Existing` accepts exactly one binding shape | `claimName`, `volume`, or `volumeClaimTemplate` — CEL `oneOf` |
| Aux `mode: Share` mounts the same volume source as `shareFrom` | V1: `shareFrom` may only be `data` |
| `mode`, binding shapes, and `disableSubPathExpr` immutable after create | CEL `x-kubernetes-validations`; `size` may expand only |

---

## Options under review

### Option A — Full Helm parity: explicit `mode` enum with all six shapes (data only)

Mirror the Helm dispatch as a discriminated `mode` enum on `spec.volumes.data`.

```yaml
spec:
  volumes:
    data:
      mode: dynamic          # defaultStorageClass | dynamic | selector | volume | volumeClaimTemplate | share
      dynamic:
        storageClassName: gp3
        size: 100Gi
        accessModes: [ReadWriteOnce]
      disableSubPathExpr: false
      labels: {}
```

| Advantages | Disadvantages |
|------------|---------------|
| Highest Helm parity — 1:1 field names | Six mutually exclusive sub-objects → heavy CEL |
| Lowest migration friction for Helm users keeping mode names | `selector` needs Helm `tpl` reimplementation or duplicate escape hatch |
| | Aux `share` still needs a parallel model per volume role |
| | Largest, most error-prone V1 OpenAPI block |

---

### Option D — `Dynamic` + `Existing` (+ `Share` for aux) — **accepted**

Two top-level modes for **data**; aux volumes add **`Share`**. All non-dynamic Helm paths map under **`Existing`**, including raw `volumeClaimTemplate` (with `selector`).

#### `spec.volumes.data`

```yaml
spec:
  volumes:
    data:
      mode: Dynamic                 # Dynamic | Existing
      dynamic:
        size: 100Gi                   # required when mode=Dynamic
        storageClassName: gp3         # omit → cluster default (Helm defaultStorageClass)
        accessMode: ReadWriteOnce     # V1: ReadWriteOnce only
        labels: {}                    # safe PVC metadata
      existing:                       # required when mode=Existing — exactly one of:
        claimName: my-neo4j-pvc       # Helm volume — bind named PVC
        # volume:                      # Helm volume — inline StatefulSet volume source
        #   persistentVolumeClaim:
        #     claimName: my-neo4j-pvc
        # volumeClaimTemplate:         # Helm selector | volumeClaimTemplate — advanced
        #   metadata:
        #     name: data
        #   spec:
        #     storageClassName: gp3
        #     accessModes: [ReadWriteOnce]
        #     resources:
        #       requests:
        #         storage: 100Gi
        #     selector:
        #       matchLabels:
        #         neo4j-pv: "true"
      disableSubPathExpr: false
```

**Standalone quick-start:**

```yaml
spec:
  volumes:
    data:
      mode: Dynamic
      dynamic:
        size: 50Gi
```

#### `spec.volumes.<aux>` — `Share` | `Dynamic` | `Existing`

Aux roles: `backups`, `logs`, `metrics`, `import`, `licenses`, plus operator extension `plugins` (`/plugins`).

```yaml
spec:
  volumes:
    data:
      mode: Dynamic
      dynamic:
        size: 100Gi
    logs:
      mode: Share                     # default — reuses data volume at /logs
      shareFrom: data                 # V1: only `data` allowed
    backups:
      mode: Dynamic                   # dedicated PVC for /backups
      dynamic:
        size: 200Gi
        storageClassName: gp3
    metrics:
      mode: Existing
      existing:
        claimName: neo4j-metrics-pvc
```

| `mode` | Operator behaviour |
|--------|-------------------|
| **Share** | No separate PVC; mount `data` volume (or sub-path) at aux path (`/logs`, `/backups`, …). Helm `share` default. |
| **Dynamic** | Per-pod PVC via `volumeClaimTemplates` on the aux role (same shape as `data.dynamic`). |
| **Existing** | Same `existing` oneOf as `data` (`claimName`, `volume`, `volumeClaimTemplate`). |

| Advantages | Disadvantages |
|------------|---------------|
| **Two intents for data** — provision vs bring-your-own | Vocabulary differs from Helm mode names (mapping table required) |
| **`volumeClaimTemplate` nested under `Existing`** — standard K8s `selector` support | Advanced users must know VCT YAML for selector cases |
| **`Share` explicit** for aux — matches Helm defaults | `Share` on multi-member cluster: shared RWO may be invalid — webhook must validate access mode + topology |
| Bounded CEL: 2-way mode on data, 3-way on aux | `data` Helm `share` is a rare edge → maps to `Existing.volume` |
| Single BDR covers data + aux | |

**Helm → operator mapping** (for `11-helm-mapping.md`):

| Helm | Operator |
|------|----------|
| `volumes.data.mode: defaultStorageClass` | `data.mode: Dynamic`, omit `storageClassName` |
| `volumes.data.mode: dynamic` | `data.mode: Dynamic` + `dynamic.*` |
| `volumes.data.mode: volume` | `data.mode: Existing` + `existing.claimName` or `existing.volume` |
| `volumes.data.mode: selector` | `data.mode: Existing` + `existing.volumeClaimTemplate` (with `spec.selector`) |
| `volumes.data.mode: volumeClaimTemplate` | `data.mode: Existing` + `existing.volumeClaimTemplate` |
| `volumes.data.mode: share` | `data.mode: Existing` + `existing.volume` (shared claim reference) |
| `volumes.logs.mode: share` (default) | `logs.mode: Share`, `shareFrom: data` |
| `volumes.backups.mode: dynamic` | `backups.mode: Dynamic` + `dynamic.*` |
| `volumes.*.mode: share` | `<role>.mode: Share`, `shareFrom: data` |

---

## Escape hatches — `additionalVolumes`, `additionalVolumeMounts`, `secretMounts`

Helm exposes three **additive** mechanisms outside `volumes.<role>`:

| Helm path | Shape | K8s effect |
|-----------|-------|------------|
| `additionalVolumes` | `[]` raw volume sources | StatefulSet `spec.template.spec.volumes` |
| `additionalVolumeMounts` | `[]` raw mounts | Neo4j container `volumeMounts` |
| `secretMounts` | `map[id]` → `{ secretName, mountPath, items?, defaultMode? }` | Paired Secret volume + mount |

**Forces:**

- **Split-list fragility (Helm).** `additionalVolumes` and `additionalVolumeMounts` are independent — nothing validates that `name` references exist.
- **secretMounts ≠ TLS.** Distinct from `ssl.*` (`spec.trust`), `passwordFromSecret` (`spec.auth`), `apoc_credentials` (`pluginDefinitions`). Used for **ad-hoc files** (S3 keys for seedURI, custom integrations).
- **Restore dual path.** V1 `Neo4jRestore.spec.source.credentials` mounts secrets on the **Job**; Helm `secretMounts` mounts on the **long-running pod** — seedURI workflows may need both or either depending on whether restore is operator-driven.
- **Safe / additive.** All three are `versioning: safe` — iterable without API break.

### Option E — Paired `additionalMounts` + `secretMounts` at spec root — **accepted** (escape hatches, complements Option D)

Mirrors Helm **top-level** layout (`volumes:` + `additionalVolumes` + `secretMounts` are siblings in `values.yaml`).

```yaml
spec:
  volumes:
    data:
      mode: Dynamic
      dynamic:
        size: 100Gi
  additionalMounts:
    - name: neo4j1-conf
      volume:
        emptyDir: {}
      mountPath: /config/neo4j1.conf
    - name: extra-data
      volume:
        persistentVolumeClaim:
          claimName: shared-config-pvc
      mountPath: /mnt/extra
      readOnly: true
  secretMounts:
    s3-credentials:
      secretName: my-s3-secret
      mountPath: /var/secrets/s3
      items:
        - key: access-key
          path: access-key
        - key: secret-key
          path: secret-key
      defaultMode: 420   # 0644 octal → JSON int
```

| Advantages | Disadvantages |
|------------|---------------|
| **Pairs volume + mount** — fixes Helm split-list footgun | `additionalMounts` is a rename of two Helm lists (mapping table) |
| **Same top-level layout as Helm** — `volumes`, `additionalMounts`, `secretMounts` siblings | Operator must validate reserved paths |
| Helm `secretMounts` map shape preserved at root | |
| Typed `secretMounts` — better than raw Secret in `additionalMounts` | |

**Validation (V1):** webhook rejects `mountPath` prefixes `/data`, `/var/lib/neo4j/certificates/`; optional allowlist on `additionalMounts[].volume` types; Secret existence check for `secretMounts`.

### Option F — Helm split lists at spec root

```yaml
spec:
  volumes:
    data: { ... }
  additionalVolumes:
    - name: neo4j1-conf
      emptyDir: {}
  additionalVolumeMounts:
    - name: neo4j1-conf
      mountPath: /config/neo4j1.conf
  secretMounts: { ... }
```

| Advantages | Disadvantages |
|------------|---------------|
| Highest Helm YAML parity for volumes/mounts | Inherits orphan-mount risk |
| Drop-in migration from values.yaml | Weaker admission story |

### Option G — Defer to `spec.podTemplate` only

Put raw volume/mount overrides under `podTemplate`; no first-class `additionalMounts` / `secretMounts`.

| Advantages | Disadvantages |
|------------|---------------|
| Smallest explicit storage API | `podTemplate` under-specified; loses validation |
| | Breaks Helm migration clarity |

### Option H — `secretMounts` only on `Neo4jRestore` (reject workload mounts)

Workload CRD has no `secretMounts`; credentials only on restore Job CRD.

| Advantages | Disadvantages |
|------------|---------------|
| Minimal Neo4j spec | **Breaks Helm seedURI on running pod** |
| Clear lifecycle boundary | Manual / legacy Helm migrations blocked |

**Helm → operator mapping (escape hatches):**

| Helm | Operator (Option E) |
|------|---------------------|
| `additionalVolumes` + `additionalVolumeMounts` (paired by `name`) | `spec.additionalMounts[]` |
| `secretMounts` | `spec.secretMounts` (same map keys) |

---

## Comparison (volume modes + escape hatches)

| Criterion | A — full Helm enum | D — Dynamic + Existing + Share | E — escape (paired + secretMounts) |
|-----------|-------------------|----------------------------------|-------------------------------------|
| Data/aux modes | ✅ | ✅ | ✅ (orthogonal) |
| Escape hatch UX | N/A | ⚠️ deferred to podTemplate | ✅ paired mounts |
| secretMounts | N/A | needs decision | ✅ structured map |
| Helm parity (escape) | N/A | ❌ | ⚠️ rename lists → paired |
| Validation | N/A | N/A | ✅ reserved paths + secret refs |

---

## Comparison (data / aux modes)

| Criterion | A — full Helm enum | D — Dynamic + Existing + Share |
|-----------|-------------------|--------------------------------|
| Helm parity | ✅ all mode names | ✅ via mapping table |
| API minimalism | ❌ six sub-objects on data | ✅ two modes on data |
| Intent clarity | ⚠️ Helm ambiguity inherited | ✅ explicit intent |
| `selector` / raw VCT | ✅ (with tpl gap) | ✅ under `Existing.volumeClaimTemplate` |
| Aux `share` | ⚠️ per-role Helm copy | ✅ `Share` + `shareFrom` |
| Validation (CEL) cost | ❌ 6-way `oneOf` on data | ✅ 2-way data + 3-way aux |
| Operator complexity | High | Medium |

---

## Decision

**We will implement Option D** — `spec.volumes.data` uses `Dynamic` | `Existing`; aux volumes (`backups`, `logs`, `metrics`, `import`, `licenses`) use `Share` | `Dynamic` | `Existing`. Non-dynamic Helm paths map under `Existing` (`claimName`, `volume`, or `volumeClaimTemplate` including `selector`).

Option A is rejected (too heavy). Escape hatches follow **Option E** (`spec.additionalMounts[]` + `spec.secretMounts`); Options F, G, H rejected.

**V1 implementation scope:**

1. **Default:** `volumes.data.mode: Dynamic`; aux default `mode: Share`, `shareFrom: data`.
2. **Validate `Existing.volumeClaimTemplate`** minimally — structural schema only.
3. **Reject `volumes.data.mode: Share`** at admission (CEL STO-005).
4. **Webhook:** cluster + aux `Share` + RWO data — warn or error when invalid.
5. Immutable after create: `mode`, binding shapes, `disableSubPathExpr`; only `dynamic.size` may grow.
6. **Escape hatches (Option E):** STO-008…010.
7. **API key is `spec.volumes`**, not `persistence` (BDR-005 supersedes `20-operator-proposal.md` naming).
8. Document `Neo4jRestore` credentials vs `spec.secretMounts` overlap.
9. Update `neo4j/spec.md`.

---

## Consequences

### Positive

- One BDR governs **data + aux** storage, including Helm `share`.
- `volumeClaimTemplate` with `selector` is standard Kubernetes — no bespoke operator selector logic.
- API surface stays small: two modes on data, three on aux.
- **Paired `additionalMounts`** removes Helm's orphan-volume footgun.
- **`secretMounts`** map preserves seedURI / S3 credential pattern with clear separation from TLS.

### Negative

- Helm mode names disappear — `11-helm-mapping.md` required.
- `Share` + RWO across N cluster members may be invalid — validation and docs must be explicit.
- `spec.md` must move from flat fields to `mode` + nested objects.
- Helm users must merge `additionalVolumes` + `additionalVolumeMounts` into `additionalMounts[]` on migration.

### Neutral

- `labels` on `data.dynamic` remains safe and additive.
- `Neo4jRestore` Job credentials remain separate — document when to use which.
- V1 optional allowlist on `additionalMounts[].volume` types — full passthrough if allowlist too restrictive.

---

## References

- `design/analysis/helm-fields/_index.csv` — `volumes.data.*`, aux volumes, `additionalVolumes`, `additionalVolumeMounts`, `secretMounts`
- `design/analysis/helm-fields/semantic-concerns.yaml` — **CONCERN-STORAGE-ESCAPE**
- `design/analysis/helm-fields/aggregation-matrix.md` — **AGG-STORAGE-DATA**, **AGG-STORAGE-AUX**
- `design/09-crd-spec/neo4j/spec.md` — `spec.volumes`
- [Neo4j — Kubernetes deployment / storage](https://neo4j.com/docs/operations-manual/current/kubernetes/)
- [Kubernetes — PersistentVolumeClaimSpec.selector](https://kubernetes.io/docs/reference/kubernetes-api/config-and-storage-resources/persistent-volume-claim-v1/#PersistentVolumeClaimSpec)
- [BDR-001](001-single-neo4j-crd.md) — single `Neo4j` CRD
