# BDR-007 — Service exposure & connectivity model

| | |
|---|---|
| **Status** | accepted |
| **Date** | 2026-06-22 (accepted 2026-06-22) |
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
- `services.admin` (`publishNotReadyAddresses`) and `services.internals` (discovery): **internals** operator-synthesized; **admin** in `connectivity.service.admin`

This BDR decides how much of that four-service surface to expose to the user vs. let the operator synthesize.

---

## Cross-cutting rules (apply to all options)

| Rule | Rationale |
|------|-----------|
| **Internals Service is operator-synthesized** — not in `connectivity.service` | `mode: Cluster` ⇒ always on (Helm `$clusterEnabled \| or .enabled`); drives `dbms.kubernetes.label_selector` |
| `connectivity.service` lists **neo4j** (client) and **admin** (ops/backup) only | Mirrors Helm `services.default` + `services.neo4j` merged into one `neo4j` block with `type` |
| Admin Service uses `publishNotReadyAddresses: true` when enabled | Helm invariant (`NEO-2-013`) |
| Exposing a port on the Service does **not** enable the Neo4j connector | `config.*` governs the process — operator must validate port↔connector coherence (`services.neo4j.ports` note) |
| Lifecycle of external resources via **owner refs / finalizer**, not Helm hooks | Replaces `services.neo4j.cleanup` (`NEO-2-018`) |
| `clusterDomain` is an **optional override**, default `cluster.local` | Rarely changed; cluster-scoped (`clusterDomain` analysis) |
| `multiCluster` (cross-K8s exposure of 7688/5000/7000/6000) is **V1=No / deferred** | Security + topology implications; `NEO-3-007-MULTI-02` |
| `connectivity.ingress` rules may target **`service`** (direct) or **`reverseProxy`** (HTTP/Bolt-ws front door) | Split north-south paths per connector |
| **`reverseProxy` upstream** is always the operator **client Service** (same namespace) — no Helm `serviceName` override in V1.1 | Single-CR ownership; proxy chart `reverseProxy.serviceName` analogue |
| A connector MUST NOT appear in both **`service.expose`** and **`reverseProxy.expose`** for external publication | Avoid duplicate/conflicting north-south paths (NET-005) |
| `connectivity.ingress.rules` hosts may feed cert-manager SANs when `trust.certManager.includeIngressHosts: true` | [BDR-006](007-tls-trust-model.md) — TLS termination at Ingress or Neo4j |

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

### Option B — Service-centric `connectivity.service` (neo4j + admin) — **superseded by Option E**

Structure: **which Services** → **exposure type** → **ports per Service**. Matches how operators reason about Helm `services.*` without leaking `internals` or multi-release LB artifacts.

```yaml
spec:
  connectivity:
    clusterDomain: cluster.local
    services:
      neo4j:                              # client access — Helm services.default + services.neo4j
        type: ClusterIP                     # ClusterIP (default) | LoadBalancer | NodePort
        annotations: {}
        loadBalancerSourceRanges: []        # when type: LoadBalancer
        ports:
          bolt: true
          http: true
          https: false
      admin:                              # backup / ops — Helm services.admin
        enabled: true
        type: ClusterIP                   # V1: ClusterIP only
        annotations: {}
        ports:
          bolt: true
          http: true
          https: false
          metrics: false
          backup: true                    # typical on admin
    multiCluster:
      enabled: false                      # V1=No (deferred) — Helm services.neo4j.multiCluster
    ingress:
      enabled: false
      className: nginx
      hosts:
        - neo4j.example.com               # → cert-manager SANs when trust.certManager.includeIngressHosts
```

| Field | Role |
|-------|------|
| `services.neo4j.type` | **Exposure method** — `ClusterIP` = in-cluster only (Helm `default`); `LoadBalancer` / `NodePort` = external (Helm `services.neo4j`) |
| `services.neo4j.ports` | Which connectors are published on the **neo4j** Service (`bolt`, `http`, `https`, `backup`, `metrics`) |
| `services.admin.enabled` | Create admin Service (`publishNotReadyAddresses`) |
| `services.admin.ports` | Same port keys — admin often exposes `backup` + `metrics` when ops/monitoring need them |

