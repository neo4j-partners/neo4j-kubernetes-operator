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

- The operator watches **only the namespace specified by the user when it's installed**.
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

## Amendment 2026-09-29 — Opt-in cluster-wide scope (`OP-2-001-SCOPE-03`)

**Status**: accepted (2026-09-30) · **Trigger**: customer demand to watch all namespaces (incl. namespaces created later) without editing operator config per namespace · **Branch**: `feature/cluster-wide-scope`

> **Verified 2026-09-30** — implementation and tests landed on `feature/cluster-wide-scope`: unit tests (`resolveWatchScope`), the `operator-scope-clusterwide` e2e suite (reconcile in un-configured namespaces, single `ClusterRole`, operator-namespace exclusion), and manual verification on AKS all pass. The namespaced `operator-scope` suite still passes (no regression).

This amendment adds the deferred cluster-wide variant as an **explicit opt-in**. It does **not** change the default: namespace-scoped install remains the default and the recommended posture. Multi-namespace (`OP-2-001-SCOPE-02`) stays as-is (explicit list). It also **supersedes the Context table's SCOPE-03 config mechanism** (the row reading "`WATCH_NAMESPACE` unset or `*`"): cluster-wide is now reached only via `clusterWide`, and unset / `*` remain rejected.

### Why an explicit mode and not `*`-as-namespaced

We evaluated a middle option — honor a sentinel meaning "all namespaces" while keeping namespaced `Role` RBAC. It does not hold together:

- Kubernetes `list/watch` is either **single-namespace** or **all-namespaces (cluster scope)**. "All namespaces" is therefore either one cluster-scoped watch (needs a `ClusterRole`) or N single-namespace watches (needs a `Role` in each of N, which must be **enumerated**).
- Enumerating at install (Helm `lookup`) only captures namespaces that exist **at that moment**, returns empty under offline `helm template` / `--dry-run` / GitOps rendering, and silently misses namespaces created later — defeating the one benefit that motivated the request.
- N per-namespace `Role`s granting `secrets: get/list/watch` carry the **same order-of-magnitude blast radius** as a `ClusterRole`: the only reduction is the namespaces you deliberately omit — and choosing that set *is* the explicit-list feature we already ship. The hybrid adds objects to keep in sync without buying safety the explicit list doesn't already give.

Conclusion: dynamic all-namespace coverage genuinely requires a `ClusterRole`. We ship it honestly rather than as a namespaced illusion.

### Decision

- Add a Helm value **`clusterWide: true`** (default `false`). When set, the chart renders a `ClusterRole` + `ClusterRoleBinding` for the **manager rules** **instead of** the per-namespace manager `Role`/`RoleBinding`. The leader-election `Role`/`RoleBinding` stay namespaced in the operator namespace either way.
- **Signal the mode out-of-band, not in `WATCH_NAMESPACE`.** The chart sets a dedicated env var **`WATCH_ALL_NAMESPACES=true`** and leaves `WATCH_NAMESPACE` empty. `WATCH_NAMESPACE` keeps its single type — a list of namespace names — with `*` and empty **still rejected**. This preserves *fail-closed*: cluster-wide requires an affirmative, self-documenting flag; an unset, empty, or mistyped scope still errors rather than silently watching the whole cluster (rationale below).
- The manager runs a cluster-scoped cache (empty `DefaultNamespaces`) in this mode.
- **NEO-016 intent is preserved, belt-and-suspenders.** The cluster-wide cache **excludes the operator's own namespace** via a cache `FieldSelector` (`metadata.namespace != POD_NAMESPACE`) *and* the reconciler keeps refusing any `Neo4j` CR whose namespace equals `POD_NAMESPACE`. The cache exclusion alone would be the only barrier — the `ClusterRole` still grants access there — so the near-free reconcile-time guard removes that single point of failure. The render-time `_helpers` guard remains the enforcement for namespaced mode.
- **Off by default; namespaced remains recommended.** Docs must state the blast radius (below) so customers opt in knowingly. At most **one** cluster-wide instance per cluster, and it must not overlap an existing namespaced install — no technical guard (leader-election leases live in each operator's own namespace, so instances in different namespaces don't contend the same lease); documented.

### Why `WATCH_ALL_NAMESPACES`, not a `WATCH_NAMESPACE` sentinel

