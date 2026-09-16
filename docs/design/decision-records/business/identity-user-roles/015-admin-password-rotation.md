# BDR-015 — Admin password rotation (day-2 `neo4j` credential)

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-09-16 |
| **Reviewers** | — |
| **Depends on** | [BDR-012](012-identity-management.md) — identity management, which froze admin auth at bootstrap only (proposed) · [BDR-006](../neo4j/007-tls-trust-model.md) — `spec.trust`, admin Bolt TLS (accepted) · [BDR-009](../neo4j/009-scale-pool-ordinal-semantics.md) — formation / admin session (accepted) |
| **Related** | V1 auth path `NEO-2-004` (bootstrap password only) · Beta feedback #5 (admin credential rotation) |
| **Triggers** | Future ADR — rotation reconcile, formation admin-session credential switch, secret watch (write when coding starts) |

---

## Context

Beta testers asked to **rotate the admin (`neo4j`) password** without recreating the deployment or
hand-running Cypher. Today that is impossible, and the reason is structural.

### What credentials do today (grounded in the code)

| Fact | Where |
|------|-------|
| `NEO4J_AUTH` is a **bootstrap-only** credential: the image reads it once, on first boot of an empty store (`neo4j-admin dbms set-initial-password`) | `render/workload/auth_secret.go`, `render/secrets/auth.go` |
| It reaches the pod as an env var via `secretKeyRef` — resolved **once at pod start**, never hot-reloaded | `render/workload/statefulset.go` (`neo4jContainerEnv`) |
| The operator **reuses the secret verbatim** every reconcile; it never writes the DB password. BYO is only validated for existence/usability | `domain/workload/reconcile.go` (`ensureAuthSecret`, `ensureReferencedAuthSecret`) |
| The operator **consumes** the password to open its own admin Bolt session (formation `ENABLE SERVER` / `SHOW SERVERS` / drain, and one-shot restore) | `domain/formation/dial.go` (`ParseAuthSecret`) |
| In **Standalone** the operator opens no admin session at all | [BDR-009](../neo4j/009-scale-pool-ordinal-semantics.md), clustering guide |

So editing the Secret changes neither the running database nor a running pod. A live password lives
in the database; changing it is a `ALTER USER neo4j SET PASSWORD` over an **authenticated** admin
session — which needs the **current** password. That chicken-and-egg (you must still hold the old
password to set the new one) drives every option below.

### Scope of this BDR

| In scope | Out of scope (other BDRs / phases) |
|----------|-------------------------------------|
| Rotating the **admin `neo4j`** credential day-2, its API surface and contract | Application **users / roles / grants** ([BDR-012](012-identity-management.md)) |
| The old→new credential handoff contract (single vs two-value Secret) | LDAP / external IdP credential lifecycle |
| Operator admin-session credential switch, incl. the Standalone implication | Cloud workload identity for backup jobs (ADR-016) |
| Status surface (applied fingerprint, condition) and Events | Password **history / reuse** policy (Neo4j-side) |

### Forces

1. **No recreation, no downtime** — the tester's explicit ask; an `ALTER` is a live DBMS operation.
2. **Chicken-and-egg** — the actor performing the `ALTER` must authenticate with the current password.
3. **Level-triggered reconcile** — the operator re-runs and can crash mid-rotation; the outcome must be idempotent and recoverable.
4. **Self-coupling** — the operator authenticates with the same credential it would be rotating.
5. **Secret is not the source of truth today** — the database is; any design must reconcile the two without ever diverging long enough to break backup/restore/formation auth.
6. **Never leak** — the value must stay out of logs, Events, and `status`.

---

## Options under review

### Option A — No operator rotation (status quo, documented manual path)

Rotation stays a manual two-step: `ALTER USER neo4j SET PASSWORD '<new>'` via Cypher, then update
the auth Secret's `NEO4J_AUTH` so the operator's admin session keeps working.

| Advantages | Disadvantages |
|------------|---------------|
| Zero new surface | Fails the tester's ask; error-prone ordering (Cluster admin session breaks if the Secret is not updated to match) |
| Helm parity (Helm does not rotate either) | Not a day-2 story |

**Baseline, not the target.**

### Option B — Operator-initiated rotation, generated secrets (trigger) — **proposer direction for v1**

For operator-generated auth (`spec.auth.generatePassword: true`), the user requests a rotation by a
declarative trigger; the operator performs the whole handoff while it still holds both passwords.

```yaml
apiVersion: neo4j.com/v1
kind: Neo4j
metadata:
  name: prod
  annotations:
    neo4j.com/rotate-password: "2026-09-16T22:00:00Z"   # change the value to request a rotation
spec:
  auth:
    generatePassword: true
```

Flow: read old value → generate new → open admin session with **old** → `ALTER USER neo4j SET
PASSWORD` → overwrite the generated Secret → record an applied **fingerprint** in status. The
operator always holds both at the moment it matters, so there is no lossy window. No pod roll.

| Advantages | Disadvantages |
|------------|---------------|
| Safe: the operator owns both old and new | Only covers operator-generated secrets, not vault/BYO |
| No recreation, no downtime | Adds a trigger convention (annotation or `spec.auth` field) |
| Idempotent via fingerprint | Requires an admin session in Standalone (see cross-cutting) |

### Option C — Secret-driven rotation, two-value contract (BYO / vault) — **v1.1 follow-on**

For BYO (`spec.auth.passwordSecretRef`, typically ExternalSecrets / Vault), the Secret carries the
**current and previous** values during the rotation window, so a level-triggered operator that reads
the Secret *after* the vault rotated can still authenticate.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: neo4j-credentials
  labels:
    neo4j.com/mountable-by-operator: "true"
    neo4j.com/allowed-for: prod
