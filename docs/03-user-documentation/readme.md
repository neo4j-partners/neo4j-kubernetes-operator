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

1. **Quickstart** — pick a platform:
   - [kind (local)](quickstart/local-kind/install.md)
   - [Azure AKS](quickstart/azure-aks/install.md)
2. **Operator** — install details, uninstall, troubleshooting → [`operator/`](operator/)
3. **Neo4j** — workload CR, status, connect → [`neo4j/`](neo4j/)

---

## Document index

### Quickstart (end-to-end)

| Doc | Description |
|-----|-------------|
| [quickstart/readme.md](quickstart/readme.md) | Platform index |
| [local-kind/install.md](quickstart/local-kind/install.md) | kind — cluster + operator + Standalone Neo4j |
| [azure-aks/install.md](quickstart/azure-aks/install.md) | AKS — cluster + operator + Standalone Neo4j |

### Operator

| Doc | Description |
|-----|-------------|
| [01-prerequisites.md](operator/01-prerequisites.md) | Shared prerequisites |
| [02-installation.md](operator/02-installation.md) | Install operator (generic — all platforms) |
| [03-uninstall.md](operator/03-uninstall.md) | Remove operator |
| [04-troubleshooting.md](operator/04-troubleshooting.md) | Common issues |

### Neo4j workload

| Doc | Description |
|-----|-------------|
| [neo4j/readme.md](neo4j/readme.md) | Neo4j install documentation index |
| [01-quickstart-standalone.md](neo4j/01-quickstart-standalone.md) | Standalone CR, status, connect |
| [02-quickstart-cluster.md](neo4j/02-quickstart-cluster.md) | Cluster sample |

### Reference

| Doc | Description |
|-----|-------------|
| [api-cheatsheet.md](reference/api-cheatsheet.md) | Short API summary |

---

## V1 scope (summary)

See [V1 scope lock](../00-discovery/13-v1-scope-lock.md) for the full list. In short:

- **Supported:** Standalone + Cluster `Neo4j`; storage Dynamic/Existing + aux volumes + mounts; generated/BYO auth; ClusterIP/NodePort/LoadBalancer; BYO TLS (`spec.trust`); `spec.scheduling` / probes.
- **Deferred:** Ingress, backup/restore CRDs, CSV/JMX/Graphite monitoring, cert-manager, multi-namespace operator scope, Neo4j version upgrade.

Examples: [`examples/storage/`](../../examples/storage/). Design: [BDR-005](../02-technical-design/decision-records/business/neo4j/005-storage-volume-mode.md), [CRD spec](../02-technical-design/crd-spec/neo4j/spec.md).