- **Fail-closed.** The conventional controller-runtime signal for all-namespaces is an *empty* namespace list — but we deliberately made empty an error to stop accidental cluster-wide. A dedicated flag keeps that: cluster-wide is reachable only by setting a flag whose name says what it does, never by forgetting or mistyping a value.
- **Out-of-band, not in-band.** `WATCH_NAMESPACE` is data (a list of DNS-1123 names); mode is control. Encoding control in the data field is the exact ambiguity we rejected `*` for; a second magic token re-introduces it.
- **No value-space collision.** A friendly token like `all` could be a real namespace name; `*` avoids collision only because it's an invalid name (still a magic string). A boolean env var can never collide.
- **Orthogonal validation.** `watch.go` validates two independent things — `WATCH_ALL_NAMESPACES ⇒ WATCH_NAMESPACE` empty; otherwise a non-empty list with `*`/empty/operator-ns rejected — instead of one parser entangling "is this cluster-wide?" with "is this list valid?".
- **Self-documenting/greppable** in the rendered Deployment; no tribal knowledge that `*` means cluster-wide.
- **Cost:** the two vars can disagree (`WATCH_ALL_NAMESPACES=true` with a non-empty `WATCH_NAMESPACE`). The chart never emits that pair, and `watch.go` rejects it at startup with a clear message — a better failure than a silently-overloaded token.

### Risks accepted with this mode

| # | Risk | Handling |
|---|------|----------|
| 1 | **Cluster-wide Secret read** — `ClusterRole` grants `secrets get/list/watch` in every namespace; K8s cannot scope to "only Neo4j's Secrets". Operator compromise = cluster-wide credential exposure. | Not mitigable via RBAC. The existing opt-in Secret **label** mechanism is unchanged (it gates *mounting*, not *reading*); the wider read surface is **documented** at install (NOTES) and in the scope guide. Deliberate trade-off of this mode. |
| 2 | **Privilege escalation (bounded)** — `create` on `roles`/`rolebindings` cluster-wide lets the operator bind privileges in any namespace. Kubernetes escalation-prevention bounds this to permissions the operator **already holds** (it lacks `bind`/`escalate`), but that held set is broad. | Documented; inherent to the mode. Reviewers must treat a cluster-wide install as a cluster-privileged component. |
| 3 | **Reconciles unintended namespaces** — acts on `Neo4j` CRs in any namespace, incl. system namespaces. | Operator namespace excluded (above). Other namespaces are in scope by definition of the mode. |
| 4 | **Memory / API load at scale** — cluster-scoped informers cache all watched kinds cluster-wide. | Documented; `resources` defaults are namespaced-sized and must be revisited for cluster-wide. |
| 5 | **Multiple-instance conflicts** — two cluster-wide operators, *or a cluster-wide instance overlapping an existing namespaced install*, fight over the shared CRs. | Documented "one cluster-wide instance, no overlap"; no auto-guard because leader-election leases live in each operator's own namespace, so cross-namespace instances don't share a lease. |
| 6 | **Security sign-off regression** — expands audit surface vs least-privilege default. | Off by default; explicit opt-in; namespaced remains the recommended, documented default. |
| 7 | **Test / support surface** — new e2e (cluster-wide reconcile, `ClusterRole` RBAC, cross-namespace isolation, operator-namespace exclusion). | Add coverage before marking this amendment `accepted`. |

### Implementation guardrails (delta from V1)

| Area | Rule |
|------|------|
| `charts/.../values.yaml` | Add `clusterWide: false`. Change the `watchNamespaces` default to `[]`. Require **exactly one**: fail render if `clusterWide` and a non-empty `watchNamespaces` are both set, and (namespaced mode) fail if both are empty. |
| RBAC templates | `clusterWide` selects a `ClusterRole`+`ClusterRoleBinding` for the **manager rules** instead of the per-namespace manager `Role`/`RoleBinding`. Leader-election `Role`/`RoleBinding` stay namespaced in the operator namespace either way. |
| `_helpers.tpl` | When `clusterWide`, emit env `WATCH_ALL_NAMESPACES=true` and leave `WATCH_NAMESPACE` empty; keep the operator-namespace `fail` guard for namespaced `watchNamespaces`. |
| `cmd/manager/watch.go` | `WATCH_ALL_NAMESPACES=true` ⇒ cluster-wide (require `WATCH_NAMESPACE` empty, else error). Otherwise today's rules: non-empty list, `*`/empty/operator-namespace rejected. |
| `cmd/manager/main.go` | Cluster-wide ⇒ empty `DefaultNamespaces` + cache `DefaultFieldSelector` `metadata.namespace != POD_NAMESPACE`. Verify the leader-election lease and webhook-cert Secret are read off-cache so the exclusion can't starve the manager of its own objects. |
| Webhook | `ValidatingWebhookConfiguration` is cluster-scoped: in cluster-wide mode it validates CRs in every namespace, including the excluded operator namespace (validated but never reconciled). Note in docs, or set a `namespaceSelector`. |
| Docs | `04-operator-scope.md` "There is no cluster-wide mode" line updated to describe the opt-in and its blast radius; NOTES warns at install. |
| Tests | Cluster-wide reconcile E2E, `ClusterRole` RBAC assertions, cross-namespace isolation, operator-namespace exclusion (cache + reconcile guard), and the off-cache own-objects check — before status → `accepted`. |

Accepted once the implementation and its tests landed on `feature/cluster-wide-scope` and passed on AKS (see the verification note above).

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