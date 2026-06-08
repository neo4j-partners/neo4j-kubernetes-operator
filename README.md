# Neo4j Enterprise Operator for Kubernetes

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/neo4j-partners/neo4j-kubernetes-operator)](https://goreportcard.com/report/github.com/neo4j-partners/neo4j-kubernetes-operator)
[![GitHub Release](https://img.shields.io/github/release/neo4j-partners/neo4j-kubernetes-operator.svg)](https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases)

A Kubernetes operator for Neo4j Enterprise — declarative clusters, databases, users, roles, backups, and plugins. Supports Neo4j Enterprise 5.26 LTS and any CalVer release (2025.x, 2026.x, …).

> [!TIP]
> 📖 **Documentation site**: [neo4j-partners.github.io/neo4j-kubernetes-operator](https://neo4j-partners.github.io/neo4j-kubernetes-operator/) — searchable, versioned, with a release dropdown. Every link in this README also resolves on the docs site.

> [!WARNING]
> **Alpha software.** Maintained by a single Neo4j employee in a personal capacity (not an official Neo4j product, not supported by Neo4j Inc.). LLM-assisted development means subtle bugs are possible. Not recommended for production without independent validation. APIs may change between releases. Issues and PRs are best-effort via [GitHub Issues](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues).

## Requirements

- **Neo4j**: Enterprise 5.26 LTS or any CalVer release (2025.x+)
- **Kubernetes**: 1.32+
- **cert-manager** 1.20+ — optional, only for TLS-enabled deployments

## Quick Start

Install the operator via Helm:

```bash
helm repo add neo4j-operator https://neo4j-partners.github.io/neo4j-kubernetes-operator/charts
helm repo update
helm install neo4j-operator neo4j-operator/neo4j-operator \
  --namespace neo4j-operator-system --create-namespace
```

Create admin credentials and deploy your first instance:

```bash
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password=your-secure-password

# Single-node standalone (dev)
kubectl apply -f https://raw.githubusercontent.com/neo4j-partners/neo4j-kubernetes-operator/main/examples/standalone/single-node-standalone.yaml

# OR a minimal cluster (prod)
kubectl apply -f https://raw.githubusercontent.com/neo4j-partners/neo4j-kubernetes-operator/main/examples/clusters/minimal-cluster.yaml
```

Access the Neo4j browser (use the Service that matches what you deployed —
standalone Services are `<name>-service`, cluster client Services are
`<name>-client`):

```bash
# Standalone (single-node-standalone → standalone-neo4j):
kubectl port-forward svc/standalone-neo4j-service 7474:7474 7687:7687

# OR cluster (minimal-cluster):
kubectl port-forward svc/minimal-cluster-client 7474:7474 7687:7687

# Open http://localhost:7474
```

For OCI registry installs, custom Kustomize, OpenShift OLM, and CRD setup details, see the [Installation guide](https://neo4j-partners.github.io/neo4j-kubernetes-operator/main/user_guide/installation/).

## What you can do declaratively

The operator ships these CRDs (all `neo4j.neo4j.com/v1beta1`):

| CRD | Purpose |
|---|---|
| `Neo4jEnterpriseCluster` | Multi-server cluster (self-organizing primary/secondary roles) |
| `Neo4jEnterpriseStandalone` | Single-node deployment |
| `Neo4jDatabase` | Database lifecycle (works with both cluster and standalone) |
| `Neo4jUser`, `Neo4jRole`, `Neo4jRoleBinding` | Declarative users, roles, and privileges |
| `Neo4jAuthRule` | Attribute-based access control (Neo4j 2026.03+) |
| `Neo4jBackup`, `Neo4jRestore` | Backup and restore via `neo4j-admin` (PVC, S3, GCS, Azure) |
| `Neo4jShardedDatabase` | Property-sharded databases with backup, restore, and full/differential chains (Neo4j 2025.12+) |
| `Neo4jPlugin` | Plugin installs (APOC, GDS, Bloom, GenAI, …) |

Each has examples under [`examples/`](examples/) and a dedicated guide on the [docs site](https://neo4j-partners.github.io/neo4j-kubernetes-operator/main/).

## Documentation

| Topic | Link |
|---|---|
| Getting started | [Quickstart](https://neo4j-partners.github.io/neo4j-kubernetes-operator/main/user_guide/getting_started/) |
| Installation methods | [Installation](https://neo4j-partners.github.io/neo4j-kubernetes-operator/main/user_guide/installation/) |
| Clustering, topology, fault tolerance | [Clustering](https://neo4j-partners.github.io/neo4j-kubernetes-operator/main/user_guide/clustering/) |
| TLS, authentication, RBAC, audit | [Security](https://neo4j-partners.github.io/neo4j-kubernetes-operator/main/user_guide/security/) |
| Backup & restore | [Backup & Restore](https://neo4j-partners.github.io/neo4j-kubernetes-operator/main/user_guide/guides/backup_restore/) |
| User & role management | [User & Role Management](https://neo4j-partners.github.io/neo4j-kubernetes-operator/main/user_guide/user_role_management/) |
| Property sharding | [Property Sharding](https://neo4j-partners.github.io/neo4j-kubernetes-operator/main/user_guide/property_sharding/) |
| Monitoring & metrics | [Monitoring](https://neo4j-partners.github.io/neo4j-kubernetes-operator/main/user_guide/guides/monitoring/) |
| Migration & upgrades | [Migration Guide](https://neo4j-partners.github.io/neo4j-kubernetes-operator/main/user_guide/migration_guide/) |
| Troubleshooting | [Troubleshooting](https://neo4j-partners.github.io/neo4j-kubernetes-operator/main/user_guide/guides/troubleshooting/) |
| API reference | [API docs](https://neo4j-partners.github.io/neo4j-kubernetes-operator/main/api_reference/) |

## Contributing

This project exclusively uses **Kind** for development, testing, and CI workflows.

```bash
git clone https://github.com/neo4j-partners/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator
make dev-cluster        # Create local Kind cluster
make operator-setup     # Deploy operator
make test-unit          # Run unit tests
```

See [CONTRIBUTING.md](CONTRIBUTING.md) and the [developer guide](https://neo4j-partners.github.io/neo4j-kubernetes-operator/main/developer_guide/development/) for the full inner-loop workflow, test layout, and PR process.

## License

[Apache License 2.0](LICENSE)

## Support

Best-effort via [GitHub Issues](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues). This is a personal-capacity project, not an officially supported Neo4j product.
