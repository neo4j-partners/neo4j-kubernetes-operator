# BDR-007 — Service exposure & connectivity model

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-26 |
| **Reviewers** | Charles Boudry |
| **Depends on** | [BDR-001](001-single-neo4j-crd.md) — single `Neo4j` CRD (accepted) · [BDR-002](002-neo4j-crd-topology.md) — topology pools (proposed) |
| **Helm scope** | `services.default`, `services.neo4j` (+`.enabled`, `.spec.type`, `.ports`, `.multiCluster`, `.cleanup`), `services.admin`, `services.internals`, `podSpec.loadbalancer`, `clusterDomain` — group **AGG-EXPOSURE** (BC-005) |
| **Constraints** | `NEO-2-007` (networking), `NEO-3-007-SVC-01/02/03`, `NEO-3-007-PRT-01..04`, `NEO-3-007-MULTI-02`, `NEO-2-013` (backup access), `NEO-2-018` (uninstall); Neo4j Operations Manual — Networking / Connectors / Clustering |

---

## Context

The operator must let clients reach Neo4j over Bolt/HTTP(S) from inside and outside the cluster, let the cluster members discover and talk to each other, and give backup/ops tooling a stable endpoint even when a pod is not `Ready`. This BDR resolves **how that exposure surface appears on `Neo4j.spec.connectivity`** ([`spec.md`](../../09-crd-spec/neo4j/spec.md) §`spec.connectivity`).

### What Helm does today (forces)

The chart emits **four distinct Services per release** plus a per-pod label, scattered across templates:

| Helm path | K8s resource | Purpose | Template |
|-----------|--------------|---------|----------|
| `services.default` | `{release}-neo4j` ClusterIP | In-cluster driver access (bolt + enabled http/s) | `neo4j-svc.yaml` |
| `services.neo4j` (LB) | `{neo4j.name}-lb-neo4j` LoadBalancer/NodePort/ClusterIP | **External** access, **shared across all releases of one `neo4j.name`** (`helm.sh/resource-policy: keep`) | `neo4j-loadbalancer.yaml`, `_loadbalancer.tpl` |
| `services.admin` | `{release}-admin` ClusterIP, `publishNotReadyAddresses: true` | Backup / ops, reachable when not Ready | `neo4j-svc.yaml` |
| `services.internals` | `{release}-internals` ClusterIP | Discovery, raft, routing, inter-member traffic | `neo4j-svc.yaml`, drives `dbms.kubernetes.label_selector` |
| `podSpec.loadbalancer` | Pod label `helm.neo4j.com/neo4j.loadbalancer: include\|exclude` | Selects which pods the shared LB targets | `neo4j-statefulset.yaml` + LB selector |
| `clusterDomain` | Pod env `SERVICE_*` FQDNs | DNS suffix for service FQDNs / routing | `neo4j-statefulset.yaml`, `neo4j-config.yaml` |

Sub-knobs on the external service: `services.neo4j.enabled` (toggle), `services.neo4j.spec.type` (LoadBalancer/NodePort/ClusterIP), `services.neo4j.ports.{http,https,bolt,backup}` (which connectors are published — note: publishing a port does **not** enable the Neo4j connector, `config.*` still governs the process), `services.neo4j.multiCluster` (opens 7688/5000/7000/6000 + `publishNotReadyAddresses` for cross-K8s clusters), and `services.neo4j.cleanup` (pre-delete hook deleting the kept LB on uninstall).

### Helm vs operator gap (key to this decision)

The **shared LoadBalancer** and the **`podSpec.loadbalancer` include/exclude label** exist only because Helm models each cluster member as a **separate release** ([BDR-002](002-neo4j-crd-topology.md) — one release = one pod, `replicas: 1`). One LB with `resource-policy: keep` is the chart's way of giving N independent releases a single front door, and the per-pod label is how a release opts its pod in or out of that shared LB. The `cleanup` hook then exists to garbage-collect the kept LB.

The operator uses **one `Neo4j` CR → one StatefulSet → N pods** ([BDR-002](002-neo4j-crd-topology.md)). There is exactly one owner for the exposure resources, so:

