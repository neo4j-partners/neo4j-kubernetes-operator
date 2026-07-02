# ADR-012 — Testing strategy: `src/` development tests vs `tests/` e2e matrix

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-07-02 |
| **Depends on** | [ADR-002](002-package-layering.md) · [ADR-001](001-crd-validation-process.md) · [ADR-011](011-implementation-language.md) |
| **Constraints** | Backlog I-01..I-07; reference e2e harness in `tests/old/` |

---

## Context

The Neo4j operator reconciles many Kubernetes object kinds, optional Bolt admin calls, and cloud-specific wiring (storage classes, pod identity, load balancers). Not every regression is visible at the same cost or speed: some are caught by fast Go tests; others only appear on a real cluster across EKS, GKE, AKS, or OpenShift.

Development is largely **LLM-assisted**: agents need a fast, local feedback loop without provisioning cloud clusters on every edit. Integration toward `main` still requires confidence that behaviour holds on real infrastructure.

We do **not** mandate Test-Driven Development (red → green → refactor). Tests are required, but may be written alongside or after implementation. What we mandate is a **clear split** between two test estates and **when each runs in CI**.

**Forces:**

- [ADR-002](002-package-layering.md) — pure `internal/render` builders and thin controllers are designed for fast unit tests colocated with source.
- [ADR-001](001-crd-validation-process.md) — CEL and webhook rules need dedicated test suites.
- [ADR-011](011-implementation-language.md) — Go `testing` + envtest is the default kubebuilder stack.
- `tests/old/` already implements a **layered e2e framework** (actions → blocks → scenarios) with cloud matrix tracking in `brief-results.csv`.
- E2e is slow and infra-dependent — unsuitable as the primary loop during feature development.

**What breaks if wrong:** every change waits on cloud e2e; or conversely, PRs merge on unit tests alone while cloud-specific regressions slip through; LLM agents cannot iterate quickly on reconcile logic.

---

## Analysis

### Option A — Single test estate (everything in `tests/` e2e)

All verification through scenario pipelines on real clusters.

| Advantages | Disadvantages |
|------------|---------------|
| High end-state confidence | Too slow for development and LLM iteration |
| One vocabulary | Cannot cheaply test render/domain in isolation |
| | Expensive matrix on every commit |

### Option B — Two estates: `src/` (fast) + `tests/` (e2e matrix) (chosen)

Development-time automated tests live with operator source under `src/`. End-to-end conformance lives in `tests/`. CI runs them in sequence on feature → `main` PRs.

| Advantages | Disadvantages |
|------------|---------------|
| Fast loop for humans and LLM agents | Two harnesses to maintain (Go + Python/shell runner) |
| E2e matrix reused from `tests/old` | Contributors must know which estate to extend |
| Clear PR gate: fast tests first, e2e second | E2e still infra-bound |

### Option C — `src/` tests only (no structured e2e)

Unit and envtest only; manual cluster checks for release.

| Advantages | Disadvantages |
|------------|---------------|
| Cheapest CI | Cloud coverage anecdotal; no matrix tracking |
| Simple layout | Regressions on install, Bolt, cloud prep found late |

---

## Comparison

| Criterion | A e2e-only | B src + tests | C src-only |
|-----------|------------|---------------|------------|
| Dev / LLM feedback | Poor | **Best** | Good |
| Cloud matrix tracking | Yes | **Yes** | No |
| PR cost | High | **Tiered** | Low |
| ADR-002 testability fit | Weak | **Strong** | Strong |
| V1 fit | Slow | **Yes** | Risky |

---

## Decision

We will split automated verification into **two estates** with distinct purpose, location, and CI timing. TDD is **optional**; both estates still require tests for behaviour changes before merge to `main`.

### Overview

```
┌─────────────────────────────────────────────────────────────┐
│  Estate 1 — src/                                            │
│  Unit, integration, envtest, golden files                   │
│  When: every development iteration (local, LLM agents)      │
│  CI: first gate on PR (feature → main)                      │
└─────────────────────────────────────────────────────────────┘
                              │ must pass
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Estate 2 — tests/                                          │
│  E2e scenarios, cloud matrix, actions / blocks / runner     │
│  When: PR feature → main (after estate 1)                   │
│  CI: second gate; full matrix on schedule / release         │
└─────────────────────────────────────────────────────────────┘
```

### CI pipeline (feature branch → `main`)

```
PR opened / updated
    │
    ▼
[Gate 1] src/ — go test, lint, envtest          ← fast; blocks merge if red
    │
    ▼ (only if Gate 1 green)
[Gate 2] tests/ — e2e scenario subset on cluster ← slower; required for merge
    │
    ▼ (scheduled / release, not every PR)
[Gate 3] tests/ — full brief × cloud matrix
```

