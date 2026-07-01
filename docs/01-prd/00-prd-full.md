# Neo4j Kubernetes Operator — Product Requirements Document

**Audience**: Product Management, Engineering leadership, Professional Services  
**Status**: draft — MVP scope proposed for decision  
**Date**: 2026-06-29  
**Source**: Consolidated from [`docs/01-prd/`](.) modular PRD package

---

## Document map

| Section | Detail in source file |
|---------|----------------------|
| Executive summary | §1 below · [`01-executive-summary.md`](01-executive-summary.md) |
| Problem & opportunity | §2 · [`02-problem-statement.md`](02-problem-statement.md) |
| Goals & non-goals | §3–4 · [`03-goals.md`](03-goals.md) · [`04-non-goals.md`](04-non-goals.md) |
| Users & stories | §5–6 · [`05-personas.md`](05-personas.md) · [`06-user-stories.md`](06-user-stories.md) |
| Requirements (traceable) | §7 · [`07-functional-requirements.csv`](07-functional-requirements.csv) |
| Non-functional requirements | §8 · [`08-nonfunctional-requirements.csv`](08-nonfunctional-requirements.csv) |
| API surface | §9 · [`09-api.md`](09-api.md) |
| Success criteria | §10 |
| Risks | §11 · [`11-risks.md`](11-risks.md) |
| Dependencies | §12 · [`12-dependencies.md`](12-dependencies.md) |
| Roadmap | §13 · [`13-roadmap.md`](13-roadmap.md) |
| Open decisions | §14 · [`14-open-questions.md`](14-open-questions.md) |

**Technical design** (out of scope for this PRD): [`../02-technical-design/`](../02-technical-design/)

---

## 1. Executive summary

The Neo4j Kubernetes Operator delivers a **declarative, continuously reconciled lifecycle** for Neo4j on Kubernetes through a single `Neo4j` custom resource — replacing the multi-release Helm model for cluster deployments.

### What we are proposing for V1 (MVP)

V1 focuses on the **`Neo4j` CRD only**:

- Deploy **Standalone** or **Cluster** (Enterprise) from one manifest
- **Per-pool StatefulSets** for primaries, analytics, and read replicas
- **Dynamic PVC** storage, **ClusterIP** exposure (HTTP + Bolt)
- **Cluster inter-member TLS** via bring-your-own secrets
- **Scale** pool members with automated `ENABLE SERVER` / drain
- **Config changes** via controlled restart
- **Status** with Ready / Installed / Error conditions

Backup, restore, monitoring, Neo4j version upgrade, external exposure (LoadBalancer, HTTPS), and day-2 CRDs are **explicitly deferred** to V1.1 / V2.

### Why now

The official Helm chart is proven for installs but does not model a cluster as one object, does not reconcile drift, and scatters operational knowledge across `values.yaml`, per-member releases, and manual operations Jobs.

### Strategic value

Self-managed customers get **cloud-like ergonomics** — faster provisioning, declarative desired state, portable manifests across AKS / EKS / GKE / OpenShift — while keeping **infrastructure sovereignty**. V1 delivers the foundation; compliance-heavy features (backup automation, declarative DB security, Web UI) follow in phased releases.

### What Product Management must decide

1. **Approve V1 MVP scope** as defined in §4 (narrower than the reference operator PRD).
2. **Secure Product Engineering sponsorship** before GA commitment.
3. **Define support tier** and escalation path for operator-managed deployments.
4. **Ratify open BDRs** that shape API and phasing (§14).

Success depends on more than code — adoption stoppers are captured in §11.

---

## 2. Problem statement

Teams running Neo4j on Kubernetes today rely primarily on the **official Helm chart**. That model works for single-server installs but creates operational friction as deployments grow.

### Pain points

| Today (Helm) | Impact |
|--------------|--------|
| One Helm release per cluster member | N releases to install, upgrade, and observe an N-node cluster |
| Manual `ENABLE SERVER` via operations Job | Scale-out requires chart-specific jobs or manual Cypher |
| ~780-line `values.yaml` with cross-field dependencies | High cognitive load; easy to misconfigure |
| Flat values tree for topology, TLS, storage, plugins | Weak separation of intent vs rendering mechanism |
| `disableLookups` for GitOps | Template-time lookups break Argo CD / Flux |
| No unified `.status` on one cluster object | Automation and support must aggregate many releases |
| No continuous reconciliation | Drift on StatefulSets, Services, or ConfigMaps is not corrected |

