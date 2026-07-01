# Operator benchmark — reference catalog

Living survey for **phase 2a** — study before locking internal ADRs.  
Methodology: skill `@operator-benchmark-analyst` · template: [`template.md`](template.md)

**Status**: Tier-1 **CNPG** + **Strimzi** complete — see [synthesis.md](synthesis.md). ECK + MongoDB pending.

---

## Why benchmark first

Internal layering (render/domain/controller) is one axis. Production operators also differ on:

- **RBAC** footprint and scope modes (BDR-003)
- **Cloud identity** (IRSA, GKE WI, Azure WI) for object-store backups
- **Dependency** policy (controller-runtime pin, minimal third-party)
- **Quality** gates (lint, vuln scan, e2e tier, release matrix)
- **Packaging** (Helm, OLM, restricted profiles)

Decisions on these topics should cite **evidence from this catalog**, not only kubebuilder defaults.

---

## Tier 1 — primary references (study first)

Stateful / database operators closest to Neo4j operator ambitions.

| Operator | Repo | Why study | Priority dimensions |
|----------|------|-----------|---------------------|
| **CloudNativePG** | [cloudnative-pg/cloudnative-pg](https://github.com/cloudnative-pg/cloudnative-pg) | Modern Go, cluster-wide default, excellent status + webhook split | Code layout, RBAC, scope modes, testing, deps |
| **Strimzi** | [strimzi/strimzi-kafka-operator](https://github.com/strimzi/strimzi-kafka-operator) | Multi-CRD, documented scope trade-offs, OLM | Watch scope, RBAC, reconcile phases, docs |
| **ECK** | [elastic/cloud-on-k8s](https://github.com/elastic/cloud-on-k8s) | Enterprise DB, `profile-restricted`, cluster-wide | Security profiles, RBAC, cloud packaging |
| **MongoDB Community** | [mongodb/mongodb-kubernetes-operator](https://github.com/mongodb/mongodb-kubernetes-operator) | Per-namespace workload SA, scope env | Multi-SA RBAC, scope, formation |
| **Percona PG Operator** | [percona/percona-postgresql-operator](https://github.com/percona/percona-postgresql-operator) | HA Postgres, backup to S3, OLM | Backup + cloud creds, OLM installModes |

---

## Tier 2 — secondary references

| Operator | Repo | Why study |
|----------|------|-----------|
| **Zalando Postgres** | [zalando/postgres-operator](https://github.com/zalando/postgres-operator) | Older Spilo pattern — anti-patterns vs CNPG |
| **Crunchy Postgres** | [CrunchyData/postgres-operator](https://github.com/CrunchyData/postgres-operator) | PGO v5 architecture, backup jobs |
| **Opstree Redis** | [OT-CONTAINER-KIT/redis-operator](https://github.com/OT-CONTAINER-KIT/redis-operator) | Simpler stateful operator, Helm-first |
| **RabbitMQ Cluster** | [rabbitmq/cluster-operator](https://github.com/rabbitmq/cluster-operator) | Official vendor, minimal CRD surface |
| **MariaDB Operator** | [mariadb-operator/mariadb-operator](https://github.com/mariadb-operator/mariadb-operator) | Recent kubebuilder layout |

---

## Tier 3 — ecosystem (not DB, but integration patterns)

| Project | Repo | Why study |
|---------|------|-----------|
| **cert-manager** | [cert-manager/cert-manager](https://github.com/cert-manager/cert-manager) | Certificate ownership, webhook scale, CRD design |
| **External Secrets** | [external-secrets/external-secrets](https://github.com/external-secrets/external-secrets) | Cloud SM → K8s Secret — alternative to inline creds |
| **Prometheus Operator** | [prometheus-operator/prometheus-operator](https://github.com/prometheus-operator/prometheus-operator) | ServiceMonitor pattern (Neo4j metrics V2+) |
| **Neo4j Helm** | [neo4j/helm-charts](https://github.com/neo4j/helm-charts) | Parity baseline — not an operator but current customer path |

---

## Tier 4 — Neo4j cloud (internal alignment)

| Source | Link | Why study |
|--------|------|-----------|
| **neo4j-cloud architecture** | [current-architecture.dot.png](https://github.com/neo-technology/neo4j-cloud/blob/master/architecture/current-architecture.dot.png) | Managed service decomposition — what self-managed operator should / should not replicate |
| **PS operator proposal** | [`20-operator-proposal.md`](../../../00-discovery/20-operator-proposal.md) | Vision, cloud IAM, security model draft |

---

## Benchmark dimensions (score every Tier-1 operator)

Copy into each `operators/{name}.md` from [`template.md`](template.md).

| # | Dimension | ADR track | Questions |
|---|-----------|-----------|-----------|
| D1 | **Repository layout** | Implementation | `api/`, `internal/`, `config/`, `hack/`, `test/` — monorepo vs split |
| D2 | **Internal layering** | Implementation | controller thin? separate builders? domain packages? |
| D3 | **CRD & controller count** | Implementation | One reconciler per CRD? aggregation? |
| D4 | **Go & K8s deps** | Engineering | go version, controller-runtime pin, client-go alignment |
| D5 | **Third-party libs** | Engineering | DB drivers, cloud SDKs in operator pod? |
| D6 | **Operator RBAC** | Security | Role vs ClusterRole verbs; least privilege; aggregate roles |
| D7 | **Workload RBAC** | Security | Separate SA per cluster? auto-created in target NS? |
| D8 | **Watch scope** | Security / Ops | Default single vs cluster; multi-NS overhead docs |
| D9 | **Webhooks** | Implementation | Deployed with operator? cert rotation? failurePolicy |
| D10 | **Admission vs reconcile** | Implementation | What runs where — compare to ADR-001 |
| D11 | **Status & conditions** | Implementation | Condition set, pool-level status, events |
| D12 | **Formation / day-2** | Implementation | Jobs vs in-reconciler admin API |
| D13 | **Backup & cloud** | Cloud | S3/GCS/Azure — static keys vs WI/IRSA vs ESO |
| D14 | **Platform profiles** | Cloud | Restricted install YAML, cloud annotation docs |
| D15 | **Testing pyramid** | Engineering | Unit / envtest / kind / cloud smoke ratio |
| D16 | **Quality gates** | Engineering | golangci-lint, govulncheck, coverage threshold |
| D17 | **Packaging** | Delivery | Helm chart, OLM bundle, installModes |
| D18 | **Release matrix** | Delivery | Operator version ↔ DB version compatibility |
| D19 | **Docs & runbooks** | Delivery | Architecture page, RBAC rationale, troubleshooting |
| D20 | **Observability** | Ops | Metrics, logging keys, tracing |

---

## Evidence → ADR mapping

| Benchmark finding | Likely ADR |
|-------------------|------------|
| RBAC patterns across Tier-1 | ADR-013 Operator & workload RBAC model |
| Scope + informer cost (Strimzi) | ADR-014 Watch scope & cache configuration |
| Cloud backup identity (CNPG, Percona) | ADR-016 Cloud identity for workloads |
| Restricted profiles (ECK, CNPG) | ADR-015 Security profiles & PSS/SCC |
| Dep policy (controller-runtime pin) | ADR-012 Dependency & upgrade policy |
| Test layout (CNPG e2e) | ADR-019 Testing strategy (extends old ADR-009) |
| Code layout consensus | ADR-011 Reference architecture synthesis |

---

## Study order (suggested)

```
Week A: CNPG + Strimzi     → D1-D12, D15-D17
Week B: ECK + MongoDB      → D6-D9, D13-D14
Week C: Ecosystem + Helm   → D13, cert-manager, neo4j helm parity
Week D: Synthesis          → operator-benchmark/synthesis.md → ADR-011
```

---

## Outputs

| File | Purpose |
|------|---------|
| `operators/*.md` | Per-operator filled templates |
| `synthesis.md` | Cross-operator comparison tables + recommendations |
| `docs/02-technical-design/security.md` | Threat model + RBAC summary (fed by ADR-013) |
| ADR-011+ | Decisions citing benchmark evidence |
