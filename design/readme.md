# Neo4j Kubernetes Operator — Design Package

This folder contains the full design baseline for the Neo4j Kubernetes Operator: requirements, validation catalog, architecture decisions, and the specification documents to complete before implementation.

**Status legend**: `[x]` done · `[~]` partial · `[ ]` to do

The package index at the repository root ([`readme.md`](../readme.md)) expands each deliverable with cross-references to Helm values, RBAC, packaging, and migration.

---

## Folder structure

```
design/
├── readme.md                         ← this file
├── 00-vision.md                       [~]  scope, personas, goals; adoption risks captured
├── 01-functional_requirements.csv     [x]  functional requirements — hierarchical IDs + Requires IDs
├── 02-acceptance_criteria_library.csv [x]  reusable acceptance criteria (AC)
├── 03-variant_matrix.csv              [x]  configuration variants per FR
├── 04-test_catalog.csv                [x]  detailed test catalog (1 row = 1 test)
├── 05-test_catalog_summary.csv        [x]  test coverage rollup by controller
├── 06-flow.md                         [x]  4-layer reconcile pipeline
├── 07-layer.md                        [x]  render / domain / controller rule
├── 08-file_structure.md               [x]  target kubebuilder repository tree
├── 09-crd-spec/                       [ ]  P0 — V1 CRDs only
│   ├── readme.md
│   ├── neo4j/
│   ├── neo4jdatabase/
│   ├── neo4jbackup/
│   └── neo4jrestore/
├── 10-status-model.md                 [ ]  P0 — conditions and phases per CRD
├── 11-helm-mapping.md                 [ ]  P0 — Helm values.yaml → CRD spec
├── 12-reconciliation.md               [ ]  P0 — reconcile loop and ordering
├── 13-v1-scope-lock.md                [ ]  frozen V1 scope — AC + tests + exclusions
├── 14-dependencies.md                 [~]  external prereqs + platform constraint matrix
├── 15-test-strategy.md                [ ]  unit / integration / E2E pyramid
├── 16-security.md                     [ ]  RBAC model and threat model
├── 17-roadmap.md                      [ ]  P0 — controller sequence and milestones
├── 18-effort-model.md                 [x]  effort assumptions and calculation rules
├── 19-delivery-estimate.csv           [x]  delivery cost by phase / workstream (data)
├── 19-delivery-estimate.md            [x]  delivery cost — human-readable view
├── 20-operator-proposal.md            [x]  full operator proposal — CRDs, security, workflows, V1/V2 scope
├── adr/                               [ ]  architecture decision records
├── samples/                           [ ]  YAML manifests per V1 scenario
└── diagrams/                          [ ]  sequence and dependency diagrams
```

Numbering follows the production flow: **requirements → architecture → specification → planning**.

---

## File guide

### Phase 1 — Scope & requirements (`00`–`03`)

| File | Purpose |
|------|---------|
| **`00-vision.md`** | Problem statement, personas, goals / non-goals, V1 / V2 phasing, and **adoption stoppers** (operational risk, support model, Helm differentiation, migration, PS vs Product ownership). Anchors all downstream decisions. |
| **`01-functional_requirements.csv`** | **116 rows** sorted by depth (level in `ID`). **`Requires IDs`** = children via `Parent ID` + composed capabilities for Neo4j outcomes. |
| **`02-acceptance_criteria_library.csv`** | 117 acceptance criteria. **`Applies To`** references the most specific FR ID (level 3 config when applicable, else level 2 capability or level 1 outcome). |
| **`03-variant_matrix.csv`** | 91 configuration variants — each row maps to a level-**3** (or level-**2** for Operator) `Configuration FR ID` in `01`. |

#### FR drill-down — ID encoding (`01`)

**Level = 2nd segment of `ID`** (`{PREFIX}-{level}-…`). File is sorted: all level **1**, then **2**, then **3**. Depth is open-ended (level 4+ reserved).

