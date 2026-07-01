# Operator benchmark — synthesis

Cross-operator comparison and recommendations for the Neo4j Kubernetes operator.

**Status**: draft — Tier-1 studies **CloudNativePG v1.25.1** and **Strimzi 0.45.0** complete (D1–D12, D15–D17). Tier-1 **ECK** and **MongoDB Community** pending for RBAC/cloud depth.

---

## Operators included

| Operator | Sheet | Version studied | Dimensions |
|----------|-------|-----------------|------------|
| CloudNativePG | [cloudnative-pg.md](operators/cloudnative-pg.md) | v1.25.1 | D1–D12, D15–D17 |
| Strimzi | [strimzi.md](operators/strimzi.md) | 0.45.0 | D1–D12, D15–D17 |
| ECK | — | — | pending |
| MongoDB Community | — | — | pending |

---

## Summary comparison

| Dimension | CloudNativePG | Strimzi | Neo4j direction (draft) |
|-----------|---------------|---------|-------------------------|
| **D1 Language / stack** | Go, kubebuilder, controller-runtime | Java, Maven, Fabric8, Vert.x | **Go** — follow CNPG |
| **D1 Layout** | `api/`, `internal/controller`, `pkg/specs`, `pkg/reconciler` | `model/`, `assembly/`, `resource/` | `api/`, `internal/{render,domain,controller}`, `internal/neo4j` |
| **D2 Thin controller** | Partial — split files, large `cluster_controller.go` | Partial — `KafkaAssemblyOperator` ~1k lines | **Strict** ~150-line `Reconcile()` pipeline (ADR-002) |
| **D3 CRD count** | Core + backup + pooler + SQL decl. | Many (Kafka, Topic, User, Connect…) | **V1**: `Neo4j` only; satellites post-V1 |
| **D6 Operator RBAC** | ClusterRole `manager`; creates Roles | Multiple ClusterRoles + delegation | Role (V1 single-ns); ClusterRole deferred |
| **D7 Workload RBAC** | Per-cluster Role + SA (`pkg/specs/roles.go`) | ClusterRole delegation per Kafka | **Adopt CNPG** per-`Neo4j` Role + resourceNames |
| **D8 Default scope** | **All namespaces** (empty `WATCH_NAMESPACE`) | **Single namespace** (`fieldRef` NS) | **Single NS** V1 (BDR-003); implement CNPG multi-cache for later |
| **D8 Multi-NS cost** | Comma-separated + multicache | RoleBinding **per namespace** (documented) | Document Strimzi overhead; avoid recommending multi-NS |
| **D9 Webhooks** | Yes, `failurePolicy: Fail` | No — reconcile-time validation | **CEL + webhook** (ADR-001), CNPG pattern |
| **D10 Admission** | Heavy webhook + async reconcile | CRD schema + reconcile errors | CEL + webhook semantics; reconciler for Bolt/cluster |
| **D11 Status** | `Ready`, backup/archiving conditions | `Ready` + **KafkaNodePool** status | `Ready` + **per-pool** sub-status |
| **D12 Formation** | Instance manager in pod; scale sacrifice | Assembly operator + Admin API | Bolt formation in `domain/formation`; no Helm ops Job |
| **D15 Unit tests** | Ginkgo, high coverage in `pkg/` | JUnit model tests | Table tests on `render/` |
| **D15 E2E** | `tests/e2e` on kind (40+ cases) | `systemtest` module (broad) | kind P0: Standalone, Cluster, scale, TLS |
| **D16 CI** | golangci-lint, gosec, govulncheck, CodeQL | Maven, SpotBugs | golangci-lint + govulncheck minimum |
| **D17 Packaging** | YAML release + external Helm + OLM | Numbered YAML + Helm | Helm + YAML V1; OLM later |

---

## Adopt / Adapt / Avoid

| Topic | Adopt | Adapt | Avoid | Evidence |
|-------|-------|-------|-------|----------|
| **Code layout** | CNPG `pkg/specs` + `pkg/reconciler` | Rename to `render` / `domain` per `layer.md` | 1.4k-line single reconciler file | CNPG `cluster_controller.go`; Strimzi `model/` |
| **Assembly pattern** | Strimzi `model` vs `assembly` naming | Go controller-runtime, not Vert.x | Java stack | `KafkaAssemblyOperator.java` |
| **Workload RBAC** | CNPG per-cluster Role + resourceNames | Neo4j discovery Secret names | Broad secret list in Role | `pkg/specs/roles.go`, CNPG `security.md` |
| **Operator RBAC docs** | Strimzi YAML comments + CNPG security.md | `security.md` in this repo | Undocumented ClusterRole | Strimzi `020-ClusterRole-*.yaml` |
| **Watch scope** | CNPG `WATCH_NAMESPACE` + multicache | V1 default single NS (Strimzi quick-start) | CNPG default all-NS for Neo4j V1 | CNPG `controller.go`; Strimzi `060-Deployment` |
| **Multi-namespace** | Support later with explicit RBAC | Comma-separated env | Undocumented middle tier | Strimzi upgrade docs, BDR-003 |
| **Admission** | CNPG webhooks Fail + CEL | ADR-001 rule IDs | Strimzi reconcile-only validation | `config/webhook/manifests.yaml` |
| **Formation / scale** | CNPG sacrifice instance; Strimzi rolling assembly | Bolt ENABLE SERVER / decommission | Helm `neo4j-operations` Job | `cluster_scale.go`, `KafkaAssemblyOperator` |
| **Pool status** | Strimzi KafkaNodePool | Embedded pools in Neo4j status | Separate pool CRD in V1 | Strimzi API |
| **Testing** | CNPG envtest + kind e2e | Neo4j testcontainers for Bolt | Strimzi-scale systemtest in V1 | `tests/e2e/`, `systemtest/` |
| **Quality gates** | CNPG golangci + govulncheck block merge | ADR-018 | — | CNPG `security.md` |
| **Packaging** | Both: YAML + Helm chart | Single-ns restricted values | OLM required for V1 | CNPG charts; Strimzi `packaging/install` |