Publishing a port on a Service does **not** enable the Neo4j connector — `config` / `trust` still govern the process.

| Advantages | Disadvantages |
|------------|---------------|
| **Helm-aligned mental model** — named services, then type, then ports | Two services to read (but only two — not four) |
| `neo4j.type: ClusterIP` default = **secure by default** (no external LB unless chosen) | Helm `default` + `neo4j` LB collapse into one block — mapping table required |
| Per-service port matrix (admin ≠ neo4j) | Operator must merge single-STS selectors (no `podSpec.loadbalancer` label) |
| `internals` stays operator invariant — users cannot break cluster discovery | |

**Helm → operator mapping:**

| Helm | Operator |
|------|----------|
| `services.default` (ClusterIP, always) | `services.neo4j.type: ClusterIP` |
| `services.neo4j` + `enabled: true` + `spec.type: LoadBalancer` | `services.neo4j.type: LoadBalancer` |
| `services.neo4j.enabled: false` | `services.neo4j.type: ClusterIP` (external LB off) |
| `services.neo4j.ports.*` | `services.neo4j.ports.*` (+ `metrics`) |
| `services.admin` | `services.admin` |
| `services.internals` | operator-synthesized (`mode: Cluster` ⇒ always) |

**Operator-synthesized (never in spec):** `services.internals` — discovery, raft, routing, cluster ports (7688/5000/7000/6000).

---

### Option C — Minimal external Service + delegate L7 to Ingress/Gateway

Expose only an opt-in external Service toggle; everything richer (host routing, TLS termination) is left to standard Ingress / Gateway API authored by the user outside the CRD. Internal/admin/discovery fully operator-owned.

```yaml
spec:
  connectivity:
    services:
      neo4j:
        type: LoadBalancer
        ports: { bolt: true, http: false, https: false, backup: false, metrics: false }
      admin:
        enabled: true
        ports: { backup: true, bolt: true }
    # ingress — user-managed or connectivity.ingress
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

Build on B, but allow exposure to target **specific topology pools** ([BDR-002](002-neo4j-crd-topology.md): `primaries`, `secondaries.analytics`, `secondaries.read`) instead of a per-pod include/exclude label. Generalises Helm's `podSpec.loadbalancer`.

```yaml
spec:
  connectivity:
    services:
      neo4j:
        type: LoadBalancer
        pools: [primaries, read]    # V1.1 — which pools receive client/LB traffic
        ports: { bolt: true, https: true }
