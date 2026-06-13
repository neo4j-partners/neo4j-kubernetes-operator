---
name: cut-release
description: Cut a tagged release of the Neo4j Kubernetes Operator through the gated pipeline, with a mandatory human go/no-go before the tag is pushed.
---

# Cut a release

Releases are fired by pushing a `vX.Y.Z` git tag, which triggers
`.github/workflows/release.yml`. That pipeline is **autonomous once the tag
lands** — its only safety is the pre-flight gates you run by hand and the
explicit human go below. **NEVER auto-tag.**

## STOP-POINTS (read before doing anything)

- **You must obtain explicit human "go" before step 5 (tagging).** Do not
  infer go from a green CI run. Steps 1-3 are evidence you present to the
  human; the human decides.
- A tag, once pushed, publishes to GHCR (signed), the Helm OCI + classic repo,
  a GitHub Release, OperatorHub, and versioned docs. If something is wrong
  *after* the tag, you cannot quietly re-cut — see the `retract-release` skill
  (version numbers are never reused).

## Procedure

1. **Be on a clean `main` with no generated-file drift.**
   ```bash
   git switch main && git pull --ff-only
   git status --porcelain   # must be empty
   make ship-prep           # sync-all + bundle + helm-lint + check-csv-coverage
   git status --porcelain   # STILL empty — ship-prep must not have produced drift
   ```
   If `ship-prep` changed any file, the generated artifacts were stale: commit
   the regeneration via a normal PR and start over. Do not tag off a tree that
   `ship-prep` mutates — the drift gate in `validate-release` will fail the
   release anyway.

2. **Extended integration suite GREEN on post-merge `main`.** Dispatch and
   poll to completion:
   ```bash
   gh workflow run integration-tests.yml --ref main
   # find the run, then watch it to the end:
   gh run list --workflow=integration-tests.yml --branch=main --limit=1
   gh run watch <run-id> --exit-status
   ```
   (Defaults to the pinned anchor CalVer; pass `-f neo4j-version=<tag>` only if
   the human asks to validate a specific version.) Must be `success`.

3. **Install-confidence GREEN.** This is the same matrix the release pipeline
   runs as a blocking gate, but run it locally first so a failure costs minutes
   not a retraction:
   ```bash
   make install-confidence   # ~10-15 min on a throwaway Kind cluster
   ```
   It does helm install (cluster + namespaces mode) → upgrade-from-previous →
   uninstall, plus the kubectl-apply install/upgrade/uninstall path. Must exit 0.

4. **AWAIT EXPLICIT HUMAN GO.** Present steps 1-3 (clean tree, green extended
   run URL, green install-confidence) and the proposed `vX.Y.Z`. Do not proceed
   until a human explicitly approves the exact version string. If unsure of the
   version, ask — do not guess a bump.

5. **Tag and push** (only after step 4):
   ```bash
   git tag vX.Y.Z && git push origin vX.Y.Z
   ```
   This fires `release.yml`. Its job chain is the real gate wiring (each stage
   `needs:` the prior, so a failure halts publication):
   `determine-tag` → `validate-release` (unit tests, build, `check-drift`) +
   `install-confidence` → `build-and-push` (multi-arch image + `cosign sign` +
   Helm OCI push) → `create-release` (GitHub Release + OperatorHub bundle
   artifact) → `submit-operatorhub` + `publish-docs` (`pages-docs.yml`) +
   `publish-helm` (`pages-helm.yml`, classic repo).

6. **Monitor the release run end-to-end.**
   ```bash
   gh run list --workflow=release.yml --limit=1
   gh run watch <run-id> --exit-status
   ```
   Pushing/launching the tag is **not** the finish line — the pipeline must
   settle green through `publish-docs` / `publish-helm` / `submit-operatorhub`.
   On failure, `release-failure-incident` files an incident issue; read it,
   and if any public surface already shipped, go to `retract-release`.

7. **Post-release wrap-up:**
   - Polish the GitHub Release body highlights (`gh release edit vX.Y.Z`).
   - Post the team Slack note.
   - Add verification-pin comments on team-reported issues that this release
     addresses (so they can confirm against the pinned release).

## Why this exists / provenance

Distilled from the real cut-release sequence run this session. The gates exist
because a tag is irreversible across five public surfaces; `ship-prep` and
`check-drift` exist because a tag cut off a tree with stale generated artifacts
would ship a chart/bundle that doesn't match the code. The human go is the
load-bearing safety — the pipeline itself will happily publish whatever you tag.
