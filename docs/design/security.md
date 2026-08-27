# Security model — mounted Secrets and operator privilege

Why the operator requires explicit opt-in labels on Secrets it mounts, when the Helm chart required
nothing of the sort. The short answer: **the labels do not make the operator more secure than Helm —
they restore the property Helm got for free.** The operator pattern itself opens a privilege bridge,
and these controls close it.

**Status**: `[~]` partial — covers the Secret mount and delegation model (`NEO-005`, `ADD-01`).
Operator-compromise blast radius, PSS / SCC mapping and the full RBAC rationale are still to come
(backlog L-12, fed by ADR-013 / ADR-015).

**Related**: [`examples/secrets/README.md`](../../examples/secrets/README.md) (user-facing
prerequisites), [errors.md](../user-guide/05-reference/errors.md) (reasons
`SecretNotMountable` / `SecretNotDelegated`),
[BDR-005](decision-records/business/neo4j/005-storage-volume-mode.md) (`secretMounts`),
[BDR-006](decision-records/business/neo4j/006-service-exposure-connectivity.md) (`clusterDomain`,
`ADD-01`), [BDR-007](decision-records/business/neo4j/007-tls-trust-model.md) (BYO TLS),
[troubleshooting](../03-user-documentation/operator/04-troubleshooting.md).

---

## The problem the operator creates

### Helm needed no consent mechanism

With `helm install`, **you** create the StatefulSet, with **your** credentials. The chart renders
`volumes: [{secret: {secretName: X}}]` and the API server checks whether *you* may create that pod.

In Kubernetes, the ability to create a pod in a namespace is already treated as equivalent to the
ability to read every Secret in that namespace: you control the pod spec, so you mount what you want
and exfiltrate it. Naming an arbitrary Secret in Helm values therefore granted you **nothing you did
not already have**. No privileged third party acted on your behalf — no deputy, no escalation, no
label needed.

### The operator breaks that property

An operator introduces a **narrower** API (`Neo4j`) whose write permission is *not* equivalent to
`create pods`, yet which causes a third party — the operator, with its own ServiceAccount — to mount
and read Secrets. That is a privilege bridge:

> `write neo4js` becomes, in effect, `read secrets` across the whole watched namespace.

Someone allowed to declare a Neo4j instance, but deliberately *not* allowed to create pods, regains
exactly the capability that restriction was meant to withhold. The opt-in labels exist to close that
bridge.

### Helm 2 had the same bug

The parallel is exact. Helm 2's **Tiller** was a server-side component, frequently running as
cluster-admin, that applied whatever users sent it — a confused deputy. The Helm project's fix was
radical: **delete the deputy.** Helm 3 is client-side and uses only the caller's RBAC.

An operator cannot apply that remedy, because it *is* the deputy by construction. It must therefore
rebuild through consent mechanisms what Helm 3 obtains for free.

### GitOps reintroduces the deputy

Worth noting for teams comparing against their current setup: Helm driven by Argo CD or Flux puts a
privileged controller back in the loop. Whoever can write the values in git inherits that
controller's privileges, and no label mediates it. Against **Helm + GitOps**, these controls are a
net gain rather than a restoration.

---

## The two controls

### `NEO-005` — `neo4j.com/mountable-by-operator: "true"`

Required on **every** Secret the operator mounts: `storage.secretMounts`, BYO TLS material,
`auth.passwordSecretRef`, plugin `licenseSecretRef`. It moves the decision from *whoever writes the
CR* to *whoever owns the Secret*, and shrinks the blast radius from "every Secret in the namespace"
to "the Secrets explicitly offered".

Its value is **entirely conditional on RBAC**:

| Situation | Value |
|-----------|-------|
| CR authors also hold `patch secrets` in the namespace | **None** — they can label the Secret themselves. Pure ceremony. |
| CR authors hold only `neo4js` write | **Real** — an effective capability boundary between the platform team and application teams |

So this control is not a blanket hardening: it is the mechanism that makes *least-privilege
delegation* expressible. Helm structurally cannot express it, because the installer needs full
workload-create rights. That, not "more secure than Helm", is the value proposition.

Operator-generated auth Secrets (`generatePassword: true`) are labelled automatically, so the
default path requires nothing from the user.

### `ADD-01` — `neo4j.com/allowed-for: <Neo4j.metadata.name>`

Required **in addition** on a BYO `auth.passwordSecretRef`, because an auth Secret is not merely
mounted into a pod — the operator **reads `NEO4J_AUTH` itself** to open an administrative Bolt
session (cluster formation). It is a borrowed credential, not a mounted file, so the grant is
narrowed from "any Neo4j CR in this namespace" to one named CR.

The escalation chain this closes: reference another instance's auth Secret **and** steer the
operator's Bolt dial at the victim instance, so the operator runs administrative Cypher against it
with its own credentials. The second half of that chain is closed independently by `AUTH-004` (the
operator always dials the short in-cluster Service name and ignores `connectivity.clusterDomain`).
`ADD-01` is therefore **defence in depth** on an already-closed path.

