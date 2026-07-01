# Operator software architecture — decision backlog

Living checklist for **phase 2** (implementation architecture). Each row should end as **ADR-00x** (accepted) or explicitly **deferred** with reason.

Legend: `✓` decided · `○` draft exists · `·` open · `—` N/A V1

---

## A — Foundations

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| A-00 | Operator implementation language (Go vs Java) | ○ ADR-011 | K-01 | Strimzi = patterns only |
| A-01 | Validation layering (CEL / webhook / mutating / reconciler) | ✓ ADR-001 | BDR-001 | Ownership table in validation.md |
| A-02 | Package layering (`render` / `domain` / `controller`) | ○ layer.md | ADR-001 | → ADR-002 |
| A-03 | Import boundaries & forbidden cycles | · | ADR-002 | enforce in linter or ARCH doc |
| A-04 | One reconciler per CRD vs unified | · | BDR-001 | Neo4j + satellite CRDs |
| A-05 | `api/v1beta1` type organisation (`common_types`, embed structs) | · | BDRs | Kubebuilder markers |
| A-06 | Codegen: deepcopy, CRD manifests, RBAC, webhook config | · | A-05 | make targets |

---

## B — Reconcile pipeline

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| B-01 | **Global step order** for `Neo4j` reconciler | · | A-02, BDR-005..008 | e.g. persistence → trust → serverconfig → workload → connectivity → status |
| B-02 | Mode branching (Standalone vs Cluster) — where | · | BDR-002 | `domain/workload` vs scattered |
| B-03 | Short-circuit on validation / terminal errors | · | A-01 | fail-fast vs best-effort status |
| B-04 | Requeue: fixed interval vs exponential backoff | · | — | controller-runtime defaults vs custom |
| B-05 | Concurrent reconciles per CR (`MaxConcurrentReconciles`) | · | — | Neo4j reconcile is heavy |
| B-06 | Watch sources & predicates (owned vs watched Secrets) | · | BDR-006 | trust Secret rotation |
| B-07 | Finalizers: which resources, deletion order | · | BDR-005, B-01 | PVC retain policy |
| B-08 | Pause / maintenance annotation contract | · | BDR-002 | offline maintenance mode |

---

## C — Render layer (pure builders)

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| C-01 | Builder input struct (`RenderContext`) vs raw CR | · | A-02 | derived names, labels |
| C-02 | Naming conventions (SS, SVC, CM, Secret suffixes) | · | BDR-009 | pool ordinal in name |
| C-03 | Label / annotation contract (`app.kubernetes.io/*`, custom) | · | — | selector stability |
| C-04 | Owner references & controllerRef on all children | · | — | garbage collection |
| C-05 | ConfigMap / Secret key layout | · | BDR-008 | neo4j.conf, apoc.conf, jvm.options |
| C-06 | Volume claim templates vs existing PVC binding | · | BDR-005 | Dynamic vs Existing |
| C-07 | Pod template: probes, resources, securityContext defaults | · | PRD NFR | |
| C-08 | Init containers vs built-in entrypoint | · | Helm parity | |
| C-09 | Ingress / reverseProxy object shape | · | BDR-007 | V1.1+ |

---

## D — Domain layer (apply + business logic)

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| D-01 | Apply strategy: SSA vs merge patch vs recreate | · | — | SS spec immutables |
| D-02 | Diff / three-way merge for ConfigMaps | · | C-05 | avoid clobber user keys |
| D-03 | Idempotent create-or-update helper | · | D-01 | shared `domain/shared` |
| D-04 | Formation: Bolt admin API sequence | · | BDR-002 | enable servers, discover |
| D-05 | Cluster quorum / readiness gating | · | D-04 | before marking Ready |
| D-06 | Scale-out: ordinal join workflow | · | BDR-009 | |
| D-07 | Scale-in: decommission + PVC policy | · | BDR-009 | webhook vs reconciler |
| D-08 | Version upgrade: image bump strategy | · | V1 scope | rolling SS |
| D-09 | TLS reload vs pod restart on cert change | · | BDR-006 | `trust.reload` |
| D-10 | Plugin install: image vs sidecar vs download | · | BDR-004 | |
| D-11 | Backup / restore Job orchestration | · | post-V1 | separate controllers |
| D-12 | Identity reconcile order (Role → Grant → User) | · | BDR-012 | post-V1 |

