---
name: regen-artifacts
description: Regenerate the operator's generated artifacts (CRDs, deepcopy, RBAC, kustomize lists, Helm chart, OperatorHub bundle) after touching CRD types, +kubebuilder markers, or RBAC markers — then prove the blocking check-drift gate is green.
---

# Regenerate generated artifacts

Large parts of `config/`, `charts/`, `api/v1beta1/zz_generated.deepcopy.go`, and
`bundle/` are **generated, not hand-written**. When you change a Go type in
`api/v1beta1/`, a `+kubebuilder:rbac:` / CRD marker in `internal/controller/`, or
add a new CRD, those derived files go stale. The CI job **Generated Artifacts In
Sync** (`.github/workflows/ci.yml`, `make check-drift`) regenerates everything
and fails the build on any diff — so a forgotten regen is a **blocking merge
gate**, not a nit. This skill maps each change to the right generator and proves
the tree is back in sync.

## When to use

- You edited a type in `api/v1beta1/*` (added/changed/removed a field, doc
  comment, validation marker) → CRD schemas + deepcopy are stale.
- You added/changed a `+kubebuilder:rbac:` marker in `internal/controller/*.go`
  → `config/rbac/role.yaml` + the Helm ClusterRole template are stale.
- You **added a brand-new CRD** → CRD base, kustomize lists, per-CRD
  editor/viewer roles, Helm chart CRDs, ArtifactHub annotation, and the
  OperatorHub bundle/CSV all need regenerating (and the ArtifactHub script needs
  a new description case — see Guardrails).
- The **Generated Artifacts In Sync** CI job is red, or local `make check-drift`
  fails.

## Which target for which change

Sources of truth and regenerators (mirrors the `Generated artifacts` table in
`CLAUDE.md`). When in doubt, just run `make sync-all` — it chains all of these in
order.

| You changed | Stale artifact | Regenerate with |
|---|---|---|
| `+kubebuilder:rbac:` markers in `internal/controller/*.go` | `config/rbac/role.yaml` | `make manifests` |
| Go types / CRD markers in `api/v1beta1/*` | `config/crd/bases/*.yaml` | `make manifests` |
| Go types in `api/v1beta1/*` | `api/v1beta1/zz_generated.deepcopy.go` | `make generate` |
| Files in `config/crd/bases/` or new `config/samples/neo4j_*.yaml` | `config/crd/kustomization.yaml`, `config/samples/kustomization.yaml` | `make sync-kustomize` |
| A CRD's `spec.{group,names}` | `config/rbac/<crd>_{editor,viewer}_role.yaml` + kustomization | `make sync-editor-viewer-roles` |
| `config/crd/bases/*.yaml` | `charts/neo4j-operator/crds/*.yaml` | `make helm-sync-crds` |
| `config/rbac/role.yaml` + metrics roles | `charts/neo4j-operator/templates/clusterrole.yaml` + `metrics-rbac.yaml` | `make helm-sync-rbac` |
| CRD bases (a new/renamed CRD) | `charts/neo4j-operator/Chart.yaml` (`artifacthub.io/crds`) | `make helm-sync-artifacthub-crds` |
| Everything above (OperatorHub) | `bundle/manifests/*`, `bundle/metadata/*` | `make bundle` |

## Procedure

1. **Regenerate everything in one shot.** `sync-all` runs every generator above
   in dependency order (`manifests generate sync-kustomize
   sync-editor-viewer-roles helm-sync-crds helm-sync-rbac
   helm-sync-artifacthub-crds`). The bundle is generated separately:
   ```bash
   make sync-all
   make bundle
   ```
   (Running an individual target from the table is fine for a fast inner loop,
   but always finish with the full set before you push — `check-drift` runs them
   all.)

2. **Prove the tree is in sync the way CI does.** `check-drift` is exactly
   `sync-all` + `bundle` + `git diff --exit-code`, so a clean run here means the
   blocking CI gate will pass:
   ```bash
   make check-drift
   ```
   It tolerates only the `createdAt:` line in the CSV (pinned to a placeholder by
   `make bundle`); any other diff is real drift. If it fails, the regen produced
   changes you haven't committed — `git status` / `git diff` will show exactly
   which generated file moved.

3. **Commit the regenerated files alongside your source change** — the marker
   edit and its generated output belong in the same PR. Re-run `make check-drift`
   after committing; it must exit 0.

4. **Before tagging a release**, prefer the umbrella target, which also lints the
   chart and checks CSV coverage:
   ```bash
   make ship-prep   # sync-all + bundle + helm-lint + check-csv-coverage
   ```
   (`make bundle` pins `createdAt:` to a stable placeholder so concurrent PRs
   don't conflict; the release flow stamps the real timestamp via
   `make bundle-release`. Don't run `bundle-release` by hand on a feature branch
   — it would reintroduce a `createdAt:` diff.)

## Guardrails

- **Never hand-edit a generated file.** Many carry a
  `# This file is GENERATED ... DO NOT EDIT.` header (e.g. the per-CRD
  `config/rbac/*_{editor,viewer}_role.yaml`), and `check-drift` reverts any
  tampering on the next run. Edit the **source** (the Go type / kubebuilder
  marker / curated description) and regenerate.
- **A new CRD needs a description in `scripts/helm-sync-artifacthub-crds.sh`.**
  The script looks up each CRD `Kind` in a curated `case "$1" in ... esac` and
  **exits non-zero with `error: no description mapped for CRD kinds:`** if any
  Kind is missing — so `make helm-sync-artifacthub-crds` (and therefore
  `sync-all` / `check-drift`) will fail until you add the new Kind's `case` row.
- **Don't try to fix drift by deleting the diff.** If `check-drift` shows a
  generated file changed, that change is correct output for the source you
  edited; commit it. Reverting it just moves the failure to CI.
- This is the **only** generation gate that blocks merge — `make check-drift`
  (the *Generated Artifacts In Sync* job) plus `unit-tests`. The Invariant /
  knowledge-drift guards are advisory and out of scope here.

## Why this exists / provenance

The `check-drift` CI job exists to catch the single most common contributor
mistake: editing a CRD type or an RBAC marker and forgetting that CRD schemas,
deepcopy code, the Helm chart, and the OperatorHub bundle are all derived from
it. Caught at release time, a stale chart/bundle ships config that doesn't match
the code; caught in CI, it's a one-line `make sync-all` fix. The ArtifactHub
description case is a deliberate fail-closed: a new CRD that silently loses its
ArtifactHub entry is worse than a loud build break. Targets verified in the
repo `Makefile`; gate wiring verified in `.github/workflows/ci.yml`
(`name: Generated Artifacts In Sync`).
