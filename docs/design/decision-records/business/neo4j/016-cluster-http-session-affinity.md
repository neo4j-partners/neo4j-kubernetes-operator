# BDR-016 — Cluster HTTP session affinity for the client Service

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-10-22 |
| **Depends on** | [BDR-007](006-service-exposure-connectivity.md) — service exposure & connectivity model · [BDR-002](002-neo4j-crd-topology.md) — topology pools · [BDR-015](../studio/015-enterprise-studio.md) — `Neo4jStudio` (first consumer hit) · [ADR-018](../../architecture/018-enterprise-studio.md) — Studio execution |
| **Constraints** | Neo4j Operations Manual — [Neo4j clusters and session affinity](https://neo4j.com/docs/operations-manual/current/kubernetes/neo4j-clusters-session-affinity/) |

---

## Context

The client `Service` the operator renders for a `Neo4j` (`ClientService`, named `<neo4j>`) selects **all**
members of a cluster and is created with `sessionAffinity: None`, so every HTTP request is routed
independently to any member.

Neo4j's **HTTP Query API explicit transactions** span multiple HTTP requests (begin → run → commit),
and transaction state lives on the member that opened it. With `None`, a follow-up request lands on a
different member and fails:

```
Neo.ClientError.Transaction.TransactionNotFound
Transaction with Id: "…" was not found … or the routing header 'neo4j-cluster-affinity' was not provided.
```

This affects **every** HTTP client doing explicit transactions against a cluster, not just Studio —
Enterprise Studio in `ReverseProxy` mode is simply the first consumer we hit (see "Discovery"). Bolt
clients (`neo4j://`/`bolt://`) are unaffected: the Bolt driver holds one connection to one member for
the transaction's lifetime.

Neo4j's own guidance is unambiguous: configure **`sessionAffinity: ClientIP`** on the Kubernetes
Service used to reach the cluster
([Operations Manual](https://neo4j.com/docs/operations-manual/current/kubernetes/neo4j-clusters-session-affinity/)).

### Discovery (how we got here)

A `Neo4jStudio` served a cluster `cl` with `mode: ReverseProxy`. Studio deployed healthy but queries
failed with the error above. Walking the operator:

- `domain/studio.ResolveDeployments` resolves an in-cluster `neo4jRef` + `ReverseProxy` to
  `http://<name>.<ns>.svc:7474` — the cluster's round-robin client Service, no topology branch.
- The only affinity the operator enforced (`render/studio.NeedsSessionAffinity` → `ClientIP` on
  **Studio's own** Service) covers the **browser→Studio** hop, not the **Studio→cluster** hop where
  the transaction breaks.
- Live proof: `kubectl get svc cl -o jsonpath='{.spec.sessionAffinity}'` → `None`, 6 endpoints.
- Manual `kubectl patch svc cl` to `sessionAffinity: ClientIP` **fixed it immediately** (connect +
  `SHOW SERVERS` worked), and the patch survives reconciles because
  `domain/connectivity.preserveServiceServerFields` carries forward a live `SessionAffinity` whenever
  the render leaves it unset.

### Correction to ADR-018 / BDR-015

ADR-018 ("Forces") and [BDR-015 §2](../studio/015-enterprise-studio.md) state the cluster-`ReverseProxy`
prerequisite is satisfied by making **Studio's front door** sticky. That is the wrong leg — front-door
affinity pins browser→Studio and does nothing for Studio→cluster. The real prerequisite is affinity on
the **served cluster's** client Service, which this BDR owns. Those two documents are updated to point
here.

### Forces

- HTTP explicit transactions on a cluster are **incorrect** without member affinity — a data-path bug,
  not a tuning knob.
- `ClientIP` is Layer-4 (source-IP based). When the HTTP client is a **proxy** (Studio in
  `ReverseProxy`), all end users share the proxy's pod IP, so `ClientIP` pins *all* proxied traffic to
  **one** member — correct, but no load spread across members for that proxy's traffic. Acceptable for
  an admin/exploration UI; the Layer-7 cookie alternative (`neo4j-cluster-affinity`) is the only way to
  both spread load and keep affinity, at the cost of an L7 proxy component (rejected — see below).
- Defaulting a cluster to `ClientIP` changes HTTP request distribution for existing cluster users
  (single-request HTTP no longer round-robins across members). Bolt is unchanged.

---

## Decision

1. **Add `sessionAffinity` to `spec.connectivity.service`** (`ConnectivityServiceSpec`):
   `None | ClientIP`, with an optional `clientIP.timeoutSeconds` (default `10800`, matching the default
   `dbms.transaction.timeout`). Renders onto the client `Service.spec.sessionAffinity[Config]`.
2. **Default by topology** when the field is unset: `ClientIP` for `Topology.Mode: Cluster`
   (`render.IsClusterMode`), `None` for `Standalone`. This makes clusters correct-by-default per the
   Operations Manual while letting a user opt back to `None`.
3. **Studio is unchanged.** It keeps resolving `neo4jRef` to the same client Service URI (ADR-018 — no
   re-derivation). Because the served cluster's Service is now affinity-safe, `ReverseProxy` against a
   cluster works with no Studio-side logic. The misplaced front-door-affinity note in ADR-018/BDR-015 is
   corrected; `render/studio.NeedsSessionAffinity` (browser→Studio stickiness) stays as-is.

### Rejected alternatives

- **L7 cookie-sticky proxy** honoring `neo4j-cluster-affinity` (nginx/envoy per served cluster): the
  only option that also spreads load, but a new runtime component + config surface per cluster —
  contradicts ADR-018's "no new runtime dependency in the manager". Revisit only if Studio→cluster load
  distribution becomes a real requirement.
- **Fix it inside Studio** (Studio renders its own affinity Service over the cluster's pods): duplicates
  a `Neo4j` connectivity concern into the satellite, helps no other HTTP consumer, and violates
  ADR-018's resolve-don't-re-derive rule.
- **Pin to a single per-member Service** (`<name>-N`): no new field, but hard-fails if that ordinal's
  pod is down. Fragile.

---

## Consequences

- HTTP explicit transactions against a clustered `Neo4j` work out of the box; the `TransactionNotFound`
  footgun is removed, matching the "enforce prerequisites, don't document footguns" principle.
- `+`: one small field + a topology default; no new component, no new image, Bolt path untouched,
  Studio untouched.
- `−`: HTTP traffic to a cluster no longer round-robins per request by default (it pins per client IP).
  Documented; opt out with `spec.connectivity.service.sessionAffinity: None`.
- Upgrade note: existing cluster CRs gain `ClientIP` on next reconcile. Because the client Service stays
  `ClusterIP`-typed and only `sessionAffinity` changes, this is a non-disruptive in-place Service update.

### Implementation touchpoints (for the follow-up ADR/PR)

- `api/v1/common_types.go` — `ConnectivityServiceSpec.SessionAffinity` (+ `clientIP.timeoutSeconds`), CRD regen.
- `render/connectivity.ClientService` — set `SessionAffinity`/`SessionAffinityConfig` from spec, defaulting by `IsClusterMode`.
- `domain/connectivity.preserveServiceServerFields` — already compatible (carries forward when unset); ensure the render now sets it so the default is authoritative.
- Docs: correct ADR-018 "Forces" + BDR-015 §2 to reference this BDR; add a note to `examples/studio/README.md` (cluster + ReverseProxy just works).
- Tests: `ClientService` cluster-default vs standalone-default vs explicit `None` override.
