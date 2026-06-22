# Agent Guardrails — enforcement & gates

The five hard invariants themselves — rule, why, enforcement status, violation
symptom, and **recovery** — live in
**[`docs/knowledge/invariants.md`](../knowledge/invariants.md)** (the single
detailed home; [`AGENTS.md`](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/AGENTS.md) has the canonical short list). This
page is the *enforcement* companion: which checks block a merge, which are
advisory, and the generic gates that apply to every change.

## Two tiers — what actually stops you

If you are an LLM agent, treat enforcement as two tiers:

- **Blocking** — `make check-drift` (generated artifacts) and the `unit-tests`
  job reject a violating change mechanically; there is no arguing past review.
- **Advisory** — `make check-invariants` and `make check-knowledge-drift` (run by
  the agent skills and a non-blocking `Invariant Guards` CI job) surface a
  violation without gating merge. A flag there means you have broken the product
  contract, not tripped a lint nit. Fix the change, never the guard.

INV-3 additionally has **runtime** teeth (`internal/validation/image_validator.go`
rejects `-community`, pinned by `image_validator_test.go`); INV-4 and the INV-5
pod-naming half are **test-pinned** (blocking). The per-invariant enforcement
status is in [`invariants.md`](../knowledge/invariants.md).

## Generic gates (apply to every change)

| Gate | What happens if violated | Enforced by | Recovery |
|---|---|---|---|
| **Generated-artifact drift gate.** Any edit to `api/v1beta1/*_types.go`, a `+kubebuilder:rbac:` marker, or `config/crd/bases/*.yaml` must be followed by regeneration (CRDs, RBAC, kustomize lists, Helm chart, OperatorHub bundle). | PR is blocked; CRDs/RBAC/chart/bundle ship out of sync, producing a broken install. | **CI gate** — `make check-drift` (`sync-all` + `bundle` + `git diff --exit-code`), run by the `Generated Artifacts In Sync` job in `.github/workflows/ci.yml`. Same check available locally as a pre-commit hook via `make install-hooks`. | Run `make sync-all` (or `make ship-prep` before a release) and commit the regenerated files. Adding a new CRD also needs a description row in `scripts/helm-sync-artifacthub-crds.sh`. |
| **Lint.** `golangci-lint` must be clean. | Style/lint regressions land unnoticed. | **Local only** — `make lint`. **NOTE: lint is *not* run in CI** (only `check-drift` and `test-unit`, which itself runs `fmt` + `vet`). Run `make lint` yourself before pushing. | Fix the reported issues; re-run `make lint`. |

## Why "DO NOT trust a stale guide" matters here

This guardrails system exists because a prior `AGENTS.md` drifted into instructing
agents to *preserve* a centralized backup StatefulSet and *wire webhooks* — both
banned and long since removed from the code. **When a doc and the code
disagree, the code wins.** Before citing any symbol (file, function, test,
field) in a change or a doc, grep the tree to confirm it still exists. The five
invariants are verified against the current `main`. Two guards keep the docs from
silently rotting again: `make check-invariants` (the architectural constructs)
and `make check-knowledge-drift` (every test/file citation in `docs/knowledge/`
must resolve) — and the `fix-knowledge-drift` skill repairs the latter when a
rename breaks a citation.
