# Kyverno Conformance Policies

A pack of [Kyverno](https://kyverno.io/) ClusterPolicies that audit Neo4j
Operator custom resources against the production posture this operator
recommends. Use them as a starting point — copy into a cluster, run in
`Audit` for a release or two, then graduate the ones that fit your
environment to `Enforce`.

## What's here

| File | What it checks | Severity |
|---|---|---|
| `01-enterprise-image.yaml` | `spec.image.tag` ends with `-enterprise` | high |
| `02-tls-required.yaml` | `spec.tls.mode != "disabled"` | medium |
| `03-monitoring-enabled.yaml` | `spec.monitoring.enabled == true` | low |
| `04-runAsNonRoot-not-disabled.yaml` | User does not override `runAsNonRoot=false` | high |
| `05-resource-limits.yaml` | `spec.resources.limits.memory` and `.cpu` are set | medium |

All five policies match only the operator's user-facing CRs
(`Neo4jEnterpriseCluster`, `Neo4jEnterpriseStandalone`). They do **not**
match the operator-emitted StatefulSets, Services, ConfigMaps, or any
other downstream resources, so they do not interfere with reconciliation.

## Will these break my operator or my CRs?

No. By design:

- **`validationFailureAction: Audit`** — every policy ships in Audit mode.
  Violations are surfaced via `PolicyReport` / `ClusterPolicyReport`
  resources and Kyverno's metrics; they do not block CREATE or UPDATE.
- **No `status` interception** — Kyverno's `match.any.resources.kinds`
  matches only the parent resource. Reconciler status writes go through
  the `status` subresource and Kyverno does not validate those, so
  status updates from the operator are never gated by these policies.
- **Defence in depth** — policy 01 (enterprise image) duplicates the
  operator's inline validator (CLAUDE.md guarantees community images are
  rejected). Even if Kyverno is missing or disabled, the operator still
  rejects the CR. The Kyverno pass just surfaces the same error at
  admission time so users see it before a reconcile.

If you graduate a policy to `Enforce`, the policy will reject CREATE /
UPDATE that violates the pattern. The operator itself is never the one
hitting these admission checks — only user-applied CRs are.

## Install

```bash
# 1. Install Kyverno (see https://kyverno.io/docs/installation/)
kubectl create -f https://github.com/kyverno/kyverno/releases/latest/download/install.yaml

# 2. Apply the policy pack
kubectl apply -f examples/security/policies/

# 3. Inspect violations
kubectl get clusterpolicyreport -A
kubectl get policyreport -A
```

## Audit → Enforce migration

When you are ready to move a policy from Audit to Enforce:

```bash
# Inspect what would be blocked
kubectl get clusterpolicyreport -A -o json | jq '.items[].results[] | select(.policy=="neo4j-tls-required" and .result=="fail")'

# Patch a single policy
kubectl patch clusterpolicy neo4j-tls-required \
  --type merge \
  -p '{"spec":{"validationFailureAction":"Enforce"}}'
```

Suggested rollout order:

1. `neo4j-enterprise-image-required` — already enforced by the operator;
   moving to Enforce just shifts the rejection earlier. Safe.
2. `neo4j-runAsNonRoot-not-disabled` — only fires on explicit overrides,
   so most users are not affected.
3. `neo4j-resource-limits-required` — surfaces production-readiness gaps.
4. `neo4j-tls-required` — keep Audit on dev/test namespaces, Enforce on
   prod namespaces (use Kyverno's `match.any.resources.namespaces` to
   scope).
5. `neo4j-monitoring-enabled` — lowest severity, keep in Audit unless
   monitoring is contractually required.

## Expected Audit warnings on the bundled examples

These example CRs intentionally use minimal configuration and will show
up in `PolicyReport` once the pack is applied. They are not bugs — they
are valid configurations for development and the policies are advisory:

| Example | Will fail |
|---|---|
| `examples/clusters/minimal-cluster.yaml` | 02 (tls disabled), 03 (no monitoring) |
| `examples/clusters/auth-example.yaml` | 02, 03 |
| `examples/clusters/cluster-with-read-replicas.yaml` | 02, 03 |
| `examples/clusters/storage-expansion.yaml` | 02, 03 |
| `examples/clusters/three-node-simple.yaml` | 02, 03 |
| `examples/standalone/single-node-standalone.yaml` | 02 |
| `examples/standalone/ldap-standalone.yaml` | 02, 03 |
| `examples/clusters/tls-cluster*.yaml` | 03 (most have monitoring unset) |
| `examples/clusters/production-optimized-cluster.yaml` | 03 |

Most cluster examples that enable TLS still leave `spec.monitoring`
unset; production deployments should set `spec.monitoring.enabled: true`.

## Limitations

- These policies do not validate operator-emitted resources (StatefulSet
  pods, init containers, services). Use Kyverno's own
  [pod-security-standards](https://kyverno.io/policies/pod-security/)
  policies in parallel if you want to gate the pods the operator creates.
- They do not check `Neo4jBackup`, `Neo4jRestore`, `Neo4jDatabase`,
  `Neo4jUser`, `Neo4jRole`, `Neo4jRoleBinding`, `Neo4jPlugin`, or
  `Neo4jAuthRule` CRs. The validation those CRs need is already inline
  in the controllers (see CLAUDE.md rule 26).
- Pattern matching uses Kyverno v1 syntax. If you are on an older
  Kyverno version (<1.10) the `=(field)` conditional anchor and the
  multi-kind `match.any.resources.kinds` form may need adjustment.
