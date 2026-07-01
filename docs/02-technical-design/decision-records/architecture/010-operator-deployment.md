# ADR-010 — Operator deployment and HA

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-06-22 |
| **Depends on** | [BDR-003](../business/operator/003-operator-install-scope.md) · [ADR-001](001-crd-validation-process.md) · [ADR-009](009-watches-and-predicates.md) |
| **Constraints** | V1 single-namespace; Helm + raw YAML install |

---

## Context

The operator itself runs as a Deployment in the target namespace. We must decide leader election, webhook TLS, resource limits, and packaging — aligned with [BDR-003](../business/operator/003-operator-install-scope.md) (single namespace V1).

**Forces:**

- Validating/mutating webhooks ([ADR-001](001-crd-validation-process.md)) — cert volume or cert-manager.
- CNPG: leader election ID, webhook cert dir, metrics on `:8080`, OLM + Helm chart.
- Strimzi: default **single-namespace** via `STRIMZI_NAMESPACE` from downward API; numbered install YAML.

**What breaks if wrong:** split-brain reconciles, webhook cert expiry, operator OOM during e2e clusters.

---

## Analysis

### Option A — Single replica V1; leader election enabled for HA-ready chart (chosen)

Deployment `replicas: 1` default; `--leader-elect=true` so scaling to 2+ is safe without code change.

| Advantages | Disadvantages |
|------------|---------------|
| CNPG/kubebuilder standard | Two replicas idle in V1 default install |
| PS can enable HA later | Webhook must target Service with stable endpoint |

### Option B — No leader election V1

| Advantages | Disadvantages |
|------------|---------------|
| Simpler | Unsafe if replicas > 1 — footgun |

### Option C — OLM-only install

| Advantages | Disadvantages |
|------------|---------------|
| Enterprise OpenShift | Not all V1 targets use OLM |

---

## Comparison

| Criterion | A Leader-elect | B No elect | C OLM only |
|-----------|----------------|------------|------------|
| HA path | **Ready** | Blocked | Ready |
| V1 PS deliverable | **Helm + YAML** | Yes | Partial |
| Strimzi/CNPG alignment | **Yes** | No | CNPG yes |

---

## Decision

We will adopt **Option A** with **Helm chart + `config/deploy/` kustomize overlay** (kubebuilder default).

### Deployment

| Setting | V1 value |
|---------|----------|
| `replicas` | `1` (Helm value `replicaCount`) |
| `leader-elect` | `true` |
| `leader-election-id` | `neo4j.com` (fixed) |
| `WATCH_NAMESPACE` | unset → operator pod namespace only ([BDR-003](../business/operator/003-operator-install-scope.md) Option A) |
| Resources | requests `100m/256Mi`; limits `500m/512Mi` (tune in e2e) |
| Probes | `/healthz`, `/readyz` on metrics port |

**Improvement over Strimzi default:** document `replicaCount: 2` for HA in HA guide — Strimzi uses 1 replica in quick-start YAML too.

### Webhook TLS

| Mode | V1 |
|------|-----|
| cert-manager | Optional Helm subchart / annotation |
| Self-signed | `config/certmanager` kubebuilder pattern — cert rotated by cainjector |

`failurePolicy: Fail` — match CNPG ([cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md) D9).

### RBAC (operator SA)

- V1: **Role** + **RoleBinding** in install namespace (not ClusterRole) — per [BDR-003](../business/operator/003-operator-install-scope.md) Option A.
- Operand RBAC creation requires `roles`/`rolebindings` verbs in that Role — detailed in future ADR-013.

### Packaging artifacts

| Artifact | Path |
|----------|------|
| Kustomize | `config/default`, `config/manager`, `config/rbac` |
| Helm chart | `charts/neo4j-operator/` (or external repo like CNPG) |
| Single YAML | `dist/install.yaml` from `make build-installer` |

Strimzi-style numbered manifests **not** required — Helm + kustomize sufficient V1.

### Metrics & logging

- Metrics: controller-runtime defaults on `:8080`.
- Structured logging: `logr` with `neo4j`/`namespace` keys (Zap development/production via flags).

---

## Consequences

### Positive

- Matches PS single-namespace deliverable; HA is configuration not rewrite.
- Webhook + leader election align with kubebuilder best practices.

### Negative

- ClusterRole / multi-namespace installs need chart overlay — deferred BDR-003 B/C.

### Neutral

- OLM bundle post-V1.

---

## References

- [BDR-003](../business/operator/003-operator-install-scope.md)
- Strimzi `060-Deployment-strimzi-cluster-operator.yaml` — [strimzi.md](../../architecture/operator-benchmark/operators/strimzi.md) D8, D17
- CNPG `controller.go` leader/webhook — [cloudnative-pg.md](../../architecture/operator-benchmark/operators/cloudnative-pg.md) D9, D17
- [ADR-001](001-crd-validation-process.md) · [ADR-009](009-watches-and-predicates.md)
- kubebuilder deployment guide
