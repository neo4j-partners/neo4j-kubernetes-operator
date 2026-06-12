# CI/CD & Workflows

All automation lives in `.github/workflows/`. This page is the source of truth
for what each workflow does, when it runs, and how to invoke it manually. The
in-repo `.github/workflows/README.md` is a short pointer back here.

| Workflow | File | Triggers |
|---|---|---|
| [CI](#ci) | `ci.yml` | push/PR to `main`/`develop`, manual dispatch |
| [Integration Tests](#integration-tests) | `integration.yml` | PR + push to `main` on runtime paths |
| [Extended Integration Tests](#extended-integration-tests) | `integration-tests.yml` | nightly (full); `run-extended` label on a PR (extended-only); manual dispatch (full) |
| [Release](#release) | `release.yml` | push of a `vX.Y.Z` tag, manual dispatch |
| [Pages — Docs](#pages-docs) | `pages-docs.yml` | push to `main`, push of a `v*` tag, manual dispatch |
| [Pages — Helm Repo](#pages-helm-repo) | `pages-helm.yml` | push of a `v*` tag, manual dispatch |

Shared Go setup/caching lives in the composite action `.github/actions/setup-go`.

## CI

**`ci.yml` — runs on every push and PR to `main`/`develop`.** Fast feedback; the
gate that blocks merge. Jobs:

1. **Generated Artifacts In Sync (`check-drift`)** — runs `make check-drift`
   (`sync-all` + `bundle`, then `git diff --exit-code`). Fails if any committed
   CRD, RBAC, deepcopy, Helm CRD, or OLM bundle file is stale (untracked
   generated files fail it too). Fix locally with `make sync-all` and commit
   the result. The same job then runs `make helm-lint` and
   `make check-csv-coverage` — both used to be ship-prep-only, which let a
   chart template error or a CSV missing a new CRD hide until release time.
   Static and seconds-fast; the failure summary distinguishes drift failures
   from lint/coverage failures.
2. **Unit Tests** — `make test-unit` (race-enabled, envtest-backed controller
   suite + plain unit tests). No external cluster required. The job runs the
   suite through [gotestsum](makefile_reference.md#make-gotestsum)
   (`make test-unit GO_TEST_CMD="./bin/gotestsum …"`), which emits a JUnit XML +
   test2json report; a summary step (`scripts/gotest-summary.sh`) writes the
   failed and slowest tests to the GitHub step summary.

Integration coverage lives in its own workflows, not in `ci.yml`: the fast
contributor lane is [Integration Tests](#integration-tests); the full matrix is
[Extended Integration Tests](#extended-integration-tests).

## Caching

The workflows cache the expensive, slowly-changing inputs so reruns stay fast:

- **`./bin` tools + envtest assets** — cached by the `.github/actions/setup-go`
  composite action, keyed on `hashFiles('Makefile')` (the tool versions are
  pinned there), so `kustomize`/`controller-gen`/`ginkgo`/`setup-envtest` aren't
  re-downloaded every run.
- **Go build/module cache (`GOCACHE`)** — restored via `actions/setup-go`'s
  built-in caching, keyed on `go.sum`.
- **Operator image layers** — the integration lanes build with Buildx and a
  GitHub Actions layer cache (`cache-from/to: type=gha`), so unchanged build
  stages are reused.
- **Neo4j image tarball** — the pulled `neo4j:<tag>-enterprise` image is saved to
  `/tmp/neo4j-image.tar` and restored with `actions/cache`, avoiding a Docker Hub
  pull on every integration run.

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
on docs-only changes.

This is the lane that should give a contributor a fast, legible yes/no on the
contracts they touched, on the versions users actually run.

> **Caveat — a new push cancels the in-flight run.** Each integration lane uses a
> per-PR [concurrency group](https://docs.github.com/actions/using-jobs/using-concurrency)
> with `cancel-in-progress: true`, keyed on the PR number (`integration-core-${{
> github.event.pull_request.number || github.run_id }}` and the Extended
> equivalent). Pushing a new commit while a run is still going **terminates that
> run** and starts a fresh one on the new head — even a docs-only or trivial
> push (on `pull_request` the path filter is evaluated against the cumulative
> base→head diff, so any push re-fires the lane if the PR has *ever* touched a
> runtime path). The cancelled run shows up as red/"cancelled", not a failure.
> If you're waiting on a green integration result, **let it finish before pushing
> again**, or batch your changes into one push.

## Extended Integration Tests

**`integration-tests.yml` — the full suite (`core` + `extended`, ≈90–150 min)
against a real Kind cluster, on the pinned CalVer track.** This is the
release-readiness and deep-coverage run.

**Extended does NOT auto-run on PRs** — by design, the default PR signal is the
fast core lane, keeping the dev cycle short. Extended runs:

- **Nightly** (`cron: 0 3 * * *`) on `main`, **full suite** — keeps `main`
  continuously known-good on the CalVer track, so a regression is caught the day
  it merges and a release tag ships a commit whose CalVer health is already
  established (the tag is the release trigger — too late to be the gate itself).
  This is also the only *scheduled* check, so it must exercise everything,
  including `core`.
- **Per-PR opt-in** — apply the **`run-extended`** label to a PR to run it against
  that PR. On the PR event it runs **`extended`-only** (the core lane already
  covers `core` on that PR, so re-running it would be redundant). Labels are
  maintainer-only, which also keeps fork PRs from triggering it. Use this for
  backup/restore/sharding/coordination changes that the `core` subset doesn't
  exercise.
- **Manual dispatch** (Actions tab), **full suite**, with inputs:
  - `neo4j-version` — image tag (default the pinned CalVer; pass `5.26-enterprise`
    to verify the LTS floor, or `2025.12-enterprise+` for the property-sharding
    paths). Dispatch against your branch to run the full suite before merging.
  - `timeout-minutes` — default `150` (CalVer is ~2× slower per spec).

It builds and deploys the operator, runs the selected scope, uploads
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

## Install Confidence

**`scripts/install-confidence.sh` — the install/upgrade/uninstall matrix on a
throwaway Kind cluster.** Run locally with `make install-confidence` (~10–15
min; needs kind, helm, kubectl, docker), or dispatch
**`install-confidence.yml`** manually. It builds the operator image from the
working tree, loads it into a fresh Kind cluster, and walks five legs:

1. **Helm install, cluster mode** — default values, then a smoke
   `Neo4jEnterpriseStandalone` CR proving the operator actually reconciles
   (StatefulSet appears).
2. **Helm install, namespaces mode + `rbac.perNamespaceRoles`** — asserts the
   manager permissions land as a namespaced `Role` in the watched namespace and
   NO manager ClusterRole is created.
3. **Helm upgrade from the previously published chart** — installs the latest
   released chart, applies the new CRDs with
   `kubectl apply --server-side -f config/crd/bases/` (the step real users must
   run — Helm never upgrades `crds/`), upgrades to the working-tree chart, and
   probes a new-in-this-release CRD field to prove the refresh took.
   `PREV_CHART_VERSION=<x.y.z>` pins the starting chart (empty = latest
   published).
4. **Documented-order uninstall with a live CR** — CRs first (finalizers need
   the running operator), then `helm uninstall`, then CRDs; asserts nothing
   wedges.
5. **kubectl server-side apply path** — install, re-apply (idempotence), and
   ordered teardown of the non-Helm manifests.

The same matrix runs as a **blocking job in `release.yml`** — a release tag
cannot publish an image or chart if any leg fails. Use the local run (or the
dispatch workflow) to catch problems before tagging instead of failing the tag.

## Release

**`release.yml` — tag-driven release pipeline.** Push a `vX.Y.Z` tag (or dispatch
with a tag input). Jobs:

1. **determine-tag / validate-release** — resolve the tag and run build/test
   validation, including `make check-drift` (a tag on a commit that bypassed
   per-PR CI must not ship stale generated artifacts).
2. **install-confidence** — the full [install/upgrade/uninstall
   matrix](#install-confidence) on Kind, in parallel with validate-release.
   **Blocking**: `build-and-push` depends on it, so a broken install path
   fails the release before anything is published.
3. **build-and-push** — multi-arch (`linux/amd64,linux/arm64`) image to
   `ghcr.io/neo4j-partners/neo4j-kubernetes-operator`, **signed with Sigstore
   Cosign keyless** (`id-token: write` OIDC — no long-lived secrets).
4. **create-release** — assembles the kubectl-applyable bundles
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
   `helm-lint` + `check-csv-coverage`. Review `git status` and commit anything
   regenerated.
2. (Recommended) `make install-confidence` — the same install/upgrade/uninstall
   matrix the release workflow runs as a blocking gate. Running it locally
   first means a failure costs you minutes, not a dead tag.
2b. **Dispatch the extended suite on the final main** and wait for green:
   `gh workflow run integration-tests.yml --ref main`. The extended lane runs
   nightly, not per-PR — a PR that only core lanes validated can carry a stale
   extended-lane expectation (or a real regression) that would otherwise
   surface AFTER the release. Tag the exact commit the suite validated.
3. If there are breaking changes or notable upgrade steps, add/extend the
   `Upgrading from vX to vY` section in
   [`migration_guide.md`](../user_guide/migration_guide.md).
4. Draft the **What's Changed** notes — `git log <last-tag>..HEAD --pretty=oneline`
   is a good starting point. (The release workflow renders only the
   boilerplate from `.github/release-notes-template.md`; the changelog is
   hand-written.)

**Tag and push** from `main`:

```bash
git tag v1.12.0 && git push origin v1.12.0
```

This fires **Release** only. The versioned docs (`/vX.Y/` + `/latest/`) and
the classic Helm repo (`/charts/`) are published by the Release workflow's
final jobs **after every gate and artifact exists** — a failed release
publishes nothing (#245). Push-to-main docs (`/main/`) are unaffected. Any
job failure auto-files an incident issue with the failed jobs and run link.

**After the workflows finish:**

5. Paste the **What's Changed** section into the GitHub release body (above the
   generated boilerplate).
6. Verify:
   - `gh run list` — Release, Pages — Docs, Pages — Helm Repo all green.
   - `gh release view v1.12.0` — has `…-complete.yaml` and `…operator.yaml` assets.
   - `helm repo update && helm search repo neo4j-operator/neo4j-operator --versions` — new version listed.
   - Docs `/latest/` and `/v1.12/` load and the version dropdown shows the release.
   - `cosign verify ghcr.io/neo4j-partners/neo4j-kubernetes-operator:v1.12.0 …` succeeds.
   - The release run's **Install Confidence Gate** job is green (it ran
     automatically — no manual fresh-Kind `helm install` needed).
   - The **submit-operatorhub** job opened a PR on
     k8s-operatorhub/community-operators (check the fork's branches if not).
7. Comment on every issue the release fixes that was reported by someone else,
   naming the released version and asking the reporter to re-verify against it
   — reporter-filed issues stay open until verified on a pinned release.

### Retracting a release

`release-retract.yml` (manual dispatch, #246) safely unpublishes a dire
release. Doctrine: **retract and supersede — never reuse a version** (registry
layers, Helm caches, OLM catalogs and Rekor cache immutably). **Always run
with `dry-run: true` first**: the dry-run doubles as the grace-window probe,
printing either "window OPEN" (tag delete-and-re-cut still safe — nothing
published) or "window CLOSED at <surface>" (retract + ship vX.Y.Z+1).
Execution repoints the moving image tags at `previous-good-tag`, banners the
GitHub release as RETRACTED, removes the chart from both channels, republishes
docs from the previous good tag, and files a retraction-record issue with the
manual follow-ups (notably: a MERGED OperatorHub PR can only be superseded,
never yanked). Deleting the pinned image or the git tag are explicit opt-in
flags, default off.

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
