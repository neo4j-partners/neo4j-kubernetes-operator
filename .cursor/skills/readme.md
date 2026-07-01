# Project skills — Neo4j operator design

Cursor Agent Skills for the Helm → CRD design pipeline. Invoke with `@skill-name` in chat.

| Skill | Invoke when |
|-------|-------------|
| [neo4j-operator-design-orchestrator](neo4j-operator-design-orchestrator/SKILL.md) | Full or phased pipeline |
| [helm-values-inventory](helm-values-inventory/SKILL.md) | Build / refresh `_index.csv` |
| [helm-template-tracer](helm-template-tracer/SKILL.md) | Trace values → templates |
| [neo4j-client-need-mapper](neo4j-client-need-mapper/SKILL.md) | Client need + Neo4j docs |
| [helm-field-categorizer](helm-field-categorizer/SKILL.md) | Write `fields/*.md` |
| [helm-semantic-concern-mapper](helm-semantic-concern-mapper/SKILL.md) | Scattered Helm paths → semantic concerns |
| [crd-synthesis-analyst](crd-synthesis-analyst/SKILL.md) | Aggregation + CRD mapping |
| [api-versioning-classifier](api-versioning-classifier/SKILL.md) | Breaking vs safe |
| [bdr-author-neo4j-operator](bdr-author-neo4j-operator/SKILL.md) | Draft BDR-005+ |
| [design-consistency-reviewer](design-consistency-reviewer/SKILL.md) | Final review gate |
| [fr-helm-coverage-validator](fr-helm-coverage-validator/SKILL.md) | FR ↔ Helm completeness audit |

### Phase 2 — software architecture

| Skill | Invoke when |
|-------|-------------|
| [operator-benchmark-analyst](operator-benchmark-analyst/SKILL.md) | Study CNPG/Strimzi/ECK/MongoDB — code, RBAC, cloud, quality |
| [operator-architecture-orchestrator](operator-architecture-orchestrator/SKILL.md) | ADR backlog, reconcile design, package layout |
| [decision-classifier-bdr-vs-adr](decision-classifier-bdr-vs-adr/SKILL.md) | Unsure BDR vs ADR for a topic |
| [adr-author-neo4j-operator](adr-author-neo4j-operator/SKILL.md) | Draft ADR-002+ / ADR-011+ |

**Start here:** [`design/analysis/helm-fields/LAUNCH.md`](../design/analysis/helm-fields/LAUNCH.md) (phase 1) · [`docs/02-technical-design/architecture/readme.md`](../docs/02-technical-design/architecture/readme.md) (phase 2)

**Rules:** [`.cursor/rules/`](../rules/) — terminology, BDR/ADR format, layering, classification