- A "shared LB across releases" is **not needed** — the operator owns one external Service for the workload and can target pods by ordinal/pool via its own selectors.
- `podSpec.loadbalancer` per-pod include/exclude becomes a **pool-level** concern (which pools receive external traffic), not a per-pod label.
- The `cleanup` hook is replaced by an **owner-reference / finalizer** (`NEO-2-018`).
- `services.admin` (`publishNotReadyAddresses`) and `services.internals` (discovery) are **operator-derived**, not user-authored config — they follow from `topology.mode` and backup needs.

This BDR decides how much of that four-service surface to expose to the user vs. let the operator synthesize.

---

## Cross-cutting rules (apply to all options)

| Rule | Rationale |
|------|-----------|
| Internal/headless + discovery Services are **operator-owned**, derived from `topology.mode` | `services.internals` drives `dbms.kubernetes.label_selector`; users should not hand-shape cluster formation endpoints (BDR-002) |
| Admin Service (`publishNotReadyAddresses`) is **operator-owned**, created when backup/ops needs it | Reachability when not Ready is an operator invariant, not a user knob (`NEO-2-013`) |
| Exposing a port on the Service does **not** enable the Neo4j connector | `config.*` governs the process — operator must validate port↔connector coherence (`services.neo4j.ports` note) |
| Lifecycle of external resources via **owner refs / finalizer**, not Helm hooks | Replaces `services.neo4j.cleanup` (`NEO-2-018`) |
| `clusterDomain` is an **optional override**, default `cluster.local` | Rarely changed; cluster-scoped (`clusterDomain` analysis) |
| `multiCluster` (cross-K8s exposure of 7688/5000/7000/6000) is **V1=No / deferred** | Security + topology implications; `NEO-3-007-MULTI-02` |

---

## Options under review

### Option A — Helm-mirrored four-service block

Expose all four Services as user-authored fields, mirroring Helm names/shape.

```yaml
spec:
  connectivity:
    clusterDomain: cluster.local
    services:
      default:   { enabled: true }                      # in-cluster ClusterIP
      admin:     { enabled: true }                       # publishNotReadyAddresses
      internals: { enabled: true }                       # discovery
      neo4j:                                             # external (shared-LB analogue)
        enabled: true
        type: LoadBalancer
        ports: { bolt: true, http: true, https: false, backup: false }
        multiCluster: false
        loadBalancerMembership: include                  # ← podSpec.loadbalancer
```

| Advantages | Disadvantages |
|------------|---------------|
| **Lowest migration friction** — Helm users recognise every field | Leaks Helm **multi-release artifacts** (shared LB, per-pod include/exclude) into a single-CR API |
| 1:1 mapping in `11-helm-mapping.md` | Exposes `admin`/`internals` that should be operator invariants — users can break cluster formation |
| Full control for power users | Four service blocks = large, error-prone surface; many invalid combinations |
| | `loadBalancerMembership` per-pod is meaningless under one-STS ownership |

**Helm parity**: highest. **API clarity**: lowest.

---

### Option B — Intent-based `internal` / `external` split (operator synthesizes Services) — **proposed**

Two user-facing intents; the operator derives the four Kubernetes Services. This is the shape already sketched in [`spec.md`](../../09-crd-spec/neo4j/spec.md) §`spec.connectivity`.

```yaml
spec:
  connectivity:
    clusterDomain: cluster.local          # optional override
    internal:
      enabled: true                        # headless + ClusterIP (+ admin, + discovery if Cluster)
    external:
      enabled: false                       # default closed
      type: LoadBalancer                   # LoadBalancer (V1 P0) | NodePort | ClusterIP | None
      annotations: {}                      # cloud LB annotations
      loadBalancerSourceRanges: []
      ports:
        bolt: true
        http: true
        https: false
        backup: false                      # V1=No
    multiCluster:
      enabled: false                       # V1=No (deferred)
```

