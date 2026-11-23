# ADR-019 — Admin password rotation execution (`Neo4jPasswordRotation`)

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-11-23 |
| **Depends on** | [BDR-017](../business/identity-user-roles/017-admin-password-rotation.md) — rotation contract (Option F, chosen) · [ADR-015](015-backup-and-restore.md) / [BDR-014](../business/backup-restore/014-backup-restore.md) — satellite one-shot CRD pattern reused · [ADR-007](007-formation-and-bolt.md) — formation & Bolt · [ADR-004](004-status-and-conditions.md) — status & conditions · [ADR-001](001-crd-validation-process.md) — CRD validation · [ADR-008](008-finalizers-and-deletion.md) — finalizers · [BDR-006](../business/neo4j/007-tls-trust-model.md) — admin Bolt TLS (NEO-004) |
| **Constraints** | Community + Enterprise · Standalone + Cluster · no StatefulSet roll · password never logged/evented/in-status · operator never generates the password |

---

## Context

[BDR-017](../business/identity-user-roles/017-admin-password-rotation.md) fixes the contract. This ADR decides **how the operator executes** a rotation: the CRD shape, the controller, the one new admin-Bolt verb, the auth-Secret write-back, run-once/recovery semantics, and failure surfacing.

What is fixed by BDR-017 and must not be redefined:

- **Admin-filed, ephemeral record.** The rotation owner stages the **new** password in a *separate* Secret and files a short-lived, spec-immutable `Neo4jPasswordRotation`. The operator never generates or initiates.
- **Old survives until the `ALTER` lands.** The live auth Secret (the old value) is not overwritten until the database change succeeds — this is how the chicken-and-egg is resolved; no persistent shadow Secret.
- **Live `ALTER` over admin Bolt against `system`**, no StatefulSet roll (`NEO4J_AUTH` is bootstrap-only; the auth Secret is in neither the config nor TLS checksum).
- **Uniform across editions and topologies.** The admin session is the same path `Neo4jRestore` already uses on Standalone (Enterprise) and formation uses on Cluster.
- **Never leak.** Only a phase/outcome is surfaced; the password goes over Bolt as a parameter and is never logged.

**Forces / what breaks if we choose wrong:**

- Overwriting the live Secret before the `ALTER` is lossy — the operator loses the credential it needs to authenticate.
- Stamping an auth checksum would roll the StatefulSet for a change the running DBMS takes live — needless downtime.
- A rotation that races formation ([ADR-007](007-formation-and-bolt.md)) authenticates before the system DB answers → spurious failure.
- A non-idempotent run re-issues the `ALTER` after a crash with the wrong credential, or leaves the Secret and DB divergent → formation/restore/backup auth breaks.
- Interpolating the password as a Cypher literal is an injection / quoting hazard — it must be a Bolt parameter.

---

## Analysis

### Option A — Dedicated `Neo4jPasswordRotation` controller, `ALTER` over Bolt (reuse `formation.Dial`) — **chosen**

A new satellite controller mirrors `RestoreReconciler`: it watches `Neo4jPasswordRotation`, dials admin Bolt through `formation.Dial`, issues one `ALTER USER … SET PASSWORD` against `system`, then writes the new value into the live auth Secret and mirrors the outcome into `status.phase`.

| Advantages | Disadvantages |
|------------|---------------|
| Reuses the proven satellite pattern (`RunPhase`, spec-immutable, `formation.Dial`) | A new CRD group + controller + RBAC to maintain |
| Explicit per-op record, RBAC, and audit; decoupled from the core hot loop | Two Secrets in play (live + staged new) during the op |
| Bolt is already the house primitive for live DDL (restore) | — |

### Option B — Annotation on `Neo4j`, handled by the core reconciler

`neo4j.com/rotate-password` on the `Neo4j` CR; the core reconciler performs the rotation inline.

| Advantages | Disadvantages |
|------------|---------------|
| No new CRD | Couples a one-shot, credential-sensitive op to the hot reconcile loop |
| | No per-op record, no distinct RBAC, weaker audit |
| | Rotation permission = edit the whole `Neo4j` CR |

