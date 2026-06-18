# Effort model — assumptions and calculation rules

This document defines **how** delivery effort is estimated. The numbers live in [`19-delivery-estimate.csv`](19-delivery-estimate.csv) ([readable view](19-delivery-estimate.md)).

The test catalog ([`04-test_catalog.csv`](04-test_catalog.csv)) answers *what* to validate and traces requirements. It does **not** carry cost columns — summing per-test effort there double-counted implementation work and ignored framework amortization.

---

## Three delivery phases

| Phase | Question it answers | Primary artifacts |
|-------|---------------------|-------------------|
| **Conception** | What are we building and under which contract? | `00`, `09`–`12`, `13`, `17`, ADRs |
| **Dev** | How do we implement it once? | Controllers, domain modules, packaging |
| **Testing** | How do we prove it works repeatably? | Pyramid, harness, CI matrix, E2E parametrization |

Each row in `19-delivery-estimate.csv` is a **deliverable**, not a test case.

---

## Core rules

### 1. Dev effort is deduplicated

Building `Neo4jDatabase` reconciler logic is paid **once** per domain module (`EST-DEV-020`), regardless of how many scenario variants exist in `03-variant_matrix.csv`.

### 2. Test harness is a one-time investment

`EST-TST-010` (harness & fixtures) and `EST-TST-020` (CI matrix) are **not** multiplied by the number of tests. Their ROI is running the same parametrized scenarios across platforms with marginal cost per variant.

### 3. E2E variants have marginal cost after founders

| Layer | Cost model | Example |
|-------|------------|---------|
| Founder scenario (per variant **group**) | ~2 d each | First standalone, first cluster, first TLS mode |
| Parametrized variant (same group) | ~0.3–0.5 d each | Edition Community vs Enterprise on same harness |
| AC spot check | Sampled, not 1:1 | 77 V1 AC rows → ~18 d of direct AC automation |

### 4. Catalog width ≠ delivery cost

| Metric | Source | Meaning |
|--------|--------|---------|
| 207 tests | `04-test_catalog.csv` | Validation **coverage** breadth |
| ~138 V1 tests | `04` filtered `V1=Yes` | DoD scope |
| ~435 person-days V1 manual | `19` sum `V1=Yes` | **Delivery** estimate |

A large test matrix is the **benefit** of automation (cross-platform, iso runs), not a linear `N tests × fixed cost` multiplier.

### 5. Test pyramid target

Per [`15-test-strategy.md`](15-test-strategy.md) (to be written):

- **Unit** — `render/` and `domain/` pure logic (`EST-TST-030`)
- **Integration** — envtest controllers without real Neo4j (`EST-TST-040`)
- **E2E** — real cluster + Neo4j, parametrized via harness (`EST-TST-050`, `EST-TST-060`)

Most ACs should be covered indirectly by scenario tests or lower layers; direct AC automation is reserved for gaps.

---

## Manual vs AI-assisted assumptions

Reduction = `(manual − AI-assisted) / manual`. Bands are applied per row in `19`; **Conception** uses the lower end — AI lacks product, Neo4j, and migration context unless heavily prompted.

Ordered to match [`19-delivery-estimate.csv`](19-delivery-estimate.csv) (Conception → Dev → Testing).

### Conception

| Workstream | Reduction | Rationale | EST IDs |
|------------|-----------|-----------|---------|
| Vision & scope | 25 % | Stakeholder alignment, phasing — little reusable context for AI | EST-CON-001 |
| Requirements lock | 25 % | Traceability validation needs domain ownership | EST-CON-002 |
| CRD & API spec | 30–35 % | OpenAPI/kubebuilder drafts help; field semantics and Helm parity need human lock | EST-CON-003 |
| Runtime contract | 28–30 % | Reconcile ordering, status model — high coupling to product decisions | EST-CON-004 |
| Architecture & ADR | 25 % | Trade-offs and boundaries — judgment over generation | EST-CON-005 |
| Security & dependencies | 25–28 % | Threat model and RBAC scope — context-specific | EST-CON-006 |
| Scope lock & roadmap | 25 % | Commitment dates and risk calls — not delegable | EST-CON-007 |

### Dev