GitOps can deploy Helm releases reliably, but **does not replace** a domain-specific controller that understands Neo4j cluster formation, member enablement, and pool-aware scaling.

### Phased operator response

| Self-managed pain | Operator direction | V1 |
|-------------------|-------------------|-----|
| Complex bootstrap (STS, Services, discovery) | One `Neo4j` CR creates pools, Services, storage | **Yes** |
| Day-2 config drift across environments | Declarative spec + continuous reconcile | **Yes** |
| Patch/minor upgrades with RAFT risk | Orchestrated rolling upgrade | V2+ |
| Backups via cron + static cloud keys | `Neo4jBackup` CRD + pod identity | V2+ |
| Ad-hoc Cypher security scripts | `Neo4jUser` / `Role` / `Grant` CRDs | V2+ |
| Non-Git users blocked by YAML | Optional Web UI wizards | Roadmap |

### What we are solving

1. **One Neo4j deployment = one CR** (`Standalone` or `Cluster`).
2. **Reconcile** child resources to match declared spec and repair drift.
3. **Automate cluster day-1/day-2** flows Helm leaves manual (member enablement, per-pool scale).
4. **First-class status** for readiness, errors, and topology.

### Organizational context

This product definition is authored from **Professional Services (PS)** field experience. Long-term production ownership requires **Product Engineering** partnership (§11).

---

## 3. Goals

### V1 product goals

**Deployment**

- **G1** — Install Neo4j **Standalone** or **Cluster** from a single `Neo4j` CR ([BDR-001](../02-technical-design/decision-records/business/001-single-neo4j-crd.md), [BDR-002](../02-technical-design/decision-records/business/002-neo4j-crd-topology.md))
- **G2** — Support **primaries** plus optional **analytics** and **read** pools with per-pool StatefulSets ([BDR-009](../02-technical-design/decision-records/business/009-scale-pool-ordinal-semantics.md))
- **G3** — **Enterprise** edition with explicit license acceptance

**Operations**

- **G4** — **Continuous reconciliation** — create, update, delete, and correct drift (`OP-1-002`)
- **G5** — **Scale** cluster members safely — per-pool scale with automated `ENABLE SERVER` / drain (`NEO-2-011`)
- **G6** — **Config change** triggers controlled or rolling restart (`NEO-2-010`)
- **G7** — **Status** exposes Ready / Installed / Error without log diving (`OP-1-003`)

**Infrastructure (MVP path)**

- **G8** — **Storage**: dynamic PVC for data ([BDR-005](../02-technical-design/decision-records/business/005-storage-volume-mode.md))
- **G9** — **Networking**: ClusterIP with HTTP + Bolt; operator-derived internals in Cluster mode ([BDR-007](../02-technical-design/decision-records/business/006-service-exposure-connectivity.md))
- **G10** — **Cluster TLS** via BYO secrets when `mode: Cluster` ([BDR-006](../02-technical-design/decision-records/business/007-tls-trust-model.md))
- **G11** — **Config** passthrough map + default JVM ([BDR-008](../02-technical-design/decision-records/business/008-neo4j-config-surface.md))
- **G12** — **Plugins** via `pluginDefinitions` + pool references ([BDR-004](../02-technical-design/decision-records/business/004-neo4j-plugin-topology.md))

**Operator platform**

- **G13** — Install operator via **YAML manifests**, **single-namespace** watch scope ([BDR-003](../02-technical-design/decision-records/business/003-operator-install-scope.md))
- **G14** — Uninstall preserves data PVCs by default

### Differentiation vs Helm (V1 must deliver)

V1 must exceed “Helm with a reconciler wrapper”:

- One CR instead of N Helm releases for a cluster
- Automatic member enablement on scale (no operations Job)
- Unified status and drift correction
- Opinionated, smaller spec surface for the MVP path

### V2+ directional goals (not V1 commitment)

Backup / restore CRDs · monitoring / ServiceMonitor · Neo4j version upgrade · Helm migration · `Neo4jDatabase` · declarative identity (`Neo4jUser` / `Neo4jRole` / `Neo4jGrant`) · LoadBalancer / NodePort / ingress · multi-namespace operator scope · operator Helm chart and self-upgrade.