| Level | Role | ID pattern | Example |
|------:|------|------------|---------|
| **1** | Root requirement (Neo4j outcome or Operator top-level FR) | `{PREFIX}-1-{nnn}` | `NEO-1-001` Deploy standalone · `OP-1-001` Install operator |
| **2** | Capability, or configuration directly under a level-1 root | `{PREFIX}-2-{nnn}` or `{PREFIX}-2-{parent}-{Group}-{nn}` | `NEO-2-006` Storage · `NEO-2-001-MODE-01` Standalone mode · `OP-2-001-PKG-01` YAML packaging |
| **3** | Configuration under a level-2 capability | `{PREFIX}-3-{parent}-{Group}-{nn}` | `NEO-3-006-PVC-01` Default StorageClass |

- **`Requires IDs`** — what this requirement needs: **direct children** (rows whose `Parent ID` points here) and, for `NEO-1-001` / `NEO-1-002`, the shared level-2 capabilities.
- **`Parent ID`** — upward link in the drill-down tree.

Storage drill-down example:

```
NEO-1-001  Deploy standalone
  └─ NEO-2-006  Configure persistent volumes
       ├─ NEO-3-006-PVC-01  Default StorageClass
       ├─ NEO-3-006-CLD-01  With Workload Identity
       └─ NEO-3-006-CLD-02  Without Workload Identity
```

Group codes: `PVC` · `VOL` · `CLD` · `SVC` · `PKG` · … (see `03-variant_matrix.csv`).


| File | Purpose |
|------|---------|
| **`04-test_catalog.csv`** | 208 tests in two layers: **91 scenario tests** (1 per configuration FR) + **117 AC-level tests** (1 per AC). Includes **`Configuration FR ID`** (Level 2 link to `01`). **No effort columns** — cost lives in `19`. |
| **`05-test_catalog_summary.csv`** | Test **coverage** rollup by controller / domain module (counts only). Source of truth for catalog breadth, not delivery cost. |

### Phase 3 — Architecture (`06`–`08`)

| File | Purpose |
|------|---------|
| **`06-flow.md`** | Four-layer internal pipeline: CRD → validation → render (pure builders) → domain (apply + status) → controller (thin orchestration). |
| **`07-layer.md`** | Three-package rule: `render/` (pure K8s objects), `domain/` (business logic), `controller/` (one reconciler per CRD). Defines testability boundaries. |
| **`08-file_structure.md`** | Target repository layout (`api/v1beta1/`, `internal/domain/`, `internal/render/`, `internal/controller/`, tests). |

### Phase 4 — Specification (`09`–`12`) — P0 before coding

| File | Purpose |
|------|---------|
| **`09-crd-spec/`** | V1 CRD OpenAPI only — one folder per CRD (`neo4j`, `neo4jdatabase`, `neo4jbackup`, `neo4jrestore`). Embedded infra sections live in `neo4j/spec.md`. `Neo4jProfile` (no FR) and `Neo4jMaintenance` (NEO-017, V1=No) are deferred — see [`09-crd-spec/readme.md`](09-crd-spec/readme.md). |
| **`10-status-model.md`** | `status.conditions`, phases, and Ready semantics for `Neo4j`, `Neo4jDatabase`, and day-2 CRDs. |
| **`11-helm-mapping.md`** | Field-by-field mapping from `crds/values.yaml` to operator CRD `spec`. Migration reference for existing Helm users. |
| **`12-reconciliation.md`** | Reconcile loop per controller, resource ownership, finalizers, ordering inside `Neo4jReconciler`, idempotence and drift handling. |

### Phase 5 — Scope lock & operations (`13`–`16`)

| File | Purpose |
|------|---------|
| **`13-v1-scope-lock.md`** | **Frozen V1 scope** — which FRs, ACs, and tests are in V1 (and what is explicitly out). Derived from `02` + `04` (`V1=Yes`, `Priority=P0`). Commitment document for release planning — not a generic “definition of done”. |
| **`14-dependencies.md`** | Inter-CRD readiness chain, external prerequisites (StorageClass, Prometheus, cert-manager), Neo4j runtime ordering, and **platform constraint matrix** (Local, AKS, GKE, EKS, OpenShift). |
| **`15-test-strategy.md`** | Test pyramid, frameworks (envtest, kind), CI gates, and environment matrix. |
| **`16-security.md`** | Operator RBAC scopes, workload identity, threat model, Pod Security Standards, network policies. |

