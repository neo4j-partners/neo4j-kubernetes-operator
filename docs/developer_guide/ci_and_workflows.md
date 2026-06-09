# CI/CD & Workflows

All automation lives in `.github/workflows/`. This page is the source of truth
for what each workflow does, when it runs, and how to invoke it manually. The
in-repo `.github/workflows/README.md` is a short pointer back here.

| Workflow | File | Triggers |
|---|---|---|
| [CI](#ci) | `ci.yml` | push/PR to `main`/`develop`, manual dispatch |
| [Extended Integration Tests](#extended-integration-tests) | `integration-tests.yml` | PRs touching key controllers/suite, manual dispatch |
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
3. **Integration Tests** — the standard integration suite, but **only** when
   opted in: a `run-integration-tests` PR label, `[run-integration]` in the
   commit message, or manual dispatch with the toggle on. When skipped, the
   `integration-tests-info` job prints how to enable it.

## Extended Integration Tests

**`integration-tests.yml` — the long (≈90-minute) suite against a real Kind
cluster with the operator deployed.** Distinct from CI's opt-in integration job:
this is the comprehensive run.

**Runs automatically on PRs** that touch the controllers most likely to silently
break cluster coordination, or the suite itself:

- `internal/controller/neo4jrestore_controller.go`, `neo4jrestore_coordination*.go`
- `internal/controller/neo4jbackup_controller.go`
- `internal/controller/neo4jenterprisecluster_controller.go`, `neo4jenterprisestandalone_controller.go`
- `test/integration/**`
- `.github/workflows/integration-tests.yml`

**Manual dispatch** (Actions tab) accepts inputs:

- `neo4j-version` — image tag to test against (default `5.26-enterprise`). Use a
  `2025.12-enterprise+` tag to exercise the property-sharding CI smoke path.
- `timeout-minutes` — default `90`.

It builds and deploys the operator, runs `ginkgo ./test/integration/...`,
uploads logs/cluster-state artifacts, and tears the cluster down.

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

## Publishing to GitHub Pages

Both Pages workflows write to the **`gh-pages`** branch and share a `gh-pages`
[concurrency group](https://docs.github.com/actions/using-jobs/using-concurrency)
so their pushes serialize instead of racing. Both authenticate with the
built-in `GITHUB_TOKEN` — no extra secrets.

### Pages — Docs

**`pages-docs.yml`** publishes the MkDocs site as **versioned** docs using
[`mike`](https://github.com/jimporter/mike):

- **Push to `main`** → publishes the `/main/` alias (rolling preview).
- **Push of a `v*` tag** → publishes `/vX.Y/` and updates the `/latest/` alias.
- **Manual dispatch** → publish under an arbitrary `version-alias`, optionally
  updating `latest`.

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
