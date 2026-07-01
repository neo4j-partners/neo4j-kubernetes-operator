# Operator benchmark — {OPERATOR_NAME}

| | |
|---|---|
| **Tier** | 1 / 2 / 3 |
| **Repo** | |
| **Version studied** | tag / commit |
| **Date** | YYYY-MM-DD |
| **Analyst** | |

---

## Executive summary

2–3 sentences: what this operator does well / poorly relative to Neo4j operator goals.

---

## D1 — Repository layout

```
(tree snapshot or described packages)
```

| Area | Path | Notes |
|------|------|-------|
| API types | | |
| Controllers | | |
| Builders / manifests | | |
| Webhooks | | |
| Tests | | |
| Deploy / config | | |

**Neo4j takeaway**:

---

## D2 — Internal layering

| Layer | Present? | Package(s) | Notes |
|-------|----------|------------|-------|
| Pure builders | | | |
| Domain / reconcile logic | | | |
| Thin controllers | | | |
| Shared admin client | | | |

**Neo4j takeaway**:

---

## D3 — CRD & controller count

| CRD | Controller | Notes |
|-----|------------|-------|

**Neo4j takeaway**:

---

## D4 — Go & Kubernetes dependencies

| Dep | Version | Pin policy |
|-----|---------|------------|
| go | | |
| controller-runtime | | |
| k8s.io/* | | |

**Neo4j takeaway**:

---

## D5 — Third-party libraries

| Library | Purpose | In operator pod? |
|---------|---------|------------------|

**Neo4j takeaway**:

---

## D6 — Operator RBAC

Paste or summarise ClusterRole/Role rules (group by resource).

| Resource | Verbs | Rationale visible in docs? |
|----------|-------|--------------------------|

**Neo4j takeaway**:

---

## D7 — Workload RBAC

Separate ServiceAccount for database pods? Created by operator or chart?

**Neo4j takeaway**:

---

## D8 — Watch scope

| Mode | Supported | Default | Config |
|------|-----------|---------|--------|

**Neo4j takeaway** (links BDR-003):

---

## D9 — Webhooks

| Webhook | Deploy | TLS | failurePolicy |
|---------|--------|-----|---------------|

**Neo4j takeaway**:

---

## D10 — Admission vs reconcile split

What is validated at admission vs async in reconciler?

**Neo4j takeaway** (compare ADR-001):

---

## D11 — Status & conditions

| Condition type | When set | Pool-level? |
|----------------|----------|-------------|

**Neo4j takeaway**:

---

## D12 — Formation / day-2

How does scale-out / membership work?

**Neo4j takeaway** (Bolt / ENABLE SERVER analogue):

---

## D13 — Backup & cloud identity

| Cloud | Mechanism | Static creds? |
|-------|-----------|---------------|

**Neo4j takeaway** (V2+):

---

## D14 — Platform profiles

Restricted / single-namespace install documented?

**Neo4j takeaway**:

---

## D15 — Testing pyramid

| Tier | Tool | Scope |
|------|------|-------|

**Neo4j takeaway**:

---

## D16 — Quality gates

CI checks: lint, vuln scan, coverage, …

**Neo4j takeaway**:

---

## D17 — Packaging

Helm / OLM / raw YAML — installModes?

**Neo4j takeaway**:

---

## D18 — Release matrix

Operator version ↔ database version policy?

**Neo4j takeaway**:

---

## D19 — Documentation

Architecture docs, RBAC rationale, runbooks?

**Neo4j takeaway**:

---

## D20 — Observability

Metrics exposed, logging patterns?

**Neo4j takeaway**:

---

## Recommendations for Neo4j operator

| Topic | Adopt | Adapt | Avoid | Evidence |
|-------|-------|-------|-------|----------|
| | | | | |

---

## References

- Upstream docs URLs
- Related BDR/ADR in this repo