### Phase 6 — Planning & estimation (`17`–`19`)

| File | Purpose |
|------|---------|
| **`17-roadmap.md`** | Controller implementation sequence, milestones, risks, and mitigations. Required for a reliable V1 date commitment. |
| **`18-effort-model.md`** | Calculation rules: deduplicated dev, harness amortization, test pyramid, manual vs AI-assisted assumptions, overhead, calibration method. |
| **`19-delivery-estimate.csv`** | Person-day estimates by **phase** (Conception / Dev / Testing / **Documentation**) and **workstream**. One row = one deliverable, not one test. Machine-readable source of truth. |
| **`19-delivery-estimate.md`** | Same estimates in readable form — rollup, tables per phase, dependency graph. Edit the CSV first; keep both in sync. |
| **`20-operator-proposal.md`** | End-to-end operator proposal derived from Helm chart analysis: vision, architecture, CRD specs (V1/V2), cloud security model, operational workflows, implementation phasing, Helm migration. Aligns with BDR-001/002 and `09-crd-spec/`. |

### Supporting folders

| Folder | Purpose |
|--------|---------|
| **`adr/`** | Decision records — [`adr/readme.md`](adr/readme.md): `business/` (BDR, product/API) · `architecture/` (ADR, implementation) |
| **`samples/`** | Reference Kubernetes manifests per V1 scenario (standalone, cluster, backup, restore). |
| **`diagrams/`** | Sequence diagrams (install, upgrade, backup) and dependency graphs. |

---

## Traceability chain

```
01 FR  ──►  03 variants  ──►  04 scenario tests (TST-SCN-*)
  │              │
  └──►  02 AC  ──────────►  04 AC tests (TST-AC-*)
                                    │
                                    ▼
                            05 coverage summary (counts)
                                    │
              19 delivery estimate ◄── 18 effort model (rules)
                                    │
                    13 V1 scope lock ◄─────┴─────► 17 roadmap
```

---

## Estimation projections

Figures below come from [`19-delivery-estimate.csv`](19-delivery-estimate.csv) ([readable view](19-delivery-estimate.md)). Calculation rules and assumptions → [`18-effort-model.md`](18-effort-model.md).

Effort is split by **delivery phase**, not by test row. The catalog (`04`) defines *what* to validate (208 tests); `19` defines *what it costs to deliver* with harness amortization and deduplicated dev.

### V1 delivery by phase

| Phase | Manual | AI-assisted | Key workstreams |
|-------|--------|-------------|-----------------|
| **Conception** | 50 d | 35.5 d | CRD spec, runtime contract, scope lock |
| **Dev** | 185 d | 69 d | Scaffold, operator core, domain modules, packaging |
| **Testing** | 198 d | 97.5 d | Harness, pyramid, E2E parametrization, CI matrix |
| **Documentation** | 27 d | 14 d | Quickstart, Helm migration, CRD ref, runbooks, samples |
| **Total V1** | **460 d** | **216 d** | |

**Catalog coverage (not cost)**: 138 V1 tests — 61 scenario + 77 AC (`05-test_catalog_summary.csv`).

**Calendar hint (2 FTE, dev + QA in parallel)**: V1 ≈ **22–26 weeks** manual, **11–13 weeks** AI-assisted — after P0 spec is locked. Conception (~50 d) runs first and is mostly sequential; **low AI leverage on Conception** (25–35 % — see `18-effort-model.md`).

### Testing cost model (why it is not `N tests × fixed cost`)

