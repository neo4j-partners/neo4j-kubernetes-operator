# Operator benchmark — Strimzi Kafka Operator

| | |
|---|---|
| **Tier** | 1 |
| **Repo** | https://github.com/strimzi/strimzi-kafka-operator |
| **Version studied** | 0.45.0 (tag) |
| **Date** | 2026-06-22 |
| **Analyst** | operator-benchmark-analyst |

---

## Executive summary

Strimzi is the reference **multi-CRD, multi-namespace** Java operator with explicit **assembly vs model** layering and granular **delegated RBAC** (cluster operator + Kafka broker + entity operator + client roles). Default install watches **one namespace**; cluster-wide and multi-namespace are documented with **manual RoleBinding per namespace**. Neo4j is Go/kubebuilder — adopt Strimzi's **operational and RBAC patterns**, not the Vert.x/Java stack. `KafkaAssemblyOperator` is ~1k lines but delegates to `model/` and sub-reconcilers.

---

## D1 — Repository layout

```
strimzi-kafka-operator/          # Maven multi-module (Java 17+)
├── api/                          # CRD Java API classes (generated + hand-written)
├── cluster-operator/             # Main Kafka cluster operator
│   └── src/main/java/io/strimzi/operator/cluster/
│       ├── ClusterOperator.java  # Entry / verticle deploy
│       ├── operator/assembly/    # *AssemblyOperator (reconcile orchestration)
│       ├── operator/resource/    # K8s resource operators (STS, Service, …)
│       └── model/                # Pure builders (KafkaCluster, EntityOperator, …)
├── topic-operator/               # Separate deployment
├── user-operator/                # Separate deployment
├── operator-common/              # Shared config, reconciliation, labels
├── certificate-manager/          # Internal CA utilities
├── install/                      # Split YAML (CRD, RBAC, Deployment)
├── packaging/
│   ├── install/                  # Same manifests for releases
│   └── helm-charts/helm3/        # Helm chart
├── systemtest/                   # Large e2e (Testcontainers / minikube / OCP)
└── documentation/                # Antora docs (deploying, configuring)
```

| Area | Path | Notes |
|------|------|-------|
| API types | `api/src/main/java/io/strimzi/api/kafka/model/` | Kafka, KafkaTopic, KafkaUser, KafkaConnect, … |
| Controllers | `cluster-operator/.../operator/assembly/` | `KafkaAssemblyOperator`, `KafkaConnectAssemblyOperator`, … |
| Builders / manifests | `cluster-operator/.../model/` | `KafkaCluster`, `KafkaBridgeCluster`, … |
| Webhooks | — | **No kubebuilder-style admission webhooks**; validation in operator |
| Tests | `cluster-operator/src/test`, `systemtest/` | JUnit unit + system tests |
| Deploy / config | `install/cluster-operator/`, Helm | Numbered YAML sequence 010–060 |

**Neo4j takeaway**: **Adopt** naming split **`model/` = render**, **`assembly/` = domain controller**. Strimzi's **separate modules per operator** (topic, user) maps to Neo4j satellite CRD controllers (backup, database) — not separate binaries required for V1.

---

## D2 — Internal layering

| Layer | Present? | Package(s) | Notes |
|-------|----------|------------|-------|
| Pure builders | Yes | `operator/cluster/model/` | `KafkaCluster.fromCrd(...)` — no K8s client |
| Domain / reconcile logic | Yes | `operator/assembly/`, `operator/resource/` | Async Vert.x `Future` chains |
| Thin controllers | Partial | `AbstractAssemblyOperator` | `KafkaAssemblyOperator` still large |
| Shared admin client | Yes | Kafka AdminClient, Cruise Control API | In operator JVM |

**Neo4j takeaway**: **Adopt** `model` → `internal/render`, `assembly` → `internal/domain`, entry `ClusterOperator` → `internal/controller`. **Avoid** Vert.x async — use controller-runtime reconcile.Result. Sub-reconcilers (`KafkaListenersReconciler`, `EntityOperatorReconciler`) → named steps in ADR-003.

---

## D3 — CRD & controller count

| CRD | Controller / Operator | Notes |
|-----|----------------------|-------|
| `Kafka` | `KafkaAssemblyOperator` | Core — brokers, EO, CC, exporter |
| `KafkaNodePool` | Used by Kafka reconciler | Pool / role model (analogous to Neo4j pools) |
| `KafkaTopic` | Topic Operator (separate Deployment) | |
| `KafkaUser` | User Operator (separate Deployment) | |
| `KafkaConnect`, `KafkaConnector`, `KafkaMirrorMaker2`, … | Dedicated assembly operators | |
| `StrimziPodSet` | Custom workload primitive | Alternative to STS for Kafka pods |

**Neo4j takeaway**: **Adapt** `Kafka` + `KafkaNodePool` pattern to `Neo4j` + pool sections (BDR-002/009) without extra CRD for pools in V1. **Defer** splitting Topic/User-style concerns to V2 identity CRDs (BDR-012).

---

## D4 — Go & Kubernetes dependencies