---

## E — Neo4j client (`internal/neo4j`)

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| E-01 | Driver choice (official neo4j-go-driver) | · | — | |
| E-02 | Connection routing: bolt vs routing for cluster | · | BDR-002 | |
| E-03 | Auth: password Secret rotation | · | — | |
| E-04 | TLS to Neo4j: trust store from projected certs | · | BDR-006 | |
| E-05 | Timeouts, retries, circuit breaker | · | — | |
| E-06 | Which admin procedures / Cypher are allowed | · | — | allowlist |
| E-07 | Health check vs `dbms.cluster.*` queries | · | D-05 | |

---

## F — Status & conditions

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| F-01 | Condition types & precedence (`Ready`, `Formation`, …) | · | BDR-002 | BDR defines semantics |
| F-02 | Pool-level sub-status structure | · | BDR-009 | |
| F-03 | `observedGeneration` / generation sync | · | — | |
| F-04 | Warning vs Error conditions (reconciler-only rules) | · | ADR-001 | |
| F-05 | Event recorder messages (user-facing hints) | · | — | |
| F-06 | Status patch conflict handling | · | — | retry on conflict |

---

## G — Admission & webhooks

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| G-01 | Mutating webhook: defaults list | · | ADR-001 | normalisation only |
| G-02 | Validating webhook: external lookups | · | ADR-001 | Secret, StorageClass |
| G-03 | Webhook failure policy (`Fail` vs `Ignore`) | · | — | prefer Fail for validate |
| G-04 | Webhook timeout budget | · | — | |
| G-05 | cert-manager for webhook certs vs OLM | · | BDR-003 | |

---

## H — Operator runtime & ops

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| H-01 | Leader election (single active manager) | · | BDR-003 | |
| H-02 | Namespace-scoped cache vs cluster-wide | · | BDR-003 | V1 single-ns |
| H-03 | RBAC: minimal rules per controller | · | — | kubebuilder `+kubebuilder:rbac` |
| H-04 | Metrics: reconcile duration, queue depth | · | — | |
| H-05 | Structured logging (logr keys, CR namespace/name) | · | — | |
| H-06 | Graceful shutdown: in-flight reconcile drain | · | — | |
| H-07 | Operator version / build info in status or logs | · | — | |
| H-08 | Feature gates env vars (internal) | · | V1 scope | |

---

## I — Testing

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| I-01 | Render golden file layout (`testdata/`) | · | C-01 | |
| I-02 | envtest suite scope per domain package | · | A-02 | |
| I-03 | Webhook tests in envtest vs dedicated suite | · | ADR-001 | |
| I-04 | kind / e2e tier: which scenarios are P0 | · | `13-v1-scope-lock` | |
| I-05 | Bolt integration tests: real Neo4j container | · | E-01 | testcontainers? |
| I-06 | CRD validation tests: CEL unit vs API server | · | ADR-001 | |
| I-07 | Upgrade / migration test fixtures | · | — | |

---

## J — Delivery & migration

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| J-01 | Operator Helm chart layout | · | BDR-003 | |
| J-02 | OLM bundle vs raw Helm only V1 | · | PRD | |
| J-03 | Offline migration CLI (`cmd/migrate`) | · | BDR-001 | cluster/standalone → Neo4j |
| J-04 | CRD upgrade / conversion webhook | · | api-versioning | post-beta |
| J-05 | Helm values → Neo4j CR translator tool | · | helm-fields | |

