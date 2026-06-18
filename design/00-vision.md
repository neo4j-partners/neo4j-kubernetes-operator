# Vision — Neo4j Kubernetes Operator

Problem statement, personas, goals / non-goals, and V1 / V2 phasing. Anchors all downstream decisions in `01`–`19`.

**Status**: partial — risks and organizational constraints captured; personas and phasing narrative to complete.

---

## Problem statement

*(To be completed — summarize why a Kubernetes operator is needed beyond the existing Helm chart and GitOps workflows.)*

---

## Goals / non-goals

*(To be completed — link to V1 scope in `01-functional_requirements.csv` and `13-v1-scope-lock.md`.)*

---

## What could go wrong? (Adoption stoppers)

These risks must be addressed in the product and delivery model — not only in technical design — before treating the operator as a production-grade platform.

### Operational risk

Operator bugs can impact production clusters. Unlike Helm, a continuously running controller can actively modify resources and unintentionally introduce cluster-wide issues if reconciliation logic is incorrect.

**Mitigations to define**: blast-radius limits (namespace scope, dry-run, pause reconciliation), conservative defaults, extensive test pyramid (`15-test-strategy.md`), staged rollout, and clear runbooks for operator failure modes.

### Support model

Without a clear support model, the operator risks being perceived as an alternative deployment mechanism rather than a production-grade operational platform. Customers running production Neo4j fleets on Kubernetes typically require an officially supported solution with:

- clear ownership (who maintains the controller, CRDs, and compatibility matrix),
- maintenance commitments and release cadence,
- upgrade compatibility guarantees across operator and Neo4j versions,
- defined escalation paths (L1 → engineering).

**Mitigations to define**: formal support tier, SLA alignment with Neo4j Enterprise, and Product Engineering as long-term maintainer — not PS alone.

### Insufficient differentiation from Helm

If the operator only covers install, upgrade, backup, and delete, customers may see little value compared to the existing Helm chart and GitOps workflows (Argo CD, Flux).

**Mitigations to define**: articulate operator-native value — continuous reconciliation, drift correction, day-2 CRDs (`Neo4jDatabase`, backup/restore), status model, multi-instance lifecycle — in `00` goals and customer-facing positioning. V1 scope must exceed "Helm with a reconciler wrapper."

### Migration path

The operator may primarily benefit **new deployments** rather than the installed base. Adoption by existing customers depends on a supported migration strategy from Helm-managed deployments. If migration requires cluster reinstallation, ownership transfer, or manual backup/restore procedures, adoption may be significantly reduced.

**Mitigations to define**: `11-helm-mapping.md` as the migration contract, supported in-place migration paths (where feasible), and explicit documentation of breaking vs non-breaking transitions. Treat migration as a first-class deliverable, not an afterthought.

### Organizational constraint — PS is not an engineering team

This design package is authored from **Professional Services (PS)** context. PS can drive discovery, requirements, field validation, and early prototypes — but PS is **not** a product engineering organization and cannot alone guarantee:

- long-term code ownership and on-call,
- Neo4j release train integration,
- official GA/support commitments,
- sustained investment across Neo4j versions.

**For a durable operator, PS must partner with Product Engineering** (and ideally align with an existing Helm/operator roadmap owner). Without that partnership, the project remains a field experiment — useful for demos and selected engagements, but an **adoption stopper** for enterprise customers who require vendor-backed software.

**Recommended next step**: secure Product sponsorship before V1 commitment — define RACI (PS vs Product vs Support), target GA criteria, and handoff path from PS-led design to engineering-owned delivery.

---

## Personas

*(To be completed.)*

| Persona | Primary need |
|---------|--------------|
| Platform engineer | Declarative Neo4j lifecycle on Kubernetes |
| Neo4j admin | Backup, restore, upgrade without manual kubectl |
| PS consultant | Repeatable deployment patterns across customers |
| Support engineer | Clear status, logs, escalation to engineering |

---

## V1 / V2 phasing

*(To be completed — cross-reference `01` V1 column and `17-roadmap.md`.)*

---

## Open questions

| # | Question | Owner | Blocks |
|---|----------|-------|--------|
| 1 | Product Engineering sponsorship and long-term ownership? | Product + PS | GA, support model |
| 2 | Official support tier and SLA for operator-managed deployments? | Product + Support | Enterprise adoption |
| 3 | Supported Helm → operator migration path for installed base? | PS + Product | Migration risk |
| 4 | Operator value proposition vs Helm + GitOps — what is V1-only? | Product + PS | Differentiation risk |