| Dep | Version | Pin policy |
|-----|---------|------------|
| Language | **Java** (Maven) | Not applicable to Neo4j Go ADR |
| K8s client | Fabric8 kubernetes-client | Version in parent `pom.xml` |
| Async | Vert.x | Cluster operator runtime |

**Neo4j takeaway**: Strimzi is **not a Go dependency reference**. Use CNPG/ECK for controller-runtime pins. Strimzi informs **CRD design, RBAC, scope, and test strategy** only.

---

## D5 — Third-party libraries

| Library | Purpose | In operator pod? |
|---------|---------|------------------|
| Fabric8 K8s client | API server access | Yes |
| Kafka AdminClient | Broker management | Yes |
| BouncyCastle / cert-manager libs | TLS / CA | Yes |
| Cruise Control client | Rebalancing | When enabled |

**Neo4j takeaway**: Operator JVM carries Kafka clients — Neo4j should **limit** `neo4j-go-driver` to formation/health modules; no APOC/GDS code in operator.

---

## D6 — Operator RBAC

Split across **multiple ClusterRoles** with delegation:

| ClusterRole | Purpose | Key verbs |
|-------------|---------|-----------|
| `strimzi-cluster-operator-namespaced` | Operand namespace work | pods, sts, deploy, secrets, cm, sa, **roles, rolebindings**, pvc | 
| `strimzi-cluster-operator-global` | CRDs, webhooks, cluster-scoped | strimzi CRDs, validatingwebhookconfigurations |
| `strimzi-cluster-operator-watched` | Cluster-wide watch mode | namespaces get/list/watch |
| `strimzi-kafka-broker` | **Delegated** to Kafka pods | kafka broker needs |
| `strimzi-entity-operator` | **Delegated** to EO deployment | topics/users |
| `strimzi-kafka-client` | Clients | optional |

Evidence: `install/cluster-operator/020-ClusterRole-strimzi-cluster-operator-role.yaml` (comments per rule); `030-` / `031-` delegation bindings.

**Neo4j takeaway**: **Adopt** commented RBAC manifests (rule rationale in YAML). **Adapt** delegation: Neo4j workload Role (bolt, discovery) bound to `neo4j-{pool}` SA. Operator needs `roles` + `rolebindings` create — ADR-013. Strimzi is **more RBAC-heavy** than CNPG — expect similar for clustered Neo4j with discovery.

---

## D7 — Workload RBAC

- **Cluster Operator** creates **ServiceAccount** `*-kafka`, **RoleBindings** to `strimzi-kafka-broker`, `strimzi-entity-operator`, `strimzi-kafka-client` ClusterRoles in the **Kafka namespace**.
- Entity Operator (topic/user) runs as separate Deployment with its own SA.
- Pattern: **cluster-scoped ClusterRole + namespace RoleBinding** per operand.

Evidence: `install/cluster-operator/030-ClusterRoleBinding-strimzi-cluster-operator-kafka-broker-delegation.yaml`; `model/EntityOperator.java`.

**Neo4j takeaway**: **Adapt** for Neo4j: one SA per pool STS; RoleBinding to a **narrow ClusterRole** only if cross-namespace discovery is required (usually not in V1). Prefer **namespace Role** like CNPG for simpler installs.

---

## D8 — Watch scope

| Mode | Supported | Default | Config |
|------|-----------|---------|--------|
| Single namespace | Yes | **Yes** — `STRIMZI_NAMESPACE` = `fieldRef: metadata.namespace` | `060-Deployment-strimzi-cluster-operator.yaml` |
| Multiple namespaces | Yes | — | Comma-separated `STRIMZI_NAMESPACE` + **RoleBinding per NS** |
| All namespaces | Yes | — | `STRIMZI_NAMESPACE=*` + ClusterRoleBindings |

Evidence: `ClusterOperatorConfig.NAMESPACE` (`NAMESPACE_SET` parser); `documentation/modules/operators/con-operators-namespaces.adoc`; upgrade docs warn multi-NS needs binding per namespace.

**Neo4j takeaway** (BDR-003): **Adopt** Strimzi as evidence that **multi-namespace is supported but operationally heavy** (per-NS RoleBindings). Neo4j V1 single-ns matches Strimzi quick-start default. Document upgrade path to `*` only with ClusterRole + security review.

---

## D9 — Webhooks

| Webhook | Deploy | TLS | failurePolicy |
|---------|--------|-----|---------------|
| Kubernetes admission webhooks | **Not used** for Kafka CR validation | — | — |
| Validating | In-operator + CRD schema | — | API server CRD validation |

**Neo4j takeaway**: Strimzi relies on **CRD OpenAPI + Java validation** in reconciler. Neo4j should **not** copy this — follow **ADR-001** (CEL + webhook) like CNPG for faster feedback at admission.

---

## D10 — Admission vs reconcile split

| Admission | Reconcile |
|-----------|-----------|
| CRD structural schema | `KafkaAssemblyOperator.reconcileAssembly` |
| `InvalidConfigurationException` at reconcile start | Rolling updates, pod set, certificates |
| — | KRaft migration, listener reconciliation |

Evidence: `KafkaAssemblyOperator.java`; `InvalidResourceException` in model layer.

