# Delivery estimate — Neo4j Kubernetes Operator

Human-readable view of [`19-delivery-estimate.csv`](19-delivery-estimate.csv).

Calculation rules and assumptions → [`18-effort-model.md`](18-effort-model.md).  
Test **coverage** (not cost) → [`04-test_catalog.csv`](04-test_catalog.csv) and [`05-test_catalog_summary.csv`](05-test_catalog_summary.csv).

Each row below is a **deliverable**, not a test case. Effort is in **person-days**.

---

## Rollup

### V1 subset (`V1 = Yes`)

| Phase | Manual | AI-assisted |
|-------|--------|-------------|
| Conception | 50 | 35.5 |
| Dev | 187 | 70 |
| Testing | 198 | 97.5 |
| **Total V1** | **435** | **203** |

**Catalog coverage**: 138 V1 tests (61 scenario + 77 AC) — see `05`.

**Calendar hint (2 FTE, dev + QA in parallel)**: ~22–26 weeks manual, ~11–13 weeks AI-assisted, after P0 spec is locked. Conception runs first and is mostly sequential.

### Full scope (all rows)

| Phase | Manual | AI-assisted |
|-------|--------|-------------|
| Conception | 50 | 35.5 |
| Dev | 197 | 74 |
| Testing | 245 | 125.5 |
| **Total** | **492** | **235** |

Post-V1 rows: maintenance domain (`EST-DEV-080`) and remaining catalog automation (`EST-TST-100`–`120`).

---

## Phase 1 — Conception

| ID | Workstream | Deliverable | V1 | Manual | AI | Depends on |
|----|------------|-------------|----|--------|----|------------|
| EST-CON-001 | Vision & scope | `00-vision.md` — phasing + adoption stoppers | Yes | 4 | 3 | — |
| EST-CON-002 | Requirements lock | Review and freeze `01`, `02`, `03` | Yes | 3 | 2.5 | — |
| EST-CON-003 | CRD & API spec | `09-crd-spec/` — Neo4j, Neo4jDatabase, Neo4jBackup, Neo4jRestore | Yes | 15 | 9.5 | EST-CON-002 |
| EST-CON-004 | Runtime contract | `10-status-model`, `11-helm-mapping`, `12-reconciliation` | Yes | 12 | 8.5 | EST-CON-003 |
| EST-CON-005 | Architecture & ADR | `06`–`08` and P0 ADRs | Yes | 6 | 4.5 | EST-CON-002 |
| EST-CON-006 | Security & dependencies | `14-dependencies`, `16-security` | Yes | 5 | 3.5 | EST-CON-004 |
| EST-CON-007 | Scope lock & roadmap | `13-dod-v1`, `17-roadmap` | Yes | 5 | 4 | EST-CON-003, EST-CON-004 |

**Subtotal**: 50 d manual · 35.5 d AI-assisted

P0 blocking items: EST-CON-003 (CRD spec) and EST-CON-004 (runtime contract) before implementation starts.

---

## Phase 2 — Dev

| ID | Workstream | Deliverable | V1 | Manual | AI | Depends on |
|----|------------|-------------|----|--------|----|------------|
| EST-DEV-001 | Scaffold & tooling | kubebuilder repo, CI, lint, container image | Yes | 12 | 4 | EST-CON-005 |
| EST-DEV-002 | API types & validation | `api/v1beta1` types, webhooks, CEL rules | Yes | 9 | 3 | EST-DEV-001, EST-CON-003 |
| EST-DEV-010 | Operator core | Bootstrap, Reconciler, Status, Upgrade, Uninstall, RBAC | Yes | 37 | 14 | EST-DEV-002 |
| EST-DEV-020 | Domain workload | Neo4jDatabase — SS, cluster, scale, upgrade, probes | Yes | 35 | 13 | EST-DEV-010 |
| EST-DEV-030 | Domain persistence | Neo4jPersistence — storage modes, volume roles | Yes | 10 | 4 | EST-DEV-020 |
| EST-DEV-040 | Domain connectivity | Neo4jConnectivity — services, ports, ServiceMonitor | Yes | 9 | 3 | EST-DEV-020 |
| EST-DEV-050 | Domain trust | Neo4jTrust — credentials, TLS, secrets, cert reload | Yes | 14 | 5 | EST-DEV-020 |
| EST-DEV-060 | Domain server config | Neo4jServerConfig — neo4j.conf, JVM, APOC, restart policy | Yes | 12 | 4 | EST-DEV-020 |
| EST-DEV-070 | Domain backup & restore | Neo4jBackup, Neo4jRestore — jobs and storage | Yes | 26 | 10 | EST-DEV-020 |
| EST-DEV-080 | Domain maintenance | Neo4jMaintenance — offline mode, dump/load | **No** | 10 | 4 | EST-DEV-020 |
| EST-DEV-090 | Observability | Structured logs, Prometheus metrics | Yes | 4 | 2 | EST-DEV-010 |
| EST-DEV-100 | Packaging | Helm chart, install manifests, release artifacts | Yes | 7 | 3 | EST-DEV-010 |
| EST-DEV-110 | Hardening & review | Production readiness, code review, ops documentation | Yes | 12 | 5 | EST-DEV-020..070 |

