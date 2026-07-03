# User documentation

Guides for **installing and operating** the Neo4j Kubernetes Operator and `Neo4j` custom resources.

This estate is separate from product and design documentation:

| Audience | Location |
|----------|----------|
| Install, quickstart, day-2 ops | **`03-user-documentation/`** (this tree) |
| Product requirements | [`01-prd/`](../01-prd/) |
| API design, BDR/ADR, CRD reference | [`02-technical-design/`](../02-technical-design/) |
| Test strategy & catalog | [`04-test-plan/`](../04-test-plan/) |

---

## Reading order

1. [Prerequisites](operator/01-prerequisites.md)
2. [Install the operator](operator/02-installation.md)
3. [Quickstart — Standalone Neo4j](neo4j/01-quickstart-standalone.md)
4. [Uninstall](operator/03-uninstall.md) when tearing down a lab cluster

---

## Document index

### Operator

| Doc | Description |
|-----|-------------|
| [01-prerequisites.md](operator/01-prerequisites.md) | Cluster, tools, StorageClass |
| [02-installation.md](operator/02-installation.md) | CRD + controller Deployment |
| [03-uninstall.md](operator/03-uninstall.md) | Remove operator; PVC retention |
| [04-troubleshooting.md](operator/04-troubleshooting.md) | Common install / reconcile issues |

### Neo4j workload

| Doc | Description |
|-----|-------------|
| [01-quickstart-standalone.md](neo4j/01-quickstart-standalone.md) | Minimal Standalone graph (V1) |
| [02-quickstart-cluster.md](neo4j/02-quickstart-cluster.md) | Cluster sample (preview — Slice 2) |

### Reference

| Doc | Description |
|-----|-------------|
| [api-cheatsheet.md](reference/api-cheatsheet.md) | Short API summary + links to full spec |

---

## V1 scope (summary)

See [V1 scope lock](../00-discovery/13-v1-scope-lock.md) for the full list. In short:

- **Supported now (Slice 1):** Standalone `Neo4j`, Dynamic data volume, generated auth Secret, ClusterIP Bolt/HTTP.
- **Deferred:** Cluster formation, TLS, Ingress, backup, monitoring, multi-namespace / cluster-wide operator scope.

Design details: [BDR-003](../02-technical-design/decision-records/business/operator/003-operator-install-scope.md) (install scope), [CRD spec](../02-technical-design/crd-spec/neo4j/spec.md).
