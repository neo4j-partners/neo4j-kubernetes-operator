# Changing the code

What a change has to carry with it, where each thing lives, and the one subsystem with rules of its
own — the catalog of condition and Event reasons. For how a change reaches `main` (branches, CI,
fast-forward merges), see [Contributing](01-contributing.md).

## What a change usually has to touch

The reviewer will look for these, so save a round trip:

**API changes** (`src/api/v1beta1/`) need generated output regenerated and committed:

```bash
make generate manifests
```

That refreshes `zz_generated.deepcopy.go` and `config/crd/bases/`. The committed CRD is what
`make install` applies, so a stale one means users install a schema that does not match the code. Do
not run `controller-gen rbac`: the manager Role is hand-maintained per watched namespace and would be
overwritten with a ClusterRole.

**New condition reasons and Event reasons** are declared in `src/internal/oracle/catalog.go` and
nowhere else, then `make errors` regenerates what reads from them. See
[Adding a condition or Event reason](#adding-a-condition-or-event-reason) below.

**New spec fields** need an example under [`examples/`](../../examples/) and a mention in the matching
user-guide topic page, plus a row in
[`feature-status.md`](../user-guide/01-getting-started/feature-status.md) if the capability is
user-visible. A field that exists in the schema and nowhere else is indistinguishable from an inert
one.

**New end-to-end coverage** is recorded in [`tests/coverage.md`](../../tests/coverage.md).

**User-facing behaviour** is documented in the user guide, which is self-contained: it links only to
its own pages and to `examples/`. Design rationale belongs in [`docs/design/`](../design/) instead.

## Adding a condition or Event reason

Every reason the operator can write to `status.conditions[].reason` or to an Event lives in
`src/internal/oracle/catalog.go`
([ADR-014](../design/decision-records/architecture/014-operator-observability.md)). `Reason` and
`Condition` are opaque value types — structs with an unexported field — so no other package can
build one, and unlike a defined string type they do not accept an untyped constant. A reason that is
not declared there cannot be emitted: the operator does not compile.

### 1. Declare it

Group the declaration with the condition it belongs to, and describe it as a user would read it —
the summary *is* the documentation, it is copied verbatim into the reference page:

```go
// A reason that reports a problem, an operation in progress or a decision.
ReasonCertificatePending = declare("CertificatePending", SeverityWarn, SurfaceCondition,
    on(ConditionTLSReady, "Waiting for cert-manager to issue the certificate into the operator-provisioned Secret"))

// A reason that can only mean things are fine — severity is info by construction.
ReasonPVCBound = declareNominal("PVCBound", SurfaceCondition,
    on(ConditionStorageReady, "The data PVC is Bound"))

// Event-only: no condition carries it, the CR stays healthy.
ReasonDuplicateEntry = declare("DuplicateEntry", SeverityWarn, SurfaceEvent,
    asEvent("Two values collided on the same key in a spec field; the Event names the field, the value kept and the one dropped"))
```

Pass several `on(...)` when the same reason appears under two conditions with different meanings, as
`UnsupportedSinglePrimary` does on `ClusterFormed` and on `ServersPendingDrain`.

The three axes to get right:

| Axis | Choose |
|------|--------|
| `Severity` | `SeverityError` when the operator has stopped making progress and needs a human, `SeverityWarn` when it is waiting on something that may resolve itself, `SeverityInfo` for narration |
| `declare` vs `declareNominal` | `declareNominal` only when the reason cannot mean anything bad; those are listed apart in the reference and answer `oracle_nominal` in the e2e harness |
| `Surface` | `SurfaceCondition`, `SurfaceEvent`, or `SurfaceBoth` when the same identifier goes on a condition *and* on an Event — as the `Error` reasons do |

The variable name must be `Reason` + the reason itself (`ReasonPVCBound` for `PVCBound`); a unit test
enforces it, so the identifier and the string a user greps for stay one search apart.

### 2. Emit it

Both `setCondition` helpers — `internal/status` and `internal/domain/formation` — take catalogued
values, so there is nothing to convert:

```go
setCondition(neo4j, oracle.ConditionStorageReady, metav1.ConditionFalse, oracle.ReasonPVCPending, msg)
```

For Events, prefer the advisory memo when the statement is derived from the spec rather than from
the cluster: it records once per `metadata.generation` instead of once per pass, which matters
because client-go budgets Events per object and a repeated advisory starves the next real decision.

```go
r.advisories.Emit(r.Recorder, neo4j, corev1.EventTypeWarning, oracle.ReasonInsecureAdminConnection, msg)
```

Call `.String()` only where a Kubernetes API insists on a plain string, such as
`Recorder.Eventf` or `meta.FindStatusCondition`.

### 3. Regenerate

```bash
make errors
```

That rewrites the tables between the markers of
[`docs/user-guide/05-reference/errors.md`](../user-guide/05-reference/errors.md) — the prose around
them stays hand-written — and `tests/lib/oracle.sh`, the shell lookups the e2e asserts source. Both
are committed, and `make test` then refuses four things: a stale projection, a catalogued reason no
production code emits any more, a mis-named `Reason` variable, and a raw string passed as the reason
argument of an `EventRecorder` call, which is the one API still taking a `string`.

To assert on the new reason end to end, the harness reads the same catalog through
`tests/lib/oracle.sh` — see
[Asserting on a condition reason](../../tests/contribute.md#asserting-on-a-condition-reason).

So declare and emit in the same change. A row added ahead of the code that emits it fails the build
as a documented reason nothing can produce.

## Where the code lives

| Path | Contents |
|------|----------|
| `src/api/v1beta1/` | CRD types, validation markers, CEL rules |
| `src/cmd/manager/` | Entry point, flags, watch scope |
| `src/internal/controller/neo4j/` | The reconciler and its pipeline |
| `src/internal/domain/` | One package per pipeline step: persistence, trust, serverconfig, workload, connectivity, formation |
| `src/internal/render/` | Pure functions building Kubernetes objects from a resource |
| `src/internal/oracle/` | The reason catalog, and the projections `make errors` generates from it |
| `src/internal/status/` | Conditions and phases written from observed state |
| `src/internal/validation/` | Checks that need an API read rather than CEL |
| `config/` | CRD, RBAC, manager manifests |
| `charts/neo4j-operator/` | Helm chart |
| `tests/` | End-to-end harness: suites, actions, fixtures |

The layering is deliberate: `render/` is pure and unit-testable, `domain/` applies what render
produces, and the controller sequences the domains. A pull request that reaches into the API server
from `render/` will be sent back.