| Advantages | Disadvantages |
|------------|---------------|
| **Intent over mechanism** — "who can reach me" (in-cluster vs outside), not 4 raw Services | Helm-name parity only via `11-helm-mapping.md` (not field-for-field) |
| `admin`/`internals` become **operator-owned invariants** — users cannot break discovery | Power users lose direct per-service shaping (covered by `spec.podTemplate`/annotations escape hatch) |
| Already the spec.md direction — no churn to CRD doc | Operator must encode the Helm four-service derivation logic |
| `external.enabled: false` default = **secure by default** | Pool-scoped exposure not modelled (see Option D / deferred) |
| Maps cleanly to BDR-002 single-STS model; no shared-LB / per-pod label leakage | |

**Helm parity**: medium (translation table). **API clarity**: high.

---

### Option C — Minimal external Service + delegate L7 to Ingress/Gateway

Expose only an opt-in external Service toggle; everything richer (host routing, TLS termination) is left to standard Ingress / Gateway API authored by the user outside the CRD. Internal/admin/discovery fully operator-owned.

```yaml
spec:
  connectivity:
    external:
      enabled: true
      type: LoadBalancer        # or ClusterIP when fronted by Ingress
      ports: { bolt: true, http: false, https: false }
    # no internal/admin/internals fields — all operator-derived
    # HTTP(S) routing handled by user-managed Ingress/Gateway
```

| Advantages | Disadvantages |
|------------|---------------|
| **Smallest CRD surface** | Bolt is **TCP, not HTTP** — Ingress controllers handle Bolt poorly; pushes complexity to user |
| Leverages cluster-standard networking primitives | No single place to see Neo4j exposure (`kubectl get neo4j` hides it) |
| No bespoke annotation passthrough to maintain | Weaker Helm parity than B; migration guide heavier |
| | Backup/admin still needs operator-owned Service anyway → C is really B-minus |

**Helm parity**: low. **API clarity**: medium (but leaks to external objects).

---

### Option D — Pool-scoped exposure (replaces per-pod `loadbalancer` label)

Build on B, but allow `external` to target **specific topology pools** ([BDR-002](002-neo4j-crd-topology.md): `primaries`, `secondaries.analytics`, `secondaries.read`) instead of a per-pod include/exclude label. Generalises Helm's `podSpec.loadbalancer`.

```yaml
spec:
  connectivity:
    external:
      enabled: true
      type: LoadBalancer
      pools: [primaries, read]      # which pools receive external traffic; default: all serving pools
      ports: { bolt: true, https: true }
```

| Advantages | Disadvantages |
|------------|---------------|
| Direct, **topology-aware** successor to `podSpec.loadbalancer` | Couples connectivity to topology pool names — must co-evolve with BDR-002/009 |
| Expresses "expose readers externally, keep primaries internal" cleanly | More surface + validation (pool must exist, mode-dependent) |
| Removes per-pod label artifact entirely | Likely premature for V1 — Neo4j routing already steers reads/writes |

**Helm parity**: reinterprets `podSpec.loadbalancer` semantics. **API clarity**: high but advanced.

---

## Comparison

| Criterion | A — Helm mirror | B — internal/external | C — minimal + Ingress | D — pool-scoped |
|-----------|-----------------|-----------------------|------------------------|-----------------|
| Helm parity | ✅ highest | ⚠️ via mapping | ❌ low | ⚠️ reinterprets |
| API minimalism | ❌ (4 blocks) | ✅ | ✅ smallest | ⚠️ (pools added) |
| Secure by default | ❌ (admin/internals exposed) | ✅ external off | ✅ | ✅ |
| Operator owns formation/admin | ❌ user can break | ✅ | ✅ | ✅ |
| Removes multi-release artifacts (shared LB, per-pod label) | ❌ leaks them | ✅ | ✅ | ✅ (label → pools) |
| Bolt (TCP) handled well | ✅ | ✅ | ❌ (Ingress weak on TCP) | ✅ |
| Matches current `spec.md` | ⚠️ no | ✅ yes | ⚠️ partial | ⚠️ extends |
| Breaking risk vs Helm | medium | medium | high | medium |
| V1 fit | over-broad | **best** | under-powered | advanced/defer |

---

## Decision

**Not decided — proposed.** Pending reviewer (Charles Boudry) sign-off.

