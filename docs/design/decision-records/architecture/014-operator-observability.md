# ADR-014 — Operator observability: logs, metrics, events, and exposure

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-08-04 |
| **Depends on** | [ADR-004](004-status-and-conditions.md) — status & conditions · [ADR-006](006-apply-and-idempotency.md) — apply `OperationResult` · [ADR-010](010-operator-deployment.md) — operator deployment · [BDR-010](../business/neo4j/010-neo4j-features-catalog.md) — Neo4j workload monitoring |
| **Constraints** | controller-runtime metrics registry & logr/zap; Prometheus Operator is optional in target clusters |

---

## Context

Observability of the **operator itself** (not the Neo4j workload) is currently minimal and
uneven. This ADR defines the contract for **logs**, **metrics**, **Kubernetes Events**, and
**how they are exposed** for Prometheus.

**Current state (baseline):**

- **Metrics are disabled by default.** `cmd/manager/main.go` sets `--metrics-bind-address=0`.
  Enabling a bind requires `--metrics-secure` and `filters.WithAuthenticationAndAuthorization`
  (NEO-017); plaintext `:8080` is refused.
- **Logging is production JSON by default** (`internal/logging`); `--zap-devel` for console;
  `--zap-log-level` for stderr verbosity; optional `--log-file` / `--log-file-level` tees a
  more verbose sink. Domains log apply create/update via `shared.Apply`; pipeline steps are named.
- **Domains are largely silent.** `internal/domain/{serverconfig,workload,connectivity}`
  apply operands via `shared.Apply` and **discard the `controllerutil.OperationResult`**
  ([ADR-006](006-apply-and-idempotency.md)); no create/update is logged. Only `formation` and `status/writer` log.
- **No Kubernetes Events** are emitted — no `EventRecorder` is wired into any reconciler.
- A `ServiceMonitor` exists, but for the **Neo4j workload** metrics port ([BDR-010](../business/neo4j/010-neo4j-features-catalog.md),
  `render/connectivity/servicemonitor.go`) — there is nothing for the operator pod.

**Forces:**

- Operators and SREs need to answer "is reconcile healthy, how long does it take, what
  changed, why is a CR not Ready" from logs + metrics, without reading operator source.
- Reference operators (CNPG, Strimzi, ECK) ship controller-runtime metrics + structured
  logs + Events as table stakes.
- Prometheus Operator may or may not be installed; exposure must degrade gracefully.
- Single-namespace scope for V1 ([BDR-003](../business/operator/003-operator-install-scope.md)) keeps RBAC for scraping simple.

---

## Analysis

Three concerns, decided together: **logs**, **metrics** (+ exposure), and **events**.

### A. Logging

#### A1 — Keep dev zap, ad-hoc logs (status quo)

| Advantages | Disadvantages |
|------------|---------------|
| Zero work | Not machine-parseable; no levels; silent domains; no correlation |

#### A2 — Production structured logging + per-domain convention (chosen)

Production JSON encoder by default (dev console via flag), a wired `--zap-log-level`, a named
logger per domain (`log.FromContext(ctx).WithName("serverconfig")`), and a fixed level policy.

| Level | Use | Examples |
|-------|-----|----------|
| `Error` | reconcile step failed | apply error, Bolt formation failure |
| `Info` (V0) | state-changing action | operand **created/updated** (`OperationResult`), rolling restart triggered, ENABLE SERVER, phase transition |
| `Debug` (V1) | per-reconcile detail | no-op applies, checksum values, requeue reasons |

All logs carry `reconcileID`, CR `namespace`/`name` (controller-runtime injects these into
the context logger).

| Advantages | Disadvantages |
|------------|---------------|
| Parseable, filterable, correlated | Requires touching every domain to add the create/update log |
| Directly closes the `OperationResult` gap | Log-level discipline must be reviewed |

### B. Metrics

#### B1 — controller-runtime built-ins only

Expose the default registry: `controller_runtime_reconcile_total{result}`,
`controller_runtime_reconcile_errors_total`, `controller_runtime_reconcile_time_seconds`,
`workqueue_*`, `rest_client_request_*`, `leader_election_*`.

| Advantages | Disadvantages |
|------------|---------------|
| Free, standard dashboards exist | No domain-level or business signal (config change, formation, drift) |

#### B2 — Built-ins + a small custom operator metrics package (chosen)

Add `internal/metrics` registering to controller-runtime's global registry, with a focused
custom set on top of the built-ins:

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `neo4j_operator_reconcile_step_duration_seconds` | histogram | `domain` | per-domain step latency |
| `neo4j_operator_reconcile_step_errors_total` | counter | `domain` | per-domain step failures |
| `neo4j_operator_operand_apply_total` | counter | `kind`,`result` | create/update/none from `OperationResult` |
| `neo4j_operator_config_change_total` | counter | — | config-driven rolling restarts (NEO-2-010) |
| `neo4j_operator_formation_action_total` | counter | `action`,`result` | ENABLE SERVER / drain (ADR-007) |
| `neo4j_operator_cr_ready` | gauge | `namespace`,`name` | 1 when CR Ready, else 0 (mirrors [ADR-004](004-status-and-conditions.md)) |
| `neo4j_operator_drift_corrected_total` | counter | `kind` | operator-owned drift corrections ([ADR-006](006-apply-and-idempotency.md)) |

| Advantages | Disadvantages |
|------------|---------------|
| Business-level visibility aligned with domains/ADRs | Must maintain a metrics package + avoid cardinality blowups |
| Reuses the built-in registry & scrape path | Per-CR gauge cardinality bounded by single-namespace scope |

