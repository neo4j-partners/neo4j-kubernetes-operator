# BDR-012 — Neo4j identity management (users, roles, grants)

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-22 |
| **Reviewers** | Charles Boudry |
| **Depends on** | [BDR-001](001-single-neo4j-crd.md) — single `Neo4j` workload CRD (accepted) · [BDR-008](008-neo4j-config-surface.md) — `spec.config` passthrough (accepted) |
| **Related** | [BDR-006](007-tls-trust-model.md) (TLS) · V1 auth path in `NEO-2-004` (bootstrap password only) |
| **Reference** | [`export.md`](../../../00-discovery/export.md) §F-14..F-18 · [`20-operator-proposal.md`](../../../00-discovery/20-operator-proposal.md) §3.6 |

---

## Context

Enterprise Neo4j deployments require **database identities** — native users, roles, and fine-grained privileges — often managed today through:

| Today | Impact |
|-------|--------|
| Manual Cypher (`CREATE USER`, `GRANT ROLE`, privilege statements) | No Git review; drift between environments |
| Helm `config.dbms.security.*` for LDAP/Kerberos | Provider config only; no user/role lifecycle in CRDs |
| Bootstrap admin password via Secret on install | Sufficient for V1; not a day-2 RBAC model |
| Ad-hoc automation (Jobs, CI scripts) | Ordering bugs; non-idempotent re-runs |

The operator's **V1** scope covers bootstrap authentication only (`NEO-2-004`): generated or existing password Secret on the `Neo4j` CR. **Declarative identity management is post-V1** — this BDR defines the API and reconciliation model before CRD folders are authored.

### Scope of this BDR

| In scope | Out of scope (other BDRs / phases) |
|----------|-------------------------------------|
| Native Neo4j **users**, **roles**, **privileges** as Kubernetes CRDs | **Cloud workload identity** (IRSA / GKE WI / Azure MI) — backup/storage ([BDR-005](../005-storage-volume-mode.md), `NEO-3-006-CLD-*`) |
| Reconcile order, idempotency, audit Events | **Full LDAP/Kerberos directory sync** — operator consumes provider config; does not replicate external directories (reference PRD N-3) |
| `clusterRef` / `neo4jRef` binding to a `Neo4j` instance | **Kubernetes RBAC** for the operator itself ([BDR-003](../003-operator-install-scope.md)) |
| Status conditions on identity CRDs | **Web UI** for security wizards (product roadmap) |

### Forces

1. **GitOps** — security changes must be reviewable manifests, not runbook Cypher.
2. **Ordering** — roles must exist before grants; grants before user role assignment (reference PRD F-17).
3. **Idempotency** — re-applying the same CR must not flap Cypher or thrash status (reference PRD F-18).
4. **Separation** — identity lifecycle is a different concern from cluster topology ([BDR-002](../002-neo4j-crd-topology.md)) and must not bloat `Neo4j.spec`.
5. **V1 frozen** — no change to MVP: bootstrap password on `Neo4j` only ([`13-v1-scope-lock.md`](../../../00-discovery/13-v1-scope-lock.md)).

---

## Options under review

### Option A — `spec.config` only (Helm parity)

Users configure `dbms.security.*` and run Cypher out-of-band. No identity CRDs.

| Advantages | Disadvantages |
|------------|---------------|
| Zero new CRDs | No declarative RBAC; no operator reconcile |
| Helm migration trivial | Fails GitOps / audit requirements |

**Rejected** for operator value proposition beyond V1.

---

### Option B — `Neo4jUser` + `Neo4jRoleBinding` (minimal pair)

From [`20-operator-proposal.md`](../../../00-discovery/20-operator-proposal.md) §3.6: users reference role **names**; roles are pre-created in Neo4j or via config.

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4jUser
metadata:
  name: app-reader
spec:
  neo4jRef:
    name: my-graph
  username: app-reader
  passwordSecretRef:
    name: app-reader-creds
  roles: [reader]
  databases: [neo4j]