**Rejected** — loses the audit/RBAC/record benefits that motivated Option F in BDR-017.

### Option C — Kubernetes `Job` running `cypher-shell ALTER`

A `Job` (neo4j image) runs the `ALTER` via `cypher-shell`.

| Advantages | Disadvantages |
|------------|---------------|
| Consistent with the backup Job model | Must mount both credentials into the Job pod — wider secret exposure |
| | No advantage over Bolt, which the operator already speaks for live DDL |
| | Harder to implement try-old-then-new recovery than an in-process reconcile |

**Rejected** — backup uses a Job because it shells out to `neo4j-admin` + cloud SDKs; a single `ALTER` is live DDL, which is exactly what Bolt + `formation.Dial` already do (restore).

---

## Comparison

| Criterion | A — satellite + Bolt (chosen) | B — annotation + core loop | C — Job + cypher-shell |
|-----------|-------------------------------|----------------------------|------------------------|
| Per-op record / audit | **native** | none | native (Job) |
| RBAC isolation | **dedicated CRD** | couples to `Neo4j` edit | dedicated, but mounts secrets |
| Reuse of existing patterns | **high** (restore) | medium | medium (backup) |
| Secret exposure | live + staged (control-plane) | live (control-plane) | mounted into Job pod |
| Recovery logic (try old→new) | **in-process, simple** | in hot loop | awkward across Job runs |

---

## Decision

We will use **Option A** — a dedicated `Neo4jPasswordRotation` controller that applies the rotation as one `ALTER` over admin Bolt, reusing `formation.Dial`, then writes the new value back into the live auth Secret.

### CRD

```go
// api/v1/neo4jpasswordrotation_types.go  (shape, not final)
type Neo4jPasswordRotationSpec struct {
    Neo4jRef             Neo4jRef          `json:"neo4jRef"`             // target in the same namespace
    NewPasswordSecretRef PasswordSecretRef `json:"newPasswordSecretRef"` // Secret holding NEO4J_AUTH: neo4j/<new>
    // DeleteNewSecretOnSuccess opts the operator into deleting the staged Secret once applied.
    DeleteNewSecretOnSuccess bool `json:"deleteNewSecretOnSuccess,omitempty"`
}
type Neo4jPasswordRotationStatus struct {
    Phase              RunPhase           `json:"phase,omitempty"` // Pending|Running|Succeeded|Failed (reused)
    Conditions         []metav1.Condition `json:"conditions,omitempty"`
    ObservedGeneration int64              `json:"observedGeneration,omitempty"`
    Reason             string             `json:"reason,omitempty"`
    Message            string             `json:"message,omitempty"`
}
```

Markers mirror `Neo4jRestore`: `+subresource:status`, `+storageversion`, `shortName=n4jpr`, print columns (Cluster / Phase / Reason / Age), and a CEL `self.spec == oldSelf.spec` immutability guard (a rotation is a run-to-completion record — re-rotate by filing a new object). Both Secrets reuse `PasswordSecretRef` and the `neo4j.com/mountable-by-operator` + `neo4j.com/allowed-for` labels (NEO-005 / ADD-01).

### Reconcile flow (run-once; try-old-then-new recovery)

```go
// sketch — not binding
func (r *RotationReconciler) Reconcile(ctx, req) (Result, error) {
    rot := get(req)
    if rot.Status.Phase == Succeeded || rot.Status.Phase == Failed {
        return done // immutable record; never re-run
    }
    n4j := getNeo4j(rot.Spec.Neo4jRef)                 // TargetNotFound → Failed
    if !Ready(n4j) {                                    // same gate restore uses (standalone+cluster)
        return retryable(RotationBeforeReady)
    }
    oldAuth := readAuthSecret(n4j)                      // live Secret = OLD; delegation-guarded
    newAuth := readSecret(rot.Spec.NewPasswordSecretRef)// staged = NEW;  delegation-guarded
    if isNone(oldAuth) { return succeed(AuthDisabledSkipped) } // NEO4J_AUTH: none → N/A

    admin, err := dialWith(ctx, n4j, oldAuth)           // formation.Dial (NEO-004 TLS)
    if err != nil {
        if admin, err = dialWith(ctx, n4j, newAuth); err == nil {
            // already applied (crashed after ALTER, before write-back) → converge
            writeAuthSecret(n4j, newAuth); return succeed(RotationCompleted)
        }
        return retryable(OldAuthFailed)                 // neither works → likely out-of-band drift
    }
    defer admin.Close(ctx)
    if err := admin.AlterUserPassword(ctx, "neo4j", newAuth.password); err != nil {
        return fail(AlterFailed, err)
    }
    if err := writeAuthSecret(n4j, newAuth); err != nil { // operator is authoritative during the op
        return retryable(SecretWriteBackFailed)         // ALTER already done; retry converges
    }
    if rot.Spec.DeleteNewSecretOnSuccess { deleteSecret(rot.Spec.NewPasswordSecretRef) }
    return succeed(RotationCompleted)                    // Event names the outcome, never the value
}
```