```

| Advantages | Disadvantages |
|------------|---------------|
| Direct, **topology-aware** successor to `podSpec.loadbalancer` | Couples connectivity to topology pool names — must co-evolve with BDR-002/009 |
| Expresses "expose readers externally, keep primaries internal" cleanly | More surface + validation (pool must exist, mode-dependent) |
| Removes per-pod label artifact entirely | Likely premature for V1 — Neo4j routing already steers reads/writes |

**Helm parity**: reinterprets `podSpec.loadbalancer` semantics. **API clarity**: high but advanced.

---

## Helm port problem (why Option B is insufficient)

Helm splits **process** ports and **Service** ports across unrelated fields. Changing a connector requires manual sync in two places.

| Layer | Helm location | Example |
|-------|---------------|---------|
| Neo4j process | `config.server.http.listen_address` / `server.http.enabled` | `:7474` |
| K8s Service (neo4j LB) | `services.neo4j.ports.http.port` + `.targetPort` | `80` → `7474` |
| K8s Service (default ClusterIP) | hard-coded in `neo4j-svc.yaml` | `7474` → `7474` |
| K8s Service (admin) | auto from `config` flags — **no per-port user knobs** | backup 6362 if `server.backup.enabled` |
| K8s Service (internals) | auto + cluster ports 7688/5000/7000/6000 | forced when `$clusterEnabled` |

`services.neo4j.ports` documents `port` / `targetPort` / `nodePort` remapping (values L271–299), but **`default` and `admin` ignore those knobs** — only the LB template reads them. Admin/internals port lists are **derived** from `config` (http/https/backup/prometheus enabled), not from `services.admin.ports`.

**Operator opportunity:** one CRD field per connector port → operator sets `neo4j.conf`, `containerPort`, and all Service `targetPort`s. Optional **service port remap** only on the client-facing Service (Helm LB `port` ≠ `targetPort` use case).

---

### Option E — `features` + `connectivity` (listeners + service + reverseProxy + ingress) — **accepted**

**One connectivity block, layered routing inside it.**

| Section | Question it answers | Helm analogue |
|---------|---------------------|---------------|
| **`spec.connectivity.listeners`** | On which ports does **Neo4j listen** inside the pod? | `config.server.{connector}.*` |
| **`spec.connectivity.service`** | Which connectors are published **directly** on the client Service (LB / NodePort / ClusterIP)? | `services.default` + `services.neo4j` |
| **`spec.connectivity.reverseProxy`** | Optional **reverse-proxy Deployment** — HTTP + Bolt-over-WebSocket to upstream Neo4j Service | `neo4j-reverse-proxy` chart |
| **`spec.connectivity.ingress`** | Optional **Ingress** with per-path **backend** (`service` or `reverseProxy`) | `reverseProxy.ingress` in proxy chart |
| **`spec.connectivity.clusterDomain`** | Kubernetes DNS suffix for operator-built FQDNs | `clusterDomain` |
| **`spec.connectivity.multiCluster`** | Cross-K8s cluster port exposure on client Service | `services.neo4j.multiCluster` |

`connectivity.listeners.{name}` (port integer) is the **single listen port** — key present ⇒ connector **enabled** at that port; operator sets `neo4j.conf`, `containerPort`, and every Service `targetPort`.  
`connectivity.service.expose` lists which connectors are published on the client Service.  
`connectivity.service.ports.{name}` (optional integer) is only the **Service façade** port (Helm LB `port` ≠ `targetPort`). Omit → Service port = `connectivity.listeners.{name}`.

**admin** and **internals** Services are **not** in spec — derived from `features` + `topology` (see below).

#### Layer 1 — `spec.features`

All optional workload capabilities live here (including monitoring).

```yaml
spec:
  features:
    backup:
      enabled: true
    monitoring:
      prometheus:
        enabled: true
      serviceMonitor:
        enabled: true
        interval: 30s
        labels: {}
```

| Feature | Gates | Derived Service |
|---------|-------|-----------------|
| `features.backup.enabled` | `connectivity.listeners.backup` set (non-null port) | **admin** |
| `features.monitoring.prometheus.enabled` | `connectivity.listeners.metrics` set (non-null port) | **admin** (+ metrics scrape) |
| `topology.mode: Cluster` | cluster ports on **internals** | **internals** (always) |

Replaces top-level `spec.monitoring` — same fields under `features.monitoring`.

#### Layer 2 — `spec.connectivity` (process + Kubernetes)

All network-facing configuration in one block. **No `services.neo4j` nesting** — one client Service per CR.

##### `connectivity.listeners` (Neo4j process)

Each connector is a **port integer** (1–65535). **Key present** ⇒ Neo4j listens on that port; **key absent** ⇒ operator default; **`null`** ⇒ explicitly disabled.

```yaml
  connectivity:
    listeners:
      bolt: 7687
      http: 7474
      https: 7473
      backup: 6362              # CEL: requires features.backup.enabled
      metrics: 2004             # CEL: requires features.monitoring.prometheus.enabled
