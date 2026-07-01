# Software architecture — phase 2

Implementation design for the Neo4j Kubernetes operator. **API / product choices** live in [BDRs](../decision-records/business/); **how we build** lives in [ADRs](../decision-records/architecture/).

## Two sub-phases

| Phase | Goal | Primary skill |
|-------|------|---------------|
| **2a — Benchmark** | Study reference operators (code, RBAC, cloud, quality) | `@operator-benchmark-analyst` |
| **2b — Decide** | ADRs for implementation, security, delivery | `@operator-architecture-orchestrator` |

Do **not** accept ADR-002 until benchmark synthesis has been reviewed (`operator-benchmark/synthesis.md`).

## Status

| Artifact | State |
|----------|-------|
| [ADR-001](../decision-records/architecture/001-crd-validation-process.md) | accepted |
| [ADR-002](../decision-records/architecture/002-package-layering.md) – [ADR-010](../decision-records/architecture/010-operator-deployment.md) | **proposed** (Track 2) |
| [ADR-011](../decision-records/architecture/011-implementation-language.md) | **proposed** — Go / kubebuilder |
| [operator-benchmark/](operator-benchmark/readme.md) | CNPG + Strimzi + [synthesis](operator-benchmark/synthesis.md) |
| [layer.md](layer.md) | promoted into ADR-002 |
| [file_structure.md](file_structure.md) | draft target tree |
| [dependencies.md](../dependencies.md) | platform matrix draft |
| [security.md](../security.md) | empty → fed by ADR-013/015 |

## Decision backlog

~110 topics in domains **A–O**:  
`.cursor/skills/operator-architecture-orchestrator/architecture-backlog.md`

## Suggested ADR sequence (Track 2 — implementation)

1. ADR-002 → ADR-003 → ADR-005 → ADR-006 → ADR-007 → ADR-004 → ADR-008 → ADR-009 → ADR-010 (**all proposed**)
2. Cross-cutting next: ADR-013 (RBAC), ADR-014 (watch scope), ADR-020 (tests) — not yet drafted

Benchmark evidence: [`operator-benchmark/synthesis.md`](operator-benchmark/synthesis.md) · language decision: [ADR-011](../decision-records/architecture/011-implementation-language.md)

## Reference operators (Tier-1)

| Operator | Focus |
|----------|-------|
| [CloudNativePG](https://github.com/cloudnative-pg/cloudnative-pg) | Layout, testing, scope |
| [Strimzi](https://github.com/strimzi/strimzi-kafka-operator) | Multi-CRD, watch overhead |
| [ECK](https://github.com/elastic/cloud-on-k8s) | Restricted profiles, RBAC |
| [MongoDB Community](https://github.com/mongodb/mongodb-kubernetes-operator) | Workload SA per NS |

Full catalog: [operator-benchmark/readme.md](operator-benchmark/readme.md)

## BDR → ADR map (summary)

| BDR topic | Primary ADRs |
|-----------|--------------|
| Install scope (BDR-003) | ADR-013, ADR-014, ADR-010 |
| Topology (BDR-002) | ADR-003, ADR-007, ADR-004 |
| TLS (BDR-006) | ADR-003, ADR-006, M-09 |
| Connectivity (BDR-007) | ADR-003, ADR-005, M-08 |
| Storage (BDR-005) | ADR-003, M-07 |

Skills: `@operator-benchmark-analyst` · `@decision-classifier-bdr-vs-adr` · `@adr-author-neo4j-operator`
