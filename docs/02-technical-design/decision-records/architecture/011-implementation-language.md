# ADR-011 — Operator implementation language (Go)


|                 |                                                                                                                                                       |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Status**      | proposed                                                                                                                                              |
| **Date**        | 2026-07-01                                                                                                                                            |
| **Depends on**  | [operator-benchmark/synthesis.md](../../architecture/operator-benchmark/synthesis.md) · [BDR-003](../business/operator/003-operator-install-scope.md) |
| **Constraints** | V1 operator; kubebuilder / controller-runtime assumed in ADR-001..010                                                                                 |


---

## Context

The Neo4j Kubernetes operator is a continuously running controller: it watches Custom Resources, builds Kubernetes objects, calls the Neo4j admin API where needed, and updates status. That **operator pattern is language-agnostic** — any runtime that can talk to the Kubernetes API and run a reconcile loop qualifies.

We must choose the **primary implementation language** for the operator binary before locking package layout, dependencies, CI, and contributor onboarding. Existing architecture ADRs (002–010) already sketch Go packages (`internal/render`, `internal/domain`, `internal/controller`, `internal/neo4j`), but the choice was implicit.

**Forces:**

- Tier-1 benchmark: **CloudNativePG** (Go, kubebuilder) and **Strimzi** (Java, Fabric8, Vert.x) — same problem domain, different stacks ([synthesis.md](../../architecture/operator-benchmark/synthesis.md)).
- [ADR-001](001-crd-validation-process.md) — CEL markers on Go types + validating webhooks; kubebuilder generates CRD and webhook scaffolding from Go structs.
- [ADR-007](007-formation-and-bolt.md) — cluster formation via **Bolt**; Neo4j ships an official **Go driver** (`neo4j-go-driver`).
- Neo4j Helm chart operator-adjacent code is already **Go** (`helm-charts/neo4j` values parsing, template helpers).
- PS and enterprise customers care about **operator image size**, memory footprint, and supply-chain familiarity — JVM vs single static binary.

**What breaks if wrong:** rework of all ADRs and scaffold; mismatched hiring andgo CI; Bolt/admin client work duplicated in another language; harder alignment with CNPG reference implementation.

---



## Analysis



### The operator pattern (language-independent)

Regardless of language, the operator is:

1. A **Deployment** (or similar) with RBAC in the cluster
2. A **watch loop** on CRs and owned/watched objects
3. **Reconcile** logic: read desired state → compute manifests → apply → patch status
4. Optional **admission webhooks** and **leader election**

The Kubernetes API server does not require Go. Strimzi proves a production-grade Java operator at scale; CNPG proves the same in Go.

### Option A — Go with kubebuilder / controller-runtime (chosen)


| Advantages                                                                                                                                                | Disadvantages                                                                                 |
| --------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| **Official K8s client path** — `client-go`, **controller-runtime**, Operator SDK / kubebuilder                                                            | Reconciler discipline required — large controllers are still possible (CNPG ~1.4k-line files) |
| **First-class operator tooling** — CRD generation, RBAC markers, webhook manifests, envtest                                                               | Go version / `k8s.io/`* pin matrix must be maintained (→ ADR-012)                             |
| **Primary benchmark reference** — CNPG layout, webhooks, testing ([cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md)) | Strimzi Java patterns must be **translated**, not copied                                      |
| **Official Neo4j Bolt client** — `neo4j-go-driver/v5` for formation ([ADR-007](007-formation-and-bolt.md))                                                |                                                                                               |
| **Small runtime** — single static binary; typical operator image tens of MB vs JVM hundreds of MB                                                         |                                                                                               |
| **Existing ADR chain** — ADR-002..010, `layer.md`, `file_structure.md` assume Go                                                                          |                                                                                               |
| **Ecosystem default** — most CNCF / database operators in Go; easier hiring and community examples                                                        |                                                                                               |


**Stack (V1):**