**Proposer direction:** Adopt **Option B** — `spec.connectivity` exposes user intent as **`internal`** and **`external`** (plus an optional `clusterDomain` override and a deferred `multiCluster`), and the operator **synthesizes** the underlying Kubernetes Services (the Helm `default` / `admin` / `internals` quartet). This is already the shape in [`spec.md`](../../09-crd-spec/neo4j/spec.md); this BDR ratifies it and records why the Helm four-service / shared-LB / per-pod-label surface is **intentionally not** reproduced.

**Recommendation:**

1. **V1 = Option B.** `external.enabled` defaults `false` (secure by default); `type` defaults `LoadBalancer` (V1 P0), with `NodePort` / `ClusterIP` / `None` allowed. Ports `bolt`/`http` default on, `https`/`backup` off.
2. **Admin & internals are operator-owned** — derived from `topology.mode` and backup needs, not user fields (cross-cutting rules above).
3. **Replace the `cleanup` hook** with owner references + a finalizer for external resources (`NEO-2-018`).
4. **`multiCluster` = V1=No / deferred** (`NEO-3-007-MULTI-02`), consistent with [BDR-002](002-neo4j-crd-topology.md) V1 scope.
5. **Defer Option D (pool-scoped exposure)** to V1.1+ as the principled successor to `podSpec.loadbalancer`; revisit alongside BDR-009 (pool / scale semantics). For V1, external exposure targets all serving pods.
6. Validate **port ↔ connector coherence** (publishing `https` requires the HTTPS connector enabled in `config` / `trust`) — see [`validation.md`](../../09-crd-spec/neo4j/validation.md).

---

## Consequences

### Positive

- One coherent, intent-based exposure surface aligned with the single-CR / single-STS model (BDR-002).
- Secure by default (`external.enabled: false`); cluster formation endpoints cannot be misconfigured by users.
- Eliminates Helm multi-release artifacts (shared LB `resource-policy: keep`, per-pod `loadbalancer` label, `cleanup` hook) from the public API.
- `11-helm-mapping.md` gets a clear translation table instead of four leaky pass-through blocks.

### Negative

- Helm users do not get field-for-field parity; migration guide must map `services.*` → `connectivity.internal/external`.
- Operator must own the derivation logic for admin/internals/headless Services and keep it correct across `topology.mode`.
- Advanced per-pool exposure (`podSpec.loadbalancer` use cases) is unavailable until Option D ships.

### Neutral

- `clusterDomain` becomes an optional override (`cluster.local` default) rather than an always-present value.
- Cloud-LB customization handled via `external.annotations` + `loadBalancerSourceRanges`; deeper Service shaping via `spec.podTemplate` escape hatch.
- `breaking-change-register.md` BC-005 resolved by this BDR; `_index.csv` AGG-EXPOSURE rows point here.

---

## References

- `design/analysis/helm-fields/fields/services.default.md`, `services.neo4j.md`, `services.neo4j.enabled.md`, `services.neo4j.spec.type.md`, `services.neo4j.ports.md`, `services.neo4j.multiCluster.md`, `services.neo4j.cleanup.md`, `services.admin.md`, `services.internals.md`, `podSpec.loadbalancer.md`, `clusterDomain.md`
- `design/analysis/helm-fields/aggregation-matrix.md` — group **AGG-EXPOSURE**
- `design/analysis/helm-fields/semantic-concerns.yaml` — `CONCERN-EXPOSURE`
- `design/analysis/helm-fields/breaking-change-register.md` — **BC-005**
- [`09-crd-spec/neo4j/spec.md`](../../09-crd-spec/neo4j/spec.md) — `spec.connectivity`
- FRs: `NEO-2-007`, `NEO-3-007-SVC-01/02/03`, `NEO-3-007-PRT-01..04`, `NEO-3-007-MULTI-02`, `NEO-2-013`, `NEO-2-018`
- [Neo4j — Networking (Kubernetes)](https://neo4j.com/docs/operations-manual/current/kubernetes/) · [Connectors](https://neo4j.com/docs/operations-manual/current/configuration/connectors/) · [Clustering](https://neo4j.com/docs/operations-manual/current/clustering/)
- [BDR-001](001-single-neo4j-crd.md) · [BDR-002](002-neo4j-crd-topology.md) · [BDR-004](004-neo4j-plugin-topology.md)
