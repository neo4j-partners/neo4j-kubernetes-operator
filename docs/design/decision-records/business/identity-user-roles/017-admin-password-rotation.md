# BDR-017 — Admin password rotation (day-2 `neo4j` credential)


|                |                                                                                                                                                                                                                                                                                                          |
| -------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Status**     | proposed                                                                                                                                                                                                                                                                                                 |
| **Date**       | 2026-09-16 · amended 2026-11-23 (ephemeral rotation CR; uniform topology & edition support)                                                                                                                                                                                                                                            |
| **Reviewers**  | Marouane Gazanayi                                                                                                                                                                                                                                                                                        |
| **Depends on** | [BDR-012](012-identity-management.md) — identity management, which froze admin auth at bootstrap only (proposed) · [BDR-006](../neo4j/007-tls-trust-model.md) — `spec.trust`, admin Bolt TLS (accepted) · [BDR-009](../neo4j/009-scale-pool-ordinal-semantics.md) — formation / admin session (accepted) |
| **Related**    | [BDR-014](../backup-restore/014-backup-restore.md) — satellite one-shot CRDs (`Neo4jBackup` / `Neo4jRestore`) this reuses as a pattern · V1 auth path `NEO-2-004` (bootstrap password only) · Beta feedback #5                                                                                            |
| **Triggers**   | Future ADR — `Neo4jPasswordRotation` CRD + reconciler (admin `ALTER` via `formation.Dial`), auth-Secret write-back, uniform admin session across topologies & editions (write when coding starts)                                                                                                                             |


---



## Context

We need to **rotate the admin (**`neo4j`**) password** without recreating the deployment or
hand-running Cypher. The operative constraint is that **rotation is driven externally**: a secret
manager (Vault, External Secrets Operator, or similar) or a human admin owns password generation.
The operator must **not** generate or initiate rotation; its only job is to make the live database
accept a value the rotation owner already produced.

### What credentials do today (grounded in the code)


| Fact                                                                                                                                                 | Where                                                                             |
| ---------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| `NEO4J_AUTH` is a **bootstrap-only** credential: the image reads it once, on first boot of an empty store (`neo4j-admin dbms set-initial-password`)  | `render/workload/statefulset.go` (`neo4jContainerEnv`)                            |
| It reaches the pod as an env var via `secretKeyRef` — resolved **once at pod start**, never hot-reloaded                                             | `render/workload/statefulset.go` (`neo4jContainerEnv`)                            |
| The operator **reuses the Secret verbatim** every reconcile; it never writes the DB password. BYO is only validated for existence/usability          | `domain/workload/reconcile.go` (`ensureAuthSecret`, `ensureReferencedAuthSecret`) |
| The operator **re-reads the Secret fresh** on every admin session, so it already picks up whatever value the Secret currently holds — nothing cached | `domain/formation/dial.go`, `domain/formation/reconcile.go` (`ParseAuthSecret`)   |
| The operator **consumes** the password for its admin Bolt session; satellite CRDs (`Neo4jRestore`) reuse the same `formation.Dial` admin connection  | `internal/neo4j/admin.go` (`ParseAuthSecret`), `domain/formation/dial.go`         |
| The auth Secret is in **neither** the config checksum nor the TLS checksum, so changing it does **not** roll the StatefulSet today                   | `render/serverconfig/configmap.go`, `render/trust/checksum.go`                    |
| In **Standalone** *formation* opens no admin session, but satellite ops already dial admin Bolt there (`Neo4jRestore` via `formation.Dial`)          | `controller/neo4jrestore/reconciler.go`, `domain/formation/dial.go`               |


Two consequences fall straight out of these facts:

1. **Writing a Secret alone rotates nothing.** `NEO4J_AUTH` is bootstrap-only; the live password
  lives in the system database and only changes via an authenticated `ALTER USER neo4j SET PASSWORD`.
   The **only** actor that can apply a new value to the database is the operator (over admin Bolt).
2. **Chicken-and-egg.** To run the `ALTER`, the operator must authenticate with the **current** (old)
  password. If the single auth-Secret key is overwritten in place, the old password is destroyed the
   instant the edit lands and the operator can no longer authenticate. **So the old value must survive
  until the `ALTER` succeeds** — that preservation is the heart of the design.

### Scope of this BDR


| In scope                                                                                       | Out of scope (other BDRs / phases)                                             |
| ---------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| Applying an **externally-produced** admin `neo4j` password to the live DB, day-2               | Application **users / roles / grants** ([BDR-012](012-identity-management.md)) |
| How the operator holds **old + new at the same moment** (ephemeral CR vs shadow / two-value)   | Operator **generating** or **initiating** password rotation (explicitly out)   |
| Operator admin-session credential handling, uniform across topologies & editions               | LDAP / external IdP credential lifecycle                                       |
| Status surface (phase, conditions) and Events                                                  | Password **history / reuse** policy (Neo4j-side)                               |


