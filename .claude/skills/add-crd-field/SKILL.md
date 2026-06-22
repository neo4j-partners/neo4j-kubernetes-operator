---
name: add-crd-field
description: Add a new field to an existing CRD end-to-end — edit the api/v1beta1 struct + kubebuilder markers, regenerate CRDs/deepcopy/chart, wire any validation INLINE (never a webhook), extend the validator test, and prove check-drift + test-unit are green.
---

# Add a field to an existing CRD

CRD types are hand-written Go structs in `api/v1beta1/*_types.go`; almost
everything downstream of them is **generated** — the CRD YAML in
`config/crd/bases/`, the deepcopy methods in
`api/v1beta1/zz_generated.deepcopy.go`, the Helm chart CRDs, and the
OperatorHub bundle. Adding a field means editing the one source-of-truth struct
and then re-running the generators so every artifact agrees. Forgetting the
regeneration is the classic failure: `check-drift` (a **blocking** CI gate)
fails because `config/crd/bases/` no longer matches the Go type.

If the field needs validation, that validation lives **inline** in
`internal/validation/` and is called from the reconciler — **never** an
admission webhook (Invariant 1, NO WEBHOOKS; all validation is controller-side).

## When to use

- A user-facing knob is being added to a `*Spec` (e.g. a new option on
  `Neo4jBackupSpec`, `Neo4jEnterpriseClusterSpec`) or a new observed value to a
  `*Status`.
- You are NOT defining a brand-new CRD kind — that is a larger task (new
  `_types.go`, controller, `SetupWithManager`, RBAC markers, samples, an
  ArtifactHub description row in `scripts/helm-sync-artifacthub-crds.sh`). This
  skill is the additive single-field case.

## Read these patterns first

| File | What to copy |
|---|---|
| `api/v1beta1/neo4jbackup_types.go` | struct field shape: a `+kubebuilder:validation:*` marker line, a doc comment, the `json:"name,omitempty"` tag (json tags are **required** for serialization) |
| `internal/validation/image_validator.go` | inline-validator shape: `Validate(...) field.ErrorList`, `field.NewPath(...)`, `field.Invalid/Required/NotSupported`, and the comment style that cites the invariant/intent |
| `internal/validation/cluster_validator.go` | how a sub-validator is registered (struct field + `New*` in `NewClusterValidator`) and aggregated (`allErrs = append(allErrs, v.xValidator.Validate(cluster)...)`) |
| `internal/controller/neo4jenterprisecluster_controller.go` | where validation errors surface: `result.Errors.ToAggregate().Error()` → event + status, return with requeue |

## Procedure

1. **Edit the struct.** Add the field to the relevant `*Spec` (or `*Status`) in
   `api/v1beta1/<kind>_types.go`. Give it a doc comment, a `json:"...,omitempty"`
   tag, and the right `+kubebuilder:validation:*` / `+kubebuilder:default=`
   markers (`Enum`, `Minimum`, `MaxLength`, `Pattern`, `Optional`, …). Markers
   are the cheapest validation — push as much as you can into the CRD schema so
   the API server rejects bad input before the reconciler even runs.

2. **Regenerate the generated artifacts.** Run the schema + deepcopy generators,
   then the full sync so the Helm chart CRDs and bundle pick the change up too:
   ```bash
   make manifests   # regenerates config/crd/bases/*.yaml (+ config/rbac/role.yaml)
   make generate    # regenerates api/v1beta1/zz_generated.deepcopy.go
   make sync-all    # manifests + generate + sync-kustomize + editor/viewer roles
                    # + helm-sync-crds + helm-sync-rbac + helm-sync-artifacthub-crds
   ```
   `make sync-all` is a superset of `manifests`/`generate`, so running it alone
   suffices — the two explicit calls above are shown so you understand what
   feeds what. **Never hand-edit** any generated file (each carries a
   `# This file is GENERATED. DO NOT EDIT.` header).

3. **If the field needs validation the markers can't express, add it INLINE.**
   Put the logic in the matching `internal/validation/<kind>_validator.go`
   returning a `field.ErrorList` (use `field.NewPath("spec", "<yourField>")` and
   `field.Invalid`/`field.Required`). For a `Neo4jEnterpriseCluster` field this
   usually means extending an existing sub-validator (or adding a new one wired
   into `NewClusterValidator` + appended in `validateCluster`). For the simpler
   CRDs (backup, plugin, user, role, …) the validator is called directly from
   the reconciler, e.g. `validation.NewBackupValidator().Validate(backup)` in
   `internal/controller/neo4jbackup_controller.go`. Confirm the reconciler
   actually calls the validator and surfaces the error
   (`errs.ToAggregate().Error()` → event/status). **Do NOT** add a
   `ValidatingWebhookConfiguration` or a `_webhook.go` file — that violates
   Invariant 1.

4. **Add or extend the validator test.** Add a table-driven case to the matching
   `internal/validation/<kind>_validator_test.go` (pattern in
   `image_validator_test.go`: `tests := []struct{ name; ...; expectedErrs int }`
   driving `validator.Validate(...)` and asserting the `field.ErrorList` length /
   message). Cover both the accept and reject paths for your new field.

5. **Prove the blocking gates are green.** `check-drift` re-runs every generator
   and `bundle`, then `git diff --exit-code` — it is one of the two BLOCKING CI
   gates (with `unit-tests`), so a stale tree fails the build:
   ```bash
   make check-drift   # sync-all + bundle, then fails on any diff — must be clean
   make test-unit     # runs manifests/generate/fmt/vet then the unit + validator tests
   ```
   To iterate faster on just your validator test:
   ```bash
   go test ./internal/validation/ -run TestImageValidator_Validate -v   # swap in your test name
   ```

6. **Advisory hygiene (recommended, non-blocking).** If your field touches an
   invariant surface, sanity-check the advisory guards:
   ```bash
   make check-invariants   # grep guard for the 5 hard invariants
   ```

## Guardrails

- **No webhooks (Invariant 1).** Validation is inline in `internal/validation/`
  and called from the reconciler. Never add `ValidatingWebhookConfiguration`,
  `MutatingWebhookConfiguration`, or a `*_webhook.go`.
- **Generated files are not hand-editable.** If `make check-drift` shows a diff,
  the fix is to re-run `make sync-all` (and commit its output), not to patch
  `config/crd/bases/`, `zz_generated.deepcopy.go`, or the chart by hand.
- **Respect CRD separation of concerns.** A field on `Neo4jDatabase` must not
  reach into cluster/server-level settings (those belong to the Cluster /
  Standalone CRDs); a `Neo4jUser` must not carry privileges (those live on
  `Neo4jRole`). Add the field to the CRD that actually owns the concern.
- **`check-drift` and `unit-tests` are the only blocking gates.**
  `check-invariants` / `check-knowledge-drift` are advisory (the non-blocking
  `Invariant Guards` job); don't claim otherwise, but do run them when relevant.

## Why this exists / provenance

A CRD field has one source of truth (the Go struct) and a long tail of
generated mirrors. The repeatable failure is editing the struct, skipping the
regen, and getting blocked by `check-drift` in CI — or worse, adding validation
as a webhook and breaking the project's first invariant. This skill encodes the
edit → regenerate → inline-validate → test → prove-the-gates loop, grounded in
the real targets (`make manifests`/`generate`/`sync-all`/`check-drift`/
`test-unit`) and the real inline-validation pattern (`field.ErrorList` in
`internal/validation/`, aggregated in `cluster_validator.go`, surfaced via
`ToAggregate()` in the reconcilers).