| Layer                 | Library                                                        |
| --------------------- | -------------------------------------------------------------- |
| Language              | Go (version policy → ADR-012)                                  |
| Controller framework  | controller-runtime (kubebuilder v4 scaffold)                   |
| Kubernetes API        | `sigs.k8s.io/controller-runtime/pkg/client` → client-go        |
| CRD / webhook codegen | controller-gen, kubebuilder markers                            |
| Neo4j admin           | `github.com/neo4j/neo4j-go-driver/v5` in `internal/neo4j` only |
| Tests                 | Go `testing`, Ginkgo/Gomega optional; envtest; kind e2e        |




### Option B — Java with Fabric8 / Vert.x (Strimzi model)


| Advantages                                                                                                                                           | Disadvantages                                                                                      |
| ---------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| **Proven at scale** — Strimzi: multi-CRD, multi-namespace, delegated RBAC ([strimzi.md](../../architecture/operator-benchmark/operators/strimzi.md)) | **Not the Kubernetes default** — no kubebuilder; hand-built assembly operators                     |
| Rich **Kafka AdminClient** in-process (analogue for broker lifecycle)                                                                                | Neo4j admin path is **Bolt**, not a Java-first in-JVM API for our use case                         |
| Maven multi-module structure maps cleanly to `model/` + `assembly/`                                                                                  | **Vert.x async** (`Future` chains) vs idiomatic synchronous `Reconcile()` — different mental model |
| Large Strimzi community and docs                                                                                                                     | **JVM footprint** — memory, image size, cold start for a control-plane-only pod                    |
|                                                                                                                                                      | **Invalidates** proposed Go ADRs 002–010 and envtest/kind toolchain                                |
|                                                                                                                                                      | CI: Maven, SpotBugs; different from golangci-lint / govulncheck plan                               |


Strimzi entry: `ClusterOperator.java` → `KafkaAssemblyOperator.reconcileAssembly()` with pure builders in `model/KafkaCluster.java` and Fabric8 for API calls. **We adopt that layering idea in Go**; we do **not** adopt the Java stack ([synthesis.md](../../architecture/operator-benchmark/synthesis.md) — Avoid Java stack).

### Option C — Other runtimes (Python Kopf, Rust kube-rs, …)


| Advantages                                  | Disadvantages                                                             |
| ------------------------------------------- | ------------------------------------------------------------------------- |
| Kopf / kube-rs viable for smaller operators | Weak Neo4j ecosystem fit — no official Bolt story comparable to Go driver |
|                                             | No Tier-1 database operator benchmark in repo for these stacks            |
|                                             | Team and PS tooling already oriented to Go (Helm chart Go code)           |


**Not pursued** — insufficient evidence and no Neo4j-specific client advantage.

---



## Comparison


| Criterion                        | A Go + kubebuilder     | B Java + Fabric8 (Strimzi)                   | C Other         |
| -------------------------------- | ---------------------- | -------------------------------------------- | --------------- |
| K8s ecosystem alignment          | **Best**               | Good (Fabric8)                               | Variable        |
| Neo4j Bolt / formation           | **Official Go driver** | Possible via driver; not Neo4j operator norm | Weak            |
| Reference operator (CNPG)        | **Direct**             | Patterns only                                | None in catalog |
| Image / memory                   | **Small binary**       | JVM                                          | Varies          |
| Webhook + CEL (ADR-001)          | **Native**             | Hand-rolled                                  | Varies          |
| Existing ADR / scaffold fit      | **Yes**                | Rewrite                                      | Rewrite         |
| Strimzi operational patterns     | Adapt in Go            | **Native**                                   | N/A             |
| Contributor / hiring for K8s ops | **High**               | Medium (Java, not K8s-Go)                    | Low             |


---



## Decision

**We will implement the Neo4j Kubernetes operator in Go**, using **kubebuilder** and **controller-runtime**, following **CloudNativePG** as the primary code-layout reference and **Strimzi** for operational patterns (scope, RBAC delegation, `model`/`assembly` split) translated into Go packages per [ADR-002](002-package-layering.md).

