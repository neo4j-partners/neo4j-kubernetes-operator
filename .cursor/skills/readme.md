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

**Start here:** [`design/analysis/helm-fields/LAUNCH.md`](../design/analysis/helm-fields/LAUNCH.md)

**Rules:** [`.cursor/rules/`](../rules/) — terminology, output format, BDR format