---

## Consensus across CNPG + Strimzi

1. **Split builders from reconciliation** — both operators isolate manifest building (`pkg/specs` / `model`) from apply logic.
2. **Per-workload RBAC** — operator provisions **separate SA** for database/Kafka pods with **narrower** permissions than operator SA.
3. **Configurable watch scope** — both support single / multi / all namespaces; **defaults differ** (CNPG all-NS, Strimzi single-NS).
4. **Multi-namespace is painful** — Strimzi documents per-NS RoleBindings; CNPG uses multicache — Neo4j must document trade-offs in BDR-003 / ADR-014.
5. **Day-2 in operator** — scale and rolling updates are reconciler-driven, not external Jobs.
6. **Conditions + pool awareness** — `Ready` is universal; pool/node-pool status is required for multi-role clusters.

## Divergence (choose explicitly for Neo4j)

| Topic | CNPG | Strimzi | Neo4j proposal |
|-------|------|---------|----------------|
| Default install scope | Cluster-wide | Single namespace | **Single namespace** (V1 PS) |
| Admission webhooks | Strong | Weak (CRD only) | **Strong** (ADR-001) |
| Language | Go | Java | Go |
| Operand admin | SQL via instance manager | Kafka Admin API | **Bolt** via `internal/neo4j` |

---

## ADR recommendations (priority)

| Priority | ADR | Rationale | Evidence |
|----------|-----|-----------|----------|
| **P0** | ADR-011 Reference architecture synthesis | This document | CNPG + Strimzi sheets |
| **P0** | ADR-013 Operator & workload RBAC | Both create delegated Roles; CNPG `roles.go` | D6, D7 |
| **P0** | ADR-014 Watch scope & cache | Opposite defaults — must choose + implement `WATCH_NAMESPACE` | D8, BDR-003 |
| **P0** | ADR-020 Testing pyramid | CNPG e2e depth + Strimzi systemtest discipline | D15 |
| **P1** | ADR-002 Package layering | CNPG `pkg/` map + Strimzi model/assembly | D1, D2 |
| **P1** | ADR-003 Reconcile pipeline | Strimzi sub-reconcilers; CNPG `cluster_*.go` steps | D2, D12 |
| **P1** | ADR-007 Formation & Bolt | CNPG scale/join; Strimzi assembly lifecycle | D12 |
| **P1** | ADR-018 CI quality gates | CNPG lint/vuln policy | D16 |
| **P2** | ADR-012 Go dependency policy | CNPG controller-runtime v0.20.2 | D4 |
| **P2** | ADR-010 Operator deployment | Strimzi numbered YAML; CNPG Helm chart | D17 |

**Gate G-0 satisfied** for CNPG + Strimzi (≥2 Tier-1 sheets). ADR-002 should move to **proposed** only after ADR-011 draft.

---

## Open questions → BDR or ADR

| Question | BDR or ADR | Notes |
|----------|------------|-------|
| V1 default single-ns vs cluster-wide | **BDR-003** | Strimzi evidence supports single-ns default; CNPG supports cluster-wide — ratify BDR-003 Option A |
| Per-pool ServiceAccount vs one SA per Neo4j CR | **ADR-013** | Strimzi per-Kafka; CNPG per-Cluster — Neo4j pools may share or split SA |
| KafkaNodePool-style status without pool CRD | **ADR-004** | Strimzi pattern via embedded pool status |
| Instance manager sidecar for Neo4j? | **ADR-007** | CNPG runs management in pod; Neo4j may use main container + Bolt from operator only |
| Complete ECK/MongoDB RBAC survey | **Phase 2a cont.** | Needed before ADR-013 accepted |

---

## Next benchmark steps

1. **ECK** — restricted profile (`profile-restricted.yaml`), ClusterRole vs Role (D6, D14).
2. **MongoDB Community** — `WATCH_NAMESPACE`, per-namespace SA manual steps (D7, D8).
3. **CNPG Helm** `clusterWide: false` values — map to Neo4j restricted install overlay.

---

## References

- [operators/cloudnative-pg.md](operators/cloudnative-pg.md)
- [operators/strimzi.md](operators/strimzi.md)
- [BDR-003](../../decision-records/business/operator/003-operator-install-scope.md)
- [ADR-001](../../decision-records/architecture/001-crd-validation-process.md)
- [architecture/readme.md](../readme.md)
