# ADR-018 — Enterprise Studio deployment architecture

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-10-06 |
| **Depends on** | [BDR-015](../business/studio/015-enterprise-studio.md) — `Neo4jStudio` CRD contract · [ADR-002](002-package-layering.md) — `render` / `domain` / `controller` layering · [ADR-005](005-render-conventions.md) — naming, labels, owner refs · [ADR-006](006-apply-and-idempotency.md) — apply strategy · [ADR-007](007-formation-and-bolt.md) — Bolt (asset-DB path) · [ADR-001](001-crd-validation-process.md) — CEL vs webhook |
| **Constraints** | Studio is a stateless web app (no PVC) · its license + served DBs are external inputs · one-to-many deployments · V2 |

---

## Context

[BDR-015](../business/studio/015-enterprise-studio.md) fixes the API: a `Neo4jStudio` satellite CRD that stands up Enterprise Studio and points it at one-to-many `Neo4j` deployments. This ADR decides **how the operator reconciles it**: what Kubernetes objects it renders, how Studio is configured (env vs mounted `config.yaml`), how the controller fits the existing layering, and how the asset-DB prerequisite is handled.

What is fixed by [BDR-015](../business/studio/015-enterprise-studio.md) and must not be redefined here:

- One `Neo4jStudio` `kind`; `deployments[]` is one-to-many (`neo4jRef` **or** external `uri`), each `mode: Direct | ReverseProxy`.
- The Studio license is an **optional** `secretRef`, distinct from the Neo4j Enterprise license — Studio starts without it ([prerequisites](https://neo4j.com/docs/enterprise-studio/current/prerequisites/#_acquire_a_license_key_optional)).
- Asset-store credentials and per-deployment auth are **referenced Secrets**, never inline.
- The asset-DB provisioning owner (BYO vs operator) is the open §4 sub-decision — this ADR resolves the **execution** of whichever is chosen and recommends BYO first.

**Forces / what breaks if we choose wrong:**

- Studio is **stateless** — modelling it as anything with a PVC/StatefulSet (like `Neo4j`) is wrong; it is a `Deployment`.
- Studio **validates its config at startup and exits immediately** if a required key is missing ([configuration](https://neo4j.com/docs/enterprise-studio/current/configuration/)) — a config regression is a crash-loop, so the operator must assemble the full required set deterministically.
- Env vars **override** the mounted `config.yaml` ([configuration reference](https://neo4j.com/docs/enterprise-studio/current/configuration-reference/)); mixing both without a precedence rule produces surprising runtime values.
- `NES_*` carries **secrets** (asset-store password, OIDC client secret) — putting them in a ConfigMap or a plain env literal leaks them into `kubectl describe`.
- A `ReverseProxy` deployment against a **cluster** without session affinity breaks Studio↔cluster HTTP sessions ([prerequisites](https://neo4j.com/docs/enterprise-studio/current/prerequisites/)). **Correction (BDR-016):** the affinity that fixes this is on the **served cluster's** client Service (`sessionAffinity: ClientIP`), not Studio's front door — the front-door stickiness below only pins browser→Studio. Studio holds no cluster-affinity logic; it resolves to a now-affinity-safe Service (see [BDR-016](../business/neo4j/016-cluster-http-session-affinity.md)).
- Resolving an in-cluster `neo4jRef` to a URI must reuse the **existing Service naming** ([ADR-005](005-render-conventions.md), [BDR-007](../business/neo4j/006-service-exposure-connectivity.md)) — re-deriving it independently drifts from the workload's real Services.

---

## Analysis

### Option A — Native render: operator builds the Deployment + Service + ConfigMap/Secret + Ingress — **chosen**

A `Neo4jStudioReconciler` runs a thin pipeline; `render/studio` builds pure objects from `spec`; `domain/studio` applies them and (optionally) talks Bolt for the asset DB. This is exactly how `Neo4j` and the backup CRDs already work ([ADR-002](002-package-layering.md), [ADR-005](005-render-conventions.md)).

| Advantages | Disadvantages |
|------------|---------------|
| Identical layering/testing to the rest of the operator — table-driven render golden files, envtest domain | Operator owns the full object set (Deployment/Service/Ingress/Secret) |
| No new runtime dependency (no Helm/Tiller/subchart in the manager) | Must track Studio's required-config set across Studio versions |
| `ownerReferences` → `Neo4jStudio` give free GC and status mirroring ([ADR-005](005-render-conventions.md), [ADR-006](006-apply-and-idempotency.md)) | — |
| Reuses `render/connectivity` for Service/Ingress and `render/trust` for TLS | — |

### Option B — Wrap the upstream Studio Helm chart (operator shells out to Helm / applies the subchart)

The reconciler renders the official Studio Helm chart with values derived from `spec` and applies the output.

| Advantages | Disadvantages |
|------------|---------------|
| Upstream owns the manifest shape and tracks Studio changes | A Helm engine (or `helm template` shell-out) in the operator hot path — new dependency, new failure surface |
| Less render code | Chart values drift from our CRD; every chart bump risks our mapping |
| — | Breaks the pure-`render` layering ([ADR-002](002-package-layering.md)); no golden-file testability |
| — | Chart-rendered objects are not natively owner-referenced to `Neo4jStudio` — status/GC become bespoke |

### Option C — Sidecar: run Studio as a container in a `Neo4j` pod

Studio runs alongside the database in the same pod.

| Advantages | Disadvantages |
|------------|---------------|
| No separate Deployment to schedule | Cannot be one-to-many ([BDR-015](../business/studio/015-enterprise-studio.md)) — a sidecar serves one DB |
| — | Couples Studio rollout to the StatefulSet rollout ([ADR-013](013-neo4j-conf-directory-fragments.md)) |
| — | Studio restart/upgrade churns the database pod; contradicts "stateless, independent lifecycle" |
| — | Shares the DB pod's security context + resources |

---

## Comparison

| Criterion | A — native render (chosen) | B — Helm wrap | C — sidecar |
|-----------|----------------------------|---------------|-------------|
| Testability (golden render + envtest) | **high** | low | medium |
| Layering fit ([ADR-002](002-package-layering.md)) | **clean** | breaks `render` purity | poor |
| New runtime dependency | none | Helm engine | none |
| One-to-many support | ✅ | ✅ | ❌ |
| ownerRef GC + status mirror | native | bespoke | shared pod |
| controller-runtime fit | **high** (owned Deployment) | medium | low |
| V2 fit | **yes** | no | no |

---

## Decision

We will use **Option A** — the operator **natively renders** Enterprise Studio as a `Deployment` and companions, with a `Neo4jStudioReconciler` that mirrors the existing CRD reconcilers.

### Packages & layering ([ADR-002](002-package-layering.md), [ADR-005](005-render-conventions.md))

| Layer | Package | Responsibility |
|-------|---------|----------------|
| `render` | `internal/render/studio` | Pure builders: `Deployment`, `Service`, `ServiceAccount`, projected license/credentials `Volume`s, optional `Ingress`. Zero client, zero I/O. Reuses `render/connectivity` (Service/Ingress) and `render/trust` (web TLS) |
| `domain` | `internal/domain/studio` | Resolve `neo4jRef` → client URI; assemble the `NES_*` env + optional `config.yaml`; apply the object set ([ADR-006](006-apply-and-idempotency.md)); (4-b only) ensure the asset DB over Bolt ([ADR-007](007-formation-and-bolt.md)); write `status` |
| `controller` | `internal/controller/neo4jstudio` | Thin pipeline: validate → resolve deployments → render/apply → probe reachability → set conditions. ~150 lines ([layer.md](../../architecture/layer.md)) |

```go
// internal/controller/neo4jstudio — sketch only, not binding
func (r *Neo4jStudioReconciler) Reconcile(ctx context.Context, s *v1.Neo4jStudio) (ctrl.Result, error) {
    if err := r.validate(ctx, s); err != nil { return r.error(s, err) }      // refs exist, Enterprise, HTTP enabled (license optional)
    uris, err := studio.ResolveDeployments(ctx, r.Client, s)                  // neo4jRef → Service URI; or external uri verbatim
    if err != nil { return r.error(s, err) }
    if err := studio.EnsureAssetDB(ctx, r.Client, s); err != nil { return r.error(s, err) } // no-op under 4-a (BYO)
    if err := studio.Apply(ctx, r.Client, s, uris); err != nil { return r.error(s, err) }   // Deployment+Service+SA+Ingress
    return r.observe(s)                                                       // Deployment Available + asset-DB reachable → Ready
}
```

### What gets rendered

- **`Deployment`** (1 replica default; `replicas` configurable later) running `image.repository`:`spec.version`, container port `NES_server_port` (default `8080`), owner-referenced to the `Neo4jStudio` ([ADR-005](005-render-conventions.md)). No PVC — Studio is stateless.
- **`Service`** fronting the Studio port, shape from `spec.connectivity.service` via `render/connectivity`. When any served deployment is `ReverseProxy` against a `Cluster`, Studio's own Service is rendered with **`sessionAffinity: ClientIP`** (Ingress gets the sticky-session annotation) — this pins the **browser→Studio** hop. Note this does **not** fix the Studio→cluster transaction routing; that requires affinity on the served cluster's own client Service, owned by [BDR-016](../business/neo4j/016-cluster-http-session-affinity.md).
- **optional `Ingress`** from `spec.connectivity.ingress.rules`, reusing the [BDR-007](../business/neo4j/006-service-exposure-connectivity.md) builder.
- **`ServiceAccount`** for the Studio pod (no special RBAC — Studio talks to Neo4j over Bolt/HTTP, not the K8s API).

### Config assembly — env for secrets, projected file for the rest

Studio reads a `config.yaml` and lets any `NES_*` env var override it (env wins) ([configuration](https://neo4j.com/docs/enterprise-studio/current/configuration/)). We exploit that split deliberately:

| Source | Carries | Why |
|--------|---------|-----|
| Projected **`config.yaml`** (ConfigMap, mounted) | Non-secret keys: `server.port`, deployment URIs + `mode`, `assetStore.default.{uri,database}`, passthrough `spec.config` | Declarative, diffable, non-sensitive |
| **env from `secretRef`** (`NES_assetStore_default_authentication_basic_password`, OIDC `clientSecret`, …) | Secrets only | Env **overrides** the file, and `valueFrom.secretKeyRef` keeps secrets out of the ConfigMap and `describe` output |
| Mounted **license file** (optional) | the NES license (`spec.license.secretRef`, when set) → `NES_license_path` points at the mount | Studio starts without it; when provided, a file mount matches the product's `license.path` model |

The operator assembles the **complete required-config set** ([configuration — minimum required](https://neo4j.com/docs/enterprise-studio/current/configuration/)) deterministically, so Studio never crash-loops on a missing key. A single `studio.RequiredConfig(version)` function is the source of truth and is version-aware for forward compatibility.

### Resolving `neo4jRef` → URI

`domain/studio` resolves an in-cluster `neo4jRef` to a client URI using the **same Service naming the workload renders** ([ADR-005](005-render-conventions.md), [BDR-007](../business/neo4j/006-service-exposure-connectivity.md)) — never a re-derived guess:

- `mode: Direct` → `bolt://<client-service>.<ns>.svc:7687` (`neo4j://` for cluster-aware routing).
- `mode: ReverseProxy` → `http(s)://<client-service>.<ns>.svc:7474/7473`, and the fronting Service/Ingress is made sticky (above).
- external `uri` → used verbatim; the operator does not resolve or validate reachability at admission (documented, [BDR-015](../business/studio/015-enterprise-studio.md)).

### Validation placement ([ADR-001](001-crd-validation-process.md))

| Check | Where | Why |
|-------|-------|-----|
| exactly one of `neo4jRef` / `uri` per deployment; `mode` enum (license is optional — no presence check) | **CEL** | pure, self-contained — no lookups |
| referenced `Neo4j` exists, is `edition: enterprise`, exposes `http`/`https` | **webhook/reconcile** | cross-object/runtime state — no CEL ([ADR-001](001-crd-validation-process.md)) |
| asset DB exists (4-a) / is reachable | **reconcile** (`SHOW DATABASES` over Bolt) | runtime fact; mirrors the restore existence gate ([ADR-015](015-backup-and-restore.md) §restore) |

### Asset-DB provisioning ([BDR-015](../business/studio/015-enterprise-studio.md) §4 — open)

The CRD contract leaves the owner open; this ADR fixes **how each path executes** and recommends starting with 4-a:

- **4-a (BYO) — chosen first.** `EnsureAssetDB` is a **reachability probe only**: over Bolt (asset-store `secretRef`), confirm `assetStore.database` exists and the account has `architect`; on failure set `status.phase=Degraded`, `reason=AssetDatabaseMissing`, Event. The operator **creates nothing**. Smallest surface, no Bolt admin verbs, no overlap with [BDR-012](../business/identity-user-roles/012-identity-management.md).
- **4-b (operator-provisioned) — deferred.** Would issue `CREATE DATABASE <assetStore.database>` + role/user grant over Bolt ([ADR-007](007-formation-and-bolt.md)) and write the generated credential to a Secret. We **defer** this until [BDR-012](../business/identity-user-roles/012-identity-management.md) identity management lands, so asset-DB/account creation reuses that controller rather than a parallel Bolt admin path. `ponytail:` 4-a is the known-ceiling simplification — ceiling is "user must pre-create the asset DB"; upgrade path is 4-b on top of BDR-012.

### Apply & idempotency ([ADR-006](006-apply-and-idempotency.md))

Server-side apply the owned object set; re-reconcile is a no-op when the live objects match. A `spec.version` bump is a normal Deployment image change → rolling update, no data migration (stateless).

### Status & conditions ([ADR-004](004-status-and-conditions.md))

`status.phase` ∈ {Pending, Running, Degraded, Failed}; `Ready` condition gated on **Deployment `Available`** AND **asset-DB reachable**; `status.url` resolved from Service/Ingress; `status.deployments[].{resolvedUri,reachable}` per entry. Reason strings live in `internal/oracle` and project into [`errors.md`](../../../user-guide/05-reference/errors.md) ([ADR-014](014-operator-observability.md)) — not enumerated here.

### RBAC delta

Operator SA gains `apps/deployments`, `core/services`, `core/serviceaccounts`, `core/configmaps`, (optional) `networking/ingresses` create/update/watch, read on referenced `Secrets` (`resourceNames`-scoped where possible), and the new `neo4jstudios` CRD group. No `pods/exec`. Bolt admin verbs only if/when 4-b is adopted.

### Finalizers ([ADR-008](008-finalizers-and-deletion.md))

**No finalizer** — all rendered objects are owner-referenced and GC with the `Neo4jStudio`. Studio holds no persistent state; deleting the CR fully tears it down. (A finalizer is only revisited if 4-b must drop an operator-created asset DB on delete — opt-in, consistent with [ADR-008](008-finalizers-and-deletion.md).)

**Rejected:** Option B (Helm engine in the hot path — new dependency, breaks `render` purity, bespoke GC/status), Option C (sidecar — cannot be one-to-many, couples lifecycle to the DB pod). Also rejected: config entirely via ConfigMap (leaks secrets) or entirely via env literals (unmanageable, leaks in `describe`); re-deriving Service URIs instead of reusing [ADR-005](005-render-conventions.md) naming.

---

## Consequences

### Positive

- Studio reconciliation is the **same shape** as every other CRD in the operator — one reconciler, pure `render/studio`, applied by `domain/studio`; nothing new to learn or test differently.
- No new runtime dependency in the manager (no Helm engine).
- Secrets stay in `secretKeyRef` env / mounted files; the ConfigMap is non-sensitive and diffable.
- Session-affinity and HTTP-connector prerequisites are enforced by render/validation, not left as runbook footguns.
- Owner references give free GC and status mirroring; `spec.version` upgrades are plain rollouts.

### Negative

- The operator owns the full Studio object set and must track Studio's required-config set across versions (`studio.RequiredConfig(version)`).
- New RBAC (`apps/deployments` et al.) and a new controller + webhook + E2E surface.
- Under 4-a, the asset-DB prerequisite is a reachability probe only — a missing DB surfaces as `Degraded`, not auto-healed.

### Neutral

- `replicas`/HA for Studio is a later `spec` addition — V2 defaults to 1 (stateless, cheap to restart).
- 4-b (operator-provisioned asset DB) rides on top of [BDR-012](../business/identity-user-roles/012-identity-management.md) when that lands; this ADR's package layout already has the Bolt seam for it.
- A slimmer Studio-only image is upstream's concern, not the operator's.

---

## References

- [BDR-015](../business/studio/015-enterprise-studio.md) — `Neo4jStudio` CRD contract (this ADR implements it)
- [ADR-001](001-crd-validation-process.md) · [ADR-002](002-package-layering.md) · [ADR-004](004-status-and-conditions.md) · [ADR-005](005-render-conventions.md) · [ADR-006](006-apply-and-idempotency.md) · [ADR-007](007-formation-and-bolt.md) · [ADR-008](008-finalizers-and-deletion.md) · [ADR-014](014-operator-observability.md) · [ADR-015](015-backup-and-restore.md) — satellite-reconciler precedent
- [BDR-007](../business/neo4j/006-service-exposure-connectivity.md) — Service/Ingress builders reused here
- [layer.md](../../architecture/layer.md) — `render` / `domain` / `controller` rule
- [Enterprise Studio — deployments](https://neo4j.com/docs/enterprise-studio/current/deployments/) · [Docker](https://neo4j.com/docs/enterprise-studio/current/docker/) · [configuration](https://neo4j.com/docs/enterprise-studio/current/configuration/) · [configuration reference](https://neo4j.com/docs/enterprise-studio/current/configuration-reference/) · [prerequisites](https://neo4j.com/docs/enterprise-studio/current/prerequisites/)