```

| Advantages | Disadvantages |
|------------|---------------|
| Small API surface | Roles/privileges still manual or config-only |
| Familiar K8s `RoleBinding` mental model | No declarative privilege grammar |

---

### Option C — `Neo4jUser` + `Neo4jRole` + `Neo4jGrant` (full declarative RBAC) — **proposer direction**

Three CRDs matching reference PRD F-14..F-16. Operator executes ordered Cypher; stores checksum in annotations to prevent flapping.

```yaml
# Role — inline privileges optional
apiVersion: neo4j.com/v1beta1
kind: Neo4jRole
metadata:
  name: app-reader-role
spec:
  neo4jRef:
    name: my-graph
  privileges:
    - action: GRANT
      privilege: access
      resource: database
      graph: neo4j

---
# Grant — statements array + whenNotMatched policy
apiVersion: neo4j.com/v1beta1
kind: Neo4jGrant
metadata:
  name: app-reader-grants
spec:
  neo4jRef:
    name: my-graph
  target:
    kind: Role
    name: app-reader-role
  statements:
    - "GRANT MATCH {*} ON GRAPH neo4j TO app-reader-role"
  whenNotMatched: error   # error | ignore | replace

---
# User — password in Secret; role names
apiVersion: neo4j.com/v1beta1
kind: Neo4jUser
metadata:
  name: app-reader
spec:
  neo4jRef:
    name: my-graph
  username: app-reader
  passwordSecretRef:
    name: app-reader-creds
    key: password
  roles: [app-reader-role]
  mustChangePassword: false
  suspended: false
```

| Advantages | Disadvantages |
|------------|---------------|
| Full Git-auditable RBAC (SOX / ISO evidence) | Three reconcilers + webhook grammar validation |
| Matches reference operator PRD and enterprise expectations | Privilege syntax must track Neo4j minor versions |
| Clear reconcile ordering contract | Larger test matrix |

---

### Option D — Embedded `spec.auth.users[]` on `Neo4j`

Identity list nested under the workload CR.

| Advantages | Disadvantages |
|------------|---------------|
| Single manifest for small demos | Blurs workload vs security lifecycle |
| | Multi-team RBAC PRs touch cluster CR |
| | Violates single-responsibility of [BDR-001](001-single-neo4j-crd.md) spirit |

**Rejected.**

---

## Cross-cutting rules (all options C+)

| Rule | Rationale |
|------|-----------|
| **`neo4jRef` required** — namespaced `Neo4j` in same namespace | Clear ownership; webhook rejects missing target |
| **Reconcile order: Role → Grant → User** | Reference F-17; avoids Cypher dependency failures |
| **Idempotent Cypher** — checksum annotation on each CR | Reference F-18; identical spec → no-op |
| **Admission validates** username regex, privilege grammar, `whenNotMatched` enum | Fail-fast; reference F-12 |
| **Password only via Secret** — never in CR spec | Same pattern as V1 bootstrap (`NEO-2-004`) |
| **Kubernetes Events** on create/update/delete/failure | Audit trail; UI timeline post-V1 |
| **External auth (LDAP/Kerberos/JWT)** — provider config via Secret mount + `spec.config` keys; **no directory sync** | Reference N-3; operator does not replace IdP |

### Reconcile flow (Option C)

```
Neo4jRoleReconciler
    └── CREATE ROLE / SET PRIVILEGES (inline or deferred to Grant)

Neo4jGrantReconciler
    └── execute statements[]; respect whenNotMatched

Neo4jUserReconciler
    └── CREATE/ALTER USER; GRANT ROLE TO USER; mustChangePassword / suspended
