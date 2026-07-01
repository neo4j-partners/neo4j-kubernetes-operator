# BDR-003 — Operator install scope (namespace / namespaces / cluster)

| | |
|---|---|
| **Status** | proposed |
| **Reviewers** | Charles Boudry; Marouane Gazanayi |
| **Date** | 2026-06-22 |
| **Deciders** | Operator design team |
| **Constraints** | `OP-1-001`, `OP-1-002`, `OP-1-006`; variants `OP-2-001-SCOPE-01`…`03` |

---

## Context

The Neo4j Kubernetes Operator is a continuously running controller. At install time, we must decide **which namespaces it watches** for `Neo4j`, `Neo4jDatabase`, `Neo4jBackup`, and `Neo4jRestore` resources.

Three scope modes are already defined in requirements:

| Mode | Variant ID | Behaviour | Typical config |
|------|------------|-----------|----------------|
| **Single namespace** | `OP-2-001-SCOPE-01` | Watch and reconcile only the namespace where the operator runs | `WATCH_NAMESPACE` = operator pod namespace |
| **Multiple namespaces** | `OP-2-001-SCOPE-02` | Watch a configured list of namespaces | `WATCH_NAMESPACE` = comma-separated list |
| **Cluster-wide** | `OP-2-001-SCOPE-03` | Watch all namespaces | `WATCH_NAMESPACE` unset or `*` |

This decision affects RBAC (`Role` vs `ClusterRole`), blast radius, install documentation, OLM `installModes`, and PS delivery patterns. It is independent of [BDR-001](../neo4j/001-single-neo4j-crd.md) (workload CRD shape) and [BDR-002](../neo4j/002-neo4j-crd-topology.md) (topology model).

**Important**: CRDs remain cluster-scoped API objects in all modes. Scope applies to the **controller**, not to CRD registration.

**Watch scope vs deployment layout**: Single-namespace **watch scope** (Option A) means the controller reconciles CRs only in the namespace where it runs. That is independent of **where customers should install** the operator. We always recommend a **dedicated operator namespace** (default `neo4j-operator-system`) — not co-located with unrelated application workloads. With V1 scope, `Neo4j` CRs are created in that same namespace.

---

## Analysis

### Option A — Single namespace

| Advantages | Disadvantages |
|------------|---------------|
| Smallest RBAC footprint — `Role` in one namespace | One operator install per namespace if teams are isolated |
| Aligns with Neo4j Helm chart (namespace-local release) | Platform teams must duplicate operator if each team has its own namespace |
| Lowest blast radius — reconciliation bugs contained | Neo4j CRs in other namespaces are ignored |
| Easier security review and customer sign-off | Does not satisfy "one operator for whole cluster" out of the box |
| PS can install without cluster-admin (after CRDs) | |
| Matches P0 tests (`TST-SCN-003`) and AC group `AC-OP-SCOPE-SINGLE` | |

### Option B — Multiple namespaces

| Advantages | Disadvantages |
|------------|---------------|
| One operator serves several fixed namespaces | RBAC must be provisioned **per watched namespace** |
| Useful for platform teams with a known namespace set | Highest informer overhead (Strimzi documents this) |
| Middle ground between isolation and centralisation | More complex install docs and support matrix |
| OLM `MultiNamespace` install mode | Easy to misconfigure (missing Role in one namespace) |
| | MongoDB pattern: operator does **not** auto-create ServiceAccounts in target namespaces — extra manual steps |

### Option C — Cluster-wide

| Advantages | Disadvantages |
|------------|---------------|
| One operator for entire cluster | `ClusterRole` — harder security approval |
| New namespaces automatically covered | Largest blast radius — aligns with `00-vision.md` operational risk |
| Natural fit for central platform / OLM `AllNamespaces` | Higher controller memory / watch load at scale |
| CNPG / ECK default for "platform" positioning | Overkill when customer runs a single Neo4j in one namespace |
| | Conflicts if multiple operator instances watch same CRs |


### What other operators do

Survey of stateful / database operators (2024–2026). Most mature operators support **more than one mode**; defaults vary by vendor and audience.