**Subtotal V1**: 187 d manual · 70 d AI-assisted  
**Subtotal full**: 197 d manual · 74 d AI-assisted

Dev effort is **deduplicated** — one row per domain module, not per test in `04`.

---

## Phase 3 — Testing

| ID | Workstream | Deliverable | V1 | Manual | AI | Depends on |
|----|------------|-------------|----|--------|----|------------|
| EST-TST-001 | Test strategy | `15-test-strategy.md` — pyramid, CI gates | Yes | 4 | 3 | EST-CON-007 |
| EST-TST-010 | Harness & fixtures | kind setup, install helper, wait Ready, driver assertions | Yes | 30 | 12 | EST-DEV-001 |
| EST-TST-020 | CI matrix | Parametrized runs — GKE, EKS, AKS, OpenShift | Yes | 10 | 4 | EST-TST-010 |
| EST-TST-030 | Unit tests | `render/` + `domain/` table-driven per module | Yes | 30 | 10 | EST-DEV-010..070 |
| EST-TST-040 | Integration (envtest) | Controller tests without real Neo4j | Yes | 25 | 10 | EST-DEV-010..070 |
| EST-TST-050 | E2E founder scenarios | One full scenario per variant group (~18 V1 groups) | Yes | 35 | 20 | EST-TST-010 |
| EST-TST-060 | E2E parametrized variants | Remaining V1 scenarios via YAML fixtures | Yes | 28 | 16 | EST-TST-050 |
| EST-TST-070 | AC spot checks | P0 AC tests not covered by scenario tests | Yes | 18 | 10 | EST-TST-030, EST-TST-040 |
| EST-TST-080 | Stabilisation | Flakiness budget — HA, TLS, backup/restore debug | Yes | 12 | 8 | EST-TST-050 |
| EST-TST-090 | DoD V1 validation | Execute P0 catalog, sign-off report | Yes | 6 | 4.5 | EST-TST-050..070 |
| EST-TST-100 | E2E non-V1 scenarios | Remaining scenario tests beyond V1 DoD | No | 20 | 12 | EST-TST-060 |
| EST-TST-110 | AC non-V1 coverage | Remaining AC tests beyond V1 DoD | No | 15 | 9 | EST-TST-070 |
| EST-TST-120 | Maintenance E2E | Neo4jMaintenance scenario automation | No | 12 | 7 | EST-DEV-080, EST-TST-010 |

**Subtotal V1**: 198 d manual · 97.5 d AI-assisted  
**Subtotal full**: 245 d manual · 125.5 d AI-assisted

### Testing cost model

```
                    ┌─────────────────────────────────────┐
                    │  EST-TST-010  Harness  (30 d)     │  ← one-time
                    │  EST-TST-020  CI matrix (10 d)     │  ← one-time
                    └──────────────┬──────────────────────┘
                                   │ amortized across
           ┌───────────────────────┼───────────────────────┐
           ▼                       ▼                       ▼
    EST-TST-030/040          EST-TST-050/060         EST-TST-070
    Unit + envtest           E2E founders +          AC spot checks
    (55 d)                   variants (63 d)         (18 d)
```

A large variant matrix in `04` is the **return on** harness investment — same tests, parametrized YAML, iso runs across platforms. It is not `N tests × fixed cost`.

---

## Dependency graph (simplified)

```
EST-CON-001..007  (Conception)
        │
        ▼
EST-DEV-001 → EST-DEV-002 → EST-DEV-010 → EST-DEV-020..070
        │                              │
        │                              ├── EST-DEV-090, EST-DEV-100, EST-DEV-110
        │
        └── EST-TST-010 → EST-TST-020
                    │
                    └── EST-TST-050 → EST-TST-060 → EST-TST-090
                              │
                    EST-TST-030/040 (parallel with E2E)
```

Harness work (`EST-TST-010`) can start once scaffold exists (`EST-DEV-001`), in parallel with operator core development.

---

## Overhead not included

Add explicitly before a release date commitment — see `18-effort-model.md`:

| Item | Range |
|------|-------|
| User-facing documentation | 10–15 d |
| Release engineering | 5–8 d |
| Security review | 5–10 d |
| Field feedback buffer | 10–20 d |

---

## Maintenance

- **Source of truth for numbers**: `19-delivery-estimate.csv` (edit there first).
- **Regenerate this file** when CSV rows change, or keep both in sync manually.
- **Recalibrate** after shipping scaffold + one domain module — compare actuals vs estimates per workstream.