### Forces

1. **External owner holds the password** — the operator may neither generate nor initiate; it only applies what the owner produced.
2. **No recreation, no downtime** — an `ALTER` is a live DBMS operation; the pod must not roll.
3. **Old must survive until the `ALTER` lands** — otherwise the operator cannot authenticate to apply the new one.
4. **Level-triggered reconcile** — the operator re-runs and can crash mid-rotation; the outcome must be idempotent and recoverable.
5. **Self-coupling** — the operator authenticates with the same credential being rotated.
6. **Never leak** — the value stays out of logs, Events, and `status` (a hash only).

---



## Options under review



### Option A — No operator involvement (status quo, documented manual path)

Manual two-step: `ALTER USER neo4j SET PASSWORD '<new>'` via Cypher, then update the auth Secret so
the operator's admin session keeps working.


| Advantages                                | Disadvantages                                                                                            |
| ----------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| Zero new surface                          | Fails the ask; error-prone ordering (Cluster admin session breaks if the Secret is not updated to match) |
| Helm parity (Helm does not rotate either) | Not a day-2 story                                                                                        |


**Fallback only** — the honest answer *if* the operator is forbidden from ever opening an admin session to run the `ALTER`.

### Option B — Operator-generated, operator-initiated rotation

The operator generates a new password on a trigger, holds old→new, runs the `ALTER`, rewrites the Secret.


| Advantages                           | Disadvantages                                                        |
| ------------------------------------ | -------------------------------------------------------------------- |
| Safe handoff: the operator owns both | **Does not fit**: the external owner, not the operator, makes the password |


**Rejected** — the confirmed model has the rotation owner generating the password.

### Option F — Ephemeral `Neo4jPasswordRotation` CR (admin-initiated) — **chosen**

The rotation owner (admin / external tool) stages the **new** password in a separate Secret —
leaving the instance's live auth Secret (the **old** value) untouched — and creates a short-lived,
run-to-completion CR that tells the operator to perform the rotation. This mirrors the existing
satellite one-shot CRDs `Neo4jBackup` / `Neo4jRestore` ([BDR-014](../backup-restore/014-backup-restore.md)).

```yaml
# 1. The rotation owner stages the NEW password in its own Secret (live auth Secret stays = OLD)
apiVersion: v1
kind: Secret
metadata:
  name: neo4j-prod-new-credential
  labels:
    neo4j.com/mountable-by-operator: "true"
    neo4j.com/allowed-for: prod
stringData:
  NEO4J_AUTH: "neo4j/<new>"
---
# 2. The admin files an ephemeral, immutable rotation record
apiVersion: neo4j.com/v1
kind: Neo4jPasswordRotation
metadata:
  name: rotate-prod-20261123
spec:
  neo4jRef:
    name: prod
  newPasswordSecretRef:
    name: neo4j-prod-new-credential   # NEW; OLD is read from the instance's current auth Secret
# status: { phase: Pending|Running|Succeeded|Failed, conditions: [...] }
```

Flow: operator reads **old** from the instance's live auth Secret and **new** from
`newPasswordSecretRef` → opens an admin Bolt session with **old** (reusing `formation.Dial`) →
`ALTER USER neo4j SET PASSWORD <new>` → writes **new** into the instance's live auth Secret →
`phase: Succeeded`, emits `RotationCompleted`. The record is spec-immutable and cleaned up by the
admin or a TTL. The operator holds both values at the decisive moment because the old Secret is not
overwritten until the `ALTER` has landed — no persistent second copy is required.


| Advantages                                                                      | Disadvantages                                                      |
| ------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| Old preserved during the op by construction — resolves the chicken-and-egg      | New CRD + RBAC surface (heavier than an annotation)                |
| Explicit admin ownership, RBAC, and audit trail per rotation                    | Operator opens an admin session and runs `ALTER` (same path restore uses) |
| Per-operation status / conditions / Events, like `Neo4jRestore`                 | A new-password Secret lives briefly on-cluster (TTL-cleaned)       |
| Reuses existing patterns: satellite CRD, `RunPhase`, spec-immutable, `formation.Dial` | Operator becomes authoritative for the live auth Secret during the op |
| No persistent operator-owned shadow Secret                                      |                                                                    |


### Option C — External write + operator-owned last-applied (shadow) Secret — **alternative**