```

| Connector | Default when key absent | Default port |
|-----------|-------------------------|--------------|
| `bolt` | **enabled** | 7687 |
| `http` | **enabled** | 7474 |
| `https` | disabled | 7473 |
| `backup` | disabled | 6362 |
| `metrics` | disabled | 2004 |

To disable a default-on connector: `http: null`.

Cluster-internal ports (7688, 5000, 7000, 6000) — operator-injected on **internals** when `mode: Cluster`; not user fields in V1.

Reserved in [BDR-008](008-neo4j-config-surface.md): port-owned `server.*` keys — **denylisted** in `spec.config`; contradictions fail admission ([CFG-LISTENER-*](../../09-crd-spec/neo4j/validation.md)).

##### `connectivity.service` (client Service)

**`expose`** lists connectors published on the client Service. **`ports`** remaps Service port numbers only (façade). **admin** and **internals** are derived — never in spec.

```yaml
    service:
      type: LoadBalancer              # ClusterIP (default) | LoadBalancer | NodePort
      annotations: {}
      loadBalancerSourceRanges: []
      expose:
        - bolt
        - http
        - https
      ports:
        http: 80                      # optional — Service port; targetPort = listeners.http
        https: 443
```

| Field | Role |
|-------|------|
| `service.type` | `ClusterIP` = in-cluster (Helm `services.default`); `LoadBalancer` = external (Helm `services.neo4j`) |
| `service.expose` | Connector names published on this Service (Helm `ports.{name}.enabled`) |
| `service.ports.{name}` | Optional Service port integer; **targetPort always** `connectivity.listeners.{name}` |

Default when `expose` omitted: `[bolt, http]` for enabled listeners.

CEL: each name in `service.expose` ⇒ `connectivity.listeners.{name}` is set (non-null).

##### `connectivity.reverseProxy` (optional HTTP front door)

Replaces the standalone **`neo4j-reverse-proxy`** Helm release as an operator-managed Deployment + Service in the same namespace. The proxy forwards to the **client Service** (operator default upstream — not a user-supplied `serviceName`).

Typical pattern: **Bolt direct on LoadBalancer**, **HTTP(S) via Ingress → reverseProxy → Neo4j** (Ingress terminates TLS; Neo4j may stay HTTP on `listeners.http`).

```yaml
    reverseProxy:
      enabled: true
      image: neo4j/helm-charts-reverse-proxy:2026.04.0   # optional override
      expose:
        - http
        # Bolt drivers over WebSocket Upgrade share the same proxy port (Helm behaviour)
      resources:
        requests: { cpu: 100m, memory: 128Mi }
        limits: { cpu: 500m, memory: 256Mi }
      service:
        type: ClusterIP
        ports:
          http: 80          # proxy Service port (Ingress backend); targets proxy container
```

| Field | Role |
|-------|------|
| `enabled` | Create reverse-proxy Deployment + Service |
| `expose` | Connectors published **via proxy** (not on client Service LB) — usually `http`; subset of enabled listeners |
| `service.type` | Proxy front Service (`ClusterIP` default) — Ingress backend |
| `service.ports.{name}` | Service port the Ingress / in-cluster clients use to reach the proxy |

CEL: `reverseProxy.enabled` ⇒ `expose` non-empty. Each `reverseProxy.expose` name ⇒ listener set. **`reverseProxy.expose` ∩ `service.expose` = ∅`** (NET-005).

##### `connectivity.ingress`, `clusterDomain`, `multiCluster`

Ingress rules declare **which backend** receives traffic: the **client Service** (`backend: service`) or the **reverse-proxy Service** (`backend: reverseProxy`). Supports split routing in one host.

```yaml
    ingress:
      enabled: true
      className: nginx
      annotations:
        cert-manager.io/cluster-issuer: letsencrypt-prod
      tls:
        - hosts: [neo4j.example.com]
          secretName: neo4j-ingress-tls
      rules:
        - host: neo4j.example.com
          paths:
            - path: /
              pathType: Prefix
              backend: reverseProxy    # service | reverseProxy
              port: http               # connector name → backend Service port
        - host: bolt.neo4j.example.com
          paths:
            - path: /
              pathType: Prefix
              backend: service
              port: bolt
    clusterDomain: cluster.local
    multiCluster:
      enabled: false
```

