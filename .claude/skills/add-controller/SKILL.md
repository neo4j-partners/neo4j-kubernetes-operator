---
name: add-controller
description: Add a new controller/reconciler the project way — RBAC markers + regenerated role.yaml, finalizer + retry.RetryOnConflict + non-fatal status patterns, inline validation (NO webhook), and a Label()-tagged spec wired into cmd/main.go.
---

# Add a controller

The operator runs **every** reconciler inside one controller-runtime manager
wired in `cmd/main.go` — there is **no webhook server** and never will be
(invariant 1: all validation is inline in `internal/validation/`, called from
the reconciler). A new controller is: a `*Reconciler` struct + `Reconcile` +
`SetupWithManager` under `internal/controller/`, `+kubebuilder:rbac:` markers
that regenerate `config/rbac/role.yaml`, a wiring entry in **both** controller
maps in `cmd/main.go`, an inline validator, and a `Label("core")`/`Label("extended")`
spec. This skill is the copy-the-existing-pattern checklist so you don't
reinvent (or violate) any of it.

## When to use

- You're introducing a new CRD kind that needs its own reconcile loop, or
  splitting an existing one out into its own controller.
- You need the canonical shape of finalizer handling, conflict-safe status
  writes, requeue cadence, and manager wiring as the rest of the package does it.

Do **not** use this to add admission/mutating webhooks — they are banned
(invariant 1). Cross-object enforcement that feels webhook-shaped goes inline in
`internal/validation/` and is called from `Reconcile`.

## Procedure

Copy a small existing reconciler as your template — `internal/controller/neo4jrole_controller.go`
is the cleanest (finalizer, inline validator, cluster-ref resolve, conflict-safe
status, `Watches`-based re-enqueue). `internal/controller/neo4jbackup_controller.go`
also owns `GetTestRequeueAfter()` and shows `Owns(...)`-based wiring.

1. **Scaffold the reconciler file** under `internal/controller/`. Mirror the
   `Neo4jRoleReconciler` shape: embed `client.Client`; carry `Scheme`,
   `Recorder record.EventRecorder`, `RequeueAfter time.Duration`, optional
   `MaxConcurrentReconciles int`, and a `Validator` field from
   `internal/validation/`. Define a finalizer constant like
   `Neo4jRoleFinalizer = "neo4j.com/role-finalizer"`.

2. **Write the RBAC markers** directly above `Reconcile` — three for your own
   kind plus whatever K8s/CR objects you read or write. Mirror the role
   controller verbatim, swapping the resource name:
   ```go
   // +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jthings,verbs=get;list;watch;create;update;patch;delete
   // +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jthings/status,verbs=get;update;patch
   // +kubebuilder:rbac:groups=neo4j.neo4j.com,resources=neo4jthings/finalizers,verbs=update
   // +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
   ```
   These are the **source of truth** for `config/rbac/role.yaml` (a generated
   file — never hand-edit it). Regenerate after editing markers:
   ```bash
   make manifests   # regenerates config/rbac/role.yaml + config/crd/bases/*
   make generate    # regenerates api/v1beta1/zz_generated.deepcopy.go
   ```

3. **Implement `Reconcile`** following the package contract:
   - `Get` the object; on `apierrors.IsNotFound` return `ctrl.Result{}, nil`.
   - **Deletion path first:** if `obj.DeletionTimestamp != nil`, run cleanup, then
     `controllerutil.RemoveFinalizer(...)` + `Update`.
   - **Add finalizer** when absent via `controllerutil.AddFinalizer` + `Update`,
     then return `ctrl.Result{Requeue: true}, nil`.
   - **Validate inline** — call your `validation.*Validator.Validate(ctx, obj)`,
     emit `res.Warnings` as `corev1.EventTypeWarning`/`EventReasonValidationWarning`
     events, and on `res.Errors` set a Failed status + `EventReasonValidationFailed`
     event and requeue. Validation lives in `internal/validation/`, **never** a
     webhook.
   - **Requeue cadence:** use the local `requeueAfter()` helper that returns
     `r.RequeueAfter` when set (else a 30s default), as `Neo4jRoleReconciler.requeueAfter`
     does. Production/dev wiring sets `RequeueAfter: controller.GetTestRequeueAfter()`,
     which returns 1s when `TEST_MODE=true` else 30s.

4. **Use the project's status + event constants.**
   - Events: reasons are constants in `internal/controller/events.go` — reuse an
     existing one (e.g. `EventReasonValidationFailed`, `EventReasonClusterNotFound`)
     or add a new `EventReason*` constant there. Always pass
     `corev1.EventTypeNormal` / `corev1.EventTypeWarning`, never a raw string.
   - **Conflict-safe writes:** wrap every status/finalizer update in
     `retry.RetryOnConflict` (re-`Get` the latest inside the closure, mutate,
     `Status().Update`), exactly like `Neo4jRoleReconciler.setStatus`. Required
     for Neo4j 2025.01.0 cluster-formation churn.
   - **Non-fatal diagnostics:** any best-effort enrichment (e.g. live `SHOW …`
     collection) must record its failure to a status field and **never**
     `return err` — see `appendCollectionError` in
     `internal/controller/diagnostics_users_roles.go`. A diagnostics failure
     must not fail the reconcile.