No rotation CR: the tool overwrites the live auth Secret in place, and the operator keeps its own
last-applied Secret so it still holds the prior value to authenticate (read old from the shadow,
`ALTER` to the desired value, update the shadow; recover with try-old-then-new). Retained as a
fallback where admins cannot file a CR and prefer a pure secret-watch trigger (the existing
`mapSecretToNeo4j` watch already wakes reconcile).


| Advantages                                            | Disadvantages                                              |
| ----------------------------------------------------- | ---------------------------------------------------------- |
| No new CRD; triggered by the existing Secret watch    | Operator persists a second copy of the password on-cluster |
| Fits a tool that can only overwrite in place          | No explicit per-op audit object; drift surfaces as `AuthFailed` |


### Option D — Two-value Secret contract (`current` + `previous`) — **alternative**

The Secret carries both values during the rotation window (`NEO4J_AUTH` + `NEO4J_AUTH_PREVIOUS`).


| Advantages                                       | Disadvantages                                                           |
| ------------------------------------------------ | ----------------------------------------------------------------------- |
| No operator-owned Secret; no rotation CR         | The tool must emit the **previous** value, which Vault / ESO rarely can  |
| Survives crash mid-rotation (current, then prev) | Couples the contract to tooling capability                              |


### Option E — Single-key edit, no durable memory — **rejected**

Operator detects the changed single key and runs `ALTER`, caching old only in memory.


| Advantages            | Disadvantages                                                                                                     |
| --------------------- | ----------------------------------------------------------------------------------------------------------------- |
| Simplest mental model | **Lossy**: the old password is gone once the key is overwritten; an in-memory cache is lost on restart → no `ALTER` |


---



## Cross-cutting rules (chosen: Option F)


| Rule                                                         | Rationale                                                                                                                                                                                                                                                                        |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Old auth Secret is left intact until the `ALTER` lands**   | This is the mechanism that resolves the chicken-and-egg: the new value is staged in a *separate* Secret, so the operator authenticates with the still-live old value, applies the `ALTER`, then writes the new value into the live Secret.                                         |
| **No StatefulSet roll on password change**                   | Already true — the auth Secret is in neither the config nor the TLS checksum, and `NEO4J_AUTH` is a bootstrap-only `secretKeyRef` env Kubernetes never hot-reloads. The `ALTER` takes effect live. Rotation must **not regress** this by stamping a checksum.                     |
| **Run-to-completion, spec-immutable record**                 | Mirror `Neo4jRestore`: `RunPhase` (Pending/Running/Succeeded/Failed), a CEL `self.spec == oldSelf.spec` guard, status subresource + print columns. Recovery is per-CR and idempotent (re-auth with new ⇒ already applied ⇒ Succeeded).                                            |
| **Reuse the** `neo4j.com/allowed-for` **delegation guard**   | The operator already gates *using* a Secret as a credential (`RequireAuthSecretDelegated`, `render/secrets/mount.go`); both the live and the new-password Secret are gated unchanged.                                                                                            |
| **Admin session is uniform across topologies & editions**    | Rotation opens an admin Bolt session the same way everywhere, reusing the `formation.Dial` path `Neo4jRestore` already uses in **Standalone** (Enterprise). The standard [BDR-006](../neo4j/007-tls-trust-model.md) NEO-004 rules (verified bolt TLS or `insecureAdminConnection`) apply uniformly; **Community Standalone** meets that precondition for the first time, but it is a normal supported requirement, not special scope. |
| **Operator is authoritative for the live auth Secret during a rotation** | After a successful `ALTER`, the operator writes the new value into the instance's auth Secret so every admin consumer (formation, restore) stays consistent. A manual out-of-band `ALTER` surfaces as an auth-failure, not silent repair.                               |
| **New-password Secret is short-lived**                       | It holds plaintext credentials; TTL-clean it after the rotation completes to avoid lingering copies.                                                                                                                                                                             |
| **Auth disabled (**`NEO4J_AUTH: none`**) → rotation is N/A** | No credential to rotate.                                                                                                                                                                                                                                                         |
| **Never in logs / Events / status**                          | Events name the outcome, never the value; `status` carries a phase and (optionally) a fingerprint, never the secret.                                                                                                                                                             |



### Status surface

The `Neo4jPasswordRotation` record owns its own `status.phase` + conditions, like `Neo4jRestore`.
On the `Neo4j` side, `status.credentials` (today `secretName` + `generated`) may gain a last-rotation
timestamp / fingerprint for observability. Exact condition catalog and reason strings are owned by
[status.md](../../../crd-spec/neo4j/status.md) and `src/internal/oracle` (ADR-014), not this record.