Detail: [`13-roadmap.md`](13-roadmap.md).

---

## 4. Non-goals (V1)

Engineering source of truth: [`../02-technical-design/13-v1-scope-lock.md`](../02-technical-design/13-v1-scope-lock.md) and `V1=No` rows in [`07-functional-requirements.csv`](07-functional-requirements.csv).

### Deferred domains

| Area | V1 stance |
|------|-----------|
| **Backup / restore** (`Neo4jBackup`, `Neo4jRestore`, `features.backup`) | Entire domain post-V1 |
| **`Neo4jDatabase`** logical database CRD | Post-V1 |
| **Declarative identity** (`Neo4jUser`, `Neo4jRole`, `Neo4jGrant`) | Post-V1 ([BDR-012](../02-technical-design/decision-records/business/012-identity-management.md)) |
| **Maintenance jobs** (dump/load, consistency check, offline mode) | Post-V1 |
| **Monitoring / Prometheus / ServiceMonitor** | Post-V1 ([BDR-010](../02-technical-design/decision-records/business/010-neo4j-features-catalog.md)) |
| **Neo4j rolling version upgrade** | `spec.version` set at install only in V1 |
| **Community edition / eval license** | Enterprise + accepted license only |
| **LoadBalancer / NodePort / HTTPS / Bolt TLS / Ingress** | ClusterIP + plain HTTP + Bolt |
| **Custom scheduling** (affinity, tolerations, topology spread) | Kubernetes defaults |
| **Operator Helm chart / multi-namespace scope / self-upgrade** | YAML install, single namespace |
| **PS-only long-term ownership** | Requires Product Engineering |

### Permanent non-goals (all versions)

- Replacing Neo4j Server or changing database semantics
- Managing non-Kubernetes infrastructure
- Being a generic graph-database operator framework

Full list: [`04-non-goals.md`](04-non-goals.md).

---

## 5. Personas

### Primary (V1)

| Persona | Need | V1 role |
|---------|------|---------|
| **Platform engineer** (Alex) | Declarative lifecycle; GitOps-friendly CR; namespace blast radius | Installs operator, defines `Neo4j` CR, integrates CI/CD |
| **Neo4j administrator** (Dana) | Cluster formation, scale, config without manual ops | Topology, scale, config passthrough, status |
| **PS consultant** | Repeatable patterns across engagements | MVP samples, predictable scope |
| **Support engineer** | Clear status and escalation path | `Ready` / `Error` conditions |

### Secondary (V2+)

| Persona | Need | When |
|---------|------|------|
| **Security / compliance** (Sia) | TLS, RBAC, audit | V1: cluster TLS only; V2+: identity CRDs |
| **SRE / observability** | Metrics, alerts | Post-V1 |
| **Backup operator** | Scheduled backup, DR | Post-V1 |
| **Business analyst** (Olivia) | Self-service without YAML | Roadmap — Web UI |
| **Auditor** (Ada) | Exportable audit trail | V2+ |

Detail: [`05-personas.md`](05-personas.md).

---

## 6. User stories and core workflows

Full story catalog with FR traceability: [`06-user-stories.md`](06-user-stories.md).

### V1 user stories (summary)

**Platform engineer**

- Install operator once via YAML in a dedicated namespace (US-PE-01)
- Restrict operator to **single namespace** for blast-radius control (US-PE-02)
- Apply one `Neo4j` manifest and get pools, Services, storage, cluster TLS wired (US-PE-03)

**Neo4j administrator**

- Deploy Standalone for dev and Cluster for production from the same CRD shape (US-NA-01)
- Scale read or analytics pools; operator runs `ENABLE SERVER` (US-NA-02)
- Change `spec.config`; operator applies via controlled restart (US-NA-03)
- See `Ready` / `Error` on the `Neo4j` object (US-NA-04)

**PS consultant**

- Deliver the same MVP manifest shape across engagements (US-PS-01)

**Support**

- Triage failed installs from `status.conditions` and Events (US-SE-01)

### Core workflows

| Flow | V1 | Success signal |
|------|-----|----------------|
| **CW-1 Cluster provisioning** | **Yes** | `Ready=True`; pods Ready; RAFT quorum in Cluster mode |
| **CW-3 Scale out/in** | **Yes** | Desired replicas; new members enabled |
| **CW-2 Online upgrade** | No | V2+ |
| **CW-4 Nightly backup** | No | V2+ |
| **CW-5 Restore drill** | No | V2+ |
| **CW-6 Security change** | No | V2+ |
| **CW-9 Operator upgrade** | No | V2+ |

