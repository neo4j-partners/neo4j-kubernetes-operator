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

| Part | Rule | Example |
|------|------|---------|
| `NNN` | Zero-padded sequence **per folder** (001, 002…) | `001` |
| `short-kebab-title` | Lowercase, hyphens, ≤8 words | `single-neo4j-crd` |

Full ID in document header: **BDR-001**, **ADR-001** (not repeated in filename).

---

## Document template

Each file follows [Michael Nygard's ADR format](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions):

1. **Title** + status + date  
2. **Context** — forces at play, no judgement  
3. **Analysis** — market survey, pros/cons per option (optional but recommended for BDRs)  
4. **Decision** — full sentences, active voice ("We will…")  
5. **Consequences** — positive, negative, neutral (post-decision impacts)  

Optional: **Alternatives considered**, **References** (FR IDs, `09-crd-spec/`, industry operators).

---

## Index

### Business (`business/`)

| ID | Title | Status |
|----|-------|--------|
| [BDR-001](business/001-single-neo4j-crd.md) | Single `Neo4j` CRD instead of `Neo4jStandalone` + `Neo4jCluster` | accepted |
| [BDR-002](business/002-neo4j-crd-topology.md) | `Neo4j` CRD topology — modes, cores / readReplicas / readGDSReplicas, user guidance | proposed |
| [BDR-003](business/003-operator-install-scope.md) | Operator install scope — single namespace for V1; multi / cluster-wide deferred | proposed |
| [BDR-004](business/004-neo4j-plugin-topology.md) | Plugin model — options A–E under review (refs + definitions favored) | proposed |

### Architecture (`architecture/`)

| ID | Title | Status |
|----|-------|--------|
| — | *(none yet)* | — |

---

## When to write which

| Question | Folder |
|----------|--------|
| What CRDs / fields does the **user** see? | `business/` |
| What is in / out of **V1** from a customer perspective? | `business/` |
| How do we **structure Go packages** or the reconcile pipeline? | `architecture/` |
| CEL vs webhook, envtest vs kind for a gate? | `architecture/` |

A business decision may trigger one or more architecture ADRs — cross-link them in **References**.
