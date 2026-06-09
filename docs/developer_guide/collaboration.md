# Collaboration

How we work together on this repository. For the hands-on "clone, build, test,
open a PR" walkthrough see [Contributing](contributing.md); for the automation
behind it see [CI/CD & Workflows](ci_and_workflows.md).

## Branching model

- **`main`** is the default and release branch. It is always releasable: every
  commit on `main` has passed CI, and tags are cut from it.
- **`develop`** is an optional integration branch; CI runs on it too. Most work
  can target `main` directly via a feature branch + PR.
- **Feature branches** use a `type/short-description` convention, e.g.
  `feat/sharded-restore`, `fix/storageclass-not-found`, `docs/ci-workflows`.
- Do **not** commit directly to `main`. Open a pull request.

## Pull requests

Every change lands through a PR. Keep PRs focused — one logical change per PR.

1. Branch from up-to-date `main`.
2. Make the change. If you touched anything that drives generated files (Go API
   types, kubebuilder markers, RBAC markers, CRDs), run `make sync-all` and
   commit the regenerated output — CI's drift gate will fail otherwise (see
   [Generated artifacts](#generated-artifacts)).
3. Run `make test-unit` and `make lint` locally before pushing.
4. Open the PR; fill in the PR template (summary, type of change, testing,
   checklist).
5. A reviewer is auto-requested via [CODEOWNERS](#code-owners-and-review).
   Address feedback by pushing follow-up commits.
6. Merge once required checks pass and the PR is approved.

> **Pushing cancels in-flight test runs.** The integration lanes use per-PR
> concurrency with `cancel-in-progress`, so a new push **terminates the currently
> running integration run** and restarts it on the new head — the old run is
> marked "cancelled", not failed. If you're waiting on a green integration
> result, let the run finish before pushing again (or batch fixes into one
> push) rather than pushing on top of an in-flight run. See
> [CI/CD & Workflows → Integration Tests](ci_and_workflows.md#integration-tests).

### Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/): `feat:`,
`fix:`, `docs:`, `refactor:`, `test:`, `chore:`, `ci:`. Breaking changes get a
`!` (`feat!:`) and a `BREAKING CHANGE:` footer. The commit-msg hook installed by
`make install-hooks` enforces this.

### Required checks before merge

These run automatically on every PR (see [CI/CD & Workflows](ci_and_workflows.md)):

| Check | What it verifies |
|---|---|
| **Generated Artifacts In Sync** (`check-drift`) | CRDs, RBAC, deepcopy, Helm CRDs, and the OLM bundle match the source. |
| **Unit Tests** | `make test-unit` (race-enabled) passes. |
| **Integration Tests** | The `core` subset on 5.26 + CalVer (parallel). Runs automatically when the PR touches runtime paths (`internal/**`, `api/**`, `cmd/**`, `test/integration/**`, `Makefile`, `go.{mod,sum}`). |
| **Extended Integration Tests** | The full suite on CalVer. Runs nightly; **does not auto-run on PRs** — opt a PR in with the `run-extended` label (runs extended-only) or dispatch manually (see below). |

Lint (`golangci-lint`, `staticcheck`) runs via the pre-commit hooks locally; run
`make lint` before pushing.

See [CI/CD & Workflows → Test tiers](ci_and_workflows.md#test-tiers-core-vs-extended)
for the `core` vs `extended` label convention and how to run each tier locally.

### Running the Extended Integration Tests on demand

The full (~90–150 min) CalVer suite doesn't run on every PR. Trigger it when your
change could affect cluster coordination, backup/restore, or sharding:

- **On a PR:** apply the **`run-extended`** label. The workflow then runs the
  `extended` specs against that PR (the `core` lane already covers `core`, so it
  isn't re-run). Labels are maintainer-only.
- **Manually:** run the *Extended Integration Tests* workflow from the Actions
  tab **against your branch** (lets you pick the Neo4j version) — runs the full
  suite.

The fast `core` lane (*Integration Tests*) runs automatically on any
runtime-path PR — no opt-in needed.

## Code owners and review

`.github/CODEOWNERS` controls automatic review requests. Today the whole repo is
owned by the maintainer; as the team grows, add path-scoped owners (e.g. one
team for `internal/controller/`, another for `docs/`) so reviews route to the
right people. At least one CODEOWNER approval is expected before merge.

## Releases and publishing

Releases are **tag-driven** — pushing a `vX.Y.Z` tag fans out to several
automated workflows; no manual artifact building is required. See
[CI/CD & Workflows → Release](ci_and_workflows.md#release) for the full chain
(multi-arch images, signed with Cosign; the kubectl-applyable bundle; the
GitHub release; the versioned docs site; the Helm chart repo).

Day-to-day, contributors don't publish anything: merging to `main` refreshes the
rolling `/main/` docs preview, and tagging does the rest. Cutting a tag is a
maintainer action.

## Getting help

- **Bugs / features:** open a [GitHub Issue](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues)
  using the templates.
- **Project conventions and invariants:** read `CLAUDE.md` at the repo root — it
  captures the hard constraints (KIND-only dev, no admission webhooks, V2 discovery,
  server-based architecture, etc.) that reviews enforce.