| Field | Role |
|-------|------|
| `ingress.enabled` | Create Ingress resource(s) |
| `ingress.className` | `ingressClassName` |
| `ingress.annotations` | Controller-specific (cert-manager, ALB, …) |
| `ingress.tls[]` | `hosts` + `secretName` per TLS block (pattern B — Ingress-terminated TLS) |
| `ingress.rules[]` | Per-host routing |
| `ingress.rules[].host` | HTTP host header |
| `ingress.rules[].paths[]` | `path`, `pathType` (`Prefix`, `Exact`, `ImplementationSpecific`) |
| `ingress.rules[].paths[].backend` | `service` — client Service; `reverseProxy` — proxy Service |
| `ingress.rules[].paths[].port` | Connector name (`bolt`, `http`, `https`) — resolves to port on chosen backend |
| `clusterDomain` | DNS suffix for operator-built FQDNs (`SERVICE_*` env, routing) |
| `multiCluster.enabled` | Helm `services.neo4j.multiCluster` — expose cluster discovery ports on client Service |

CEL: `paths[].backend: reverseProxy` ⇒ `reverseProxy.enabled`. `paths[].port` must be in `expose` of the chosen backend (NET-006). `ingress.tls.hosts` may merge into cert-manager SANs via `trust.certManager.includeIngressHosts`.

**Deprecated:** flat `ingress.hosts: []` only — replaced by `ingress.rules[].host` (hosts derivable as union of rule hosts for SAN helpers).

#### Derived Services (never in spec)

| Service | Created when | Ports |
|---------|--------------|-------|
| **neo4j** (client) | always | per `connectivity.service.expose` |
| **reverse-proxy** | `reverseProxy.enabled` | per `reverseProxy.expose` on proxy Service |
| **admin** | `features.backup` OR `features.monitoring.prometheus` OR `mode: Cluster` | enabled connectors relevant to ops (`backup`, `metrics`, `bolt`, `http`…); `publishNotReadyAddresses: true` |
| **internals** | `mode: Cluster` OR analytics secondary | enabled connectors + cluster ports |

#### North-south routing (split example)

Bolt **direct** on LoadBalancer; Browser **via Ingress → reverseProxy**:

```
                    ┌─────────────────────────────────────────┐
  Driver (Bolt)     │  LB :7687  ──► service.expose [bolt]     │
  ───────────────►  │              ──► Neo4j pod :7687        │
                    └─────────────────────────────────────────┘

                    ┌─────────────────────────────────────────┐
  Browser (HTTPS)   │  Ingress :443 (TLS terminate)           │
  ───────────────►  │    ──► reverseProxy Service :80         │
                    │          ──► Neo4j client Service       │
                    │                ──► Neo4j pod :7474 http │
                    └─────────────────────────────────────────┘
```

```
connectivity.listeners.http: 7474
    ├──► server.http.listen_address=:7474
    ├──► containerPort 7474
    └──► Service targetPort 7474  (client, admin, internals)

connectivity.service.ports.http: 80  (optional, when http ∈ service.expose)
    └──► client Service port 80 only

connectivity.reverseProxy.service.ports.http: 80
    └──► proxy Service port 80  (Ingress backend when backend: reverseProxy)

connectivity.ingress.rules[].paths[].backend: reverseProxy | service
    └──► selects which Service receives north-south traffic per path
```

#### Full example

```yaml
spec:
  edition: enterprise
  topology:
    mode: Cluster
    primaries:
      members: 3
  features:
    backup:
      enabled: true
    monitoring:
      prometheus:
        enabled: true
      serviceMonitor:
        enabled: true
  connectivity:
    listeners:
      bolt: 7687
      http: 7474
      https: null              # off — Ingress terminates TLS at proxy
    service:
      type: LoadBalancer
      expose:
        - bolt                  # drivers: direct LB → Neo4j
      ports:
        bolt: 7687
    reverseProxy:
      enabled: true
      expose:
        - http
      service:
        ports:
          http: 80
    ingress:
      enabled: true
      className: nginx
      annotations:
        cert-manager.io/cluster-issuer: letsencrypt-prod
      tls:
        - hosts: [neo4j.example.com]
          secretName: neo4j-ingress-tls
      rules:
        - host: neo4j.example.com
          paths:
            - path: /
              pathType: Prefix
              backend: reverseProxy
              port: http
    clusterDomain: cluster.local
    multiCluster:
      enabled: false
  trust:
    enabled: true
```

