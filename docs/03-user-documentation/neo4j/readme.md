# Neo4j workload documentation

Guides for deploying and operating `Neo4j` custom resources (after the operator is installed).

## Documents

| Doc | Description |
|-----|-------------|
| [01-quickstart-standalone.md](01-quickstart-standalone.md) | Standalone Neo4j — apply CR, watch status, connect, customize |
| [02-quickstart-cluster.md](02-quickstart-cluster.md) | Cluster sample (preview — Slice 2, not yet supported) |

## Prerequisites

- Operator installed — see [quickstart](../quickstart/readme.md) or [operator/02-installation.md](../operator/02-installation.md)
- [Shared prerequisites](../operator/01-prerequisites.md) — StorageClass, Neo4j Enterprise image, license acceptance

## End-to-end quickstarts (operator + Neo4j)

| Platform | Guide |
|----------|-------|
| kind (local) | [quickstart/local-kind/install.md](../quickstart/local-kind/install.md) |
| Azure AKS | [quickstart/azure-aks/install.md](../quickstart/azure-aks/install.md) |

## Reference

- [API cheatsheet](../reference/api-cheatsheet.md)
- [CRD spec](../../02-technical-design/crd-spec/neo4j/spec.md)
