---
name: decision-classifier-bdr-vs-adr
description: >-
  Classifies Neo4j operator design decisions as BDR (business/API) or ADR
  (software architecture). Use when unsure where to document a choice, when
  splitting a topic across records, or before drafting a new decision record.
---

# Decision classifier — BDR vs ADR

## Quick rule

| If the answer changes… | Record |
|------------------------|--------|
| What users **declare** in YAML (`spec`, CRD inventory, defaults, V1 scope) | **BDR** → `docs/02-technical-design/decision-records/business/` |
| How **implementers** structure code, reconcile, validate, test, deploy | **ADR** → `docs/02-technical-design/decision-records/architecture/` |

**One business choice often needs one ADR** — cross-link both ways in **References**.

Canonical index: [`decision-records/readme.md`](../../../docs/02-technical-design/decision-records/readme.md)

---

## Decision tree

```
New design question
│
├─ Would a cluster admin see or configure it in a CRD field?
│  ├─ Yes → BDR (unless purely internal operator ConfigMap — then ADR)
│  └─ No  → continue
│
├─ Does it affect Helm → CRD field mapping or customer-facing behaviour?
│  ├─ Yes → BDR
│  └─ No  → continue
│
├─ Is it Go package layout, reconcile order, client usage, or test harness?
│  └─ Yes → ADR
│
├─ Is it validation rule *semantics* (what is invalid)?
│  ├─ User-visible message / rule meaning → document in validation.md + cite BDR
│  └─ CEL vs webhook vs reconciler *placement* → ADR
│
└─ Still mixed?
   └─ Split: BDR = contract; ADR = implementation. Never one file for both.
```

---

## BDR topics (business/)

| Domain | Examples | Existing |
|--------|----------|----------|
| CRD inventory & naming | Single `Neo4j` CRD | BDR-001 |
| Topology & pools | primaries / secondaries | BDR-002, BDR-009 |
| Operator install scope | single-ns vs cluster-wide | BDR-003 |
| Plugins | `pluginDefinitions` | BDR-004 |
| Storage model | `spec.volumes` | BDR-005 |
| TLS trust surface | `spec.trust` | BDR-006, BDR-011 |
| Connectivity | listeners, service, ingress | BDR-007 |
| Config surface | config / jvm / apoc | BDR-008 |
| Features catalog | `spec.features` | BDR-010 |
| Identity CRDs | User / Role / Grant | BDR-012 |
| Database CRD | `Neo4jDatabase` | BDR-013 |
| V1 scope / non-goals | what ships when | `13-v1-scope-lock.md`, PRD |

**BDR must include**: options with YAML sketches, comparison table, customer impact, Helm parity column.

---

## ADR topics (architecture/)

| Domain | Examples | Status |
|--------|----------|--------|
| **Benchmark synthesis** | Code layout patterns from CNPG/Strimzi/ECK | backlog → ADR-011 |
| **Dependencies** | go.mod policy, controller-runtime pin, third-party allowlist | backlog → ADR-012 |
| **RBAC** | Operator Role/ClusterRole, workload SA, aggregate roles | backlog → ADR-013 |
| **Watch scope** | WATCH_NAMESPACE, cache, informer cost | backlog → ADR-014, BDR-003 |
| **Pod security** | PSS, SCC, restricted install profile | backlog → ADR-015 |
| **Cloud identity** | IRSA, GKE WI, Azure WI for backup Jobs | backlog → ADR-016 |
| **Platform wiring** | LB annotations, CSI — portable vs spec fields | backlog → ADR-017 |
| **CI / quality** | lint, vuln scan, coverage | backlog → ADR-018 |
| **Release matrix** | operator ↔ Neo4j version | backlog → ADR-019 |
| **Testing pyramid** | golden, envtest, kind, cloud smoke | backlog → ADR-020 |
| Validation placement | CEL first, webhook thin | ADR-001 ✓ |
| Layering | render / domain / controller | draft layer.md → ADR-002 |
| Reconcile pipeline | step order, mode branch | backlog → ADR-003 |
| Status writer | conditions, pool sub-status | backlog → ADR-004 |
| Render conventions | naming, labels, owner refs | backlog → ADR-005 |
| Apply strategy | SSA vs merge patch | backlog → ADR-006 |
| Formation & Bolt | bootstrap, quorum wait | backlog → ADR-007 |
| Finalizers & deletion | order, PVC policy | backlog → ADR-008 |
| Operator deploy & HA | leader election, Helm chart | backlog → ADR-010 |

Full backlog: [architecture-backlog.md](../operator-architecture-orchestrator/architecture-backlog.md) (domains A–O)  
Benchmark catalog: [operator-benchmark/readme.md](../../../docs/02-technical-design/architecture/operator-benchmark/readme.md)

---

## Split patterns (common)

| Topic | BDR owns | ADR owns |
|-------|----------|----------|
| TLS | `spec.trust` shape, mTLS modes, cert-manager opt-in | Secret projection, reload, cert-manager owner refs |
| Connectivity | listeners, expose, ingress rules | Service builder, Ingress backend resolution |
| Topology | pool members, plugins per pool | StatefulSet-per-pool naming, ordinal labels |
| Status | condition **types** users read (`Ready`, `Formation`) | when/how `status` is patched, conflict handling |
| Validation | rule **meaning** (validation.md IDs) | CEL vs webhook assignment table |
| Scale-in | policy exposed in spec (if any) | PVC retention, Neo4j decommission sequence |

---

## Output format

When classifying a question, respond with:

```markdown
## Classification: [topic]

**Primary record**: BDR | ADR | Split
**Rationale**: …
**Suggested filename**: `NNN-short-title.md`
**Depends on**: BDR-00x / ADR-00x
**Blocks**: …
**If split**: BDR section … / ADR section …
```

---

## Do not

- Put Go package names in BDR body (link ADR instead)
- Put CRD field defaults in ADR without citing the BDR that defined them
- Duplicate ADR-001 validation split — extend it or add ADR-00x for new layers only
- Create ADR for settled BDR with no implementation fork (wait until coding starts)
