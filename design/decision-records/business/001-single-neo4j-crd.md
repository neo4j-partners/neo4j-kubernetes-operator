# BDR-001 — Single `Neo4j` CRD instead of `Neo4jStandalone` + `Neo4jCluster`

| | |
|---|---|
| **Status** | proposed |
| **Reviewers** | Charles Boudry, Marouane Gazanayi |
| **Date** | 2026-06-18 |
| **Deciders** | Operator design team |
| **Constraints** | `01-functional_requirements.csv` (NEO-001, NEO-002), Helm chart parity |

---

## Context

Neo4j can be deployed in two topologies:

- **Standalone** — single instance (`NEO-001`)
- **Cluster** — multi-member HA deployment (`NEO-002`)

We must expose this in the Kubernetes API. Four structural options are on the table:

1. **Separate topology CRDs** — `Neo4jStandalone` and `Neo4jCluster` as distinct `kind`s (Option B)
2. **Single workload CRD** — one `Neo4j` `kind`, topology selected via `spec.topology.mode` (Option A)
3. **Parent + child infra CRDs** — `Neo4j` plus `Neo4jPersistence`, `Neo4jConnectivity`, … (Option C)
4. **Cluster + instance hierarchy** — `Neo4jInstance` and `Neo4jCluster` at the top; cluster creates child `Neo4jInstance` CRs (Option D)
5. **Cluster-only API** — no standalone mode; single-node = cluster of 1 member (Option E) — *likely aligned with Neo4j **Aura** product modelling; worth reviewing internal Product design before dismissing*

The existing Helm chart already treats both modes as the same release with different values (`neo4j.standalone` vs cluster sizing). Users think in terms of **one Neo4j deployment**, not two different resource kinds.

---

## Analysis

### What other operators do

Survey of database / stateful workload operators (2024–2026). Pattern for **single-instance vs clustered** deployment:

| Operator | Workload CRD | How topology is expressed | Single vs cluster |
|----------|--------------|---------------------------|-------------------|
| [MongoDB Community Operator](https://github.com/mongodb/mongodb-kubernetes-operator) | `MongoDBCommunity` | `spec.members` (replica set size) | **One kind** — scale defines topology |
| [Percona Server MongoDB Operator](https://github.com/percona/percona-server-mongodb-operator) | `PerconaServerMongoDB` | `replsets[].size` | **One kind** |
| [CloudNativePG](https://cloudnative-pg.io/) | `Cluster` | `spec.instances` (1 = single-node) | **One kind** — 1 instance ≈ standalone, same code path |
| [Strimzi](https://strimzi.io/) (`Kafka` + `KafkaNodePool`) | `Kafka` + `KafkaNodePool` | Node pools group brokers under a Kafka CR | **Partial hierarchy** — pool CR, not per-broker |
| [Vitess](https://vitess.io/) | `VitessCluster` + `VitessTablet` | Cluster orchestrates tablet CRs | **Parent creates children** — closest to Option D |
| [Elasticsearch ECK](https://www.elastic.co/elastic-cloud-kubernetes) | `Elasticsearch` | `nodeSets[]` | **One kind** — node sets replace mode split |
| [CockroachDB Operator](https://github.com/cockroachdb/cockroach-operator) | `CrdbCluster` | `spec.nodes` | **One kind** |
| [Neo4j Helm chart](https://neo4j.com/docs/operations-manual/current/kubernetes/) | *(no CRD)* | `neo4j.standalone` / `minimumClusterSize` values | **One chart** — boolean + sizing |
| [Opstree Redis Operator](https://github.com/OT-CONTAINER-KIT/redis-operator) | `Redis`, `RedisCluster`, `RedisReplication`… | **Separate kinds** per deployment pattern | **Split** — exception to the trend |
| [MongoDB Multi-Cluster](https://github.com/mongodb/mongodb-kubernetes-operator) (`MongoDBMultiCluster`) | Advanced CRD | Multi-**Kubernetes**-cluster deployments | Separate **advanced** CRD, not standalone vs cluster |

**Trend**: mainstream operators use **one workload CRD** and express size/topology via spec fields. Splits by topology (Opstree Redis) exist but add RBAC, documentation, and migration overhead. Advanced multi-cluster scenarios get a **separate** CRD when the problem domain is genuinely different — not for standalone vs cluster on the same cluster.

---

### Option A — Single `Neo4j` CRD with `spec.topology.mode`

| Advantages | Disadvantages |
|------------|---------------|
| Aligns with Helm chart and CNPG / Strimzi / MongoDB Community operators | Larger OpenAPI schema |
| Single `kind` for RBAC, docs, monitoring, `kubectl` | Conditional validation rules (CEL / webhook) |
| Shared `spec` — no duplication of persistence / TLS / networking | `status` and Ready phases differ by mode |
| GitOps-friendly — same resource, topology patch (with immutability rules) | `Neo4jReconciler` complexity risk if layering is wrong |
| One reconciler; branch only in `domain/workload` | Learning curve — must read `topology` before interpreting spec |
| Consistent with embedded infra `spec` sections (no `Neo4jPersistence` / `Neo4jConnectivity` CRDs) | |

---

### Option B — `Neo4jStandalone` + `Neo4jCluster` CRDs

| Advantages | Disadvantages |
|------------|---------------|
| Smaller schema per CRD | ~80% `spec` field duplication |
| Simpler validation per kind | Two kinds to document, authorize in RBAC, and monitor |
| Explicit separation in `kubectl get` | Resource migration if topology changes |
| Potentially simpler reconciler per kind | Behaviour drift between standalone and cluster over time |
| | Inconsistent with Helm chart (single `values.yaml`) |
| | Against dominant market practice (see table above) |

---

### Option C — Parent `Neo4j` CRD + child infra CRDs

A **parent** workload CRD holds topology, image, and edition. Infra concerns become **child CRDs** linked via `ownerReferences` or `spec.neo4jRef`:

```yaml
kind: Neo4j
metadata:
  name: orders
spec:
  topology: { mode: Cluster, members: 3 }
  edition: enterprise
---
kind: Neo4jPersistence
spec:
  neo4jRef: { name: orders }
  data: { size: 100Gi, storageClass: fast }
---
kind: Neo4jConnectivity
spec:
  neo4jRef: { name: orders }
  services: { bolt: { type: LoadBalancer } }
# Neo4jTrust, Neo4jServerConfig, …
```

| Advantages | Disadvantages |
|------------|---------------|
| Smaller OpenAPI schema per concern — easier to read one CRD at a time | **Many kinds** per deployment (5+ CRDs for a single Neo4j instance) |
| Independent reconcile and RBAC per domain (platform team owns `Neo4jPersistence`, security owns `Neo4jTrust`) | Complex **ordering and dependencies** — `Neo4j` must wait for child CRs or vice versa |
| Focused reconciler per infra CRD — narrow blast radius on change | **GitOps friction** — easy to apply an incomplete set of manifests |
| Theoretical reuse of infra CRDs across workloads | No major database operator uses this pattern for persistence / TLS / networking |
| Clear separation of concerns in large, siloed organisations | Fragmented **status** — health spread across multiple `kubectl get` |
| Child CRs can be added or updated independently after install | Inter-CRD references (`neo4jRef`) and validation webhooks across kinds |
| | Inconsistent with Helm chart (single `values.yaml` tree) |
| | Higher support burden — "which CR is failing?" |
| | Duplicates BDR-001 topology question *and* adds infra CR proliferation |

---

### Option D — `Neo4jInstance` + `Neo4jCluster` (cluster creates instance CRs)

**`Neo4jInstance`** is the atomic server unit. **`Neo4jCluster`** is the top-level cluster orchestrator that **materialises** one `Neo4jInstance` CR per cluster member (via `ownerReferences`). Standalone = user applies a single `Neo4jInstance` with no cluster parent.

```yaml
# Standalone — one top-level Instance CR
kind: Neo4jInstance
metadata:
  name: orders
spec:
  edition: enterprise
  persistence: { ... }
  connectivity: { ... }
---
# Cluster — Cluster CR + operator-owned Instance children
kind: Neo4jCluster
metadata:
  name: orders
spec:
  members: 3
  edition: enterprise
  persistence: { ... }    # shared defaults propagated to instances
  connectivity: { ... }
# ↓ created by Neo4jClusterReconciler (not hand-written in GitOps)
kind: Neo4jInstance
metadata:
  name: orders-0
  ownerReferences: [{ kind: Neo4jCluster, name: orders }]
spec:
  clusterRef: { name: orders }
  serverId: 0
# orders-1, orders-2 …
```

| Advantages | Disadvantages |
|------------|---------------|
| **Per-server CR** — visibility, status, and events per cluster member in `kubectl` | **N+1 resources** in cluster mode (`1 Cluster + N Instances`) |
| Unified server primitive — standalone and cluster members share `Neo4jInstance` schema | Still **two top-level kinds** (`Neo4jInstance` vs `Neo4jCluster`) for entry |
| Targeted ops — replace or debug one member via its Instance CR | Operator-owned children **fight GitOps** if users also manage Instance manifests |
| Mirrors Neo4j domain model (cluster = ensemble of servers) | Risk of **spec drift** between Cluster template and child Instances |
| `Neo4jClusterReconciler` focuses on formation / scale; `Neo4jInstanceReconciler` on one server | Two reconcilers + cross-watch logic (cluster ↔ instances) |
| Closer to Vitess (`VitessCluster` → `VitessTablet`) than Option B | **Vitess is the exception**, not the database operator norm |
| Per-member status (e.g. formation, enablement) on Instance `status` | Inconsistent with Helm chart (no per-server CR) and StatefulSet idiom (one SS, many pods) |
| Scale up = create new Instance CR; scale down = delete — explicit audit trail | Duplicated shared config (image, TLS, persistence) on Cluster **and** each Instance unless carefully templated |
| | User confusion: "Do I create Instances or does the Cluster?" |
| | `Neo4jDatabase`, backup, restore must reference Cluster or Instance? — ambiguous `neo4jRef` |
| | More CRDs to document and RBAC than Option A |

---

### Option E — Cluster-only (`members: 1` instead of standalone)

**No standalone mode.** The API always deploys Neo4j in **cluster topology**. A former "standalone" deployment is a **cluster of one member** (`spec.members: 1`). HA production remains `spec.members: 3` (or more).

#### Aura / Product alignment

Neo4j **Aura** (managed product) likely follows a similar philosophy: there is no separate “standalone product” vs “cluster product” at the control-plane level — topology is expressed as **member count and roles** on a unified deployment model. The Product engineering group may already have solved standalone-vs-cluster unification, validation rules, and upgrade paths under that assumption.

**It would be prudent to review Aura’s internal API and lifecycle design** (or engage Product architecture) before treating Option E as rejected:

- How single-node vs HA is represented without a `Standalone` mode
- Whether formation / discovery overhead for `members: 1` is acceptable in practice
- Shared reconciler patterns PS can reuse rather than reinvent

This does **not** commit PS to Option E — but ignoring Product’s direction risks building an operator API that diverges from Neo4j’s long-term platform model. Outcome of that review should feed [BDR-002](002-neo4j-crd-topology.md) and `00-vision.md` (Product sponsorship).

```yaml
kind: Neo4j
spec:
  members: 1          # dev / single-node — no separate Standalone mode
  edition: enterprise
  persistence: { ... }
  connectivity: { ... }
---
kind: Neo4j
spec:
  members: 3          # production HA — same kind, same code path
  edition: enterprise
  persistence: { ... }
```

No `spec.topology.mode` field — only `members` (and related cluster settings).

| Advantages | Disadvantages |
|------------|---------------|
| **Single reconcile path** — always cluster logic; no `topology.mode` branch in `domain/workload` | **Neo4j product constraints** — real clusters typically require ≥3 members; `members: 1` may need non-HA / dev-only cluster config |
| Simpler API surface — one dimension (`members`) instead of `mode` + `members` | **Helm mismatch** — chart has explicit `neo4j.standalone: true`; mapping is indirect |
| Scale from 1 → 3 by patching `members` on the **same** CR (no mode flip) | Cluster **formation, discovery, and internals** run even for a single member — heavier than true standalone |
| Aligns with CNPG (`instances: 1` is valid on the same `Cluster` kind) | **UX confusion** — users apply a "cluster" CR for a dev single-node database |
| Fewer conditional validations than Option A (no `mode` / `members` cross-rules) | Risk of **misconfiguration** — `members: 1` deployed expecting production HA |
| One `kind`, one mental model for the operator codebase | FR **NEO-001** (standalone) and **NEO-002** (cluster) collapse in the API — documentation must explain the mapping |
| Test catalog can keep separate scenarios; implementation unifies | Longer **startup / Ready** time vs standalone (cluster bootstrap on one node) |
| | May expose cluster-only ports, services, and probes unnecessarily on single-node |
| | `03-variant_matrix.csv` distinguishes Standalone vs Cluster variants — still need behaviour gaps somewhere |

---

### Synthesis

Option A scores best on **user model**, **Helm parity**, **market alignment**, and **long-term maintenance**.

Option B only wins on per-topology schema simplicity — better addressed by splitting `neo4j/spec.md` than by splitting the public API.

Option C trades API granularity for **operational and GitOps complexity** without a strong market precedent. Infra separation belongs in **Go packages** (`internal/domain/*`) and **embedded `spec` sections**, not in separate Kubernetes kinds — see [`09-crd-spec/readme.md`](../../09-crd-spec/readme.md).

Option D offers genuine **per-server observability** and maps well to Neo4j's mental model of servers in a cluster, but introduces **hierarchy complexity**, **GitOps ownership tension** on operator-created children, and **two top-level kinds** — without the simplicity win of Option A. Per-member concerns are better handled via **pod labels**, **StatefulSet ordinals**, and **aggregated status on a single `Neo4j` CR** than by proliferating Instance CRs.

Option E maximises **implementation uniformity** (one code path) at the cost of **product and UX fidelity**. Neo4j standalone and cluster are distinct operational profiles in the Helm chart and in customer language (`NEO-001` vs `NEO-002`). Forcing single-node through cluster topology adds formation overhead and blurs the production HA contract (`members: 1` ≠ HA). Option A keeps an explicit `mode` so the operator can take the **lightweight standalone path** when appropriate while sharing most of the `spec`.

**However**, Option E deserves a **formal Product / Aura review** — if Aura already unifies on cluster-only topology, aligning the self-managed operator may reduce long-term divergence and strengthen the case for Product Engineering ownership (see `00-vision.md`). Until that review completes, Option A remains the default recommendation for Helm parity and explicit `NEO-001` / `NEO-002` semantics.

---

## Decision

We will expose **one CRD**:

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
spec:
  topology:
    mode: Standalone   # Standalone | Cluster
    members: 1         # 1 when Standalone; ≥3 when Cluster
  persistence: { ... }
  connectivity: { ... }
  trust: { ... }
  config: { ... }
```

We will **not** ship `Neo4jStandalone` or `Neo4jCluster` as separate API kinds.

Infra concerns (persistence, connectivity, trust, server config) remain **`spec` sections** on `Neo4j`, not separate CRDs.

> **Topology detail** (primaries, secondaries, analytics, user guidance) → [BDR-002](002-neo4j-crd-topology.md).

---

## Consequences

### Positive

- **One mental model** for users — align with Helm chart and industry operators (single workload CRD + mode field).
- **Shared `spec`** — no duplication of persistence/TLS/networking fields across two OpenAPI schemas.
- **Single reconciler entry point** — `Neo4jReconciler`; only `internal/domain/workload` branches on `topology.mode`.
- **Simpler RBAC and discovery** — one `kind` to grant, document, and monitor.
- **GitOps-friendly** — topology change is a spec patch on the same resource (with immutability rules TBD in `neo4j/validation.md`).

### Negative

- **OpenAPI complexity** — CEL / webhook rules must express mode-dependent constraints.
- **Larger single CRD schema** — `neo4j/spec.md` is the biggest design artifact.
- **Status semantics** differ by mode (cluster formation vs single-pod Ready) — documented in `10-status-model.md`.

### Neutral

- Day-2 operations (`Neo4jBackup`, `Neo4jRestore`, `Neo4jDatabase`) reference `Neo4j` via `neo4jRef` regardless of topology.
- Test catalog keeps separate scenario variants for standalone vs cluster (`03-variant_matrix.csv`) — validation surface unchanged, only the API shape is unified.

---

## Open actions

| Action | Owner | Feeds |
|--------|-------|-------|
| Review Aura / Product topology model vs Option E (cluster-only, unified deployment) | PS + Product Engineering | BDR-002, `09-crd-spec/neo4j/`, Product sponsorship in `00-vision.md` |