`status.phase` is the run-once guard. The order — **`ALTER` first, write-back second** — is what preserves the old value until the change lands; a crash between them is recovered by the try-new branch.

### New admin-Bolt verb

One method on the allowlisted `Admin` interface ([ADR-007](007-formation-and-bolt.md)), parameterized exactly like the existing `CreateUserIfNotExists` (no literal interpolation), against `system`:

```go
func (a *driverAdmin) AlterUserPassword(ctx context.Context, user, password string) error {
    _, err := neo4j.ExecuteQuery(ctx, a.driver,
        "ALTER USER $user SET PASSWORD $password CHANGE NOT REQUIRED",
        map[string]any{"user": user, "password": password}, neo4j.EagerResultTransformer,
        neo4j.ExecuteQueryWithDatabase("system"))
    return err
}
```

`CHANGE NOT REQUIRED` is load-bearing: without it an admin-set password is flagged for a forced change at next login, which would break the operator's own non-interactive admin session. `ALTER USER … SET PASSWORD` is edition- and topology-agnostic (it runs on `system`), so the same call serves Community/Enterprise and Standalone/Cluster.

### Credential handling & write-back

- Both the live and the staged Secret pass `RequireAuthSecretDelegated` (`render/secrets/mount.go`) — rotation reuses the existing delegation guard unchanged.
- The password crosses the wire only as a Bolt parameter; it is never logged, evented, or written to `status`.
- After the `ALTER`, the operator writes the new value into the **live** auth Secret so every admin consumer (formation, restore, backup) reads the current password. With rotation enabled, the live Secret is the source of truth; a manual out-of-band `ALTER` surfaces as `OldAuthFailed`, not silent repair.

### Partial failure & recovery (no rollback)

If the `ALTER` succeeds but the live-Secret write-back fails, the state is `DB = new`, `live Secret = old`. **We do not roll back** — an `ALTER` new→old would re-introduce the password being retired and contradicts the desired state. The record stays `SecretWriteBackFailed` (retryable) and **rolls forward**: the next pass fails to dial with old (the DB is already new), falls back to new, recognises "already applied", and completes only the write-back — converging on `DB = new, Secret = new` without re-issuing the `ALTER`.

Two consequences, accepted and documented:

- **Divergence window.** Until write-back lands, operator admin consumers that dial with the live Secret (formation, backup, restore) hit a transient auth failure — an availability blip, not data loss, that self-heals on the next reconcile. The write-back requeues immediately to keep it to seconds.
- **The staged Secret is the sole copy of the new password until write-back succeeds.** The operator deletes it only after `Succeeded` (never on the failure path), and the contract requires the owner **not** to delete it before the record reports `Succeeded`. If both copies of the new value are lost while the DB is already on new, neither credential authenticates → terminal `OldAuthFailed`, needing manual recovery via a `system`-db admin path.

### No StatefulSet roll

Rotation must **not** stamp any pod-template checksum. `NEO4J_AUTH` is a bootstrap-only `secretKeyRef` env; the running DBMS takes the `ALTER` live, and the stale pod env is irrelevant until a future pod (re)creation, which legitimately re-reads the then-current Secret.

### Sequencing

