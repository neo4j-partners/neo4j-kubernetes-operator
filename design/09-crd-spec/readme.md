# CRD specification (`09-crd-spec/`)

OpenAPI / CRD design for the Neo4j Kubernetes Operator. **One folder per V1 CRD.** Each folder is self-contained: spec, example manifest(s), and validation rules.

**Status**: `[~]` in progress · **Priority**: P0 — blocks implementation

---

## Layout (V1 only)

```
09-crd-spec/
├── readme.md
├── neo4j/                    ← workload CRD (largest spec)
│   ├── spec.md               ← topology, persistence, connectivity, trust,
│   │                           config, scheduling (embedded sections)
│   ├── status.md             ← conditions, phases, Ready semantics
│   ├── example.yaml
│   └── validation.md
├── neo4jdatabase/
│   ├── spec.md
│   ├── example.yaml
│   └── validation.md
├── neo4jbackup/              ← Neo4jBackup + Neo4jBackupSchedule
│   ├── spec.md
│   ├── example.yaml
│   ├── example-schedule.yaml
│   └── validation.md
└── neo4jrestore/
    ├── spec.md
    ├── example.yaml
    └── validation.md
```

### Per-folder files

| File | Purpose |
|------|---------|
| **`spec.md`** | OpenAPI field definitions, defaults, immutability. For `neo4j/`, documents the full `spec` including embedded infra sections (not separate CRDs). |
| **`status.md`** | `status` subresource for this CRD — phases, conditions, Ready semantics (`neo4j/` only for now). |
| **`example.yaml`** | Minimal valid manifest for review and copy-paste into `config/samples/` during implementation |
| **`validation.md`** | CEL rules and admission webhook logic for this CRD — rule, mechanism (CEL / webhook), error message |

If `neo4j/spec.md` grows too large, split into `neo4j/spec/` sub-files (`topology.md`, `persistence.md`, …) — still under the `neo4j` CRD folder.

Full V1 scenario manifests (multi-resource, edge cases) → [`../samples/`](../samples/).

---

## V1 CRD inventory

| Folder | `kind`(s) | FR | Reconciler |
|--------|-----------|-----|------------|
| [`neo4j/`](neo4j/) | `Neo4j` | NEO-1-001…NEO-2-016, NEO-2-018 | `Neo4jReconciler` | `[x]` spec · status · validation · example |
| [`neo4jdatabase/`](neo4jdatabase/) | `Neo4jDatabase` | *(logical DB — TBD in FR)* | `Neo4jDatabaseReconciler` |
| [`neo4jbackup/`](neo4jbackup/) | `Neo4jBackup`, `Neo4jBackupSchedule` | NEO-013 | `Neo4jBackupReconciler` |
| [`neo4jrestore/`](neo4jrestore/) | `Neo4jRestore` | NEO-014 | `Neo4jRestoreReconciler` |

There is **no** `Persistence`, `Connectivity`, `Trust`, or `ServerConfig` CRD — those are `spec` sections on `Neo4j`, documented in [`neo4j/spec.md`](neo4j/spec.md).

---

## Not in this folder (and why)

| Idea | In `01` FRs? | V1? | Decision |
|------|--------------|-----|----------|
| **`Neo4jProfile`** | **No** — not in requirements | — | **Removed.** Speculative `profileRef` pattern with no FR backing. Revisit in `17-roadmap.md` / ADR if platform teams need reusable templates. |
| **`Neo4jMaintenance`** | Yes — NEO-017 | **No** | **Deferred to V2.** Not designed until V1 ships. Offline mode may become a `neo4j.spec` field; dump/load jobs may become a day-2 CRD (like backup) — decision at V2 scoping. |

This folder documents **what we are building now**. Deferred CRDs get a folder here only when they enter scope — not as empty placeholders.

---

## Principles (locked)

- **Single `Neo4j` CRD** — `spec.topology.mode: Standalone | Cluster`; Cluster composes `primaries`, `secondaries`, `analytics` ([BDR-002](../decision-records/business/002-neo4j-crd-topology.md)).
- **No infra sub-domain CRDs** — persistence, connectivity, trust, and config are `spec` sections on `Neo4j`.
- **One reconciler per CRD** — folder boundaries follow controller boundaries.
- **Validation co-located** — all rules for a CRD live in that CRD's `validation.md`.

### `common_types.go` is code layout, not a CRD

Shared Go structs (`TopologySpec`, `PersistenceSpec`, …) in `api/v1beta1/common_types.go` are a **Go packaging choice** — not a Kubernetes resource and not a folder here.

---

## API versioning

| Version | Purpose |
|---------|---------|
| `v1alpha1` | Internal iteration — breaking changes allowed |
| `v1beta1` | **V1 target** — field-complete, reviewed |
| `v1` | GA — after V1 field freeze and conversion webhook tested |

Conversion webhook design → `neo4j/validation.md` + variant *CRD conversion webhook* in `03-variant_matrix.csv`.

---

## Traceability

| Source | Maps to |
|--------|---------|
| `01-functional_requirements.csv` | FR scope per folder |
| `03-variant_matrix.csv` | `spec` fields and enum values in each `spec.md` |
| `11-helm-mapping.md` | Superseded by [ADR-002](../decision-records/architecture/002-helm-values-mapping.md) |
| `10-status-model.md` | `status` subresources per CRD |

---

## Suggested authoring order

1. [`neo4j/`](neo4j/) — V1 blocker
2. [`neo4jdatabase/`](neo4jdatabase/)
3. [`neo4jbackup/`](neo4jbackup/) → [`neo4jrestore/`](neo4jrestore/)

Within each folder: `spec.md` → `example.yaml` → `validation.md`.