---



## Decision

**Proposed.**

1. **Option F chosen** — an ephemeral, admin-created `Neo4jPasswordRotation` satellite CR drives the
  rotation. The owner stages the new password in a separate Secret; the operator authenticates with
   the still-live old value, runs `ALTER USER neo4j SET PASSWORD`, then writes the new value into the
  instance's auth Secret. No persistent shadow Secret, no pod roll.
2. **The old value is preserved by not overwriting the live Secret until the `ALTER` succeeds** — this
  is how the chicken-and-egg is resolved.
3. **Option B rejected** — the operator does not generate or initiate rotation.
4. **Option E rejected** — single-key in-place edit without durable memory is structurally lossy.
5. **Options C (shadow Secret) and D (two-value Secret) are alternatives** for environments that cannot
  file a CR and prefer a pure secret-driven trigger; they are not the primary path.
6. **Operator must open an admin session and run the `ALTER`** — "we do not rotate ourselves" means the
  operator neither chooses nor generates the password, not that it never touches the database. If opening
   an admin session is itself unacceptable, the only coherent fallback is **Option A** (manual).
7. **Uniform support** — rotation works the same in Standalone and Cluster, Community and Enterprise. The
  operator opens its admin Bolt session the same way everywhere (the path `Neo4jRestore` already uses in
  Standalone), under the standard NEO-004 TLS rules ([BDR-006](../neo4j/007-tls-trust-model.md)).
8. **No recreation** — rotation never rolls the StatefulSet.



### Helm parity


| Concern                  | Helm chart                     | This BDR                                                      |
| ------------------------ | ------------------------------ | ------------------------------------------------------------- |
| Bootstrap admin password | `NEO4J_AUTH` Secret on install | Unchanged (`NEO-2-004`)                                       |
| Day-2 rotation           | None                           | Net-new: an admin-filed `Neo4jPasswordRotation` satellite CR  |


---



## Consequences



### Positive

- Resolves the chicken-and-egg by construction (old survives until the `ALTER` lands) — no persistent shadow Secret.
- Explicit, auditable, RBAC-gated per-rotation object; rotation permission is decoupled from editing the `Neo4j` CR.
- Reuses established patterns (`Neo4jRestore`: satellite CRD, `RunPhase`, spec-immutable, `formation.Dial`), so implementation cost and review surface are familiar.
- No recreation — the no-roll behaviour already holds today and is preserved.
- Fits an external secret manager: the owner stages the new value and files the CR; the operator never generates a password.



### Negative

- New CRD, controller, and RBAC — more surface than an annotation.
- Community Standalone meets the NEO-004 admin-Bolt TLS precondition (verified bolt TLS or `insecureAdminConnection`) for the first time when rotating; Enterprise and Cluster already satisfy it via restore/formation.
- A new-password Secret holds plaintext on-cluster for the duration of the op (TTL-cleaned).
- During a rotation the operator becomes authoritative for the live auth Secret's value; environments that also change it by hand must stop (out-of-band `ALTER` → auth-failure condition).



### Neutral

- No new transport: the operator already opens admin Bolt in Standalone via `Neo4jRestore` (Enterprise) and in Cluster via formation; rotation reuses the same `formation.Dial` one-shot, adding only one allowlisted `Admin` method (`ALTER USER … SET PASSWORD`).
- Backup/restore and formation already read the live auth Secret fresh, so they stay consistent once the operator writes the new value post-`ALTER`.
- The CRD name/shape (`Neo4jPasswordRotation`, `newPasswordSecretRef`, TTL) is an implementation detail for the triggering ADR; this BDR fixes the mechanism, not the field spelling.

---



## References

- Beta feedback #5 — admin credential rotation
- Neo4j Operations Manual — [Password and user management](https://neo4j.com/docs/operations-manual/current/authentication-authorization/manage-users/)
- [BDR-012](012-identity-management.md) — identity management (froze admin auth at bootstrap)
- [BDR-014](../backup-restore/014-backup-restore.md) — satellite one-shot CRD pattern (`Neo4jBackup` / `Neo4jRestore`) reused here
- [BDR-006](../neo4j/007-tls-trust-model.md) — TLS trust / admin Bolt · [BDR-009](../neo4j/009-scale-pool-ordinal-semantics.md) — formation and admin session
- `domain/formation/dial.go`, `domain/formation/reconcile.go`, `internal/neo4j/admin.go`, `render/workload/statefulset.go`, `render/serverconfig/configmap.go`, `render/trust/checksum.go` — bootstrap-only auth path, fresh-read admin session, and the no-roll checksums
