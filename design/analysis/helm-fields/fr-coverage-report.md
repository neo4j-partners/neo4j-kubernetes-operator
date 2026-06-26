# FR coverage report — 2026-06-22

**Validator**: fr-helm-coverage-validator  
**Helm source**: `helm-charts/neo4j/values.yaml` → [`_index.csv`](_index.csv) (93 paths)  
**FR source**: `design/01-functional_requirements.csv` (116 rows)

---

## Summary

| Metric | Count |
|--------|------:|
| Helm paths with `client_need` | 93 |
| Mapped to ≥1 FR (`fr_ids` in index) | 90 |
| Out of scope (Helm-only / operator N/A) | 3 |
| Missing from FR (in-scope, unmapped) | **0** |
| **Coverage %** (mapped / in-scope) | **100%** |

**Gate G-FR-1**: **PASS** — all V1-scoped Helm client needs mapped or explicitly out-of-scope.

Script: `python design/analysis/helm-fields/scripts/check-fr-coverage.py` → `90/93 (96.8%)` raw; 3 gaps are packaging/Helm workarounds (below).

---

## Coverage by domain

| Domain | Paths | Covered | Partial | Missing / OOS |
|--------|------:|--------:|--------:|---------------|
| packaging | 3 | 0 | 0 | 3 OOS |
| neo4j-core | 8 | 8 | 0 | 0 |
| topology-lifecycle | 9 | 9 | 0 | 0 |
| storage | 17 | 17 | 0 | 0 |
| network | 11 | 11 | 0 | 0 |
| tls | 4 | 4 | 0 | 0 |
| config | 5 | 5 | 0 | 0 |
| plugins | 2 | 2 | 0 | 0 |
| security | 2 | 2 | 0 | 0 |
| health | 3 | 3 | 0 | 0 |
| scheduling | 14 | 14 | 0 | 0 |
| observability | 4 | 4 | 0 | 0 |
| resilience | 1 | 1 | 0 | 0 |
| image | 8 | 8 | 0 | 0 |
| other | 2 | 2 | 0 | 0 |

---

## Out of scope (documented — no new FR)

| helm_path | client_need (short) | status | notes |
|-----------|---------------------|--------|-------|
| `nameOverride` | K8s name prefix | out_of_scope | `metadata.name` on `Neo4j` CR |
| `fullnameOverride` | Full resource prefix | out_of_scope | Same |
| `disableLookups` | ArgoCD/Helm lookup skip | out_of_scope | No Helm in operator reconcile |

---

## FR reverse coverage (index → FR file)

| Metric | Count |
|--------|------:|
| Unique FR IDs referenced in `_index.csv` | 74 |
| FR rows never referenced by any helm path | 43 |

**Expected unreferenced FRs** (not gaps):

- **Operator lifecycle** (`OP-1-*`, `OP-2-*`) — not Helm values
- **Level-3 variants** (`NEO-3-007-PCMB-*`, `NEO-3-013-*`, …) — covered by parent level-2 via `Requires IDs` chain
- **Deferred V1** (`NEO-2-001-MODE-01` Standalone mode FR at L2 — mapped via `NEO-1-001` / topology paths)

No proposed **new** level-2/3 FR rows required for Helm parity at this stage.

---

## Partial / implicit coverage (watch list)

| Area | FR | notes |
|------|-----|-------|
| `secondaries.read` | `NEO-2-011`, `NEO-3-011-*` | Operator net-new pool — FR describes cluster scale, not fixed `read` key |
| Scale ordinal safety | `NEO-3-011-SRV-01` | BDR-009 addresses semantics beyond FR wording |
| Multi-cluster exposure | `NEO-3-007-MULTI-*` | `versioning=deferred` on helm path |

Recommend: when BDR-009 accepted, add AC row for tail-only vs per-pool STS — **do not edit FR CSV without approval**.

---

## Requires IDs updates

None required for Helm coverage gate.

---

## Variant matrix / AC gaps

Defer until BDR-009 / BDR-007 accepted:

| Related FR | Suggested follow-up |
|------------|---------------------|
| `NEO-3-011-SRV-01` | AC for operator-managed ENABLE SERVER (replaces Helm Job) |
| `NEO-3-007-SVC-01` | AC for `connectivity.external` default-off |

---

## Obsolete FR candidates

None identified from Helm analysis alone.

---

## Actions

- [x] `fr-coverage-report.md` populated
- [x] `validation-gaps.md` updated
- [ ] User approved new FR rows — **N/A** (none proposed)
- [ ] BDR-009 accepted → optional AC additions