Its residual value is consequently limited, and worth stating plainly: within a single namespace,
Kubernetes offers no tenancy boundary anyway — co-tenants can read each other's Secrets and exec into
each other's pods directly. Compare Gateway API's `ReferenceGrant`, the same explicit-consent
pattern, which is required **only** for cross-namespace references, precisely because no trust
boundary is crossed within a namespace. Keeping `ADD-01` is defensible; it is not free, and it is the
control to revisit first if the two-label onboarding cost proves too high.

---

## What these labels do **not** protect

A reader could mistake them for a confidentiality guarantee against the operator. They are not.

The operator's Role grants `get, list, watch` (and `create, update, patch, delete`) on **all**
Secrets in the watched namespaces, with no `resourceNames` scoping. The manager cache declares no
`ByObject` selector for Secrets, so the informer **lists and watches every Secret in the namespace**
and holds its `data` in memory. `EnsureMountable` reads the Secret first and evaluates the label
second: at the moment the operator refuses a Secret, it already has its contents.

So the property is:

- **Enforced:** the operator will not *propagate* the Secret to somewhere the CR author can reach (a
  volume in their pod), nor *use* it as a credential.
- **Not enforced:** the operator's own access. If the operator is compromised, logs an object, or its
  ServiceAccount token leaks, every Secret in the namespace is exposed regardless of labels.

It is a control on the **output** side, not the input side — a "must not", never a "cannot".

Turning it into a "cannot" is structurally impossible with RBAC alone: **Kubernetes RBAC cannot
filter by label**, only by `resourceNames`, which would require knowing Secret names ahead of time.
The one realistic lever is a cache label selector (`cache.Options.ByObject`), so unlabelled Secrets
are never loaded — a real reduction in exposure and in memory footprint, but still not a boundary
(the ServiceAccount can always issue a direct, uncached `Get`), and it would degrade the diagnostics
below from "label missing" to "secret not found" unless the validation path uses an uncached
`APIReader`.

---

## Enforcement points and diagnostics

The same check runs on two paths:

| Path | When | Availability |
|------|------|--------------|
| Validating webhook (`ValidateCreate` / `ValidateUpdate`) | at `kubectl apply` | **off by default** (`--enable-webhooks` unset; chart `webhook.enabled: false`) |
| Reconcile (first pipeline step, before any operand is rendered) | shortly after apply | always |

The label check requires an API read, so it is out of reach for CEL by construction — it cannot be
expressed as a CRD `XValidation` rule. With the webhook disabled, enforcement is therefore
*a posteriori*, which is sufficient to block the escalation (no StatefulSet is ever created) but
means the CR is accepted and then refused.

Because that refusal is invisible in a plain `kubectl get`, it is surfaced with a stable identifier
on two surfaces at once:

- `status.conditions[type=Error]` with reason `SecretNotMountable` (NEO-005) or
  `SecretNotDelegated` (ADD-01) — the machine-readable contract, catalogued in
  `src/internal/oracle/catalog.go` and projected onto
  [errors.md](../user-guide/05-reference/errors.md);
- a `Warning` Event under the **same** reason, so `kubectl describe neo4j <name>` explains why
  nothing was deployed.

Tests assert on the reason, never on the message text. End-to-end coverage lives in the
`feature-credentials` suite (`secret-ref-unlabeled`, `secret-ref-not-delegated`), which also asserts
that no StatefulSet exists — the actual security property.

---

## Alternatives considered

| Option | Why not chosen |
|--------|----------------|
| **No control** — treat `Neo4j` write as a privileged operation, as most operators do (CNPG, Strimzi, ECK, Prometheus Operator all reference Secrets by name with no opt-in) | Makes `write neo4js` equivalent to `read secrets`; forecloses the least-privilege delegation model this operator wants to support |
| **RBAC `resourceNames` scoping** on the operator Role | Secret names come from user CRs at runtime and cannot be enumerated when the Role is written |
| **CEL / `XValidation`** on the CRD | Cannot read other objects; the label lives on the Secret, not on the CR |
| **A delegation CRD** (`ReferenceGrant`-style object instead of a label) | Heavier API surface and an extra kind for a same-namespace reference, where no trust boundary is crossed |
| **Cache label selector only**, no validation | Not a boundary (uncached `Get` still works) and destroys the diagnostics; complementary, not a substitute |
| **Delegate to cluster policy engines** (PSA, ValidatingAdmissionPolicy, Kyverno) | Cannot express "this operator may mount only Secrets its owner offered"; useful alongside, not instead |

---

## Identifier reference

These IDs are cited across the CRD godoc, validation tables, examples and error messages, so they
are defined here once:

| ID | Scope | Rule |
|----|-------|------|
| `NEO-005` | Every Secret the operator mounts | Requires label `neo4j.com/mountable-by-operator=true`; projections must list named `items`; `serviceAccountToken` / `downwardAPI` / `clusterTrustBundle` sources are rejected |
| `ADD-01` | BYO `auth.passwordSecretRef` only | Requires label `neo4j.com/allowed-for=<Neo4j.metadata.name>`, or an operator-managed auth Secret for this instance. Companion rule `AUTH-004`: the operator's admin Bolt dial ignores `connectivity.clusterDomain` |

Validation rule IDs (`STO-011`, `STO-012`, `AUTH-002b`, `AUTH-002c`, `TLS-008`…`TLS-010`) are listed
in [validation.md](crd-spec/neo4j/validation.md).