| Workstream | Reduction | Rationale | EST IDs |
|------------|-----------|-----------|---------|
| Scaffold & tooling | 60–70 % | High AI leverage on kubebuilder, CI, and boilerplate | EST-DEV-001 |
| API types & validation | 55–65 % | CRD types and webhooks; human review on validation rules | EST-DEV-002 |
| Operator core | 55–60 % | Reconcile loops + RBAC; integration and edge cases remain human | EST-DEV-010 |
| Domain modules | 55–65 % | Reconcile logic per domain; review and Neo4j-specific behaviour | EST-DEV-020..080 |
| Observability & packaging | 50–55 % | Metrics, Helm chart — patterns exist but chart migration needs check | EST-DEV-090, EST-DEV-100 |
| Hardening & review | 30–40 % | Production readiness and ops doc — mostly human gate | EST-DEV-110 |

### Testing

| Workstream | Reduction | Rationale | EST IDs |
|------------|-----------|-----------|---------|
| Test strategy | 25–30 % | Pyramid and gates — product/test policy, same context gap as Conception | EST-TST-001 |
| Harness & fixtures | 35–45 % | Cluster + Neo4j realities; first-time debugging | EST-TST-010 |
| CI matrix | 40–45 % | Pipeline wiring; platform credentials and flakes stay human | EST-TST-020 |
| Unit tests | 60–70 % | Table-driven, pattern-heavy — good AI fit | EST-TST-030 |
| Integration (envtest) | 45–55 % | Correct API semantics and controller wiring | EST-TST-040 |
| E2E founder scenarios | 35–45 % | Full scenario authoring — cluster, startup, assertions | EST-TST-050 |
| E2E parametrized variants | 40–45 % | Mostly YAML + assertion deltas on existing harness | EST-TST-060 |
| AC spot checks | 40–45 % | Targeted assertions; depends on coverage gaps | EST-TST-070 |
| Stabilisation | 25–35 % | Empirical debugging — low AI leverage | EST-TST-080 |
| DoD validation run | 25–30 % | QA execution and sign-off — not authoring | EST-TST-090 |
| Post-V1 catalog completion | 35–45 % | Same bands as V1 E2E/AC rows | EST-TST-100..120 |

AI-assisted totals in `19` apply these bands per row. Recalibrate after the first domain module is shipped (velocity spike).

---

## Overhead not in `19`

Add explicitly before committing a release date:

| Item | Typical range |
|------|---------------|
| User-facing documentation (install, migrate, troubleshoot) | 10–15 d |
| Release engineering (signing, registry, changelog) | 5–8 d |
| Security review / penetration test coordination | 5–10 d |
| Field feedback buffer (first GA customers) | 10–20 d |

---

## Accuracy by project stage

| Stage | Artifacts locked | Expected accuracy | Use for |
|-------|------------------|-------------------|---------|
| **Now** | `19` directional + `04` catalog | ±40–50 % | Go/no-go, budget envelope, team sizing |
| **After P0 spec** | `09`–`12` locked | ±25 % | Release planning, milestone dates |
| **After scope lock** | `13` + `17` frozen | ±15 % | V1 commitment date |
| **After velocity spike** | Scaffold + one domain module shipped | ±10 % | Sprint-level planning |

---

## Traceability chain

```
01 FR  ──►  03 variants  ──►  04 scenario tests (TST-SCN-*)
  │              │
  └──►  02 AC  ──────────►  04 AC tests (TST-AC-*)
                                    │
                                    ▼
                            05 coverage summary (counts only)
                                    │
                    19 delivery estimate ◄── 18 effort model (rules)
                                    │
                    13 DoD V1 ◄─────┴─────► 17 roadmap
```

---

## Calibration method

1. Ship `EST-DEV-001` + `EST-DEV-010` (scaffold + operator core).
2. Ship one domain module (`EST-DEV-020` Neo4jDatabase) with its test pyramid slice (`EST-TST-030` + `EST-TST-050`).
3. Compare actual person-days vs `19` estimates per workstream.
4. Adjust reduction factors and marginal E2E cost in this document; update `19` rows.

Do not recalibrate by summing columns in `04` — that file has no effort data.
