---
name: run-extended-suite
description: Run the extended integration suite (backup/restore, sharding, multi-node, MinIO) — the full `core || extended` Ginkgo tier that does NOT run per-PR — via the Extended workflow on CI or label-filtered Ginkgo locally on a Kind cluster.
---

# Run the extended integration suite

Every integration spec's top-level `Describe` carries `Label("core")` or
`Label("extended")` (e.g. `cluster_lifecycle_test.go` → `core`,
`backup_integration_test.go` / `property_sharding_test.go` → `extended`). The
**core** subset is the fast reconcile-contract lane that auto-runs per-PR on
both Neo4j tracks; the **extended** tier (backup/restore matrix, sharding,
split-brain, multi-node, MinIO/cloud) is heavier and is **not** triggered by an
ordinary PR. This skill runs that extended tier — on CI via the Extended
Integration Tests workflow, or locally with a label-filtered Ginkgo run.

## When to use

- You changed backup/restore, sharding, multi-node formation, split-brain, or
  any path the per-PR **core** lane (`.github/workflows/integration.yml`) does
  not exercise, and you want the heavy specs green before merge.
- You're prepping a release and need the extended suite GREEN on `main` (this is
  step 2 of the `cut-release` skill).
- You want to verify a specific Neo4j tag (LTS floor `5.26-enterprise`, or a
  newer CalVer than the pinned anchor) against the full matrix.

## How the tiers are wired (so you pick the right lane)

- **Core, per-PR**: `.github/workflows/integration.yml` ("Integration Tests")
  runs `--label-filter='core'` on a matrix of `5.26-enterprise` and the pinned
  CalVer (`2026.04-enterprise`), in parallel. Auto-triggered on PRs touching
  runtime paths.
- **Extended**: `.github/workflows/integration-tests.yml` ("Extended
  Integration Tests") runs nightly (`cron: '0 3 * * *'`), on
  `workflow_dispatch`, and on a PR when the **`run-extended`** label is applied
  (maintainer-only). Scope is **event-aware**: schedule/dispatch run the full
  `core || extended` suite; a label-gated PR run does `extended`-only (core is
  already covered by the core lane on that PR).
- Both lanes are **Kind-only** (cluster `neo4j-operator-test`), deploy the
  operator in prod mode to `neo4j-operator-system`, and run `--procs=1`
  (one shared Kind cluster + operator, so specs must run serially).

## Procedure

1. **CI — dispatch the Extended workflow and watch it to completion.** This is
   the canonical way to run the full tier:
   ```bash
   gh workflow run integration-tests.yml --ref main
   gh run list --workflow=integration-tests.yml --branch=main --limit=1
   gh run watch <run-id> --exit-status
   ```
   Defaults to the pinned anchor CalVer (`2026.04-enterprise`) with a 150-minute
   Ginkgo timeout. To verify the LTS floor or a different version/timeout:
   ```bash
   gh workflow run integration-tests.yml --ref main \
     -f neo4j-version=5.26-enterprise \
     -f timeout-minutes=150
   ```

2. **CI — or run extended against a PR by label** (maintainer-only; runs
   `extended`-only on the PR event):
   ```bash
   gh pr edit <pr-number> --add-label run-extended
   ```
   A new push to that PR cancels its in-flight extended run (per-PR
   `concurrency` + `cancel-in-progress`) — let a run finish before pushing again
   if you're waiting on the result.

3. **Local — bring up the Kind test cluster + operator once**, then run the
   label-filtered suite. `make test-integration` (no FOCUS) deploys the operator
   from `config/overlays/integration-test` and then runs Ginkgo:
   ```bash
   export NEO4J_VERSION=2026.04-enterprise   # or 5.26-enterprise for the LTS floor
   make test-integration                     # creates neo4j-operator-test, deploys, runs
   ```
   That target runs the whole suite. To run **only** the extended tier (or the
   full `core || extended` set) against the already-deployed cluster, drive the
   pinned Ginkgo binary directly (matches the workflow invocation):
   ```bash
   make ginkgo                               # downloads ./bin/ginkgo
   ./bin/ginkgo run -v --timeout=150m --procs=1 \
     --label-filter='extended' \
     ./test/integration/...
   # full tier: --label-filter='core || extended'
   ```

4. **Local — run a single heavy spec by name** while iterating (focus a
   description substring):
   ```bash
   make test-one TEST="Backup Integration"
   # e.g. "Property Sharding", "Multi-Node Cluster Formation", "Split-Brain"
   ```

5. **Property-sharding gating (local).** The sharded specs self-skip unless the
   Neo4j tag is CalVer **≥ 2025.12** — `isPropertyShardingCompatible()` in
   `test/integration/property_sharding_test.go` returns false otherwise, so the
   `Label("extended")` sharding specs (`property_sharding_test.go`,
   `property_sharding_ci_smoke_test.go`, etc.) skip silently on older tags:
   ```bash
   export NEO4J_VERSION=2025.12-enterprise
   make test-one TEST="Property Sharding"
   ```
   The richer sharded specs also need a real 4Gi/server + 1-core floor. To run
   the CI smoke spec (`property_sharding_ci_smoke_test.go`) on a small cluster,
   the operator must be deployed with `NEO4J_SHARDING_RELAX_MEMORY_MIN=true`,
   which downgrades that hard reject — set **only** via
   `config/overlays/integration-test/` (the CI deploy overlay), never in a
   production overlay. `make test-integration` uses that overlay, so the smoke
   spec works; a plain `make deploy-dev-local` does not relax the floor.

## Guardrails

- **Kind only.** Both lanes target the `neo4j-operator-test` Kind cluster and
  deploy to `neo4j-operator-system`. No minikube/k3s, no cluster-external
  operator (Invariants 1 & 2; DNS resolution fails for an out-of-cluster
  operator — never `make dev-run`).
- **`--procs=1` always.** The suite shares one Kind cluster + one operator;
  parallel procs would race on it. The workflows make `--procs=1` explicit as a
  guard against an accidental `-p`; keep it when running Ginkgo by hand.
- **Label every new spec.** A spec file whose top-level `Describe` carries
  neither `Label("core")` nor `Label("extended")` runs in *neither* lane and is
  silently never executed. New extended-tier specs must be `Label("extended")`.
- **`NEO4J_SHARDING_RELAX_MEMORY_MIN` is CI-only.** It exists solely so the
  sharded smoke spec fits a GitHub-hosted runner; it lives in
  `config/overlays/integration-test/kustomization.yaml`. Do not add it to a prod
  overlay — the 4Gi/1-core floor prevents silent OOMs in real workloads.
- **Enterprise images, 300s spec timeouts, V2_ONLY discovery (port 6000)** all
  still apply — this skill only changes which *tier* runs, not the runtime
  contract.

## Why this exists / provenance

The integration suite was split into per-PR **core** (fast, two-track,
auto-triggered) and on-demand **extended** (heavy backup/restore/sharding/
multi-node, nightly + `run-extended` label + dispatch) so the dev cycle stays
short while the expensive specs still run on a schedule and before a release —
see the header comments in `.github/workflows/integration.yml` and
`.github/workflows/integration-tests.yml`. This skill captures the exact,
copy-pasteable way to invoke the extended tier in either place, including the
two easy-to-miss gates: the event-aware `core || extended` vs `extended`-only
label filter, and the CalVer-≥-2025.12 + `NEO4J_SHARDING_RELAX_MEMORY_MIN`
conditions that decide whether the sharding specs actually run rather than
silently skip.