Gate on `ConditionReady` before dialing (the same conservative gate `RestoreReconciler` uses, which is satisfied by both Standalone and Cluster). Rotation never touches topology or formation.

### Finalizers

None on the record (consistent with `Neo4jRestore` / `Neo4jBackup`): deleting a rotation record must not undo a password change. The staged Secret is the owner's unless `deleteNewSecretOnSuccess` opts the operator into cleaning it up; a TTL/ownerRef cleanup of the record itself is an optional enhancement.

### RBAC delta

Operator SA gains the `neo4jpasswordrotations` CRD group (+ `/status`), `get`/`update` on the target auth Secret, and `get`/`delete` (delete only for the opt-in) on the staged Secret — `resourceNames`-scoped where the label contract allows.

### Failure modes & conditions

Every failure sets `status.phase=Failed` (or a retryable requeue) with a stable `reason`, an Event, and a mirror into [`errors.md`](../../../user-guide/05-reference/errors.md):

| `reason` | When | Retriable |
|----------|------|-----------|
| `TargetNotFound` | `neo4jRef` does not resolve | no |
| `RotationBeforeReady` | target not `Ready` yet | yes (requeue) |
| `NewPasswordSecretMissing` / `NewPasswordInvalid` | staged Secret absent or not `user/password` | no |
| `AuthSecretUndelegated` | either Secret fails the delegation guard | no |
| `AuthDisabledSkipped` | `NEO4J_AUTH: none` — nothing to rotate | n/a (Succeeded/Skipped) |
| `OldAuthFailed` | neither old nor new authenticates (out-of-band drift) | yes, then Failed after timeout |
| `AlterFailed` | the `ALTER` statement errored | no |
| `SecretWriteBackFailed` | `ALTER` applied but the live Secret update failed | yes (converges) |

---

## Consequences

### Positive

- Reuses the restore satellite pattern end-to-end — small, familiar review surface.
- Correct by construction: old preserved until the `ALTER` lands; crash recovery via try-old-then-new; run-once via `status.phase`.
- No downtime: live `ALTER`, no StatefulSet roll.
- Uniform across editions and topologies; the password never leaves the control plane except as a Bolt parameter.

### Negative

- A new CRD group, controller, and RBAC rules to maintain.
- The operator is authoritative for the live auth Secret during a rotation — environments that also change the password by hand must stop (surfaces as `OldAuthFailed`).
- Two Secrets exist on-cluster during the window; the staged one lingers unless `deleteNewSecretOnSuccess` is set.

### Neutral

- No new transport: admin Bolt is already opened in Standalone (restore) and Cluster (formation); rotation adds only `AlterUserPassword`.
- An applied fingerprint / last-rotation timestamp on `Neo4j.status.credentials` is an optional observability add ([ADR-004](004-status-and-conditions.md)); the authoritative record is the `Neo4jPasswordRotation` object.
- Community Standalone meets the NEO-004 admin-Bolt TLS precondition for the first time when rotating.

---

## References

- [BDR-017](../business/identity-user-roles/017-admin-password-rotation.md) — rotation contract (this ADR implements Option F)
- [ADR-015](015-backup-and-restore.md) / [BDR-014](../business/backup-restore/014-backup-restore.md) — satellite one-shot CRD pattern reused (`Neo4jRestore`)
- [ADR-007](007-formation-and-bolt.md) — admin Bolt / `formation.Dial` · [ADR-004](004-status-and-conditions.md) — status & conditions · [ADR-001](001-crd-validation-process.md) — CRD validation · [ADR-008](008-finalizers-and-deletion.md) — finalizers
- [BDR-006](../business/neo4j/007-tls-trust-model.md) — admin Bolt TLS (NEO-004)
- `internal/neo4j/driver.go` (`CreateUserIfNotExists` — parameterized `SET PASSWORD` precedent), `internal/controller/neo4jrestore/reconciler.go` (satellite reconciler mirrored), `render/secrets/mount.go` (`RequireAuthSecretDelegated`)
- [Neo4j — manage users (`ALTER USER … SET PASSWORD`)](https://neo4j.com/docs/operations-manual/current/authentication-authorization/manage-users/)
