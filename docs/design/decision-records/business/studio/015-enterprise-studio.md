# BDR-015 — Enterprise Studio CRD: `Neo4jStudio`

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-10-06 |
| **Depends on** | [BDR-001](../neo4j/001-single-neo4j-crd.md) — single `Neo4j` workload CRD, day-2 satellites via `neo4jRef` (accepted) · [BDR-007](../neo4j/006-service-exposure-connectivity.md) — `service` + `ingress.rules` exposure model (accepted) · [BDR-010](../neo4j/010-neo4j-features-catalog.md) — Enterprise-only gating + HTTP/HTTPS connector (accepted) |
| **Related** | [BDR-012](../identity-user-roles/012-identity-management.md) — identity management, owns the asset-DB/account creation path if §4-b is ever adopted (proposed) · [ADR-018](../../architecture/018-enterprise-studio.md) — deployment architecture (triggered by this BDR) |
| **Constraints** | Enterprise only · Studio is a **stateless** web app (no PVC) · license + served DBs are **external** inputs · one-to-many deployments · V2 deliverable |
| **Reference** | [Enterprise Studio — deployments](https://neo4j.com/docs/enterprise-studio/current/deployments/) · [configuration](https://neo4j.com/docs/enterprise-studio/current/configuration/) · [prerequisites](https://neo4j.com/docs/enterprise-studio/current/prerequisites/) |

---

## Context

Neo4j **Enterprise Studio** is a self-hosted web application for querying, visualizing, and managing Neo4j deployments. Customers running the operator want to stand Studio up **declaratively**, point it at one or more `Neo4j` workloads the operator already manages, and let it participate in the same GitOps/lifecycle flow as every other `kind`. This BDR fixes the **customer-facing contract**: whether Studio is a new `kind` at all, what a user declares in YAML, how it references the databases it serves, and what is in / out of the first release. **How** the operator reconciles that contract — the objects it renders, config assembly, validation placement — is owned by [ADR-018](../../architecture/018-enterprise-studio.md).

What is already fixed by accepted decisions (cited, not redefined here):

- **Satellite CRDs reference the workload by `neo4jRef`** (same-namespace, name-only), never by embedding into `Neo4j.spec` — the pattern established for backup/restore ([BDR-001](../neo4j/001-single-neo4j-crd.md), [BDR-014](../backup-restore/014-backup-restore.md)).
- **Service exposure and Ingress** use the `service` + `ingress.rules` model ([BDR-007](../neo4j/006-service-exposure-connectivity.md)) — Studio's own front door reuses it, it does not invent a new exposure surface.
- **Enterprise is the gate** and the HTTP/HTTPS connector is a first-class listener ([BDR-010](../neo4j/010-neo4j-features-catalog.md)) — Studio's reachability depends on that connector being enabled on the served workload.

### Neo4j mechanism (the contract must map to this)

Studio is not a database — it is a stateless front end with three external dependencies the contract must express:

| Dependency | What it is | Provided by |
|-----------|------------|-------------|
| **Served deployment(s)** | The Neo4j DBMS(s) a user queries through Studio | `neo4jRef` (in-cluster) **or** external `uri` |
| **Asset store** | A Neo4j database where Studio persists its own assets (saved queries, visualizations, per-user state) | A reachable Neo4j database + an account with the `architect` role |
| **License** (optional) | The Enterprise Studio license key — **distinct** from the Neo4j Enterprise license | A `Secret`; Studio **starts without it** ([prerequisites](https://neo4j.com/docs/enterprise-studio/current/prerequisites/#_acquire_a_license_key_optional)) |

Studio reaches a served deployment in one of **two modes** ([deployments](https://neo4j.com/docs/enterprise-studio/current/deployments/)):

| Mode | How the browser reaches the DB | When |
|------|--------------------------------|------|
| **`Direct`** | The user's browser connects straight to the DBMS over Bolt (`bolt://`/`neo4j://`) | DB is network-reachable from the client |
| **`ReverseProxy`** | Studio proxies HTTP traffic to the DBMS's HTTP connector; the browser only talks to Studio | DB is not directly reachable; Studio is the single front door |

A **`ReverseProxy`** deployment against a **cluster** breaks Studio↔cluster HTTP sessions unless the front door has **session affinity** — a hard prerequisite, not a tuning knob ([prerequisites](https://neo4j.com/docs/enterprise-studio/current/prerequisites/)).

### Forces

1. **Satellite parity** — the operator already models day-2 add-ons (backup, restore) as satellite CRDs that reference the workload by `neo4jRef` ([BDR-001](../neo4j/001-single-neo4j-crd.md), [BDR-014](../backup-restore/014-backup-restore.md)); Studio is the same shape of problem (declarative add-on over an existing workload).
2. **One-to-many is intrinsic** — a single Studio instance serves *multiple* deployments; any model that binds Studio 1:1 to a `Neo4j` cannot express the product ([deployments](https://neo4j.com/docs/enterprise-studio/current/deployments/)).
3. **Secrets are the user's** — the Studio license, asset-store credentials, and per-deployment auth are **referenced `Secret`s**, never inline literals, consistent with the rest of the API.
4. **Stateless add-on** — Studio holds no persistent state of its own (its assets live in the asset DB, which is external), so the contract must not imply a PVC, a StatefulSet, or data-migration semantics.
5. **API minimalism** — every `kind` adds webhook, RBAC and E2E surface ([BDR-001](../neo4j/001-single-neo4j-crd.md)); a new `kind` must earn it.
6. **External deployments must be expressible** — Studio can serve a Neo4j the operator does **not** manage (another cluster, Aura, a bare URI); the contract cannot assume every served DB is an in-cluster `Neo4j`.

---

## Options under review

### Option A — Dedicated `Neo4jStudio` satellite CRD — **chosen**

A new `kind` in the `neo4j.com` group, referencing served deployments by `neo4jRef` or external `uri`, exactly the satellite pattern backup/restore already use.

```yaml
apiVersion: neo4j.com/v1
kind: Neo4jStudio
metadata:
  name: studio
spec:
  version: "1.2.3"                         # Studio image tag; a bump is a rolling Deployment update
  license:                                 # OPTIONAL — Studio starts without it
    secretRef: { name: studio-license }    # distinct from the Neo4j Enterprise license
  deployments:                             # one-to-many — at least one
    - name: prod-graph                     # display name in Studio
      neo4jRef: { name: my-graph }         # in-cluster Neo4j (same namespace, BDR-001) …
      mode: ReverseProxy                   # Direct | ReverseProxy
      auth:
        secretRef: { name: prod-graph-auth }   # per-deployment credentials (referenced, never inline)
    - name: external-aura
      uri: "neo4j+s://xxxx.databases.neo4j.io"  # … OR an external URI (mutually exclusive with neo4jRef)
      mode: Direct
      auth:
        secretRef: { name: aura-auth }
  assetStore:                              # the Neo4j DB where Studio persists its own assets
    uri: "bolt://my-graph-client.default.svc:7687"
    database: studio-assets                # must exist; the account needs the `architect` role (see §4)
    auth:
      secretRef: { name: asset-store-auth }
  connectivity:                            # Studio's own front door — reuses BDR-007
    service: { type: ClusterIP }
    ingress:
      rules: [ { host: studio.example.com } ]
  config: {}                               # OPTIONAL passthrough of non-secret NES_* keys
status:
  phase: Running                           # Pending | Running | Degraded | Failed
  url: "https://studio.example.com"
  deployments:
    - name: prod-graph
      resolvedUri: "https://my-graph-client.default.svc:7473"
      reachable: true
```

| Advantages | Disadvantages |
|------------|---------------|
| Identical mental model to backup/restore satellites — `neo4jRef`, status, GitOps re-apply ([BDR-001](../neo4j/001-single-neo4j-crd.md), [BDR-014](../backup-restore/014-backup-restore.md)) | A new `kind` = webhook + RBAC + E2E surface |
| **One-to-many** `deployments[]` falls out naturally | Operator must track Studio's required-config set across versions ([ADR-018](../../architecture/018-enterprise-studio.md)) |
| External `uri` deployments are first-class, not a special case | — |
| Studio lifecycle is independent of any served workload — restart/upgrade Studio without touching a DB | — |
| Owner references give free GC and status; no finalizer needed (stateless) | — |

### Option B — Embed Studio in `Neo4j.spec.studio`

Studio becomes an optional block on the workload CR; the operator stands it up alongside the DBMS.

```yaml
apiVersion: neo4j.com/v1
kind: Neo4j
spec:
  edition: enterprise
  studio:                        # optional block
    enabled: true
    license: { secretRef: { name: studio-license } }
```

| Advantages | Disadvantages |
|------------|---------------|
| No new `kind` | **Cannot be one-to-many** — a `Neo4j` is one workload; Studio serving three clusters has no home |
| Single manifest | Couples Studio's lifecycle to the DBMS CR — a Studio version bump mutates the workload spec |
| — | Bloats the already-largest `Neo4j.spec` ([BDR-001](../neo4j/001-single-neo4j-crd.md)) |
| — | Cannot serve an **external** (non-operator) deployment at all |

### Option C — No CRD: ship a Helm chart / documented raw manifests

The operator does nothing; users deploy upstream Studio via its own Helm chart and wire it by hand.

| Advantages | Disadvantages |
|------------|---------------|
| Zero operator surface | Studio is outside the operator's lifecycle, status, and GitOps flow entirely |
| Upstream owns the manifests | Users hand-assemble config, session affinity, and asset-DB wiring — the footguns the operator exists to remove |
| — | No `neo4jRef` resolution — users hard-code Service URIs that drift from the workload ([BDR-007](../neo4j/006-service-exposure-connectivity.md)) |
| — | Breaks parity with every other day-2 add-on the operator manages |

---

## Comparison

| Criterion | A — `Neo4jStudio` CRD (chosen) | B — embed in `Neo4j.spec` | C — no CRD |
|-----------|-------------------------------|---------------------------|------------|
| One-to-many deployments | ✅ | ❌ | ⚠️ manual |
| External (non-operator) DB support | ✅ | ❌ | ✅ (manual) |
| Satellite parity ([BDR-001](../neo4j/001-single-neo4j-crd.md), [BDR-014](../backup-restore/014-backup-restore.md)) | ✅ | ❌ | ❌ |
| Independent lifecycle | ✅ | ❌ coupled to DBMS | ✅ |
| `neo4jRef` resolution + status | ✅ native | ⚠️ | ❌ |
| GitOps re-apply safety | ✅ | ⚠️ workload churn | ⚠️ |
| API surface cost | medium (one `kind`) | low | none |
| Bloats `Neo4j.spec` | no | **yes** | no |

---

## Decision

**Proposed** — 2026-10-06.

**We will adopt Option A** — a dedicated **`Neo4jStudio`** satellite CRD in the `neo4j.com` group.

1. **One `kind`: `Neo4jStudio`** — a satellite that references served workloads by `neo4jRef` (in-cluster) or external `uri`, mirroring the backup/restore pattern ([BDR-001](../neo4j/001-single-neo4j-crd.md), [BDR-014](../backup-restore/014-backup-restore.md)). It is **not** a field of `Neo4j`.
2. **`deployments[]` is one-to-many** — each entry has **exactly one** of `neo4jRef` (resolved to the workload's client Service URI by the operator, never re-derived — [ADR-018](../../architecture/018-enterprise-studio.md)) **or** external `uri` (used verbatim; the operator does not resolve or validate reachability at admission), plus a `mode: Direct | ReverseProxy` and a per-deployment `auth.secretRef`.
3. **Studio license is optional and distinct** — `spec.license.secretRef` carries the **Enterprise Studio** license, which is a different artifact from the Neo4j Enterprise license. Studio **starts without it** ([prerequisites](https://neo4j.com/docs/enterprise-studio/current/prerequisites/#_acquire_a_license_key_optional)), so admission does **not** require it.
4. **Asset-DB provisioning owner — left open; BYO recommended first.** Studio needs a reachable asset database whose account holds the `architect` role. The *owner* of creating that database is deliberately **not fixed** by this release:
    - **4-a (BYO) — recommended first.** The user pre-creates the asset database and account; the operator **creates nothing** and only treats a missing/unreachable asset DB as `status.phase=Degraded`. Smallest surface, no operator-side Bolt admin verbs, no overlap with identity management.
    - **4-b (operator-provisioned) — deferred.** The operator would `CREATE DATABASE` + grant the `architect` role and write the generated credential to a Secret. We **defer** this until [BDR-012](../identity-user-roles/012-identity-management.md) identity management lands, so asset-DB/account creation reuses that controller rather than a parallel Bolt-admin path. `ponytail:` 4-a is the known-ceiling simplification — ceiling is "user must pre-create the asset DB"; upgrade path is 4-b on top of BDR-012.
5. **All credentials are referenced `Secret`s** — the license, each `deployments[].auth`, and `assetStore.auth` are `secretRef`s. **No inline literals** — secrets never land in a spec field or `kubectl describe`.
6. **Enterprise + HTTP connector required for served deployments** — Studio serves Enterprise DBMSs, and a `ReverseProxy` (or HTTP-mode) deployment requires the served workload to expose its HTTP/HTTPS connector ([BDR-010](../neo4j/010-neo4j-features-catalog.md)). For an in-cluster `neo4jRef`, this is validated at reconcile against the referenced `Neo4j`; for an external `uri`, it is the user's responsibility (documented).
7. **Session affinity is enforced, not documented** — when any served deployment is `ReverseProxy` against a cluster, Studio's own front door is made sticky; this prerequisite is enforced by the reconciler/render, not left as a runbook footgun (execution: [ADR-018](../../architecture/018-enterprise-studio.md)).
8. **Studio exposure reuses `service` + `ingress.rules`** — `spec.connectivity` follows the established exposure model ([BDR-007](../neo4j/006-service-exposure-connectivity.md)); Studio does not invent a new exposure surface.
9. **Config is split secret-vs-non-secret** — non-secret `NES_*` keys are declarative (`spec.config` passthrough + operator-assembled required set); secrets ride in `secretKeyRef` env. The *how* (file vs env precedence, required-config assembly) is owned by [ADR-018](../../architecture/018-enterprise-studio.md).
10. **Scope — V2, stateless, no HA knob yet.** Studio is a V2 deliverable. It is stateless (no PVC); `replicas`/HA is a **later** `spec` addition (V2 defaults to a single replica — cheap to restart). A `spec.version` bump is a plain rolling Deployment update, **no data migration**.

**Rejected:** Option B (cannot be one-to-many, couples Studio to the DBMS lifecycle, bloats `Neo4j.spec`, cannot serve external DBs), Option C (Studio outside the operator's lifecycle/status/GitOps flow, hand-wired config and session affinity). Also rejected: **inline credential literals** (§5 — leak in `describe`), **requiring the Studio license at admission** (§3 — Studio starts without it), and **re-deriving served-Service URIs** instead of resolving `neo4jRef` through the operator's own naming (§2, [ADR-018](../../architecture/018-enterprise-studio.md)).

---

## Consequences

### Positive

- Studio is managed exactly like every other day-2 satellite — `neo4jRef`, status, GitOps re-apply — nothing new for users to learn.
- One-to-many and external-`uri` deployments are both first-class, matching the product.
- Credentials stay in referenced `Secret`s; nothing sensitive lands in the spec or `describe`.
- Studio's lifecycle is independent of the DBMS — upgrade or restart Studio without touching a database.
- Session-affinity and HTTP-connector prerequisites are enforced by the operator, not left as runbook caveats.
- Stateless + owner-referenced → free GC, no finalizer, `spec.version` bumps are plain rollouts.

### Negative

- A new `kind` adds a validating webhook, RBAC, and E2E surface.
- Under §4-a, a missing/misconfigured asset DB surfaces as `Degraded`, not auto-healed — the user must pre-create it.
- The operator must track Studio's required-config set across Studio versions ([ADR-018](../../architecture/018-enterprise-studio.md)).

### Neutral

- `replicas`/HA for Studio is a later `spec` addition — V2 defaults to one.
- Exact field spelling and CRD group/version (`neo4j.com/v1`) are finalized in the CRD spec, not here.
- §4-b (operator-provisioned asset DB) rides on top of [BDR-012](../identity-user-roles/012-identity-management.md) when that lands; this contract already leaves the seam open.

---

## References

- [BDR-001](../neo4j/001-single-neo4j-crd.md) — single workload CRD, satellites via `neo4jRef`
- [BDR-007](../neo4j/006-service-exposure-connectivity.md) — `service` + `ingress.rules` exposure (reused by Studio)
- [BDR-010](../neo4j/010-neo4j-features-catalog.md) — Enterprise-only gating + HTTP/HTTPS connector
- [BDR-012](../identity-user-roles/012-identity-management.md) — identity management (owns §4-b asset-DB/account creation)
- [BDR-014](../backup-restore/014-backup-restore.md) — satellite-CRD precedent (`neo4jRef`, immutable records, GitOps safety)
- [ADR-018](../../architecture/018-enterprise-studio.md) — deployment architecture (implements this BDR)
- [Enterprise Studio — deployments](https://neo4j.com/docs/enterprise-studio/current/deployments/) · [configuration](https://neo4j.com/docs/enterprise-studio/current/configuration/) · [prerequisites](https://neo4j.com/docs/enterprise-studio/current/prerequisites/)