**Helm → operator:**

| Helm | Operator |
|------|----------|
| `config.server.http.*` + `services.neo4j.ports.http.port/targetPort` | `connectivity.listeners.http` + optional `connectivity.service.ports.http` |
| `services.default` + `services.neo4j` | `connectivity.service.type` + `expose` |
| `neo4j-reverse-proxy` chart | `connectivity.reverseProxy` + `ingress.rules` |
| `reverseProxy.ingress` | `connectivity.ingress` (backend = proxy Service) |
| `services.admin` | derived from `features` + topology |
| `services.internals` | derived from `topology.mode` |
| `config.server.backup.enabled` | `features.backup.enabled` + `connectivity.listeners.backup` |
| `monitoring.prometheus` (N/A in chart) | `features.monitoring.prometheus.enabled` |

---

## Comparison

| Criterion | A — Helm mirror | B — services (superseded) | E — ports+features | D — pool-scoped |
|-----------|-----------------|---------------------------|-------------------------|-----------------|
| Single port source | ❌ dual config | ❌ port bools | ✅ `connectivity.listeners.{name}` | ⚠️ |
| API minimalism | ❌ | ⚠️ admin in spec | ✅ derived admin/internals | ⚠️ |
| Simpler than Helm | ❌ | ⚠️ | ✅ | ⚠️ |
| Secure by default | ❌ | ✅ | ✅ | ✅ |
| V1 fit | over-broad | superseded | **best** | defer |

---

## Decision

**Accepted — definitive for V1** — Charles Boudry, 2026-06-22.

**We will implement Option E** — `spec.features` + unified **`spec.connectivity`** (`listeners` + `service` + **`reverseProxy`** + `ingress` + `clusterDomain` + `multiCluster`). **admin** / **internals** Services are operator-derived. No alternative exposure model for V1.

1. **V1 = Option E + Amendments B, D, E** (MVP subset). **V1.1+** adds `reverseProxy` + rich `ingress.rules` per Amendment F. V1 MVP: `service` only (ClusterIP, `expose: [bolt, http]`); no proxy, no Ingress.
2. **`connectivity.listeners.{name}`** (integer port) → `neo4j.conf` + `containerPort` + Service `targetPort` (single change).
3. **`connectivity.service.expose`** — which connectors are on the client Service; **`connectivity.service.ports.{name}`** — optional Service port remap only.
4. CEL: `connectivity.listeners.backup` set ⇒ `features.backup.enabled`; `connectivity.listeners.metrics` set ⇒ `features.monitoring.prometheus.enabled`; each `service.expose` entry ⇒ listener set (non-null).
5. **Derived:** admin when backup OR prometheus OR Cluster; internals when Cluster.
6. **Config vs ports:** port-owned `neo4j.conf` keys MUST NOT appear in `spec.config`, or MUST NOT contradict `connectivity.listeners` — admission fails with an explicit message naming both sides ([CFG-LISTENER-*](../../09-crd-spec/neo4j/validation.md)); denylist coordinated with [BDR-008](008-neo4j-config-surface.md).
7. **HTTPS ↔ TLS coupling** (including mTLS) — [BDR-011](011-https-connector-tls-coupling.md).

### Amendment B — unified `connectivity` block (accepted)

**Accepted** — Charles Boudry, 2026-06-22. Supersedes Amendment A (rejected).

1. **`spec.connectivity`** groups `listeners`, `service`, **`reverseProxy`**, `ingress`, `clusterDomain`, and **`multiCluster`** — one place for all reachability concerns.
2. **`connectivity.service`** — flat client Service (no `neo4j` key).
3. **`multiCluster`** stays inside `connectivity` — it modifies client Service port exposure (Helm `services.neo4j.multiCluster`), same domain as `service` and `ingress`.
4. **`spec.trust`** remains separate — TLS/crypto is not connectivity routing.