```

**Controller-runtime ordering:** separate controllers with explicit `Watches` or a single **Security** controller with internal queue ordering — implementation detail for ADR; product contract is Role → Grant → User.

### Validation IDs (draft — to land in CRD `validation.md`)

| ID | Rule |
|----|------|
| IDN-001 | `neo4jRef` must resolve to existing `Neo4j` in namespace |
| IDN-002 | `username` matches `^[a-z][a-z0-9_]{2,30}$` |
| IDN-003 | `Neo4jGrant.spec.statements[]` matches privilege grammar |
| IDN-004 | `Neo4jUser.spec.roles[]` — each role must exist or have pending `Neo4jRole` CR |
| IDN-005 | Duplicate role name in namespace → admission error |
| IDN-006 | Cypher execution failure → `Ready=False`, `Reason=CypherError`, Event |

---

## Decision

**Proposed** — Charles Boudry, 2026-06-22.

**We will implement Option C** — three CRDs: **`Neo4jUser`**, **`Neo4jRole`**, **`Neo4jGrant`**, bound to a parent `Neo4j` via `neo4jRef`. **Not in V1.**

1. **V1 unchanged** — bootstrap password / existing Secret on `Neo4j` only (`NEO-2-004`). No identity controllers in MVP.
2. **V2+ deliverable** — CRD folders under `crd-spec/` (`neo4juser`, `neo4jrole`, `neo4jgrant`); reconcilers after backup/restore or in parallel per roadmap prioritisation.
3. **Reconcile order** — Role → Grant → User (mandatory).
4. **Idempotency** — spec hash annotation; identical re-apply produces zero Cypher side effects.
5. **External auth** — mount provider Secret + passthrough `dbms.security.*` via `spec.config` ([BDR-008](008-neo4j-config-surface.md)); no LDAP sync service.
6. **Status** — each identity CR exposes `Ready` + `Error` conditions; aggregate diagnostics on `Neo4j.status` optional (`users` / `roles` stubs in [`status.md`](../../crd-spec/neo4j/status.md)).
7. **FR traceability** — new `NEO-2-0xx` / `NEO-3-0xx` rows to be added when V2 scope is locked; not retrofitted into V1 CSV.

### V1 vs post-V1

| Concern | V1 | Post-V1 (BDR-012) |
|---------|-----|-------------------|
| Admin bootstrap password | `Neo4j` CR + Secret | Unchanged |
| Application users / roles | Manual Cypher or external IdP | `Neo4jUser` / `Neo4jRole` / `Neo4jGrant` |
| LDAP / SSO | Not in V1 FR scope | Provider config mount only |
| Audit trail for RBAC | Git + K8s Events on `Neo4j` only | Events on all identity CRDs |

---

## Consequences

### Positive

- Declarative, reviewable RBAC aligned with enterprise operator reference material.
- Clear separation from workload CR — cluster teams and security teams can own different repos.
- Idempotent reconcile safe for GitOps re-sync.

### Negative

- Privilege grammar and Neo4j version skew require ongoing maintenance.
- Three CRDs increase admission webhook and E2E test surface.
- Does not replace full IAM-style governance (approval workflows, UI) — those remain product layers.

### Neutral

- `Neo4jRoleBinding`-only shortcuts (Option B) can be documented as a **subset** if product later wants a thinner MVP for identity — would be an amendment, not default.
- Cloud workload identity for backup jobs remains independent of this BDR.

---

## References

- [`export.md`](../../../00-discovery/export.md) — F-14..F-18, CW-6, N-3
- [`20-operator-proposal.md`](../../../00-discovery/20-operator-proposal.md) — §3.6 `Neo4jUser`
- [`01-prd/09-api.md`](../../../01-prd/09-api.md) — API phasing
- [`01-prd/13-roadmap.md`](../../../01-prd/13-roadmap.md) — V3+ security track
- Neo4j Operations Manual — [Authentication and authorization](https://neo4j.com/docs/operations-manual/current/authentication-authorization/)
- [BDR-001](001-single-neo4j-crd.md) · [BDR-008](008-neo4j-config-surface.md) · [BDR-003](003-operator-install-scope.md)
