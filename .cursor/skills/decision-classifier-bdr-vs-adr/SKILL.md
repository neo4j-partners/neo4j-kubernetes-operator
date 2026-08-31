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
| What users **declare** in YAML or **read** in `status` (CRD inventory, defaults) | **BDR** → `docs/design/decision-records/business/{domain}/` |
| How **implementers** structure code, reconcile, validate, test, deploy | **ADR** → `docs/design/decision-records/architecture/` |

**One business choice often needs one ADR** — cross-link both ways in **References**.

Canonical index: [`decision-records/readme.md`](../../../docs/design/decision-records/readme.md)

---

## Decision tree

```
New design question
│
├─ Would a cluster admin see or configure it in a CRD field, or gate automation on it?
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

## Where the new file goes

| | BDR | ADR |
|---|---|---|
| Path | `business/{domain}/{NNN}-{title}.md` | `architecture/{NNN}-{title}.md` |
| Domains in use | `neo4j/`, `operator/`, `database/`, `identity-user-roles/` | flat |
| Numbering | one sequence across **all** business domains (001–013 today) | one sequence (001–014 today) |
| Next free | **BDR-014** | **ADR-015** |

The number lives in the document header (`BDR-014 — …`) and in the filename. Two files break the
convention for historical reasons — `business/neo4j/006-…` carries **BDR-007** and `007-…` carries
**BDR-006** — do not imitate that, and do not renumber them either.

---

## BDR inventory (`business/`)

| ID | Topic | File |
|----|-------|------|
| BDR-001 | Single `Neo4j` CRD | `neo4j/001-single-neo4j-crd.md` |
| BDR-002 | Topology model and user guidance | `neo4j/002-neo4j-crd-topology.md` |
| BDR-003 | Operator install scope (ns / namespaces / cluster) | `operator/003-operator-install-scope.md` |
| BDR-004 | Plugin model | `neo4j/004-neo4j-plugin-topology.md` |
| BDR-005 | Storage volume mode | `neo4j/005-storage-volume-mode.md` |
| BDR-006 | TLS trust model (`spec.trust`) | `neo4j/007-tls-trust-model.md` |
| BDR-007 | Service exposure & connectivity | `neo4j/006-service-exposure-connectivity.md` |
| BDR-008 | Config surface (`spec.config`, `jvm`, `apoc`) | `neo4j/008-neo4j-config-surface.md` |
| BDR-009 | Scale, enable-server, pool ordinal semantics | `neo4j/009-scale-pool-ordinal-semantics.md` |
| BDR-010 | `spec.features` catalog | `neo4j/010-neo4j-features-catalog.md` |
| BDR-011 | HTTPS connector, exposure & mTLS | `neo4j/011-https-connector-tls-coupling.md` |
| BDR-012 | Identity management (users, roles, grants) | `identity-user-roles/012-identity-management.md` |
| BDR-013 | Logical database management | `database/013-database.md` |

Not yet written, and BDR-shaped: the **observable status contract** (the normative list of phases
and condition types, and which of them gate `Ready`) — today specified only in
`crd-spec/neo4j/status.md`, which is a contract document with no Status field, while ADR-004
explicitly declines to own the catalog.

**BDR must include**: options with YAML sketches, comparison table, customer impact, Helm parity
column.

---

## ADR inventory (`architecture/`)

| ID | Topic |
|----|-------|
| ADR-001 | CRD validation process (CEL + webhook) — the only `accepted` record so far |
| ADR-002 | Package layering (`render` / `domain` / `controller`) |
| ADR-003 | Reconcile pipeline order |
| ADR-004 | Status and conditions **writer** (not the catalog) |
| ADR-005 | Render conventions (naming, labels, owner references) |
| ADR-006 | Apply strategy and idempotent reconcile |
| ADR-007 | Formation and Bolt client usage |
| ADR-008 | Finalizers and deletion |
| ADR-009 | Watches and predicates |
| ADR-010 | Operator deployment and HA |
| ADR-011 | Implementation language (Go) |
| ADR-012 | Testing strategy: `src/` development tests vs `tests/` e2e matrix |
| ADR-013 | `neo4j.conf` as a directory of ConfigMap fragments |
| ADR-014 | Observability: logs, metrics, events, reason catalog and its projections |

Open ADR topics with no number assigned yet — take ADR-015 onward, in the order they are written:
RBAC surface, dependency policy, pod security (PSS/SCC), cloud identity (IRSA / GKE WI / Azure WI),
platform wiring (LB annotations, CSI), CI and release matrix.

Full backlog: [architecture-backlog.md](../operator-architecture-orchestrator/architecture-backlog.md)
Benchmark catalog: [operator-benchmark/readme.md](../../../docs/design/architecture/operator-benchmark/readme.md)

---

## Split patterns (common)

| Topic | BDR owns | ADR owns |
|-------|----------|----------|
| TLS | `spec.trust` shape, mTLS modes, cert-manager opt-in | Secret projection, reload, cert-manager owner refs |
| Connectivity | listeners, expose, ingress rules | Service builder, Ingress backend resolution |
| Topology | pool members, plugins per pool | StatefulSet-per-pool naming, ordinal labels |
| Status | phase list, condition **types** users read, which gate `Ready` | when/how `status` is patched, precedence, conflicts (ADR-004); where reasons live and how the reference is generated (ADR-014) |
| Validation | rule **meaning** (validation.md IDs) | CEL vs webhook assignment table |
| Scale-in | policy exposed in spec (if any) | PVC retention, Neo4j decommission sequence |

---

## Output format

When classifying a question, respond with:

```markdown
## Classification: [topic]

**Primary record**: BDR | ADR | Split
**Rationale**: …
**Suggested filename**: `business/{domain}/NNN-short-title.md` | `architecture/NNN-short-title.md`
**Depends on**: BDR-00x / ADR-00x
**Blocks**: …
**If split**: BDR section … / ADR section …
```

---

## Do not

- Put Go package names in a BDR body (link the ADR instead)
- Put CRD field defaults in an ADR without citing the BDR that defined them
- Enumerate condition **reasons** in any record: they are declared in `src/internal/oracle` and
  projected into `docs/user-guide/05-reference/errors.md` by `make errors` (ADR-014). A record
  decides which conditions exist, never the reason strings
- Duplicate the ADR-001 validation split — extend it, or add an ADR for new layers only
- Create an ADR for a settled BDR with no implementation fork (wait until coding starts)
