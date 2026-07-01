# Goals

Product goals for the Neo4j Kubernetes Operator. **V1 goals** align with the MVP scope in [`../02-technical-design/13-v1-scope-lock.md`](../02-technical-design/13-v1-scope-lock.md) and `V1=Yes` rows in [`07-functional-requirements.csv`](07-functional-requirements.csv).

---

## V1 goals (MVP)

### Deployment

- **G1** — Install Neo4j **Standalone** or **Cluster** from a single `Neo4j` CR ([BDR-001](../02-technical-design/decision-records/business/001-single-neo4j-crd.md), [BDR-002](../02-technical-design/decision-records/business/002-neo4j-crd-topology.md)).
- **G2** — Support **primaries** plus optional **analytics** and **read** pools with per-pool StatefulSets ([BDR-009](../02-technical-design/decision-records/business/009-scale-pool-ordinal-semantics.md)).
- **G3** — **Enterprise** edition with explicit license acceptance.

### Operations

- **G4** — **Continuous reconciliation** — create, update, delete, and correct drift on managed resources (`OP-1-002`).
- **G5** — **Scale** cluster members safely — per-pool scale-out/scale-in with automated `ENABLE SERVER` / drain (`NEO-2-011`).
- **G6** — **Config change** triggers controlled or rolling restart (`NEO-2-010`).
- **G7** — **Status** exposes Ready / Installed / Error conditions without requiring log diving (`OP-1-003`).

### Infrastructure (minimal path)

- **G8** — **Storage**: dynamic PVC for data (`volumes.data.mode: Dynamic` + `storageClassName`) ([BDR-005](../02-technical-design/decision-records/business/005-storage-volume-mode.md)).
- **G9** — **Networking**: ClusterIP Service with **HTTP + Bolt**; operator-derived internals in Cluster mode ([BDR-007](../02-technical-design/decision-records/business/006-service-exposure-connectivity.md)).
- **G10** — **Cluster TLS** via BYO secrets when `mode: Cluster` ([BDR-006](../02-technical-design/decision-records/business/007-tls-trust-model.md)).
- **G11** — **Config** passthrough map + default JVM ([BDR-008](../02-technical-design/decision-records/business/008-neo4j-config-surface.md)).
- **G12** — **Plugins** via `pluginDefinitions` + pool references ([BDR-004](../02-technical-design/decision-records/business/004-neo4j-plugin-topology.md)).

### Operator platform

- **G13** — Install operator via **YAML manifests**, **single-namespace** watch scope ([BDR-003](../02-technical-design/decision-records/business/003-operator-install-scope.md)).
- **G14** — Uninstall preserves data PVCs by default.

### Differentiation vs Helm (V1)

V1 must deliver more than “Helm with a reconciler wrapper”:

- One CR instead of N Helm releases for a cluster.
- Automatic member enablement on scale (no operations Job).
- Unified status and drift correction.
- Opinionated, smaller spec surface for the MVP path.

---

## V2+ goals (directional)

Not committed in V1 — tracked for roadmap:

- Backup / restore CRDs and `features.backup`
- Monitoring / Prometheus / ServiceMonitor ([BDR-010](../02-technical-design/decision-records/business/010-neo4j-features-catalog.md))
- Neo4j **version upgrade** workflow
- Helm → operator **migration** for installed base
- `Neo4jDatabase` logical database CRD
- Declarative identity — `Neo4jUser` / `Neo4jRole` / `Neo4jGrant` ([BDR-012](../02-technical-design/decision-records/business/012-identity-management.md))
- LoadBalancer / NodePort, HTTPS, Bolt TLS, ingress
- Multi-namespace / cluster-wide operator scope
- Operator Helm chart and operator self-upgrade

See [`13-roadmap.md`](13-roadmap.md).
