# Risks — adoption stoppers

Risks that must be addressed in the **product and delivery model** — not only in technical design — before treating the operator as production-grade.

Mitigations marked *to define* are open product decisions; track resolution in [`14-open-questions.md`](14-open-questions.md).

---

## Operational risk

Operator bugs can impact production clusters. Unlike Helm, a continuously running controller can actively modify resources and unintentionally introduce issues if reconciliation logic is incorrect.

**Mitigations to define**:

- Blast-radius limits — **namespace scope** (V1: [BDR-003](../02-technical-design/decision-records/business/003-operator-install-scope.md)), dry-run, pause reconciliation
- Conservative defaults on the MVP path
- Extensive test pyramid — [`../02-technical-design/15-test-strategy.md`](../02-technical-design/15-test-strategy.md), V1 tests in [`../02-technical-design/04-test_catalog.csv`](../02-technical-design/04-test_catalog.csv) (`V1=Yes`)
- Staged rollout and clear runbooks for operator failure modes

---

## Support model

Without a clear support model, the operator risks being perceived as an alternative deployment mechanism rather than a production-grade operational platform. Customers running production Neo4j fleets on Kubernetes typically require an officially supported solution with:

- clear ownership (who maintains the controller, CRDs, and compatibility matrix),
- maintenance commitments and release cadence,
- upgrade compatibility guarantees across operator and Neo4j versions,
- defined escalation paths (L1 → engineering).

**Mitigations to define**:

- Formal support tier and SLA alignment with Neo4j Enterprise
- **Product Engineering** as long-term maintainer — not PS alone

---

## Insufficient differentiation from Helm

If the operator only covers install, upgrade, backup, and delete, customers may see little value compared to the existing Helm chart and GitOps workflows (Argo CD, Flux).

**Mitigations**:

- Articulate operator-native value in [`03-goals.md`](03-goals.md) — continuous reconciliation, drift correction, unified status, per-pool scale automation
- **V1 scope must exceed** “Helm with a reconciler wrapper” even without backup in V1
- Day-2 CRDs (`Neo4jDatabase`, backup/restore) remain **V2** differentiators — document in [`13-roadmap.md`](13-roadmap.md)

---

## Migration path

The operator may primarily benefit **new deployments** rather than the installed base. Adoption by existing customers depends on a supported migration strategy from Helm-managed deployments. If migration requires cluster reinstallation, ownership transfer, or manual backup/restore procedures, adoption may be significantly reduced.

**Mitigations to define**:

- [`../02-technical-design/11-helm-mapping.md`](../02-technical-design/11-helm-mapping.md) as the migration contract *(to be authored)*
- Supported in-place migration paths where feasible
- Explicit documentation of breaking vs non-breaking transitions
- Treat migration as a **first-class deliverable**, not an afterthought

---

## Organizational constraint — PS is not an engineering team

This design package is authored from **Professional Services (PS)** context. PS can drive discovery, requirements, field validation, and early prototypes — but PS is **not** a product engineering organization and cannot alone guarantee:

- long-term code ownership and on-call,
- Neo4j release train integration,
- official GA/support commitments,
- sustained investment across Neo4j versions.

**For a durable operator, PS must partner with Product Engineering** (and ideally align with an existing Helm/operator roadmap owner). Without that partnership, the project remains a field experiment — useful for demos and selected engagements, but an **adoption stopper** for enterprise customers who require vendor-backed software.

**Recommended next step**: secure Product sponsorship before V1 commitment — define RACI (PS vs Product vs Support), target GA criteria, and handoff path from PS-led design to engineering-owned delivery.

---

## Technical risks (from reference operator experience)

| Risk | Mitigation | V1 |
|------|------------|-----|
| **Long Neo4j store migrations** on image bump | Pre-flight dry-run; block upgrade with clear status | V2+ (upgrade deferred) |
| **Misconfigured cloud identity** for backup | Webhook validation; `IdentityInvalid` condition | V2+ |
| **cert-manager outage** | BYO certs in V1; optional fallback certs documented post-V1 | V1 uses BYO cluster TLS only |
| **StatefulSet volume resize limits** | Document CSI / manual resize path; do not auto-expand in V1 | Doc + BDR-005 |

---

## Risk register (summary)

| Risk | Severity | V1 mitigation | Owner |
|------|----------|---------------|-------|
| Reconciliation bug blast radius | High | Single-namespace scope; test catalog V1 | Engineering |
| No vendor support model | High | Open — Product + Support | Product |
| Weak vs Helm + GitOps | Medium | MVP goals G4–G7; roadmap for V2 | Product + PS |
| No Helm migration | Medium | `11-helm-mapping` deliverable | PS + Product |
| PS-only ownership | High | RACI + Product sponsorship | PS + Product |
