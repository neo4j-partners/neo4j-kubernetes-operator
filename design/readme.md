# Neo4j Kubernetes Operator — Design Package

This folder contains the full design baseline for the Neo4j Kubernetes Operator: requirements, validation catalog, architecture decisions, and the specification documents to complete before implementation.

**Status legend**: `[x]` done · `[~]` partial · `[ ]` to do

The package index at the repository root ([`readme.md`](../readme.md)) expands each deliverable with cross-references to Helm values, RBAC, packaging, and migration.

---

## Folder structure

```
design/
├── readme.md                         ← this file
├── 00-vision.md                       [ ]  scope, personas, goals / non-goals
├── 01-functional_requirements.csv     [x]  functional requirements (FR)
├── 02-acceptance_criteria_library.csv [x]  reusable acceptance criteria (AC)
├── 03-variant_matrix.csv              [x]  configuration variants per FR
├── 04-test_catalog.csv                [x]  detailed test catalog (1 row = 1 test)
├── 05-test_catalog_summary.csv        [x]  effort rollup by controller
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
├── 13-dod-v1.md                       [ ]  V1 definition of done (AC subset)
├── 14-dependencies.md                 [ ]  inter-CRD and external dependencies
├── 15-test-strategy.md                [ ]  unit / integration / E2E pyramid
├── 16-security.md                     [ ]  RBAC model and threat model
├── 17-roadmap.md                      [ ]  P0 — controller sequence and milestones
├── 18-effort-model.md                 [ ]  manual vs AI-assisted assumptions
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
| **`00-vision.md`** | Problem statement, personas, goals / non-goals, and V1 / V2 phasing narrative. Anchors all downstream decisions. |
| **`01-functional_requirements.csv`** | 25 functional requirements: 7 operator (`OP-*`) + 18 Neo4j (`NEO-*`). Each row defines scope, variant groups, AC groups, and V1 inclusion. |
| **`02-acceptance_criteria_library.csv`** | 114 reusable acceptance criteria grouped by domain (`AC-OP-*`, `AC-NEO-*`). Linked to FRs via the `Applies To` column. |
| **`03-variant_matrix.csv`** | 92 configuration variants (packaging, topology, storage, networking, TLS, etc.). Drives scenario test coverage and combinatorial scope. |

### Phase 2 — Validation catalog (`04`–`05`)

| File | Purpose |
|------|---------|
| **`04-test_catalog.csv`** | 207 tests in two layers: **92 scenario tests** (1 per variant) + **115 AC-level tests** (1 per AC). Columns include steps, preconditions, expected results, priority, V1 flag, and per-test effort. |
| **`05-test_catalog_summary.csv`** | Rollup by operator controller / domain module. Source of truth for **directional effort projections** (see § Estimation below). |

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
| **`13-dod-v1.md`** | Frozen list of ACs and tests required for V1 release. Derived from `02` + `04` (filter `V1=Yes`, `Priority=P0`). |
| **`14-dependencies.md`** | Inter-CRD readiness chain, external prerequisites (StorageClass, Prometheus, cert-manager), Neo4j runtime ordering. |
| **`15-test-strategy.md`** | Test pyramid, frameworks (envtest, kind), CI gates, and environment matrix. |
| **`16-security.md`** | Operator RBAC scopes, workload identity, threat model, Pod Security Standards, network policies. |

### Phase 6 — Planning (`17`–`18`)

| File | Purpose |
|------|---------|
| **`17-roadmap.md`** | Controller implementation sequence, milestones, risks, and mitigations. Required for a reliable V1 date commitment. |
| **`18-effort-model.md`** | Formal assumptions for manual vs AI-assisted delivery, overhead not captured in the test catalog, and velocity calibration method. |

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
                            05 effort summary
                                    │
                    13 DoD V1 ◄─────┴─────► 17 roadmap
```

---

## Estimation projections

Figures below come from `05-test_catalog_summary.csv`. They are **directional** — derived from test granularity, not from a completed implementation. Add explicit overhead for documentation, code review, and production hardening before committing a release date.

### Manual delivery (baseline)

| Metric | Full scope | V1 subset |
|--------|------------|-----------|
| Tests | 207 | 138 |
| Test automation effort | **~486 person-days** | **~344 person-days** |
| Operator development (realistic) | **~175 person-days** | — |
| Shared scaffold (API types, CI, e2e harness) | **~18 person-days** | — |
| **Combined dev + V1 tests** | — | **~520 person-days** |

**Calendar hint (1 FTE developer)**: ~175 dev days ≈ **35 weeks** of implementation, before test automation is counted. With a dedicated QA/automation engineer, V1 test automation adds roughly **69 weeks** at 1 FTE — typically run in parallel with development.

### Effort by controller / domain

