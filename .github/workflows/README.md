# GitHub Actions Workflows

**Source of truth:** [`docs/developer_guide/ci_and_workflows.md`](../../docs/developer_guide/ci_and_workflows.md)
(published at the *Contribute → CI/CD & Workflows* page on the docs site). This
file is a quick index so the workflows are discoverable from the repo; keep
detailed descriptions in the docs page to avoid drift.

| Workflow | File | Triggers |
|---|---|---|
| **CI** | `ci.yml` | push/PR to `main`/`develop`, manual dispatch |
| **Integration Tests** | `integration.yml` | PR + push to `main` on runtime paths (core subset, 5.26 + CalVer) |
| **Extended Integration Tests** | `integration-tests.yml` | nightly (full); `run-extended` label on a PR (extended-only); manual dispatch (full) |
| **Release** | `release.yml` | push of a `vX.Y.Z` tag; manual dispatch |
| **Pages — Docs** | `pages-docs.yml` | push to `main`; push of a `v*` tag; manual dispatch |
| **Pages — Helm Repo** | `pages-helm.yml` | push of a `v*` tag; manual dispatch |

Shared steps live in composite actions under `.github/actions/`
(`setup-go`, `setup-k8s`, `collect-logs`).

## Common tasks

```bash
# The fast "Integration Tests" lane (core subset, 5.26 + CalVer) runs
# automatically on any runtime-path PR — no opt-in needed.

# Run the full Extended suite against your branch (e.g. for a backup/restore change):
gh workflow run "Extended Integration Tests" --ref "$(git branch --show-current)"

# Cut a release (fans out to images, GitHub release, docs, and Helm repo):
git tag v1.2.3 && git push origin v1.2.3

# Inspect runs:
gh run list --workflow=ci.yml
gh run view <run-id>
gh run download <run-id>            # artifacts (logs, cluster state) on failure
```

See the docs page for jobs, gates, the `gh-pages` publishing model, and the
secrets/permissions each workflow needs.