Gate 1 runs on every push to the PR. Gate 2 runs on the same PR once Gate 1 succeeds (or in parallel with a `needs:` dependency). Gate 3 is out of scope for day-to-day feature work.

### Estate 1 — Development tests (`src/`)

**Purpose:** Fast feedback while implementing reconcile logic, validation, and rendering. Primary loop for developers and LLM-assisted coding.

**Location:** Colocated with operator source under `src/` (Go convention: `*_test.go` next to production code, `testdata/` for fixtures and golden files).

| Tier | Scope | Tooling |
|------|-------|---------|
| **Unit** | `src/internal/render/*`, pure helpers, CEL rule fixtures | Go `testing`, golden files |
| **Integration** | Domain reconcile steps, webhooks, CRD apply | envtest, fake client |

**When they run:**

- Locally and in the IDE on every save / `go test ./src/...`
- By LLM agents after each code change during a session
- As **Gate 1** on pull requests targeting `main`

**Expectations:**

- Behaviour changes should include or update tests in `src/` before the PR is ready; order vs implementation is not prescribed (TDD not required).
- Tests must not depend on a live cloud cluster or external Neo4j license.
- Package layout follows [ADR-002](002-package-layering.md) so render and domain packages remain unit-testable.

Example (render golden):

```go
func TestStatefulSet_PrimaryPool(t *testing.T) {
    neo := minimalNeo4jCR() // testdata/fixtures/
    got := render.PrimaryStatefulSet(neo)
    golden.Assert(t, got, "primary_sts.yaml")
}
```

### Estate 2 — End-to-end conformance (`tests/`)

**Purpose:** Validate operator install, Neo4j workloads, Bolt connectivity, and cloud-specific wiring on real clusters. Track **scenario × cloud** coverage in a matrix.

**Location:** Top-level `tests/` directory (evolved from `tests/old/` reference). Not mixed into `src/`.

**When they run:**

- **Not** on every local edit or LLM iteration (too slow, needs cluster credentials).
- As **Gate 2** on pull requests feature → `main`, only after Gate 1 passes.
- Full brief matrix as **Gate 3** on a schedule or release gate (backlog I-04).

#### Layout — actions, blocks, scenarios

The conformance harness separates responsibilities (reference: `tests/old/doc/architecture.md`):

```
config/          ← campaign pins (operator + Neo4j versions), cloud context
fixtures/        ← versioned, parameterised Kubernetes manifests
actions/         ← atomic operations: run.sh + verify.sh
blocks/          ← reusable sequences with graph position metadata
scenarios/       ← chain of blocks or inline steps + cleanup
runner/          ← compose, pipeline manifest, execute
lib/             ← shared shell helpers (kubectl, Bolt, render)
results/         ← runs/<run-id>/, brief-results.csv
```

##### Actions

An **action** is one atomic operation with a single assertion boundary:

```
actions/deploy/standalone/
  run.sh      # apply fixture, create secrets if needed
  verify.sh   # pods Ready, CR status OK, no Error events
```

Conventions:

- `run.sh` exits `0` if the action was attempted.
- `verify.sh` holds the **business assertion**.
- The runner always invokes `run.sh` then `verify.sh`.

| Domain | Examples | Assertion |
|--------|----------|-----------|
| `operator/` | install, upgrade, CRD registration | Strict |
| `deploy/`, `assert/` | Neo4j workloads, Bolt connectivity | Strict |
| `cloud/<provider>/` | cert-manager, bucket, IRSA, LB checks | Strict or soft skip |
| `cloud/common/` | cross-cloud prep | Strict |
| `cleanup/` | teardown | Best-effort |

| Prefix | Meaning |
|--------|---------|
| `assert/` | System **must** reach a healthy state |
| `expect/` | System **must** stay blocked or emit expected failure signals (negative tests) |

##### Blocks

A **block** is a named, reusable sequence of actions with **position metadata** so scenarios compose safely:

```yaml
id: neo4j-install-standalone
layer: workload          # operator | workload | check | expect

position:
  requires: [operator-install-*]
  provides: [neo4j-ready]
  terminal: false
  outcome: success

steps:
  - deploy/standalone
  - assert/standalone-ready

cleanup:
  - cleanup/standalone
```

| `position` field | Role |
|------------------|------|
| `requires` | Prior block id must match (glob `*` supported) |
| `requires_provides` | Capabilities accumulated from earlier blocks |
| `provides` | Capabilities added on success |
| `excludes_provides` | Capabilities removed (fault injection) |
| `terminal` | No further blocks after this one |
| `outcome` | `success` or `failure` — `failure` implies terminal |