**Neo4j takeaway**: Strimzi tolerates **late validation** (reconcile errors → status). Neo4j should be **stricter at admission** for FR traceability (`validation.md` IDs) — hybrid ADR-001 is better UX than Strimzi alone.

---

## D11 — Status & conditions

| Condition type | When set | Pool-level? |
|----------------|----------|-------------|
| `Ready` | Kafka cluster operational | Broker-ready aggregated |
| Type-specific | NotReady, Warning with `reason` | `KafkaNodePool` status (node pools) |

Evidence: `api/.../common/Condition`; `CustomResourceConditions.isReady()`.

**Neo4j takeaway**: **Adopt** standard **`Ready`** condition + **pool-level** status for `primaries` / `secondaries` pools (BDR-009). Strimzi `KafkaNodePool` is a strong analogue for pool-aware status.

---

## D12 — Formation / day-2

- **KafkaAssemblyOperator** orchestrates broker STS / StrimziPodSet, certificates, listeners.
- **Scale**: `KafkaClusterCreator`, `PodSetUtils` — rolling pod management.
- **KRaft**: dedicated migration utilities (`KRaftMigrationUtils`, `KRaftMetadataManager`).
- **No external Job for broker join** — operator drives pod lifecycle and Kafka Admin API.

Evidence: `operator/assembly/KafkaAssemblyOperator.java`, `KafkaClusterCreator.java`.

**Neo4j takeaway**: **Adopt** assembly operator owning **full rolling lifecycle**. Neo4j formation (ENABLE SERVER, quorum) maps to Kafka's broker registration — implement in `domain/formation` with Bolt (ADR-007). **Avoid** separate Helm ops chart pattern.

---

## D15 — Testing pyramid

| Tier | Tool | Scope |
|------|------|-------|
| Unit | JUnit + Mockito | `cluster-operator/src/test` — model + assembly |
| Integration | MockKube, Fabric8 mock | Kubernetes resource operators |
| System / E2E | `systemtest/` module | Full stack: Kafka, Connect, MM2, security, OLM, migration |
| Performance | `systemtest/performance` | Dedicated perf scenarios |

Evidence: `systemtest/src/test/java/io/strimzi/systemtest/` (kafka, security, olm, …); root `Makefile` Maven modules.

**Neo4j takeaway**: **Adapt** Strimzi's **dedicated systemtest module** → `test/e2e/` with tagged suites (P0/P1). Strimzi's breadth (Connect, OLM) exceeds Neo4j V1 — scope to Standalone + Cluster + scale + TLS for P0.

---

## D16 — Quality gates

| Gate | Tool | Notes |
|------|------|-------|
| Build | Maven, `Makefile` | Multi-module |
| Static analysis | SpotBugs (`.spotbugs/`) | Java |
| CI | Azure Pipelines / GitHub Actions | Build + systemtest tiers |
| API compatibility | `api` module structural CRD tests | `StructuralCrdIT` |

**Neo4j takeaway**: Go equivalent: golangci-lint + govulncheck (CNPG) + **CRD schema drift test** (compare generated CRD to committed YAML).

---

## D17 — Packaging

| Channel | Artifact | Notes |
|---------|----------|-------|
| Install YAML | `packaging/install/cluster-operator/` | Numbered manifests; `STRIMZI_NAMESPACE` from downward API |
| Helm | `packaging/helm-charts/helm3/strimzi-kafka-operator` | watchNamespaces value |
| OLM | `systemtest/olm` tests; operator bundles | Supported in ecosystem |
| Examples | `packaging/examples/` | Kafka, metrics, certs |

**Neo4j takeaway**: **Adopt** numbered install YAML + Helm parity. Strimzi default **single-namespace** install is the right **V1 PS deliverable** shape. Document multi-NS as advanced overlay (BDR-003).

---

## Recommendations for Neo4j operator

| Topic | Adopt | Adapt | Avoid | Evidence |
|-------|-------|-------|-------|----------|
| model/assembly split | Yes | Go render/domain | Java Vert.x | `model/KafkaCluster.java`, `KafkaAssemblyOperator.java` |
| RBAC delegation | Commented rules + per-operand bindings | Namespace Role not ClusterRole where possible | Single superuser ClusterRole | `020-ClusterRole-*.yaml` |
| Watch scope default | Single NS for V1 | `WATCH_NAMESPACE` env | Multi-NS without doc | `060-Deployment-*.yaml` |
| Pool model | KafkaNodePool status pattern | Embedded pools in Neo4j CR | Extra pool CRD V1 | `KafkaNodePool` API |
| Admission | CEL/webhook (CNPG) | — | Strimzi-style reconcile-only validation | — |
| Testing | systemtest module size discipline | kind + Neo4j container | Full Strimzi matrix | `systemtest/` |

---

## References

- https://strimzi.io/docs/operators/0.45.0/deploying.html
- https://github.com/strimzi/strimzi-kafka-operator/blob/main/documentation/modules/operators/con-operators-namespaces.adoc
- [BDR-003](../../../decision-records/business/operator/003-operator-install-scope.md) · [ADR-001](../../../decision-records/architecture/001-crd-validation-process.md)