### C. Exposure

#### C1 — Plain HTTP `:8080/metrics`

Insecure; fine for dev/`kind`, not for shared clusters.

#### C2 — kube-rbac-proxy sidecar (legacy kubebuilder)

Extra container + image to maintain; superseded upstream.

#### C3 — controller-runtime built-in authn/authz filter + optional ServiceMonitor (chosen)

Serve secure metrics on `:8443` via `metricsserver.Options{SecureServing: true, FilterProvider: filters.WithAuthenticationAndAuthorization}`; ship an **operator `ServiceMonitor`** (+ metrics
`Service` and scrape RBAC) gated by a Helm value / flag so clusters without Prometheus
Operator are unaffected. Plain `:8080` is refused (NEO-017).

| Advantages | Disadvantages |
|------------|---------------|
| No sidecar; TLS + SubjectAccessReview built in | ServiceMonitor needs Prometheus Operator CRDs (made optional) |
| Matches current kubebuilder scaffolding | Requires small scrape RBAC (ClusterRole `nonResourceURLs: /metrics`) |

### D. Events

Wire `mgr.GetEventRecorderFor("neo4j-operator")` into the reconciler and emit user-facing
Events on the CR for milestones: `Installed`, `ConfigChanged`/`RollingRestart`,
`ScaledOut`/`ServerEnabled`, `Failed`. Complements conditions ([ADR-004](004-status-and-conditions.md)) and is visible in
`kubectl describe neo4j`.

---

## Comparison

| Concern | Chosen | Rejected |
|---------|--------|----------|
| Logging | A2 — structured + per-domain + level policy | A1 status quo |
| Metrics | B2 — built-ins + custom package | B1 built-ins only |
| Exposure | C3 — built-in authn/authz + optional ServiceMonitor | C1 plain, C2 kube-rbac-proxy |
| Events | D — `EventRecorder` on the CR | none |

---

## Decision

We will implement operator observability as **A2 + B2 + C3 + D**.

### Logs

- Default to **production JSON** zap; `--zap-devel` for console; wire `--zap-log-level`.
  Optional `--log-file` tees logs with `--log-file-level` (stderr stays at `--zap-log-level`).
- Each domain uses a **named context logger** (`WithName("<domain>")`); level policy per the
  A2 table. `shared.Apply` logs create/update at `Info` and no-ops at `Debug` (this also
  resolves the [ADR-006](006-apply-and-idempotency.md) discard gap at the shared layer).
- Condition **reasons** are catalogued as a test oracle:
  `src/internal/status/oracle.go` ↔ [error-overview.md](../../../03-user-documentation/reference/error-overview.md).

### Metrics

- Add `internal/metrics` registering the custom set above via
  `sigs.k8s.io/controller-runtime/pkg/metrics.Registry`.
- Instrument the pipeline: step duration/errors per domain (wrap the step runner), operand
  apply results (from `OperationResult`), config-change and formation counters, `cr_ready`
  gauge written next to status.

```go
// internal/metrics/metrics.go (sketch)
var OperandApply = prometheus.NewCounterVec(
    prometheus.CounterOpts{Name: "neo4j_operator_operand_apply_total"},
    []string{"kind", "result"},
)
func init() { metrics.Registry.MustRegister(OperandApply /*, ... */) }
```

### Exposure

- Serve secure metrics on `:8443` with the built-in authn/authz filter (Helm `metrics.enabled`).
  Default bind remains `0` (off). Plain `:8080` is refused (NEO-017).
- Ship an **optional** operator `ServiceMonitor` + metrics `Service` + scrape RBAC, gated by
  a Helm value (`metrics.serviceMonitor.enabled`, default off). No hard dependency on the
  Prometheus Operator CRDs.

### Events

- Emit CR Events via `EventRecorder` for the milestones in section D; reasons are stable,
  PascalCase, and mirror condition reasons where applicable.

### Scope

- V1: the metrics catalog above + Events for install/config-change/scale/failed. Tracing
  (OpenTelemetry) is **out of scope** — revisit post-V1 in an amendment.

---

## Consequences

### Positive

- "Is reconcile healthy / what changed / why not Ready" answerable from logs + metrics + Events.
- Closes the silent-apply gap ([ADR-006](006-apply-and-idempotency.md) `OperationResult`) at the shared layer, once, for all domains.
- Standard controller-runtime scrape path; dashboards/alerts reuse known metric names.

### Negative

- Every domain gains logging/metrics touchpoints — more code and review surface.
- Optional ServiceMonitor adds a Helm/RBAC path to maintain.

### Neutral

- Per-CR `cr_ready` cardinality is bounded by single-namespace scope ([BDR-003](../business/operator/003-operator-install-scope.md)); revisit if
  multi-namespace/cluster-wide scope lands.
- Tracing deferred; slot reserved for a future amendment.

---

## References

- `src/cmd/manager/main.go` — current metrics/logging setup (metrics disabled, dev zap)
- `src/internal/domain/shared/apply.go` — `OperationResult` currently discarded
- `src/internal/render/connectivity/servicemonitor.go` — workload ServiceMonitor pattern (BDR-010)
- [ADR-004](004-status-and-conditions.md) · [ADR-006](006-apply-and-idempotency.md) · [ADR-010](010-operator-deployment.md) · [BDR-010](../business/neo4j/010-neo4j-features-catalog.md)
- controller-runtime — `pkg/metrics`, `pkg/metrics/server`, `pkg/metrics/filters`
- Kubebuilder book — Metrics & ServiceMonitor