The runner (`runner/compose.py`) validates the chain, merges operator/neo4j config, flattens steps, and merges cleanup in **reverse chain order** (dependents first).

##### Scenarios

A **scenario** is the reporting unit: one `brief_ref`, one run directory, one cleanup scope. Preferred form uses `chain:`:

```yaml
name: helm-install-crd-registration-cluster
brief_ref: line-1,line-3,line-13
operator_install: helm-install

chain:
  - operator-install-helm
  - neo4j-install-standalone
  - check-bolt-cluster
```

Legacy inline `steps:` / `cleanup:` remains supported when the operator is pre-installed or for one-off pipelines.

Execution flow:

```
1. Load config/config.yaml           → operator + Neo4j version pins
2. Load config/active-cloud.yaml     → cloud context (from cloud-init)
3. Expand chain → flat steps (resolved-pipeline.yaml)
4. Create isolated namespace neo4j-test-<scenario>-<suffix>
5. Run steps[] in order — stop on first failure
6. On failure → collect diagnostics BEFORE cleanup
7. Run cleanup[] — log errors, do not block exit code aggregation
8. Update brief-results.csv on success
```

Each scenario is **self-contained** (deploy + assert + cleanup) so it can run in isolation on a shared cluster.

#### Cloud matrix logic

Conformance is tracked as **scenario × cloud**, not as a single “works on my cluster” run.

```
Phase 1 (manual)     Create EKS / GKE / AKS / OpenShift + kubectl context
Phase 2 (cloud-init) ./bin/cloud-init.sh <cloud>  →  config/active-cloud.yaml
Phase 3 (prep)       cloud/* actions in scenario steps (cert-manager, storage, identity)
Phase 4 (scenarios)  operator + workload pipelines
```

`config/clouds/` holds per-provider defaults. `active-cloud.yaml` records detected storage class, region, and cloud id for fixture rendering via `lib/render.sh`.

`brief.csv` lists planned test lines. Each scenario references lines via `brief_ref: line-N`.

`results/brief-results.csv` is the **conformance matrix**:

```csv
line,scenario,operator_version,neo4j_version,test,verified_eks,verified_gke,verified_aks,last_run,run_id
7,standalone-deployment,1.11.2,5.26,Standalone deployment,PASS,,,2026-06-11T14:30:22Z,20260611-143022-eks
```

- One successful run on EKS marks `verified_eks=PASS` for that line.
- Empty cells mean **not yet verified** on that cloud — visible gap, not implicit pass.
- Run id encodes cloud: `<timestamp>-<cloud>-<suffix>`.

Cloud-specific steps live under `actions/cloud/<provider>/`. The runner **skips** steps that do not apply to the active cloud (logged as `SKIP`).

### Repository placement

| Path | Estate | Role |
|------|--------|------|
| `src/**/**/*_test.go` | 1 | Unit + envtest; Gate 1 |
| `src/**/testdata/` | 1 | Golden files and CR fixtures |
| `tests/` | 2 | E2e harness; Gate 2 / Gate 3 |
| `tests/fixtures/` | 2 | Pinned manifests from operator examples / release bundles |
| `tests/results/` | 2 | Run artefacts and matrix CSV (gitignored except schema/docs) |
| `tests/old/` | — | Reference snapshot until harness is promoted |

---

## Consequences

### Positive

- LLM agents and developers iterate on `go test ./src/...` without cloud access.
- PR flow is explicit: fast tests block slow e2e; e2e blocks merge to `main`.
- Actions and blocks avoid duplicating install/deploy/assert sequences across 20+ brief lines.
- `brief-results.csv` makes cloud coverage auditable for release gates.

### Negative

- Two harnesses to learn (Go tests vs Python/shell runner).
- Gate 2 requires CI cluster infrastructure and secrets.
- Promoting `tests/old` into `tests/` needs a dedicated migration pass.

### Neutral

- TDD remains a valid personal workflow but is not a project requirement.
- Backlog I-01..I-07 refines golden layout, envtest scope, and P0 e2e subset without changing this split.
- Detailed CI job definitions (runners, secrets, P0 scenario list) are deferred to a follow-up ADR on quality gates (backlog N-05..N-08).

---

## References

- ADRs: [ADR-001](001-crd-validation-process.md), [ADR-002](002-package-layering.md), [ADR-011](011-implementation-language.md)
- Reference harness: `tests/old/doc/architecture.md`, `blocks.md`, `pipelines.md`, `brief-mapping.md`, `cloud-setup.md`
- Backlog: `.cursor/skills/operator-architecture-orchestrator/architecture-backlog.md` — section I (Testing), I-04 (P0 e2e tier)
