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

Each file follows [ADR format](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions):

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
| [BDR-001](business/neo4j/001-single-neo4j-crd.md) | Single `Neo4j` CRD instead of `Neo4jStandalone` + `Neo4jCluster` | accepted |
| [BDR-002](business/neo4j/002-neo4j-crd-topology.md) | `Neo4j` CRD topology — modes, primaries / secondaries (`analytics`, `read`) | accepted |
| [BDR-003](business/operator/003-operator-install-scope.md) | Operator install scope — single namespace for V1; dedicated operator namespace; multi / cluster-wide deferred | proposed |
| [BDR-004](business/neo4j/004-neo4j-plugin-topology.md) | Plugin model — Option E (`pluginDefinitions` + pool refs) | accepted |
| [BDR-005](business/neo4j/005-storage-volume-mode.md) | Storage — Option D: `spec.volumes` (`Dynamic` + `Existing` + aux `Share`) + Option E escape hatches | accepted |
| [BDR-006](business/neo4j/007-tls-trust-model.md) | TLS trust — Option B: `secretName` + `subPath` (BYO) + mTLS + optional cert-manager (default off) | accepted |
| [BDR-007](business/neo4j/006-service-exposure-connectivity.md) | Service exposure — **Option E**: `listeners` + `service` + `reverseProxy` + `ingress.rules`; Amendments B–F | accepted |
| [BDR-008](business/neo4j/008-neo4j-config-surface.md) | Config surface — **Option A**: `spec.config` + `spec.jvm` + `spec.apoc` (neo4j.conf / apoc.conf passthrough) | accepted |
| [BDR-009](business/neo4j/009-scale-pool-ordinal-semantics.md) | Scale — **Option B**: one StatefulSet per pool (`primary`, `analytics`, `read`) | accepted |
| [BDR-010](business/neo4j/010-neo4j-features-catalog.md) | `spec.features` — **Option C accepted**: gates + colocated neo4j.conf mirrors; CFG-FEAT coherence with `config` | accepted |
| [BDR-011](business/neo4j/011-https-connector-tls-coupling.md) | HTTPS connector ↔ Service exposure ↔ TLS/mTLS coupling rules — **Option A accepted** | accepted |
| [BDR-012](business/identity-user-roles/012-identity-management.md) | Neo4j identity — **Option C proposed**: `Neo4jUser` + `Neo4jRole` + `Neo4jGrant`; reconcile Role → Grant → User; **post-V1** | proposed |

### Architecture (`architecture/`)

| ID | Title | Status |
|----|-------|--------|
| [ADR-001](architecture/001-crd-validation-process.md) | CRD validation process — CEL first, webhook for external lookups | accepted |
| [ADR-002](architecture/002-package-layering.md) | Package layering — `render` / `domain` / `controller` | proposed |
| [ADR-003](architecture/003-neo4j-reconcile-pipeline.md) | `Neo4j` reconcile pipeline order | proposed |
| [ADR-004](architecture/004-status-and-conditions.md) | Status and conditions writer | proposed |
| [ADR-005](architecture/005-render-conventions.md) | Render conventions — naming, labels, owner references | proposed |
| [ADR-006](architecture/006-apply-and-idempotency.md) | Apply strategy and idempotent reconcile | proposed |
| [ADR-007](architecture/007-formation-and-bolt.md) | Formation and Bolt client usage | proposed |
| [ADR-008](architecture/008-finalizers-and-deletion.md) | Finalizers and deletion | proposed |
| [ADR-009](architecture/009-watches-and-predicates.md) | Watches and predicates | proposed |
| [ADR-010](architecture/010-operator-deployment.md) | Operator deployment and HA | proposed |
| [ADR-011](architecture/011-implementation-language.md) | Operator implementation language — **Go** (kubebuilder / controller-runtime); Strimzi patterns in Go, not Java | proposed |
| [ADR-012](architecture/012-testing-strategy.md) | Testing strategy — `src/` dev tests (Gate 1) vs `tests/` e2e matrix (Gate 2); TDD optional | proposed |

---

## When to write which

| Question | Folder |
|----------|--------|
| What CRDs / fields does the **user** see? | `business/` |
| What is in / out of **V1** from a customer perspective? | `business/` |
| Helm parity, defaults, migration impact? | `business/` |
| How do we **structure Go packages** or the reconcile pipeline? | `architecture/` |
| **Which language** implements the operator? | `architecture/` |
| CEL vs webhook, envtest vs kind for a gate? | `architecture/` |
| Naming of K8s child objects, labels, apply strategy? | `architecture/` |
| Bolt client usage, formation sequence, finalizers? | `architecture/` |

**Classifier:** `.cursor/skills/decision-classifier-bdr-vs-adr` · **Backlog:** `.cursor/skills/operator-architecture-orchestrator/architecture-backlog.md`

A business decision may trigger one or more architecture ADRs — cross-link them in **References** (`Triggers: ADR-NNN` in BDR; `Depends on: BDR-NNN` in ADR).