### CW-1 happy path (V1)

1. User applies `Neo4j` CR (Standalone or Cluster).
2. Admission webhook defaults and validates (topology, license, storage, connectivity).
3. Operator reconciler creates StatefulSet(s) per pool, Services, cluster TLS secrets, config + JVM.
4. Cluster mode: wait for formation / `minimumMembers`; set `Ready=True` or `Error` with message.
5. Failure branches: invalid spec → webhook reject; formation timeout → `Error` condition + Event.

---

## 7. Functional requirements

> **Traceability library**: [`07-functional-requirements.csv`](07-functional-requirements.csv) — **116 requirements**, hierarchical IDs (`NEO-1-*`, `OP-1-*`, `NEO-2-*`, `NEO-3-*`).  
> Filter: `V1=Yes` for MVP scope (**43 requirements**). Full acceptance mapping: [`10-acceptance_criteria_library.csv`](10-acceptance_criteria_library.csv).

### Requirement hierarchy

```
Level 1 — Outcomes          NEO-1-001 Standalone · NEO-1-002 Cluster · OP-1-* Operator
Level 2 — Capabilities      NEO-2-003 Config · NEO-2-006 Storage · NEO-2-011 Scale · …
Level 3 — Configurations    NEO-3-006-PVC-01 Dynamic PVC · NEO-3-007-SVC-01 ClusterIP · …
```

### V1 outcome requirements (level 1)

| ID | Requirement | V1 |
|----|-------------|-----|
| **NEO-1-001** | Deploy Neo4j standalone | Yes |
| **NEO-1-002** | Deploy Neo4j cluster | Yes |
| **OP-1-001** | Install the Neo4j Operator | Yes |
| **OP-1-002** | Reconcile desired state | Yes |
| **OP-1-003** | Report status and conditions | Yes |
| **OP-1-004** | Upgrade the operator | No |
| **OP-1-005** | Uninstall (preserve data) | Yes |
| **OP-1-006** | Manage operator RBAC | Yes |
| **OP-1-007** | Operator metrics and logs | No |

### V1 capability areas (level 2 — in scope)

| Domain | ID | V1 delivers |
|--------|-----|-------------|
| Edition & license | NEO-2-001-* | Enterprise + accepted license |
| Topology | NEO-2-002-* | Standalone / Cluster; `minimumMembers` |
| Configuration | NEO-2-003 | `spec.config` passthrough + default JVM |
| Auth & secrets | NEO-2-004 | Initial password + optional existing Secret |
| TLS | NEO-2-005 | Cluster inter-member TLS only (BYO) |
| Storage | NEO-2-006 | Dynamic data PVC only |
| Networking | NEO-2-007 | ClusterIP; HTTP + Bolt; derived internals |
| Health probes | NEO-2-009 | Operator default probes |
| Config change | NEO-2-010 | Controlled restart |
| Scale | NEO-2-011 | Per-pool scale + ENABLE SERVER |

### V1 capability areas (level 2 — deferred)

| Domain | ID | Deferred to |
|--------|-----|-------------|
| Scheduling | NEO-2-008 | V1.1 |
| Neo4j upgrade | NEO-2-012 | V2 |
| Backup | NEO-2-013 | V2 |
| Restore | NEO-2-014 | V2 |
| Monitoring | NEO-2-015 | V1.1 / V2 |
| Logging | NEO-2-016 | V1.1 |
| Maintenance | NEO-2-017 | V2 |

### Acceptance criteria (reference)

[`10-acceptance_criteria_library.csv`](10-acceptance_criteria_library.csv) — **117 criteria** grouped by AC family (`AC-OP-INSTALL`, `AC-NEO-CLUSTER`, `AC-NEO-SCALE`, …). **53 criteria** marked `V1=Yes`.

Key V1 AC families:

