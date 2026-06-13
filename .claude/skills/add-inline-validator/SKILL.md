---
name: add-inline-validator
description: Add a new spec validator the project's only way — an inline struct in internal/validation/ that returns field.ErrorList, wired into a reconciler and table-tested. NEVER an admission webhook (invariant 1).
---

# Add an inline validator

The operator has **no admission webhooks** (invariant 1, `docs/knowledge/invariants.md`).
All CR validation is plain Go in `internal/validation/`, constructed and called
**inline from the reconcile loop**. A bad CR therefore never gets blocked at the
apiserver — `kubectl apply` always succeeds; the reconciler catches the bad spec
and surfaces it on the CR's status (`phase=Invalid`/`Failed` + a Warning event),
so the user can fix the spec and re-trigger reconcile. This skill adds one more
validator the same way every existing one is built.

## When to use

- A new field (or field combination) on a CR spec needs structural validation
  before the reconciler acts on it.
- You're tempted to reach for a `ValidatingWebhookConfiguration` or a
  `*_webhook.go` — STOP. That's banned here; do this instead.
- An existing aggregate validator (`ClusterValidator`, `StandaloneValidator`)
  needs a new sub-validator slotted into its `allErrs` chain.

## The pattern (read these before writing anything)

The canonical, minimal example is `internal/validation/image_validator.go`:

| Piece | Shape | Example |
|---|---|---|
| Type | `type XValidator struct{}` (add a `client.Client` field only if you must read other objects) | `ImageValidator struct{}` |
| Constructor | `func NewXValidator() *XValidator` | `NewImageValidator()` |
| Entry point | `func (v *XValidator) Validate(cr *neo4jv1beta1.<Kind>) field.ErrorList` | `(v *ImageValidator) Validate(cluster …) field.ErrorList` |
| Errors | append `field.Required` / `field.Invalid` / `field.NotSupported` onto `allErrs`, anchored to a `field.NewPath("spec", …)` | all three appear in `image_validator.go` |

Return `field.ErrorList` (never `return err` mid-loop) so every problem is
collected and aggregated in one message via `errs.ToAggregate().Error()`.
A validator that needs to also emit non-fatal warnings returns a result struct
instead (see `*ValidationResult` in `plugin_validator.go` / `cluster_validator.go`) —
mirror an existing one rather than inventing a new shape.

## Procedure

1. **Read the template + one consumer, so you copy the real shape.**
   ```bash
   sed -n '28,113p' internal/validation/image_validator.go      # struct + New + Validate + helper
   sed -n '29,67p'  internal/validation/standalone_validator.go # how an aggregate composes sub-validators
   grep -n 'NewBackupValidator().Validate' internal/controller/neo4jbackup_controller.go
   ```

2. **Write `internal/validation/<thing>_validator.go`.** Copy the Apache header
   and `package validation` from `image_validator.go`. Implement
   `New<Thing>Validator()` and `Validate(cr) field.ErrorList`. Keep it pure:
   read only the CR spec (and, if unavoidable, objects via an injected
   `client.Client`); never mutate, never call Neo4j, never log.

3. **Wire it into the reconcile path — two shapes, pick the one that matches.**

   - **Aggregate sub-validator** (most cluster/standalone field checks): add a
     struct field + constructor wiring + an `allErrs = append(...)` line in the
     existing aggregate. For a cluster field, that's `ClusterValidator`:
     ```bash
     grep -n 'imageValidator' internal/validation/cluster_validator.go
     # add your validator the same three ways: field (~line 40),
     # NewXValidator() in the constructor (~line 56), and an append in validate() (~line 162-186)
     ```
     `StandaloneValidator` follows the identical pattern (`internal/validation/standalone_validator.go`,
     fields ~31-35, constructor ~40-45) — wire **both** if the field exists on
     both CRs.

   - **Top-level validator for its own CRD** (Backup, Plugin, Database, User,
     Role, …): construct + call inline at the top of `Reconcile`, before any
     resource is created, and convert errors to an `Invalid` status + Warning event.
     The simplest live example is the backup controller:
     ```bash
     sed -n '138,152p' internal/controller/neo4jbackup_controller.go
     ```
     If the controller injects its validator as a struct field (e.g.
     `plugin_controller.go` has `Validator *validation.PluginValidator`), also add
     the `New…Validator()` to its construction in `cmd/main.go`:
     ```bash
     grep -n 'Validator:.*validation.New' cmd/main.go
     ```

4. **Add a table-driven unit test `internal/validation/<thing>_validator_test.go`.**
   Copy the structure of `image_validator_test.go` (`TestXValidator_Validate`,
   a `tests := []struct{ name; cr; expectedErrs int; expectedError string }`
   slice, assert `len(errs)` and, where it matters, `errs[0].Field` /
   substring of the detail). Cover at least: a valid spec (0 errors), each
   rejection branch, and any boundary you special-cased.
   ```bash
   go test ./internal/validation/ -run TestXValidator -count=1
   ```

5. **Run the full local gate.** Validators are pure Go, so `test-unit` (the
   blocking CI gate) is the real check; `fmt`/`vet` keep the tree clean:
   ```bash
   make fmt vet
   make test-unit
   ```

6. **Confirm you didn't reintroduce a webhook.** The advisory invariant guard
   greps the tree for `_webhook.go`, `config/webhook/`, and references to
   `ValidatingWebhookConfiguration`/`MutatingWebhookConfiguration`:
   ```bash
   make check-invariants
   ```
   This MUST report no `no-webhooks` failure. (It is advisory/non-blocking in CI,
   but a webhook here is invariant-1 wrong by definition — fix it, don't ignore it.)

7. **Report**, as your final message: the new validator file + test, exactly
   where you wired it (aggregate append line, or reconciler call + `cmd/main.go`),
   and the `test-unit` / `check-invariants` results.

## Guardrails

- **NEVER create `*_webhook.go` or any `config/webhook/*`, and never reference
  `ValidatingWebhookConfiguration`/`MutatingWebhookConfiguration`.** That is
  invariant 1; `check-invariants` greps for exactly these.
- **Validation is read-only and inline.** It runs *inside* `Reconcile`, returns
  `field.ErrorList`, and never blocks the API. A bad CR becomes a status
  (`Invalid`/`Failed`) + Warning event, not a rejected `kubectl apply`.
- **Don't widen scope.** A `Neo4jDatabase`/`Neo4jUser` validator must not police
  cluster/server-level config — separation of concerns is strict (see CLAUDE.md).
- **No new return shape.** Reuse `field.ErrorList` (or an existing
  `*ValidationResult` if you need warnings); don't invent a bespoke error type.
- This skill touches `internal/validation/`, the reconciler that calls it, and
  (if injected) `cmd/main.go` — nothing generated. `make manifests`/`make generate`
  are not needed for a pure-Go validator.

## Why this exists / provenance

The whole `internal/validation/` package exists *because* the project banned
admission webhooks (invariant 1) and pushed every check controller-side. Each
validator is uniform on purpose — `NewXValidator()` + `Validate() field.ErrorList`,
composed into `ClusterValidator`/`StandaloneValidator` or called inline at the top
of a controller's `Reconcile`. New contributors (and agents) keep reaching for the
Kubebuilder default of a webhook; this skill is the standing answer: do it inline,
return a `field.ErrorList`, table-test it, and prove `check-invariants` stays clean.