---

## K — Operator benchmark & ecosystem (study before locking internals)

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| K-01 | Tier-1 operator survey (CNPG, Strimzi, ECK, MongoDB) | · | — | `operator-benchmark/operators/*.md` |
| K-02 | Code layout synthesis across operators | ✓ | K-01 | `synthesis.md` + ADR-011 language decision |
| K-03 | Neo4j Helm vs operator responsibility split | · | helm-fields | not an operator — parity doc |
| K-04 | neo4j-cloud managed architecture alignment | · | pg.md | what not to rebuild |
| K-05 | cert-manager / ESO integration patterns | · | BDR-006 | ownership of Secrets |
| K-06 | Prometheus Operator / ServiceMonitor pattern | · | BDR-010 | V2+ monitoring |

Skill: **operator-benchmark-analyst**

---

## L — RBAC & Kubernetes security

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| L-01 | Operator SA: Role vs ClusterRole verb matrix | · | BDR-003, K-01 | → ADR-013 |
| L-02 | Aggregate ClusterRoles for admin vs read-only | · | L-01 | `view`, `edit`, `admin` CRD roles |
| L-03 | Workload SA per Neo4j deployment | · | BDR-002 | discovery RBAC, bolt client |
| L-04 | Auto-provision RBAC in watched namespaces | · | BDR-003 | MongoDB manual SA pattern |
| L-05 | `WATCH_NAMESPACE` + informer/cache config | · | BDR-003, K-01 Strimzi | → ADR-014 |
| L-06 | Restricted / single-namespace install profile | · | K-01 ECK | Helm values overlay |
| L-07 | Pod Security Standards vs OpenShift SCC | · | dependencies.md | → ADR-015 |
| L-08 | securityContext defaults (runAsNonRoot, capabilities) | · | PRD NFR | |
| L-09 | NetworkPolicy generation (opt-in) | · | 20-operator-proposal | V2+ |
| L-10 | Webhook RBAC + TLS cert RBAC | · | G-05 | cert-manager or OLM |
| L-11 | End-user RBAC: who can patch `Neo4j` spec | · | — | docs / cluster admin guide |
| L-12 | Threat model: operator compromise blast radius | · | BDR-003 | feed `security.md` |

---

## M — Cloud & platform dependencies

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| M-01 | Portable core vs platform-specific CRD fields | · | dependencies.md | annotations in spec |
| M-02 | AWS IRSA for backup Jobs (V2+) | · | K-01 Percona/CNPG | → ADR-016 |
| M-03 | GKE Workload Identity | · | M-02 | |
| M-04 | Azure Workload Identity | · | M-02 | |
| M-05 | OpenShift / ROSA identity (SCC + cloud) | · | dependencies.md | |
| M-06 | Object store SDK in operator vs Job only | · | M-02 | avoid SDK in hot path |
| M-07 | StorageClass / CSI capability detection | · | BDR-005 | webhook or doc-only |
| M-08 | LoadBalancer annotation passthrough | · | BDR-007 | no hard-coded cloud |
| M-09 | cert-manager issuer per cloud (DNS01) | · | BDR-006 | |
| M-10 | Platform test matrix (kind / AKS / EKS / GKE / OCP) | · | dependencies.md | EST-TST-020 |
| M-11 | External Secrets vs inline Secret refs | · | K-05 | V2+ |

---

## N — Engineering quality & dependencies

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| N-01 | Go version policy & upgrade cadence | · | K-01 | → ADR-012 |
| N-02 | controller-runtime / k8s.io pin strategy | · | N-01 | align with OCP/K8s skew |
| N-03 | Allowed third-party deps (neo4j-go-driver, …) | · | N-01 | license + vuln surface |
| N-04 | Vendoring vs modules in release builds | · | — | |
| N-05 | golangci-lint rule set | · | K-01 | import boundaries, cyclo |
| N-06 | govulncheck / Dependabot / Renovate | · | — | → ADR-018 |
| N-07 | Coverage targets per package layer | · | I-* | render high, controller thin |
| N-08 | CODEOWNERS & review policy | · | — | |
| N-09 | API deprecation policy (beta → GA) | · | api-versioning | |
| N-10 | Contributor / dev environment (tilt, make) | · | — | |