- **AC-OP-INSTALL** — CRDs registered, operator pod Ready
- **AC-OP-SCOPE-SINGLE** — namespace-scoped watch and RBAC
- **AC-OP-RECONCILE** — create/update/delete + drift correction
- **AC-OP-STATUS** — Ready, Reconciling, Error on CR
- **AC-NEO-STANDALONE / AC-NEO-CLUSTER** — workload Ready, client connectivity
- **AC-NEO-SCALE** — pool scale with member enablement
- **AC-NEO-CONFIG-CHANGE** — config applied via controlled restart

Test execution catalog: [`../02-technical-design/04-test_catalog.csv`](../02-technical-design/04-test_catalog.csv) (`V1=Yes` filter).

---

## 8. Non-functional requirements

> **Traceability library**: [`08-nonfunctional-requirements.csv`](08-nonfunctional-requirements.csv) — **25 NFRs** across availability, performance, security, reliability, observability, maintainability, usability, portability, compliance, and documentation.

### V1 NFR summary (committed)

| Category | ID | Target |
|----------|-----|--------|
| **Availability** | NFR-AVL-001 | Operator resumes reconcile within ≤ 30 s after pod restart |
| **Availability** | NFR-AVL-002 | No unplanned Bolt/HTTP outage during V1 scale actions |
| **Performance** | NFR-PERF-001 | P95 reconcile < 5 s from CR change (aspirational) |
| **Security** | NFR-SEC-001 | Namespace-scoped RBAC; least privilege |
| **Security** | NFR-SEC-002 | Neo4j pods non-root (UID 7474) |
| **Security** | NFR-SEC-003 | Secrets read-only; not logged |
| **Security** | NFR-SEC-004 | Cluster inter-member TLS when `mode: Cluster` *(partial — client TLS deferred)* |
| **Reliability** | NFR-REL-001 | Fatal errors in status conditions |
| **Reliability** | NFR-REL-002 | Idempotent reconcile — no spurious updates |
| **Usability** | NFR-UX-001 | Standalone Ready < 3 min; 3-node Cluster Ready < 5 min (kind reference) |
| **Documentation** | NFR-DOC-001 | Published CRD spec + 3 sample manifests |

### Deferred NFRs (V2+)

Neo4j upgrade downtime SLO · fleet-scale memory footprint · backup throughput · client TLS · NetworkPolicies · operator Prometheus metrics · OTEL tracing · CRD conversion webhook · Web UI responsiveness · full cloud portability matrix · compliance audit trail for security CRDs.

---

## 9. API overview

Authoritative field definitions: [`../02-technical-design/crd-spec/`](../02-technical-design/crd-spec/). Summary: [`09-api.md`](09-api.md).

### Design principles vs reference operator PRD

| Principle | This project | Reference PRD |
|-----------|--------------|---------------|
| Workload CRD | Single **`Neo4j`** with `topology.mode` | Separate **`Neo4jCluster`** kind |
| Infra concerns | Embedded `spec` sections | Flat cluster fields |
| V1 scope | **`Neo4j` only** | Cluster + Backup + User/Role/Grant + UI |
| API version | `neo4j.com/v1beta1` (target) | `neo4j.com/v1alpha2` |

### V1 API — `Neo4j` CRD

| Attribute | Value |
|-----------|-------|
| Group / Kind | `neo4j.com` / `Neo4j` |
| Scope | Namespaced |
| Reconciler | `Neo4jReconciler` |

**V1 spec sections used**

| Section | V1 subset |
|---------|-----------|
| `edition`, `version`, `license` | Enterprise + calver at install |
| `topology` | Standalone or Cluster; primaries / analytics / read pools |
| `volumes.data` | `mode: Dynamic` only |
| `connectivity` | ClusterIP; HTTP + Bolt |
| `trust` | Cluster TLS — BYO certs when `mode: Cluster` |
| `config`, `jvm` | Passthrough + default JVM |
| `pluginDefinitions` + pool `plugins` | Per BDR-004 |

**V1 status**: `Ready`, `Installed`, `Error` conditions.

**Minimal V1 manifest (illustrative)**

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: prod
spec:
  edition: enterprise
  version: "2026.05.0"
  license:
    accept: "yes"
  topology:
    mode: Cluster
    primaries:
      members: 3
  volumes:
    data:
      mode: Dynamic
      dynamic:
        size: 100Gi
        storageClassName: standard
  connectivity:
    service:
      type: ClusterIP