> **Why does test automation exceed development in this table?**
>
> The two effort columns measure **different things** and must not be compared as if they were the same unit of work.
>
> | Column | What it measures | How it is computed |
> |--------|------------------|--------------------|
> | **Dev (realistic one-time)** | Cost to **implement** a controller or domain module **once** | Deduplicated estimate — building `Neo4jDatabase` reconciler logic is paid once, not per test |
> | **Test automation (all)** | Cost to **automate every test** in the catalog for that area | **Cumulative** sum across all tests (207 total, mostly E2E) |
>
> Example — `Neo4jDatabase`: **35 d** to write the workload reconciler, but **80 tests** to automate at ~2.5 d each on average → **202 d** of test automation. You implement once; you validate across many variants (standalone, cluster, scale, upgrade, probes…).
>
> If you naively summed the per-test `Operator Dev Effort` column in `04-test_catalog.csv`, you would get **~791 person-days** — that number **double-counts** implementation work (each test row carries a slice of dev effort as if the feature were rebuilt from scratch). The **175 d** realistic total avoids that inflation.
>
> This pattern is **normal for Kubernetes operators**: E2E tests are expensive (cluster provisioning, Neo4j startup, cluster formation, TLS, flakiness debugging) while the reconcile logic itself is written once and reused. A healthy project still aims for a **unit / integration pyramid** (`15-test-strategy.md`) so that most coverage does not require full E2E.

| Controller / domain | V1 tests | Dev (one-time) | Test automation (all) | Auto : Dev |
|---------------------|----------|----------------|----------------------|------------|
| Shared scaffold | — | 18 d | — | — |
| OperatorBootstrap | 7 | 10 d | 27 d | 2.7× |
| OperatorReconciler | 2 | 8 d | 17 d | 2.1× |
| OperatorStatus | 3 | 4 d | 6 d | 1.5× |
| OperatorUpgrade | 3 | 7 d | 16 d | 2.3× |
| OperatorUninstall | 1 | 3 d | 6 d | 2.0× |
| OperatorRBAC | 1 | 5 d | 9 d | 1.8× |
| OperatorObservability | 0 | 4 d | 3 d | 0.8× |
| Neo4jDatabase (workload) | 64 | 35 d | 202 d | 5.8× |
| Neo4jPersistence | 9 | 10 d | 32 d | 3.2× |
| Neo4jConnectivity | 5 | 9 d | 16 d | 1.8× |
| Neo4jTrust | 16 | 14 d | 42 d | 3.0× |
| Neo4jServerConfig | 14 | 12 d | 30 d | 2.5× |
| Neo4jBackup | 8 | 14 d | 41 d | 2.9× |
| Neo4jRestore | 5 | 12 d | 22 d | 1.8× |
| Neo4jMaintenance | 0 | 10 d | 18 d | 1.8× |
| **Total** | **138** | **~175 d** | **~486 d** | **~2.8×** |

### AI-assisted delivery (projected)

When using AI for code generation, test scaffolding, CRDs, and boilerplate — with a human reviewing, integrating, and debugging:

| Metric | Manual | AI-assisted | Reduction |
|--------|--------|-------------|-----------|
| Operator development | ~175 d | **~65 d** | ~63 % |
| Test automation (V1) | ~344 d | **~220 d** | ~36 % |
| **Combined V1** | ~520 d | **~285 d** | ~45 % |

Highest AI leverage: kubebuilder scaffold, CRDs, RBAC, table-driven unit tests (−60 to 70 %).  
Lowest AI leverage: E2E with real clusters, Neo4j HA, backup/restore, TLS (−25 to 45 %).

Full assumptions and calibration method → `18-effort-model.md` (to be written).

### When estimates become reliable

| Stage | Artifacts | Accuracy | Use for |
|-------|-----------|----------|---------|
| **Now** | `05` (+ V1 filter on `04`) | ±40–50 % | Go/no-go, budget envelope, team sizing |
| **After P0 spec** | `09`–`12` locked | ±25 % | Release planning, milestone dates |
| **After scope lock** | `13` + `17` frozen | ±15 % | V1 commitment date |
| **After velocity spike** | Scaffold + one domain module shipped | ±10 % | Sprint-level planning |

---

## Recommended reading order

1. **`00-vision.md`** — align on scope
2. **`01` → `03`** — understand what must be built and in which configurations
3. **`06` → `08`** — understand how the code is organized
4. **`09` → `12`** — lock the API and runtime contract *(P0 — blocking)*
5. **`13` + `17`** — freeze V1 and sequence work → **reliable estimate**
6. **`04` (V1 / P0 filter)** + **`15`** — drive test execution during implementation
7. **`18`** — refine projections after the first domain module is delivered

---

## Key architectural decisions (summary)

These are documented in depth in the root [`readme.md`](../readme.md) §5–§8 and will be captured as ADRs in `adr/`:

- **Single `Neo4j` CRD** with `spec.topology.mode: Standalone | Cluster` — see [BDR-001](adr/business/001-single-neo4j-crd.md).
- **Topology roles** — `cores` + `readReplicas` for cluster composition (e.g. primary + analytics) — see [BDR-002](adr/business/002-neo4j-crd-topology.md).
- **`Neo4jDatabase` CRD** for logical databases inside a deployment (`neo4jRef`).
- **No CRDs for infra sub-domains** — connectivity, persistence, trust, and server config are `spec` sections + shared `internal/domain/*` packages.
- **One reconciler per CRD**, domain modules shared; only `workload` branches on `topology.mode`.
- **Three internal layers**: `render/` (pure) → `domain/` (apply) → `controller/` (pipeline).