### Amendment C — rename `connectors` → `ports` (accepted)

**Accepted** — Charles Boudry, 2026-06-22.

`connectivity.connectors` renamed to **`connectivity.ports`** — Neo4j listen ports inside the pod. Distinct from `connectivity.services.ports` (Kubernetes Service façade). Superseded by Amendment D for field names; validation ID family renamed to CFG-LISTENER-* / TLS-LISTENER-* / LISTENER-*.

### Amendment D — rename `ports` → `listeners`, `services` → `service` (accepted)

**Accepted** — Charles Boudry, 2026-06-22.

1. **`connectivity.ports`** renamed to **`connectivity.listeners`** — Neo4j listen endpoints inside the pod (`enabled` + `port` per connector).
2. **`connectivity.services`** renamed to **`connectivity.service`** — singular flat client Service block (`type`, `annotations`, `loadBalancerSourceRanges`, `ports.*`).
3. **`connectivity.service.ports`** unchanged — Kubernetes Service port map (façade); distinct from `connectivity.listeners`.
4. Validation IDs: **CFG-LISTENER-***, **TLS-LISTENER-***, **LISTENER-*** (replace CFG-PORT / TLS-PORT / PORT).

### Amendment E — scalar listeners + `service.expose` (accepted)

**Accepted** — Charles Boudry, 2026-06-22.

1. **`connectivity.listeners.{name}`** — **integer port** when enabled; **omit** for operator default; **`null`** to explicitly disable a default-on connector (`bolt`, `http`).
2. Replaces `{enabled, port}` objects — fewer keys; port number implies enablement.
3. **`connectivity.service.expose`** — `[]string` of connector names published on the client Service (replaces `service.ports.{name}.enabled`).
4. **`connectivity.service.ports.{name}`** — optional **integer** Service port (façade only); omit ⇒ Service port = listener port. Keys not in `expose` are ignored.
5. Default `expose` when omitted: `[bolt, http]` for enabled listeners.

### Amendment F — `reverseProxy` + ingress routing rules (accepted)

**Accepted** — Charles Boudry, 2026-06-22. **Post-V1 (V1.1+)** — not in MVP scope lock.