### Why Go (summary)

1. **The operator is not Neo4j server code** — it is a Kubernetes controller. The winning runtimes for that role in 2024–2026 are Go-first because `client-go` and controller-runtime are maintained by the same ecosystem as the API machinery.
2. **CNPG is the implementation north star** — same class of problem (StatefulSets, formation, webhooks, status). Copy structure, not Java from Strimzi.
3. **Bolt formation belongs in the operator process** ([ADR-007](007-formation-and-bolt.md)) — the official **neo4j-go-driver** keeps admin I/O in one language without JNI or sidecar bridges.
4. **Admission and validation** ([ADR-001](001-crd-validation-process.md)) — CEL on Go types and kubebuilder webhook scaffolding are the supported path; Strimzi validates largely at reconcile time instead.
5. **Operational cost** — a single static binary and smaller container suit a namespace-scoped control plane ([BDR-003](../business/operator/003-operator-install-scope.md)) better than a JVM cluster operator unless there is a strong in-process library win (Strimzi has Kafka AdminClient; we have Go Bolt).
6. **Continuity** — ADR-002..010, benchmark synthesis, and Helm chart Go code already assume this stack; changing language would delay V1 without user-facing benefit.



### Why not Java (despite Strimzi)

Strimzi demonstrates that **Java can run a world-class operator**, but its advantages (Kafka AdminClient, years of Strimzi-specific modules) do not transfer cleanly to Neo4j. We **study Strimzi** for watch scope, RBAC comments, install YAML, and assembly layering — documented in [strimzi.md](../../architecture/operator-benchmark/operators/strimzi.md) — and **implement in Go**.

### Implementation guardrails


| Area         | Rule                                                                  |
| ------------ | --------------------------------------------------------------------- |
| Scaffold     | kubebuilder v4 project layout; `api/`, `cmd/`, `config/`, `internal/` |
| Controllers  | controller-runtime `Reconcile()`; no Vert.x-style custom loop         |
| Neo4j I/O    | `neo4j-go-driver` only from `internal/neo4j`; never from `render/`    |
| Benchmark    | CNPG for Go deps and tests; Strimzi for ops/RBAC only                 |
| Version pins | Deferred to **ADR-012** (Go version, `k8s.io/`*, controller-runtime)  |


---



## Consequences



### Positive

- One language for operator core, Bolt client, and envtest/e2e tooling.
- Direct reuse of kubebuilder make targets, CRD generation, and community runbooks.
- Aligns with CNPG quality gates (golangci-lint, govulncheck) planned in ADR-018.
- Strimzi remains a valid **design reference** without JVM operational cost.



### Negative

- Cannot copy Strimzi Java modules verbatim — assembly logic must be re-authored in Go.
- Must enforce thin controllers and package boundaries; Go does not prevent monolithic reconcilers.
- Teams with only Java operator experience need Go onboarding (offset by K8s operator hiring pool).



### Neutral

- CRDs, RBAC YAML, and install manifests remain language-independent.
- A future satellite component (e.g. heavy batch job) could use another language if isolated — out of scope for the main operator Deployment.

---



## References

- [operator-benchmark/synthesis.md](../../architecture/operator-benchmark/synthesis.md) — D1 language row; Adopt CNPG / Avoid Java stack
- [strimzi.md](../../architecture/operator-benchmark/operators/strimzi.md) — Java/Fabric8/Vert.x evidence
- [cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md) — Go/kubebuilder evidence
- [ADR-001](001-crd-validation-process.md) · [ADR-002](002-package-layering.md) · [ADR-007](007-formation-and-bolt.md) · [ADR-010](010-operator-deployment.md)
- [BDR-003](../business/operator/003-operator-install-scope.md)
- Kubernetes controller pattern: [Operator SDK — operator scope](https://sdk.operatorframework.io/docs/building-operators/golang/operator-scope/)