```

Examples: [`crd-spec/neo4j/example.yaml`](../02-technical-design/crd-spec/neo4j/example.yaml), [`example-cluster.yaml`](../02-technical-design/crd-spec/neo4j/example-cluster.yaml).

### Post-V1 API (designed, not V1-tested)

| Kind | Purpose | Phase |
|------|---------|-------|
| `Neo4jDatabase` | Logical database CREATE/DROP | V2 |
| `Neo4jBackup`, `Neo4jBackupSchedule` | Scheduled / on-demand backup | V2 |
| `Neo4jRestore` | Restore from backup / seed | V2 |
| `Neo4jUser`, `Neo4jRole`, `Neo4jGrant` | Declarative identity | V3+ ([BDR-012](../02-technical-design/decision-records/business/012-identity-management.md)) |

### Admission and validation (V1)

- OpenAPI structural validation on CRD
- CEL rules for topology, connectivity, license coherence
- Mutating / validating webhooks for defaults and contradiction checks

Detail: [`crd-spec/neo4j/validation.md`](../02-technical-design/crd-spec/neo4j/validation.md).

### Operator installation (not a CRD)

V1: YAML manifests (Deployment, ServiceAccount, Role, RoleBinding, CRD, webhooks) in operator namespace. No `OperatorConfig` CRD. No Helm chart in V1.

---

## 10. Success metrics and V1 exit criteria

### Product success metrics

| Metric | Target | Phase |
|--------|--------|-------|
| Time to Standalone Ready | < 3 min from `kubectl apply` (kind reference) | V1 |
| Time to 3-node Cluster Ready | < 5 min | V1 |
| Manual steps vs Helm cluster deploy | ≥ 80% reduction (no per-member releases, no manual ENABLE SERVER) | V1 |
| Drift correction | Manual STS label change reverted on reconcile | V1 |
| V1 test catalog pass rate | 100% of P0 `V1=Yes` tests | V1 GA |
| PS pilot without engineering on-call | 1 engagement on MVP path | V1 |

### V1 exit criteria (draft — from roadmap)

- [ ] All V1 P0 tests pass on reference platform (kind + one cloud)
- [ ] [`13-v1-scope-lock.md`](../02-technical-design/13-v1-scope-lock.md) status = **frozen**
- [ ] BDR-002, BDR-003 ratified (`accepted`)
- [ ] Getting-started doc + 3 sample manifests (standalone, cluster, cluster + read scale)
- [ ] Product Engineering sponsorship decision recorded

---

## 11. Risks and adoption stoppers

Detail: [`11-risks.md`](11-risks.md).

| Risk | Severity | V1 mitigation |
|------|----------|---------------|
| **Reconciliation bug blast radius** | High | Single-namespace scope; V1 test catalog |
| **No vendor support model** | High | Open — requires Product + Support decision |
| **Weak differentiation vs Helm + GitOps** | Medium | G4–G7; roadmap for V2 day-2 CRDs |
| **No Helm migration path** | Medium | `11-helm-mapping.md` as V2 deliverable |
| **PS-only ownership** | High | RACI + Product Engineering sponsorship |

### Operational risk

A continuously running controller can actively modify resources. Mitigations: namespace scope, conservative MVP defaults, test pyramid, staged rollout, operator failure runbooks.

### Insufficient differentiation

V1 must deliver more than install/delete even without backup: one CR, automatic membership, unified status, drift correction, per-pool scale.

### Migration path

Primarily benefits **new deployments** unless a supported Helm → operator migration is documented. Treat migration as a first-class V2 deliverable.

---

## 12. Dependencies

Detail: [`12-dependencies.md`](12-dependencies.md). Platform matrix: [`../02-technical-design/dependencies.md`](../02-technical-design/dependencies.md).

### V1 runtime (required)

| Dependency | Role |
|------------|------|
| Kubernetes ≥ 1.27 | API server, scheduler |
| CSI / StorageClass | Dynamic PVC binding |
| Neo4j Enterprise image | Calver in `spec.version` |
| Enterprise license | `license.accept: "yes"` or license Secret |

### V1 runtime (not required)

cert-manager · Prometheus Operator · cloud object store · Ingress controller · Load balancer controller.

### Organizational (blocks GA)

| Dependency | Owner |
|------------|-------|
| Product Engineering sponsorship | Product |
| Support tier definition | Product + Support |
| Neo4j release train alignment | Product Engineering |

```
Kubernetes API
      │
      ├── Neo4j Operator (YAML install)
      ├── StorageClass + CSI
      └── Neo4j Enterprise image
              │
              ▼
         Neo4j CR → STS / SVC / PVC
