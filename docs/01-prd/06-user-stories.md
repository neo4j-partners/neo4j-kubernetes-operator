# User stories

User journeys derived from field pain and reference operator PRD material ([`../00-discovery/export.md`](../00-discovery/export.md)), **mapped to this project's API and V1 scope**. Stories marked **V1** are in scope for MVP; **V2+** are directional only.

Source requirements: [`07-functional-requirements.csv`](07-functional-requirements.csv) (`V1` column). Acceptance criteria: [`02-acceptance_criteria_library.csv`](02-acceptance_criteria_library.csv).

---

## Platform engineer (Alex)

| ID | Story | V1 | FR / AC |
|----|-------|-----|---------|
| US-PE-01 | As a platform engineer, I install the operator once via YAML manifests in a dedicated namespace so teams can deploy Neo4j without per-cluster controller sprawl. | **V1** | OP-1-001, OP-2-001-SCOPE-01 |
| US-PE-02 | As a platform engineer, I restrict the operator to a **single namespace** so a reconciliation bug cannot affect other teams' workloads. | **V1** | OP-2-001-SCOPE-01, [BDR-003](../02-technical-design/decision-records/business/003-operator-install-scope.md) |
| US-PE-03 | As a platform engineer, I apply one `Neo4j` manifest (or GitOps commit) and get primaries, Services, storage, and cluster TLS wired without hand-rolling StatefulSets. | **V1** | NEO-1-001, NEO-1-002, AC-NEO-INSTALL |
| US-PE-04 | As a platform engineer, I watch the operator watch **all team namespaces** with prefix rules from one deployment. | V2+ | OP-2-001-SCOPE-02/03 |
| US-PE-05 | As a platform engineer, I upgrade the operator in place without losing reconciliation for managed clusters. | V2+ | OP-1-004 |

---

## Neo4j administrator (Dana)

| ID | Story | V1 | FR / AC |
|----|-------|-----|---------|
| US-NA-01 | As a Neo4j admin, I deploy **Standalone** for dev and **Cluster** for production from the same CRD shape. | **V1** | NEO-2-001-MODE-01, NEO-2-002-MODE-01 |
| US-NA-02 | As a Neo4j admin, I scale **read** or **analytics** pools by editing member counts; the operator runs `ENABLE SERVER` and updates StatefulSets. | **V1** | NEO-2-011, [BDR-009](../02-technical-design/decision-records/business/009-scale-pool-ordinal-semantics.md) |
| US-NA-03 | As a Neo4j admin, I change `spec.config` and the operator applies it via a controlled restart without manual pod deletion. | **V1** | NEO-2-010 |
| US-NA-04 | As a Neo4j admin, I see `Ready` / `Error` on the `Neo4j` object instead of correlating many Helm releases. | **V1** | OP-1-003, OP-2-003-STATUS-01 |
| US-NA-05 | As a Neo4j admin, I roll Neo4j to a new patch/minor image with zero-downtime orchestration (secondaries → primaries → leader). | V2+ | NEO-2-012 |
| US-NA-06 | As a Neo4j admin, I configure nightly backups to S3/GCS/Azure via a `Neo4jBackup` CRD and pod identity — no static cloud keys. | V2+ | NEO-2-013 |
| US-NA-07 | As a Neo4j admin, I restore from backup into a new cluster for DR drills. | V2+ | NEO-2-014 |
| US-NA-08 | As a Neo4j admin, I create logical databases via `Neo4jDatabase` without manual Cypher in runbooks. | V2+ | *(post-V1 CRD)* |

---

## PS consultant

| ID | Story | V1 | FR / AC |
|----|-------|-----|---------|
| US-PS-01 | As a PS consultant, I deliver the same MVP manifest shape across customer engagements (Dynamic PVC, ClusterIP, HTTP+Bolt). | **V1** | [`../00-discovery/13-v1-scope-lock.md`](../00-discovery/13-v1-scope-lock.md) |
| US-PS-02 | As a PS consultant, I explain Helm → operator field mapping from a single mapping doc. | V2+ | `11-helm-mapping.md` *(to author)* |
| US-PS-03 | As a PS consultant, I migrate an existing Helm cluster to the operator without full reinstall where supported. | V2+ | [`11-risks.md`](11-risks.md) § Migration |

