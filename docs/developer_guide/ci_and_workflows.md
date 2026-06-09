# CI/CD & Workflows

All automation lives in `.github/workflows/`. This page is the source of truth
for what each workflow does, when it runs, and how to invoke it manually. The
in-repo `.github/workflows/README.md` is a short pointer back here.

| Workflow | File | Triggers |
|---|---|---|
| [CI](#ci) | `ci.yml` | push/PR to `main`/`develop`, manual dispatch |
| [Integration Tests](#integration-tests) | `integration.yml` | PR + push to `main` on runtime paths |
| [Extended Integration Tests](#extended-integration-tests) | `integration-tests.yml` | nightly; PRs touching coordination-critical controllers/suite; manual dispatch |
| [Release](#release) | `release.yml` | push of a `vX.Y.Z` tag, manual dispatch |
| [Pages — Docs](#pages-docs) | `pages-docs.yml` | push to `main`, push of a `v*` tag, manual dispatch |
| [Pages — Helm Repo](#pages-helm-repo) | `pages-helm.yml` | push of a `v*` tag, manual dispatch |

Shared Go setup/caching lives in the composite action `.github/actions/setup-go`.

## CI

**`ci.yml` — runs on every push and PR to `main`/`develop`.** Fast feedback; the
gate that blocks merge. Jobs:

1. **Generated Artifacts In Sync (`check-drift`)** — runs `make check-drift`
   (`sync-all` + `bundle`, then `git diff --exit-code`). Fails if any committed
   CRD, RBAC, deepcopy, Helm CRD, or OLM bundle file is stale. Fix locally with
   `make sync-all` and commit the result.
2. **Unit Tests** — `make test-unit` (race-enabled, envtest-backed controller
   suite + plain unit tests). No external cluster required.

Integration coverage lives in its own workflows, not in `ci.yml`: the fast
contributor lane is [Integration Tests](#integration-tests); the full matrix is
[Extended Integration Tests](#extended-integration-tests).

## Branch protection

`main` is a protected branch. The rules enforce the gates above:

- **No direct pushes** — all changes land via pull request.
- **1 approval, from a [CODEOWNER](collaboration.md#code-owners-and-review)**;
  stale approvals are dismissed on new commits; review threads must be resolved.
- **Required status checks** (strict / branch must be up to date), matching the
  CI job display names exactly:
  - `Generated Artifacts In Sync`
  - `Unit Tests`
- **Linear history** (squash/rebase merges); force-pushes and deletion blocked.

Two things to know:

- **The integration checks are intentionally *not* required.** Both the
  Integration Tests lane and the Extended Integration Tests workflow are
  path-filtered, so they don't run on every PR (e.g. a docs-only change); a
  required check that doesn't run on a given PR would leave it stuck at
  "Expected — waiting for status." Reviewers gate on them manually for code
  changes.
- **`enforce_admins` is currently off** so the maintainer can merge during the
  solo→team transition (GitHub forbids approving your own PR, and the sole
  CODEOWNER would otherwise be unable to merge anything). Turn it on once a
  second reviewer is onboarded:
  ```bash
  gh api -X PUT repos/neo4j-partners/neo4j-kubernetes-operator/branches/main/protection/enforce_admins
  ```

If you rename a required CI job in `ci.yml`, update the protection contexts to
match or every merge will block.

## Test tiers: `core` vs `extended`

Every integration spec carries a Ginkgo label on its top-level `Describe`:

- **`core`** — reconcile contracts a routine change is most likely to break:
  standalone → Ready, cluster formation, `Neo4jDatabase`/`Neo4jUser`/`Neo4jRole`/
  `Neo4jRoleBinding` CRUD, config rendering, plugin install, basic TLS. Small,
  fast, deterministic on both tracks.
- **`extended`** — multi-node scaling, split-brain, rolling upgrade, the full
  backup/restore matrix, property sharding, MinIO/cloud, MCP, ABAC/OIDC. Slow,
  resource-heavy, or version-gated; high value before a release, low marginal
  value per PR.

The two workflows below select by label. Run a tier locally with
`ginkgo run --label-filter='core' ./test/integration/...` (or `'extended'`, or
`'core || extended'` for everything). When you add a spec file, **label its
`Describe`** or it runs in neither lane.

## Integration Tests

**`integration.yml` — the fast contributor lane.** Runs the **`core`** subset
against **both supported Neo4j tracks in parallel**:

- `5.26-enterprise` — the last SemVer LTS; exercises the SemVer-only operator
  paths (V2_ONLY discovery, `system_bootstrapping_strategy`).
- the pinned CalVer tag (currently `2026.04-enterprise`) — the track new users
  deploy; catches strict-mode fatals (duplicate conf keys, Cypher-25 defaults)
  that 5.26 tolerates.

Because it's the core subset and the two cells run in parallel, wall-clock ≈ the
slower (CalVer) cell, not the sum. Triggers on `pull_request` + `push` to `main`
when **runtime paths** change (`internal/**`, `api/**`, `cmd/**`,
`test/integration/**`, `Makefile`, `go.{mod,sum}`, the workflow itself) — never
on docs-only changes. A new push cancels the PR's in-flight run.

This is the lane that should give a contributor a fast, legible yes/no on the
contracts they touched, on the versions users actually run.

## Extended Integration Tests

**`integration-tests.yml` — the full suite (`core` + `extended`, ≈90–150 min)
against a real Kind cluster, on the pinned CalVer track.** This is the
release-readiness and deep-coverage run.

Triggers:

- **Nightly** (`cron: 0 3 * * *`) on `main` — keeps `main` continuously
  known-good on the CalVer track, so a regression is caught the day it merges
  and a release tag ships a commit whose CalVer health is already established
  (the tag is the release trigger — too late to be the gate itself).
- **PRs** touching the coordination-critical controllers or the suite, since the
  core lane does *not* include backup/restore/coordination specs:
  - `internal/controller/neo4jrestore_controller.go`, `neo4jrestore_coordination*.go`
  - `internal/controller/neo4jbackup_controller.go`
  - `internal/controller/neo4jenterprisecluster_controller.go`, `neo4jenterprisestandalone_controller.go`
  - `test/integration/**`, `.github/workflows/integration-tests.yml`
- **Manual dispatch** (Actions tab) with inputs:
  - `neo4j-version` — image tag (default the pinned CalVer; pass `5.26-enterprise`
    to verify the LTS floor on the full suite, or `2025.12-enterprise+` for the
    property-sharding paths). **To run the full suite against your branch before
    merging a backup/restore/sharding change, dispatch this workflow on your
    branch.**
  - `timeout-minutes` — default `150` (CalVer is ~2× slower per spec).

It builds and deploys the operator, runs the full suite, uploads
logs/cluster-state artifacts, and tears the cluster down.

> **Not everything labelled `extended` runs in this lane.** The tier label only
> selects the lane; a runtime `Skip` can still exclude a spec there. The
> resource-heavy property-sharding suites self-skip in CI (they need the
> production 4Gi/server floor) and are **local-only** — only the minimal
> `property_sharding_ci_smoke` runs here. See
> [Testing → What runs where](testing.md#what-runs-where-coverage-map) for the
> full map and how to run the local-only suites.

**Bumping the CalVer pin:** the version is pinned (not floating) for
deterministic CI, in **two** places that must move together (GitHub allows
neither an `env` var in a matrix nor an expression in a dispatch-input default,
so there's no single shared variable):

1. the `neo4j-version` **matrix list** in `integration.yml` (the core lane), and
2. the `neo4j-version` input **default** in `integration-tests.yml` (the Extended
   lane).

Bump both in the same PR — the bump is itself a tested change.

## Release

**`release.yml` — tag-driven release pipeline.** Push a `vX.Y.Z` tag (or dispatch
with a tag input). Jobs:

1. **determine-tag / validate-release** — resolve the tag and run build/test
   validation.
2. **build-and-push** — multi-arch (`linux/amd64,linux/arm64`) image to
   `ghcr.io/neo4j-partners/neo4j-kubernetes-operator`, **signed with Sigstore
   Cosign keyless** (`id-token: write` OIDC — no long-lived secrets).
3. **create-release** — assembles the kubectl-applyable bundles
   (`neo4j-kubernetes-operator-complete.yaml`, `…operator.yaml`), stamps the OLM
   CSV via `make bundle-release` (with a guard that refuses to publish if
   `createdAt:` is still the dev placeholder), renders the release body from
   `.github/release-notes-template.md`, and publishes the GitHub release.

Pushing the tag also fires **Pages — Docs** and **Pages — Helm Repo** (below),
so a single tag publishes images, the release, the docs version, and the chart.

### Cutting a release (runbook)

Releases are tag-driven. **Do not bump `Chart.yaml`** — `version`/`appVersion`
are the `0.0.1` placeholder in the repo and the workflows stamp the real value
from the tag.

**Before tagging** (on an up-to-date, green `main`):

1. `make ship-prep` — regenerates everything, builds the bundle, and runs
   `helm-lint` + `check-csv-coverage` (more than CI's drift gate). Review
   `git status` and commit anything regenerated.
2. If there are breaking changes or notable upgrade steps, add/extend the
   `Upgrading from vX to vY` section in
   [`migration_guide.md`](../user_guide/migration_guide.md).
3. Draft the **What's Changed** notes — `git log <last-tag>..HEAD --pretty=oneline`
   is a good starting point. (The release workflow renders only the
   boilerplate from `.github/release-notes-template.md`; the changelog is
   hand-written.)

**Tag and push** from `main`:

```bash
git tag v1.12.0 && git push origin v1.12.0
```

This fans out to **Release** (multi-arch signed images + GitHub release +
kubectl bundles + OLM CSV), **Pages — Docs** (`/v1.12/` + `/latest/`), and
**Pages — Helm Repo** (`/charts/`).

**After the workflows finish:**

4. Paste the **What's Changed** section into the GitHub release body (above the
   generated boilerplate).
5. Verify:
   - `gh run list` — Release, Pages — Docs, Pages — Helm Repo all green.
   - `gh release view v1.12.0` — has `…-complete.yaml` and `…operator.yaml` assets.
   - `helm repo update && helm search repo neo4j-operator/neo4j-operator --versions` — new version listed.
   - Docs `/latest/` and `/v1.12/` load and the version dropdown shows the release.
   - `cosign verify ghcr.io/neo4j-partners/neo4j-kubernetes-operator:v1.12.0 …` succeeds.
   - (Optional, highest-confidence) `helm install` the published chart on a fresh Kind cluster.

## Publishing to GitHub Pages

Both Pages workflows write to the **`gh-pages`** branch and share a `gh-pages`
[concurrency group](https://docs.github.com/actions/using-jobs/using-concurrency)
so their pushes serialize instead of racing. Both authenticate with the
built-in `GITHUB_TOKEN` — no extra secrets.

### Pages — Docs

**`pages-docs.yml`** publishes the MkDocs (Material) site as **versioned** docs
using [`mike`](https://github.com/jimporter/mike). It first runs
`mkdocs build --strict`, so a broken internal link or a nav entry pointing at a
missing page **fails the publish** — treat it as a docs gate on `main` and tags.

What gets published where:

- **Push to `main`** → the `/main/` alias, a rolling preview of unreleased docs.
  Does not touch `latest`.
- **Push of a `vX.Y.Z` tag** → published as `/vX.Y/` (the patch is dropped, so a
  later `vX.Y.Z+1` overwrites the same `/vX.Y/`), and `/latest/` is moved to it.
  `mike set-default latest` also points the site **root** at `/latest/`.
- **Manual dispatch** → publish under an arbitrary `version-alias`, optionally
  updating `latest`.

For readers: the site **root redirects to `/latest/`** (newest release), and the
**version dropdown** at the top of every page (`extra.version.provider: mike` in
`mkdocs.yml`) switches between `latest`, each released `/vX.Y/`, and the `/main/`
preview. A page added on `main` appears under `/main/` immediately and lands in
`/vX.Y/` + `/latest/` automatically on the next release tag — no manual step.

(The rolling-preview alias was historically called `dev`; it is now `main`.)

### Pages — Helm Repo

**`pages-helm.yml`** packages the chart in `charts/neo4j-operator` and appends it
to the Helm repository index under `/charts/` on `gh-pages`, so:

```bash
helm repo add neo4j-operator https://neo4j-partners.github.io/neo4j-kubernetes-operator/charts
helm repo update
```

- **Push of a `v*` tag** → packages and publishes that version.
- **Manual dispatch** → package a specific existing tag.

## OpenShift / OLM

OLM/OperatorHub is a **supported manual install path** but is **not** covered by
CI — a smoke test needs an OpenShift cluster, which standard GitHub runners can't
provide. Build and validate the bundle locally per
[OpenShift OLM](openshift_olm.md).

## Secrets & permissions

- **CI / Extended Integration Tests:** none beyond the default `GITHUB_TOKEN`.
- **Release:** `id-token: write` (Cosign keyless OIDC) and `packages: write`
  (push to GHCR). No stored signing keys.
- **Pages (Docs + Helm):** `contents: write` to push to `gh-pages`; the built-in
  `GITHUB_TOKEN`.
