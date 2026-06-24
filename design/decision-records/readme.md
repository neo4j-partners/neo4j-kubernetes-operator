# Architecture & business decision records

Immutable log of significant choices. Split by **who the decision serves** and **what it constrains**.

| Folder | Prefix | Audience | Examples |
|--------|--------|----------|----------|
| [`business/`](business/) | **BDR** | Product, users, support, technical writers | CRD inventory, naming, V1 scope, UX of the API |
| [`architecture/`](architecture/) | **ADR** | Implementers, reviewers | Layering, reconcile ordering, webhook vs CEL, package layout |

**Status**: `proposed` · `accepted` · `deprecated` · `superseded by BDR-00x`

---

## File naming

```
{folder}/{NNN}-{short-kebab-title}.md
```

Full ID in document header: **BDR-001**, **ADR-001** (not repeated in filename).

---

## Index

### Business (`business/`)

| ID | Title | Status |
|----|-------|--------|
| [BDR-001](business/001-single-neo4j-crd.md) | Single `Neo4j` CRD instead of `Neo4jStandalone` + `Neo4jCluster` | accepted |
| [BDR-002](business/002-neo4j-crd-topology.md) | Topology — `Standalone` \| `Cluster` with `primaries`, `secondaries`, `analytics` | accepted |
| [BDR-003](business/003-operator-install-scope.md) | Operator install scope | proposed |
| [BDR-004](business/004-neo4j-plugin-topology.md) | Plugin model — role refs + `pluginDefinitions` | accepted |
| [BDR-005](business/005-v1-full-crd-scope.md) | V1 full CRD scope — no deferred spec fields | accepted |

### Architecture (`architecture/`)

| ID | Title | Status |
|----|-------|--------|
| [ADR-001](architecture/001-crd-validation-process.md) | CRD validation — CEL first, webhook for external lookups | accepted |
| [ADR-002](architecture/002-helm-values-mapping.md) | Helm `values.yaml` → `Neo4j` spec mapping | accepted |
| [ADR-003](architecture/003-persistence-model.md) | Persistence — all volume roles in V1 | accepted |
| [ADR-004](architecture/004-scale-subresource.md) | `scale` subresource on `Neo4j` | accepted |

---

## When to write which

| Question | Folder |
|----------|--------|
| What CRDs / fields does the **user** see? | `business/` |
| What is in / out of **V1** from a customer perspective? | `business/` |
| How do we **structure Go packages** or the reconcile pipeline? | `architecture/` |
| CEL vs webhook, Helm mapping, persistence? | `architecture/` |

A business decision may trigger one or more architecture ADRs — cross-link them in **References**.