---

## Support engineer

| ID | Story | V1 | FR / AC |
|----|-------|-----|---------|
| US-SE-01 | As support, I read `status.conditions` and Kubernetes Events to triage a failed install without SSH to pods. | **V1** | OP-1-003, AC-OP-STATUS |
| US-SE-02 | As support, I see detailed phase timelines (upgrade steps, backup last run, security reconcile). | V2+ | OP-2-003-STATUS-02 |

---

## Security / compliance (Sia) — V2+

Reference PRD includes declarative `Neo4jUser` / `Neo4jRole` / `Neo4jGrant` CRDs ([BDR-012](../../02-technical-design/decision-records/business/012-identity-management.md)). **Not in V1** — cluster TLS and namespace-scoped operator RBAC only.

| ID | Story | V1 | Notes |
|----|-------|-----|-------|
| US-SEC-01 | As a security engineer, I manage Neo4j RBAC via Git-reviewed CRDs with idempotent Cypher reconciliation. | V2+ | Not in current FR CSV |
| US-SEC-02 | As a security engineer, I enforce TLS on Bolt/HTTPS and client auth via `spec.trust`. | V2+ | V1: cluster TLS only ([`13-v1-scope-lock`](../00-discovery/13-v1-scope-lock.md)) |
| US-SEC-03 | As a security engineer, I use cert-manager for automatic cert rotation. | V2+ | V1: BYO secrets |

---

## Business analyst (Olivia) — V2+

Reference PRD includes a Web UI for non-Git users. **Not in V1 scope.**

| ID | Story | V1 | Notes |
|----|-------|-----|-------|
| US-BA-01 | As an analyst, I provision a test Neo4j instance via a UI wizard without editing YAML. | V2+ | Optional product direction from reference PRD |

---

## Core workflows (happy path)

Mapped from reference PRD §7. **V1** implements CW-1 (subset), CW-3 (scale), and partial CW-9 (install only).

| Flow | Trigger | V1 | Success signal |
|------|---------|-----|----------------|
| **CW-1 Cluster provisioning** | `kubectl apply` `Neo4j` CR | **V1** (MVP path) | `Ready=True`; pods Ready; RAFT quorum in Cluster mode |
| **CW-2 Online patch/minor upgrade** | `spec.version` / image change | V2+ | `UpgradeInProgress=False`; probes green |
| **CW-3 Scale out/in** | Edit pool `members` | **V1** | Desired replicas; new members enabled |
| **CW-4 Nightly backup** | `Neo4jBackup` schedule | V2+ | `BackupSucceeded` condition |
| **CW-5 Restore drill** | `Neo4jRestore` or seed | V2+ | `Ready=True` with restored data |
| **CW-6 Security change** | User/Role/Grant CRDs | V2+ | All security CRDs `Ready=True` |
| **CW-7 Certificate renewal** | cert-manager or secret rotation | V2+ | V1: static BYO cluster certs only |
| **CW-8 Namespace auto-onboarding** | New namespace in prefix watch mode | V2+ | OP-2-001-SCOPE-02/03 |
| **CW-9 Operator upgrade** | Replace operator manifests | V2+ | Clusters remain Ready |

### CW-1 detail (V1)

1. User applies `Neo4j` CR (Standalone or Cluster).
2. Admission webhook defaults and validates (topology, license, storage, connectivity).
3. Operator reconciler: StatefulSet(s) per pool, Services (client + derived internals), cluster TLS secrets mounted, config + JVM.
4. Cluster mode: wait for formation / `minimumMembers`; set `Ready=True` or `Error` with message.
5. Failure branches: invalid spec → webhook reject; formation timeout → `Error` condition + Event.