```

---

## 13. Roadmap

Detail: [`13-roadmap.md`](13-roadmap.md). Engineering milestones: [`../02-technical-design/17-roadmap.md`](../02-technical-design/17-roadmap.md). Effort: [`../02-technical-design/19-delivery-estimate.md`](../02-technical-design/19-delivery-estimate.md).

### V1 — MVP (current proposal)

**Theme**: One `Neo4j` CRD, minimal happy path, production-leaning cluster operations **without backup**.

| Track | Delivers |
|-------|----------|
| Operator | YAML install, single-namespace, reconcile, basic status, RBAC, uninstall |
| Workload | Standalone + Cluster, per-pool STS, scale, config restart |
| Storage | Dynamic PVC |
| Network | ClusterIP, HTTP + Bolt |
| Security | Enterprise license, password Secret, cluster TLS (BYO) |
| Plugins | `pluginDefinitions` + pool refs |

### V1.1 — hardening & exposure

LoadBalancer / NodePort · HTTPS + Bolt TLS + ingress · reverse proxy · `features.monitoring` · storage `Existing` / aux volumes · multi-namespace scope · custom scheduling.

### V2 — day-2 platform

Backup / restore CRDs · Neo4j version upgrade · `Neo4jDatabase` · Helm migration guide · maintenance jobs · operator Helm chart · Prometheus / OTEL observability.

### V3+ — fleet & security

Declarative identity CRDs · optional Web UI · multi-namespace / prefix watch · blue/green major upgrades.

### Sequencing principle

Deliver **vertical slices** — each milestone is demoable end-to-end (standalone → cluster → production hardening → day-2). Do not defer E2E harness to late phases.

---

## 14. Open questions for Product Management

Detail: [`14-open-questions.md`](14-open-questions.md).

| # | Question | Owner | Blocks |
|---|----------|-------|--------|
| 1 | Product Engineering sponsorship and long-term ownership? | Product + PS | GA |
| 2 | Official support tier and SLA? | Product + Support | Enterprise adoption |
| 3 | Helm → operator migration for installed base? | PS + Product | Migration risk |
| 4 | V1 value proposition vs Helm + GitOps — what is V1-only? | Product + PS | Differentiation |
| 5 | Identity model (BDR-012) vs backup — V2 priority? | Product + Security | Roadmap |
| 6 | Optional Web UI — product commitment? | Product | Non-Git personas |
| 7 | cert-manager as recommended TLS path post-V1? | PS + Product | V1.1 |

### BDRs awaiting ratification

| BDR | Topic | Status |
|-----|-------|--------|
| [BDR-002](../02-technical-design/decision-records/business/002-neo4j-crd-topology.md) | Topology model (primaries / secondaries) | proposed |
| [BDR-003](../02-technical-design/decision-records/business/003-operator-install-scope.md) | Single-namespace V1 | proposed |
| [BDR-010](../02-technical-design/decision-records/business/010-neo4j-features-catalog.md) | `features` catalog | proposed |
| [BDR-011](../02-technical-design/decision-records/business/011-https-connector-tls-coupling.md) | HTTPS / TLS coupling | proposed |
| [BDR-012](../02-technical-design/decision-records/business/012-identity-management.md) | Identity CRDs | proposed |

---

## 15. Decision log

| Date | Decision | Rationale | Deciders |
|------|----------|-----------|----------|
| *pending* | Approve V1 MVP scope (§4) | | Product Management |
| *pending* | Product Engineering sponsorship | | Product Leadership |
| *pending* | Support tier for operator deployments | | Product + Support |
| *pending* | Ratify BDR-002, BDR-003 | | Product + Engineering |

---

## Appendix — traceability chain

```
Personas & stories (05, 06)
        │
        ▼
Functional requirements (07) ──► Acceptance criteria (10) ──► Test catalog (02-technical-design/04)
        │
        ▼
V1 scope lock (02-technical-design/13) ◄── Non-goals (04)
        │
        ▼
Roadmap (13) + Delivery estimate (02-technical-design/19)
```

**Modular PRD files** remain the editable source; this document is the **presentation consolidation** for Product Management reviews.
