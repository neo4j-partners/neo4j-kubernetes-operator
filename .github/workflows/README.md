# GitHub Actions Workflows

**Source of truth:** [`docs/developer_guide/ci_and_workflows.md`](../../docs/developer_guide/ci_and_workflows.md)
(published at the *Contribute → CI/CD & Workflows* page on the docs site). This
file is a quick index so the workflows are discoverable from the repo; keep
detailed descriptions in the docs page to avoid drift.

| Workflow | File | Triggers |
|---|---|---|
| **CI** | `ci.yml` | push/PR to `main`/`develop`, manual dispatch |
| **Extended Integration Tests** | `integration-tests.yml` | PRs touching cluster/restore/backup controllers or `test/integration/**`; manual dispatch |
| **Release** | `release.yml` | push of a `vX.Y.Z` tag; manual dispatch |
| **Pages — Docs** | `pages-docs.yml` | push to `main`; push of a `v*` tag; manual dispatch |
| **Pages — Helm Repo** | `pages-helm.yml` | push of a `v*` tag; manual dispatch |

Shared steps live in composite actions under `.github/actions/`
(`setup-go`, `setup-k8s`, `collect-logs`).

## Common tasks

```bash
# Run the Extended Integration Tests on a PR:
gh pr edit --add-label "run-integration-tests"      # or include [run-integration] in a commit

# Cut a release (fans out to images, GitHub release, docs, and Helm repo):
git tag v1.2.3 && git push origin v1.2.3

# Inspect runs:
gh run list --workflow=ci.yml
gh run view <run-id>
gh run download <run-id>            # artifacts (logs, cluster state) on failure
```

See the docs page for jobs, gates, the `gh-pages` publishing model, and the
secrets/permissions each workflow needs.