| Workstream | V1 effort | Role |
|------------|-----------|------|
| Harness & fixtures (`EST-TST-010`) | 30 d | One-time — install, wait Ready, driver assertions |
| CI matrix (`EST-TST-020`) | 10 d | Same tests across GKE / EKS / AKS / OpenShift |
| Unit + integration (`EST-TST-030/040`) | 55 d | Pyramid base — no real Neo4j cluster |
| E2E founders + variants (`EST-TST-050/060`) | 63 d | ~18 founder scenarios + parametrized YAML variants |
| Stabilisation + DoD run (`EST-TST-080/090`) | 18 d | Flakiness buffer and release gate |

A large variant matrix is the **return on** harness investment — not a linear multiplier on delivery cost.

### Documentation (Phase 4)

| Workstream | V1 effort | Role |
|------------|-----------|------|
| Helm migration (`EST-DOC-002`) | 6 d | Adoption stopper — `11-helm-mapping` as user guide |
| CRD reference + topology (`EST-DOC-003`) | 5 d | BDR-002 decision tree for primary + analytics |
| Operations runbook (`EST-DOC-004`) | 5 d | Support / PS field playbook |
| Getting started + samples (`EST-DOC-001/005`) | 8 d | Install path and `samples/` walkthroughs |

Runs in parallel with late Dev / Testing once P0 spec is stable.

### Full scope (post-V1)

| Phase | Manual | AI-assisted |
|-------|--------|-------------|
| Conception | 50 d | 35.5 d |
| Dev | 195 d | 73 d |
| Testing | 245 d | 125.5 d |
| Documentation | 31 d | 16 d |
| **Total** | **521 d** | **250 d** |

Post-V1 rows cover maintenance domain (`EST-DEV-080`) and remaining catalog automation (`EST-TST-100`–`120`).

### Overhead not in `19`

Add before GA commitment: release engineering (5–8 d), security review (5–10 d), field feedback buffer (10–20 d). **Documentation** is in Phase 4 (`EST-DOC-*`). See `18-effort-model.md`.

### When estimates become reliable

| Stage | Artifacts | Accuracy | Use for |
|-------|-----------|----------|---------|
| **Now** | `19` directional + `04` catalog | ±40–50 % | Go/no-go, budget envelope, team sizing |
| **After P0 spec** | `09`–`12` locked | ±25 % | Release planning, milestone dates |
| **After scope lock** | `13` + `17` frozen | ±15 % | V1 commitment date |
| **After velocity spike** | Scaffold + one domain module shipped | ±10 % | Sprint-level planning |

---

## Recommended reading order

1. **`20-operator-proposal.md`** — full operator blueprint (start here for the big picture)
2. **`00-vision.md`** — align on scope
3. **`01` → `03`** — understand what must be built and in which configurations
4. **`06` → `08`** — understand how the code is organized
5. **`09` → `12`** — lock the API and runtime contract *(P0 — blocking)*
6. **`13` + `17`** — freeze V1 and sequence work → **reliable estimate**
7. **`04` (V1 / P0 filter)** + **`15`** — drive test execution during implementation
8. **`18` + `19`** — delivery estimates; refine after the first domain module is shipped

---

## Key architectural decisions (summary)

These are documented in depth in the root [`readme.md`](../readme.md) §5–§8 and will be captured as ADRs in `adr/`:

- **Single `Neo4j` CRD** with `spec.topology.mode: Standalone | Cluster` — see [BDR-001](adr/business/001-single-neo4j-crd.md).
- **Topology roles** — `cores` + `readReplicas` (read scaling) + `readGDSReplicas` (analytics/GDS) — see [BDR-002](adr/business/002-neo4j-crd-topology.md).
- **`Neo4jDatabase` CRD** for logical databases inside a deployment (`neo4jRef`).
- **No CRDs for infra sub-domains** — connectivity, persistence, trust, and server config are `spec` sections + shared `internal/domain/*` packages.
- **One reconciler per CRD**, domain modules shared; only `workload` branches on `topology.mode`.
- **Single-namespace operator scope in V1** — watches only its install namespace; multi-namespace and cluster-wide deferred — see [BDR-003](adr/business/003-operator-install-scope.md).
- **Three internal layers**: `render/` (pure) → `domain/` (apply) → `controller/` (pipeline).
