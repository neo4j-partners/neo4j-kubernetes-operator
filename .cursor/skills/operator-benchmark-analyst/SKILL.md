---
name: operator-benchmark-analyst
description: >-
  Studies reference Kubernetes operators (CNPG, Strimzi, ECK, MongoDB, etc.) for
  code layout, RBAC, dependencies, cloud identity, testing, and quality practices.
  Use before ADR-011+, when comparing operator scope/RBAC, or when expanding
  architecture phase beyond internal reconcile design.
---

# Operator benchmark analyst

## When to use

- **Before** accepting internal-structure ADRs (layering, pipeline) — Phase 2a
- When BDR-003 (install scope) or RBAC needs **evidence**, not opinion
- When designing cloud backup identity (V2+) or platform profiles
- When choosing dependency pins, CI gates, or test pyramid

## Prerequisites

- Catalog: [`operator-benchmark/readme.md`](../../../docs/02-technical-design/architecture/operator-benchmark/readme.md)
- Template: [`operator-benchmark/template.md`](../../../docs/02-technical-design/architecture/operator-benchmark/template.md)
- Platform matrix: [`dependencies.md`](../../../docs/02-technical-design/dependencies.md)
- BDR-003 operator survey (scope modes)

---

## Process

### 1. Pick operators

| Goal | Start with |
|------|------------|
| Code layout + testing | CloudNativePG, Strimzi |
| RBAC + restricted install | ECK, CNPG (`clusterWide=false`) |
| Multi-SA / per-namespace workload | MongoDB Community |
| Backup + cloud | Percona PG, CNPG (Barman) |
| Minimal CRD surface | RabbitMQ Cluster Operator |
| Helm parity baseline | neo4j/helm-charts (not an operator) |

Study **≥2 Tier-1** operators per ADR topic before recommending.

### 2. Clone & orient (read-only)

```bash
# Example — shallow clone to /tmp for analysis
git clone --depth 1 https://github.com/cloudnative-pg/cloudnative-pg /tmp/cnpg
```

Locate quickly:

| Question | Where to look |
|----------|---------------|
| Package layout | `api/`, `internal/controller`, `internal/webhook`, `pkg/` |
| RBAC | `config/rbac/`, `//+kubebuilder:rbac` markers, Helm `templates/clusterrole` |
| Watch scope | `WATCH_NAMESPACE`, `manager.yaml`, main.go flags |
| Webhooks | `config/webhook/`, `internal/webhook` |
| Tests | `internal/*_test.go`, `test/e2e/`, `hack/` |
| Cloud / backup | backup CRD controllers, cloud credentials mounting |
| CI | `.github/workflows/`, `Makefile`, `golangci.yml` |

### 3. Fill template

Write `docs/02-technical-design/architecture/operator-benchmark/operators/{name}.md` using all **D1–D20** dimensions.

**Neo4j takeaway** on every section — mandatory.

### 4. Synthesis

After ≥3 operator sheets, update `operator-benchmark/synthesis.md`:

- Comparison tables (one row per operator, one column per dimension)
- **Adopt / Adapt / Avoid** per topic
- **Open questions** → BDR or ADR candidate

### 5. Feed ADRs

| Synthesis topic | ADR track |
|-----------------|-----------|
| Code layout consensus | ADR-011 |
| go.mod / controller-runtime policy | ADR-012 |
| Operator + workload RBAC | ADR-013 |
| Watch scope & cache | ADR-014 |
| PSS / SCC / securityContext | ADR-015 |
| Cloud identity (WI/IRSA) | ADR-016 |
| Platform annotation strategy | ADR-017 |
| CI & quality gates | ADR-018 |
| Release & compat matrix | ADR-019 |
| Testing pyramid | ADR-020 |

Use **adr-author-neo4j-operator** to draft; cite `operators/{name}.md` in References.

---

## Quality bar

- Every recommendation cites **file path or doc URL** in upstream repo
- Distinguish **default install** vs **production-hardened profile**
- Note **version studied** (tag/commit) — operators drift fast
- Compare to **BDR-003** scope table — extend, don't duplicate
- Flag **anti-patterns** (e.g. reconciler > 500 lines, cloud SDK in hot path) with evidence

---

## Do not

- Copy upstream code into this repo
- Treat one operator as sole authority — triangulate Tier-1
- Put CRD field decisions in benchmark docs — route to BDR
- Skip workload RBAC (D7) — common Neo4j gap in greenfield designs

---

## Launch prompt

```
@operator-benchmark-analyst

Study CloudNativePG and Strimzi for D1-D12, D15-D17.
Write operators/cloudnative-pg.md and operators/strimzi.md.
Update synthesis.md with comparison tables and ADR recommendations.
```