| Operator | Default install scope | Single namespace | Multiple namespaces | Cluster-wide | Config mechanism |
|----------|----------------------|------------------|---------------------|--------------|------------------|
| [CloudNativePG](https://cloudnative-pg.io/) | **Cluster-wide** | Yes (`config.clusterWide=false`) | Yes (`WATCH_NAMESPACE` comma-separated) | Yes (default) | Helm `clusterWide`, ConfigMap `WATCH_NAMESPACE` |
| [Strimzi](https://strimzi.io/) | **Single namespace** (quick start) | Yes (default install) | Yes (documented; higher overhead) | Yes (`STRIMZI_NAMESPACE=*` + ClusterRoleBindings) | Env `STRIMZI_NAMESPACE` |
| [MongoDB Community Operator](https://github.com/mongodb/mongodb-kubernetes-operator) | **Single namespace** (Helm default) | Yes | Yes (comma-separated `WATCH_NAMESPACE`) | Yes (`WATCH_NAMESPACE=*` + cluster RBAC) | Helm `operator.watchNamespace`, env var |
| [MongoDB Controllers (Enterprise)](https://www.mongodb.com/docs/kubernetes/current/tutorial/set-scope-k8s-operator/) | Configurable at install | Yes | Yes | Yes | Helm / YAML `WATCH_NAMESPACE` |
| [ECK (Elasticsearch)](https://www.elastic.co/docs/deploy-manage/deploy/cloud-on-k8s/install) | **Cluster-wide** | Yes (`profile-restricted.yaml`) | Yes (`managedNamespaces: {a,b}`) | Yes (default Helm) | Helm `managedNamespaces`, `createClusterScopedResources` |
| [Percona Operators](https://docs.percona.com/) (PG, MongoDB, MySQL) | **Single namespace** (typical Helm) | Yes | Partial / product-specific | Often via OLM `AllNamespaces` | OLM `installModes`, `WATCH_NAMESPACE` |
| [Operator SDK / OLM convention](https://sdk.operatorframework.io/docs/building-operators/golang/operator-scope/) | Varies | `OwnNamespace` / `SingleNamespace` | `MultiNamespace` | `AllNamespaces` | CSV `installModes`, `WATCH_NAMESPACE` |

**Patterns observed:**

1. **Configurable scope is the norm** for production-grade operators — rarely hard-coded cluster-only or namespace-only.
2. **Defaults split by audience**: platform-wide operators (CNPG, ECK) default cluster-wide; operators often installed per-team (Strimzi quick start, MongoDB Helm) default single-namespace.
3. **Multi-namespace is the awkward middle**: Strimzi explicitly warns it has the **highest watch overhead**; single-namespace or all-namespaces are preferred for performance.
4. **RBAC follows scope**: namespace modes use `Role` + `RoleBinding`; cluster-wide uses `ClusterRole` + `ClusterRoleBinding` (plus per-namespace workload RBAC in some designs — e.g. MongoDB database ServiceAccount in each target namespace).
5. **Security-conscious installs** document a "restricted" profile (ECK `profile-restricted`, CNPG `clusterWide=false`) — same operator, fewer cluster permissions.

---

### What customers prefer

There is **no quantitative Neo4j customer survey** in this design package. Preferences below are inferred from PS field patterns, enterprise Kubernetes practice, and requirements already captured in `01` / `03`.

| Persona / scenario | Typical preference | Rationale |
|--------------------|-------------------|-----------|
| **Enterprise app team** (one Neo4j per product) | **Single namespace** | Matches Helm mental model; minimal RBAC; clear ownership boundary |
| **Regulated industries** (finance, healthcare) | **Single namespace** | Least privilege; smaller blast radius if reconciliation bugs occur (`00-vision.md` § Operational risk) |
| **Central platform team** (many Neo4j instances) | **Cluster-wide** or **multi-namespace** | Single operator install; GitOps across namespaces |
| **OpenShift / OLM consumers** | **All install modes** expected | OLM `OperatorGroup` + CSV `installModes` are standard procurement checks |
| **Multi-tenant SaaS on shared K8s** | **Single namespace** per tenant | Strong isolation; operator compromise does not cross tenant boundary |

**Signals already in requirements:**

| Source | Signal |
|--------|--------|
| `OP-2-001-SCOPE-01` | `V1=Yes`, `Priority=P0` — single namespace is the **committed V1 variant** |
| `OP-2-001-SCOPE-02`, `OP-2-001-SCOPE-03` | `V1=No`, `Priority=P2` — deferred |
| `AC-OP-SCOPE-SINGLE-004` | RBAC namespace-scoped except CRD install — matches regulated customer asks |
| `00-vision.md` | Blast-radius mitigation explicitly lists **namespace scope** |

**Summary:** the most common first install (PS engagements, Helm migrations, app-team ownership) is **single namespace**. **Cluster-wide** is requested mainly by central platform teams — valid, but not the majority of initial V1 adopters. **Multi-namespace** is a niche; important for OLM parity later, not V1-critical.

---



## Decision

**We will ship V1 with Option A — single-namespace scope only** (`OP-2-001-SCOPE-01`).

For V1:

- The operator watches **only the namespace in which it is deployed**.
- **Deploy the operator in its own dedicated namespace** (default `neo4j-operator-system`, overridable at install). Do not install into a shared application namespace alongside unrelated workloads. With V1 scope, `Neo4j` CRs and reconciled operands live in that same namespace.
- Workload RBAC uses namespace-scoped `Role` / `RoleBinding` (per `AC-OP-SCOPE-SINGLE-004`).
- CRD installation may still require cluster-admin once; day-2 operation should not.
- Packaging (YAML / Helm) documents this as the **only supported** scope in V1.

**We will not implement** multi-namespace or cluster-wide scope in V1. Variants `OP-2-001-SCOPE-02` and `OP-2-001-SCOPE-03` remain in `01` / `03` as **deferred** requirements for V1.1+.

Options B and C are **not rejected** — deferred. Code should use the standard `WATCH_NAMESPACE` pattern so wider scope does not require a reconciler rewrite later.

### Implementation guardrails (V1)

| Area | Rule |
|------|------|
| `cmd/manager/main.go` | Read `WATCH_NAMESPACE`; V1 Helm/YAML sets it to the operator pod namespace |
| Install namespace | Default `neo4j-operator-system`; install manifests create the namespace; `Neo4j` CRs in the same namespace |
| RBAC manifests | `config/rbac/` — namespace `Role` only; no `ClusterRole` for reconciliation |
| Tests | `TST-SCN-003` (P0) is the scope gate |
| Docs | Quickstart (`EST-DOC-001`) states single-namespace as V1 default and only mode; recommends dedicated operator namespace |

### V1 scope

| In V1 | Deferred |
|-------|----------|
| `OP-2-001-SCOPE-01` — single namespace | `OP-2-001-SCOPE-02` — multiple namespaces |
| `AC-OP-SCOPE-SINGLE-*` | `AC-OP-SCOPE-MULTI-*`, `AC-OP-SCOPE-CLUSTER-*` |
| `TST-SCN-003` (P0 E2E) | `TST-SCN-004`, `TST-SCN-005` (P2) |
| Namespace-scoped operator RBAC | OLM `installModes` for Multi / AllNamespaces |
| | `profile-restricted` vs `profile-platform` Helm split (optional V1.1) |

---

## Consequences

### Positive

- Fastest path to enterprise security sign-off — minimal cluster permissions for day-2.
- Dedicated operator namespace isolates the control plane from unrelated application workloads.
- Consistent with Neo4j Helm chart: one namespace, one deployment.
- Matches the install pattern PS and app teams request most often.
- Reduces V1 test and documentation surface — one scope matrix row.
- Aligns with blast-radius mitigations in `00-vision.md`.

### Negative

- Central platform teams must install one operator per namespace in V1, or wait for V1.1+.
- Not OLM-complete until multi / all-namespaces modes are implemented.
- PS must set expectations for customers who expect ECK/CNPG-style cluster-wide defaults.