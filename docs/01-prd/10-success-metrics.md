# Success metrics

How we measure whether the operator delivers value. Quantified targets from the reference PRD ([`../00-discovery/export.md`](../00-discovery/export.md)) are **adapted to V1 scope** — full enterprise targets apply post-V1.

---

## V1 success metrics (MVP)

| Metric | Target | Measurement | Links |
|--------|--------|-------------|-------|
| **Time to first Ready cluster** | Standalone < 3 min; 3-node Cluster < 5 min on kind | E2E test timing | NFR-UX-001, CW-1 |
| **Deploy steps vs Helm** | 1 `Neo4j` CR vs N Helm releases for N-node cluster | Documented comparison | G1–G2 in [`03-goals.md`](03-goals.md) |
| **Scale without manual Cypher** | 0 manual `ENABLE SERVER` steps for supported pool scale | AC-NEO-SCALE | NEO-2-011 |
| **Drift correction** | Operator restores child object spec after manual edit | Integration test | OP-1-002 |
| **V1 test gate** | 100 % P0 tests with `V1=Yes` pass on reference platform | CI / release checklist | [`04-test-plan/04-test_catalog.csv`](../04-test-plan/04-test_catalog.csv) |
| **Scope clarity** | 0 undocumented V1 paths in getting-started | Doc review vs [`13-v1-scope-lock`](../00-discovery/13-v1-scope-lock.md) | Product |

---

## Product outcomes (directional — from reference PRD)

These express **why** customers adopt an operator beyond Helm. V1 contributes partially; full realisation is V2+.

| Outcome | Reference claim | V1 contribution | Full target phase |
|---------|-----------------|-----------------|-------------------|
| Deployment time | ~90 % reduction vs manual bootstrap | Single CR + automated STS/Services | **V1** (MVP path) |
| Config skew | Zero drift dev → prod | Declarative reconcile | **V1** |
| Upgrade duration | Minutes, no outage | Not in V1 | **V2** (NEO-2-012) |
| Backup automation | No custom cron / static keys | Not in V1 | **V2** (NEO-2-013) |
| Security RBAC audit | SOX-ready Git trail | Cluster TLS + K8s RBAC only | **V2+** (security CRDs) |
| Multi-team fleet | One operator, many namespaces | Single namespace in V1 | **V1.1+** |
| Non-Git onboarding | UI wizards | Not in V1 | **Roadmap** (optional) |

---

## Engineering health metrics

| Metric | V1 target | Notes |
|--------|-----------|-------|
| Reconcile error rate | Trending down; no silent failures | Conditions + Events |
| Admission webhook latency | P95 < 1 s (pragmatic V1) | Reference PRD: < 500 ms |
| Open BDR blockers for V1 | BDR-002, BDR-003 ratified | [`14-open-questions.md`](14-open-questions.md) |
| Product Engineering sponsorship | Decision recorded | [`11-risks.md`](11-risks.md) |

---

## Anti-metrics (what we are not optimising in V1)

- Feature parity with full reference PRD (backup, UI, security CRDs, fleet operator).
- Helm chart replacement on day one.
- Prometheus / OTEL completeness (OP-1-007 deferred).

---

## Review cadence

| When | Review |
|------|--------|
| V1 feature complete | P0 test pass rate + time-to-Ready benchmarks |
| Post-V1 each phase | Revisit quantified outcomes table; add backup / upgrade SLOs |