1. **`connectivity.reverseProxy`** — optional operator-managed Deployment + Service (Helm `neo4j-reverse-proxy` parity). Upstream = client Service in same namespace.
2. **`reverseProxy.expose`** — connectors reached **through** the proxy (`http` typical; Bolt via WebSocket upgrade on same listener).
3. **`service.expose` ∩ `reverseProxy.expose` = ∅`** — each connector has one north-south path (NET-005).
4. **`connectivity.ingress.rules[]`** — per-host `paths[]` with `backend: service | reverseProxy` and `port` (connector name). Replaces flat `ingress.hosts` for routing; rule hosts still feed cert-manager SANs.
5. **`ingress.tls[]`** — explicit TLS blocks (`hosts`, `secretName`) + annotations for cert-manager / cloud LB certs.
6. Validation: NET-005 (disjoint expose), NET-006 (ingress backend/port coherence), NET-007 (`reverseProxy` requires `http` listener).

### Config vs `connectivity.listeners` — no contradictions

`connectivity.listeners` is the **single source of truth** for listen-port enablement and port numbers. The operator renders matching `server.*` keys into `neo4j.conf`.

Users may still set other keys via `spec.config` ([BDR-008](008-neo4j-config-surface.md)). Admission **must reject** any spec where `spec.config` fights `connectivity.listeners`:

| Strategy | V1 rule |
|----------|---------|
| **Denylist (primary)** | Port-owned keys MUST NOT appear in `spec.config` — user sets `connectivity.listeners` instead. |
| **Contradiction check (fallback)** | If a denylisted key is present anyway (e.g. webhook-only path), values MUST match what the operator would derive from `connectivity.listeners`; otherwise **fail** with a message naming both fields. |

**Port-owned keys (denylist excerpt — full list in BDR-008):**

| Connector (Neo4j name) | Reserved `spec.config` keys |
|-----------|----------------------------|
| `bolt` | `server.bolt.listen_address`, `server.bolt.enabled` |
| `http` | `server.http.listen_address`, `server.http.enabled` |
| `https` | `server.https.listen_address`, `server.https.enabled` |
| `backup` | `server.backup.listen_address`, `server.backup.enabled` |
| `metrics` | `server.metrics.prometheus.enabled`, `server.metrics.prometheus.endpoint` |

**Example rejection messages:**

- `spec.config contains server.http.listen_address=:8080 but connectivity.listeners.http is 7474 — remove the config key or align values`
- `spec.config contains server.http.enabled=false but connectivity.listeners.http is set — remove server.http.enabled from spec.config`

TLS policy keys (`server.bolt.tls_level`, `dbms.ssl.policy.*`, `*.client_auth`) remain owned by `spec.trust` — same denylist / contradiction rules per [BDR-006](007-tls-trust-model.md) and BDR-008.

Validation IDs: **CFG-LISTENER-001..004** in [`validation.md`](../../09-crd-spec/neo4j/validation.md). Mechanism: **CEL** at admission (key presence); **webhook** if port parsing from `listen_address` is required.

---

## Consequences

### Positive

- One coherent, intent-based exposure surface aligned with the single-CR / single-STS model (BDR-002).
- `connectivity.listeners` as single source of truth — `spec.config` cannot silently override listen ports or enablement (CFG-LISTENER-*).
- Secure by default (`services.neo4j.type: ClusterIP`); cluster discovery (`internals`) cannot be misconfigured.
- Eliminates Helm multi-release artifacts (shared LB `resource-policy: keep`, per-pod `loadbalancer` label, `cleanup` hook) from the public API.
- `11-helm-mapping.md` gets a clear translation table instead of four leaky pass-through blocks.

### Negative

- Helm users map `services.default` + `services.neo4j` → `connectivity.service`; `services.admin` / `internals` documented as operator-derived.
- Operator must own the derivation logic for admin/internals/headless Services and keep it correct across `topology.mode`.
- Advanced per-pool exposure (`podSpec.loadbalancer` use cases) is unavailable until Option D ships.

### Neutral

- `clusterDomain` becomes an optional override (`cluster.local` default) rather than an always-present value.
- Cloud-LB customization on `services.neo4j`: `annotations` + `loadBalancerSourceRanges` when `type: LoadBalancer`.
- `breaking-change-register.md` BC-005 resolved by this BDR; `_index.csv` AGG-EXPOSURE rows point here.

---

## References

- `design/analysis/helm-fields/fields/services.default.md`, `services.neo4j.md`, … `clusterDomain.md`
- `helm-charts/neo4j-reverse-proxy/` — reverse-proxy Deployment, Ingress, upstream `SERVICE_NAME`
- `design/analysis/helm-fields/aggregation-matrix.md` — group **AGG-EXPOSURE**
- `design/analysis/helm-fields/semantic-concerns.yaml` — `CONCERN-EXPOSURE`
- `design/analysis/helm-fields/breaking-change-register.md` — **BC-005**
- [`09-crd-spec/neo4j/spec.md`](../../09-crd-spec/neo4j/spec.md) — `spec.connectivity`
- FRs: `NEO-2-007`, `NEO-3-007-SVC-01/02/03`, `NEO-3-007-PRT-01..04`, `NEO-3-007-MULTI-02`, `NEO-2-013`, `NEO-2-018`
- [Neo4j — Networking (Kubernetes)](https://neo4j.com/docs/operations-manual/current/kubernetes/) · [Connectors](https://neo4j.com/docs/operations-manual/current/configuration/connectors/) · [Clustering](https://neo4j.com/docs/operations-manual/current/clustering/)
- [BDR-001](001-single-neo4j-crd.md) · [BDR-002](002-neo4j-crd-topology.md) · [BDR-004](004-neo4j-plugin-topology.md) · [BDR-006](007-tls-trust-model.md) · [BDR-010](010-neo4j-features-catalog.md) · [BDR-011](011-https-connector-tls-coupling.md)
