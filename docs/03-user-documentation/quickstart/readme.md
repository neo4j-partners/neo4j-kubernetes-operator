# Quickstart

End-to-end guides: cluster → operator → Standalone Neo4j.

Pick your platform:

| Platform | Guide | Best for |
|----------|-------|----------|
| **Local — kind** | [local-kind/install.md](local-kind/install.md) | Development, CI smoke, no cloud account |
| **Azure — AKS** | [azure-aks/install.md](azure-aks/install.md) | Cloud validation, Azure Disk storage |

Each guide lists **platform prerequisites**, a **minimal install path**, and links to detailed docs under [`operator/`](../operator/) and [`neo4j/`](../neo4j/).

---

## After the quickstart

| Topic | Doc |
|-------|-----|
| Operator install (all platforms) | [operator/02-installation.md](../operator/02-installation.md) |
| Shared prerequisites | [operator/01-prerequisites.md](../operator/01-prerequisites.md) |
| Neo4j workload (Standalone, Cluster) | [neo4j/readme.md](../neo4j/readme.md) |
| Standalone Neo4j — apply, status, connect | [neo4j/01-quickstart-standalone.md](../neo4j/01-quickstart-standalone.md) |
| Uninstall | [operator/03-uninstall.md](../operator/03-uninstall.md) |
| Troubleshooting | [operator/04-troubleshooting.md](../operator/04-troubleshooting.md) |
| API reference | [reference/api-cheatsheet.md](../reference/api-cheatsheet.md) |