---

## O — Observability & operations

| ID | Topic | ADR? | Depends on | Notes |
|----|-------|------|------------|-------|
| O-01 | Operator metrics (reconcile, workqueue) | · | H-04 | Prometheus |
| O-02 | Structured logging contract | · | H-05 | |
| O-03 | Tracing (OpenTelemetry) — opt-in | · | — | post-V1 |
| O-04 | Kubernetes Events for user-visible hints | · | F-05 | |
| O-05 | Operator health / readiness probes | · | J-01 | |
| O-06 | `kubectl neo4j` plugin scope (V2) | · | 20-operator-proposal | |
| O-07 | Runbooks: webhook failure, stuck finalizer | · | — | ops docs |

---

## ADR tracks (multi-track — not a single linear sequence)

Benchmark **before** locking ADR-011+. Tracks can proceed in parallel after synthesis.

### Track 0 — Evidence (phase 2a)

| Step | Output |
|------|--------|
| K-01 | `operator-benchmark/operators/{cnpg,strimzi,eck,mongodb}.md` |
| Synthesis | `operator-benchmark/synthesis.md` |

### Track 1 — Cross-cutting (from benchmark)

| ADR | Topic | Backlog |
|-----|-------|---------|
| ADR-011 | Operator implementation language (Go) | A-00, K-01, synthesis.md |
| ADR-012 | Go version & dependency policy | N-01..N-04 |
| ADR-013 | Operator & workload RBAC | L-01..L-04, L-11 |
| ADR-014 | Watch scope & cache | L-05, H-02 |
| ADR-015 | Pod security & platform profiles | L-06..L-09 |
| ADR-016 | Cloud identity for workloads | M-02..M-06 |
| ADR-017 | Platform-specific wiring strategy | M-01, M-08 |
| ADR-018 | CI quality gates | N-05..N-08 |
| ADR-019 | Release & compatibility matrix | N-09, J-02 |
| ADR-020 | Testing pyramid | I-01..I-07, K-01 |

### Track 2 — Internal implementation

| ADR | Topic | Backlog |
|-----|-------|---------|
| ADR-001 ✓ | Validation layering | A-01 |
| ADR-002 | Package layering | A-02, A-03 |
| ADR-003 | Reconcile pipeline order | B-01, B-02 |
| ADR-004 | Status & conditions | F-* |
| ADR-005 | Render conventions | C-01..C-04 |
| ADR-006 | Apply & idempotency | D-01, D-03 |
| ADR-007 | Formation & Bolt | D-04, D-05, E-* |
| ADR-008 | Finalizers & deletion | B-06, B-07 |
| ADR-009 | Watches & predicates | B-06 |
| ADR-010 | Operator deployment & HA | H-01, J-01 |

**Dependency:** ADR-011 (language) and benchmark synthesis should be **proposed** before ADR-002 is **accepted** — internal layout must not contradict benchmark consensus without explicit rationale.

---

## Cross-reference map

| BDR | Likely ADRs triggered |
|-----|----------------------|
| BDR-001 | A-04, J-03 |
| BDR-002 | B-02, D-04, D-05, F-01 |
| BDR-005 | C-06, B-07, D-07 |
| BDR-006 | C-05, D-09, E-04, G-02 |
| BDR-007 | C-09, B-06 |
| BDR-008 | C-05, D-02 |
| BDR-009 | C-02, D-06, D-07, F-02 |
| BDR-003 | L-*, H-02, ADR-013, ADR-014 |
| BDR-012 | A-04, D-12 |