stringData:
  NEO4J_AUTH: "neo4j/<new>"            # current desired
  NEO4J_AUTH_PREVIOUS: "neo4j/<old>"   # accepted during the rotation window
```

Flow: operator **watches** the referenced Secret (it is not owner-referenced today, so this needs an
explicit watch + map to the `Neo4j`); on change, authenticate with `NEO4J_AUTH`, and if that fails
fall back to `NEO4J_AUTH_PREVIOUS`, run `ALTER` to the target, record the fingerprint.

| Advantages | Disadvantages |
|------------|---------------|
| Serves the real Vault/JustID workflow the tester runs | Two-value schema the vault tooling must populate |
| Survives crash mid-rotation (try current, then previous) | New watch + map-func; larger test matrix |

### Option D — Single-key in-place edit drives the `ALTER`

User overwrites the single `NEO4J_AUTH` key; the operator detects the change and runs `ALTER`.

| Advantages | Disadvantages |
|------------|---------------|
| Simplest mental model | **Lossy**: once the single key is overwritten the operator no longer holds the old password and cannot authenticate to perform the `ALTER`; caching it in memory is lost on restart |

**Rejected** — the chicken-and-egg makes it unsafe for a level-triggered controller.

---

## Cross-cutting rules (Options B and C)

| Rule | Rationale |
|------|-----------|
| **No StatefulSet roll on password change** | `NEO4J_AUTH` is bootstrap-only; the running DBMS takes the `ALTER` live, so rotation must not stamp a checksum or roll pods (contrast TLS leaf certs, which do). This is what delivers "without recreation". |
| **Applied fingerprint in `status`, never the value** | Level-triggered idempotency needs durable memory of "which password is applied"; store a hash, not the secret. |
| **Try current, then previous** | Recovery after a crash between `ALTER` and Secret update. |
| **Reuse the `neo4j.com/allowed-for` delegation guard** | The operator already gates *using* a Secret as a credential (`RequireAuthSecretDelegated`); rotation reuses it unchanged. |
| **Standalone gains an admin session** | Rotation requires the operator to dial admin Bolt in Standalone, which today it never does — this pulls the [BDR-006](../neo4j/007-tls-trust-model.md) NEO-004 admin-TLS / `insecureAdminConnection` requirement into Standalone. Called out explicitly because it is new scope. |
| **Secret is the source of truth once rotation is enabled** | A manual out-of-band `ALTER` diverges the DB from the Secret; the operator can only detect it as an auth failure. Documented contract, not silent reconciliation. |
| **Auth disabled (`NEO4J_AUTH: none`) → rotation is N/A** | No credential to rotate. |
| **Never in logs / Events / status** | Only a fingerprint and a rotation timestamp are surfaced; Events name the outcome, never the value. |

### Status surface

`status.credentials` already carries `secretName` and `generated`. Rotation adds an applied
**fingerprint** and a last-rotation timestamp, plus a condition to surface rotation progress and
failure. The exact condition catalog and reason strings are owned by
[`status.md`](../../../crd-spec/neo4j/status.md) and `src/internal/oracle` (ADR-014), not this record —
this BDR only decides that such a condition **exists**.

---

## Decision

**Proposed.**

1. **v1.x — Option B** for operator-generated secrets: a declarative trigger drives an
   operator-performed `ALTER` while it holds both passwords, then rewrites the generated Secret and
   records a fingerprint. No pod roll.
2. **v1.1 — Option C** for BYO/vault: a two-value (`current` + `previous`) Secret contract plus a
   watch on the referenced Secret. This is what serves the tester's Vault/JustID setup and is the
   harder half; it does not gate v1.x.
3. **Option D rejected** — single-key in-place edit is structurally lossy for a level-triggered controller.
4. **Standalone** gains an operator admin session when rotation is used, inheriting the NEO-004
   admin-Bolt TLS rules ([BDR-006](../neo4j/007-tls-trust-model.md)).
5. **No recreation** — rotation never rolls the StatefulSet.
6. **Contract** — with rotation enabled, the auth Secret is the source of truth for the `neo4j`
   password; out-of-band Cypher changes surface as an auth-failure condition, not silent repair.

### Helm parity

| Concern | Helm chart | This BDR |
|---------|-----------|----------|
| Bootstrap admin password | `NEO4J_AUTH` Secret on install | Unchanged (`NEO-2-004`) |
| Day-2 rotation | None | Net-new operator capability (B, then C) |

---

## Consequences

### Positive

- Delivers the tester's ask: live rotation, no recreation, no manual Cypher for the common case.
- Idempotent and crash-safe via the applied fingerprint and try-current-then-previous.
- Reuses the existing delegation-label guard; no new trust surface.

### Negative

- Introduces an operator admin session to Standalone, widening the NEO-004 requirement there.
- Option C adds a two-value Secret schema and a watch on a Secret the operator does not own.
- Rotation makes the Secret authoritative; environments that also change the password by hand must stop doing so.

### Neutral

- Backup/restore and formation already read the same Secret, so they stay consistent once the Secret always equals the live password post-rotation.
- The trigger shape (annotation vs `spec.auth` field) is an API detail to settle during v1.x design; both satisfy this contract.

---

## References

- Beta feedback #5 — admin credential rotation
- Neo4j Operations Manual — [Password and user management](https://neo4j.com/docs/operations-manual/current/authentication-authorization/manage-users/)
- [BDR-012](012-identity-management.md) — identity management (froze admin auth at bootstrap)
- [BDR-006](../neo4j/007-tls-trust-model.md) — TLS trust / admin Bolt · [BDR-009](../neo4j/009-scale-pool-ordinal-semantics.md) — formation and admin session
- `domain/workload/reconcile.go`, `domain/formation/dial.go`, `render/workload/statefulset.go` — current bootstrap-only auth path