5. **Write `SetupWithManager`** with `ctrl.NewControllerManagedBy(mgr).For(&neo4jv1beta1.YourKind{})`,
   add `Owns(...)` for child objects you create and `Watches(...)` for
   dependencies (the role controller `Watches` clusters/standalones so a CR
   re-reconciles when its target lands), `WithOptions(controller.Options{MaxConcurrentReconciles: ...})`
   if needed, then `.Complete(r)`.

6. **Wire it into `cmd/main.go` in BOTH places** (there is no webhook setup to
   add — only manager wiring):
   - the `setupProductionControllers` slice, and
   - the `setupDevelopmentControllers` `controllerMap` (add the dev key to the
     `--controllers` default flag string too, around the `flag.String("controllers", ...)`).

   Construct with `mgr.GetClient()`, `mgr.GetScheme()`,
   `mgr.GetEventRecorderFor("neo4j-thing-controller")`,
   `RequeueAfter: controller.GetTestRequeueAfter()`, and your
   `validation.NewThingValidator(mgr.GetClient())`. The `+kubebuilder:scaffold:*`
   markers in `cmd/main.go` are anchors for `operator-sdk`/kubebuilder scaffolding
   — leave them in place.

7. **Add a `Label()`-tagged integration spec** under `test/integration/`. Every
   spec's top-level `Describe` **must** carry `Label("core")` or `Label("extended")`
   or it runs in neither CI lane — `Describe("Neo4jRole end-to-end", Label("core"), func() {…})`
   is the pattern (`test/integration/neo4jrole_test.go`). Include the mandatory
   `AfterEach` that strips finalizers before delete (see `CLAUDE.md` → Testing).
   Run a tier locally:
   ```bash
   ginkgo run --label-filter='core' ./test/integration/...
   make test-one TEST="your spec text"   # single integration test
   make test-unit                        # no cluster
   ```

8. **Prove no generated drift before you push** — markers, deepcopy, CRD bases,
   Helm CRDs/RBAC, and the bundle all regenerate from your changes:
   ```bash
   make fmt vet lint
   make sync-all          # every generator (manifests + generate + helm sync + …)
   make check-drift       # CI gate: regenerates + bundles, fails on any diff
   ```
   `check-drift` and `unit-tests` are the **blocking** CI gates — a stale
   `config/rbac/role.yaml` (or unran `make generate`) fails the build.

## Guardrails

- **No webhook. Ever.** No `_webhook.go`, no `ValidatingWebhookConfiguration` /
  `MutatingWebhookConfiguration`, no webhook server in `cmd/main.go`. All
  validation is inline in `internal/validation/`, invoked from `Reconcile`
  (invariant 1).
- **Never hand-edit generated files** — `config/rbac/role.yaml`,
  `config/crd/bases/*`, `api/v1beta1/zz_generated.deepcopy.go`, and the Helm/bundle
  mirrors all carry generation provenance and are reverted by `check-drift`.
  Edit the `+kubebuilder:rbac:` markers / Go types and run `make sync-all`.
- **Wire BOTH controller maps in `cmd/main.go`** — a controller present only in
  `setupProductionControllers` silently won't load in dev mode (`--mode=dev`),
  and vice-versa.
- **Don't run the operator out-of-cluster** to test it (`make dev-run` is
  banned — DNS resolution fails); deploy into Kind. Kind is the only supported
  dev/test/CI environment (invariant 2).
- **Label every new spec** `core` or `extended`, and keep the finalizer-stripping
  `AfterEach`.
- `make check-invariants` / `make check-knowledge-drift` (the `Invariant Guards (advisory)`
  CI job) are **advisory and non-blocking** — they're a quality signal, not a
  merge gate. The only blocking gates are `check-drift` and `unit-tests`.

## Why this exists / provenance

The reconcile contract here is load-bearing and easy to half-implement: a
missing `retry.RetryOnConflict` wrap shows up as flakey status writes under
Neo4j 2025.01.0 cluster-formation churn; a missing `Label()` silently drops a
spec from both CI lanes; a forgotten `make manifests` ships an operator whose
`config/rbac/role.yaml` can't read its own CRD; and the strongest temptation —
reaching for an admission webhook to enforce a cross-object rule — directly
violates invariant 1. This skill encodes the shape every existing reconciler
(`neo4jrole_controller.go`, `neo4jbackup_controller.go`, the rest) already
follows, and the regeneration/drift loop that keeps the wiring honest, so a new
controller lands the-project-way on the first pass.
