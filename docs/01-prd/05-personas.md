# Personas

Target users for the Neo4j Kubernetes Operator. Informs requirements (`07-functional-requirements.csv`), acceptance criteria, and documentation tone.

Reference PRD personas (Alex, Sia, Dana, Olivia, Ada) are mapped below to this project's roles.

---

## Primary personas

| Persona | Alias (ref. PRD) | Primary need | V1 relevance |
|---------|------------------|--------------|--------------|
| **Platform engineer** | Alex | Declarative Neo4j lifecycle on Kubernetes; GitOps-friendly CR; namespace-scoped blast radius | **Core** — installs operator, defines `Neo4j` CR, integrates with CI/CD |
| **Neo4j administrator** | Dana | Cluster formation, scale, config changes without manual `kubectl` / Cypher ops | **Core** — topology, scale, config passthrough, status |
| **PS consultant** | — | Repeatable, supportable deployment patterns across customer engagements | **Core** — samples, predictable MVP path, runbooks (post-V1 docs) |
| **Support engineer** | — | Clear status, conditions, escalation path to engineering | **Core** — `Ready` / `Error` conditions; deep diagnostics post-V1 |

---

## Secondary personas (V2+)

| Persona | Alias (ref. PRD) | Primary need | When |
|---------|------------------|--------------|------|
| **Security / compliance officer** | Sia | TLS, RBAC, audit, regulated deployment patterns | V1: cluster TLS + namespace scope; V2+: [BDR-012](../../02-technical-design/decision-records/business/012-identity-management.md) identity CRDs |
| **SRE / observability owner** | — | Metrics, alerts, ServiceMonitor integration | Post-V1 ([BDR-010](../02-technical-design/decision-records/business/010-neo4j-features-catalog.md)) |
| **Backup operator** | — | Scheduled backups, restore, DR | Post-V1 (entire backup domain deferred) |
| **Business analyst** | Olivia | Self-service test DB without YAML | Roadmap — Web UI not in FR scope |
| **Auditor** | Ada | Exportable security / change audit trail | V2+ — security CRDs + Events |

---

## Persona → V1 capability map

```
Platform engineer     →  OP-1-001 install · OP-2-001-SCOPE-01 · YAML packaging
Neo4j administrator   →  NEO-1-001/002 · NEO-2-011 scale · NEO-2-010 config change
PS consultant         →  MVP samples · 13-v1-scope-lock (predictable scope)
Support engineer      →  OP-1-003 status · AC-OP-STATUS-*
```

User stories → [`06-user-stories.md`](06-user-stories.md).
