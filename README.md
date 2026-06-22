# Neo4j Enterprise Operator for Kubernetes

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/priyolahiri/neo4j-kubernetes-operator)](https://goreportcard.com/report/github.com/priyolahiri/neo4j-kubernetes-operator)
[![GitHub Release](https://img.shields.io/github/release/priyolahiri/neo4j-kubernetes-operator.svg)](https://github.com/priyolahiri/neo4j-kubernetes-operator/releases)

A Kubernetes operator for Neo4j Enterprise — declarative clusters, databases, users, roles, backups, and plugins. Supports Neo4j Enterprise 5.26 LTS and any CalVer release (2025.x, 2026.x, …).

> [!IMPORTANT]
> **Independent project — not affiliated with Neo4j, Inc.** This is a personally
> maintained, community open-source project licensed under **Apache-2.0**. It is
> **not** an official Neo4j product and is **not** affiliated with, endorsed by,
> or sponsored by Neo4j, Inc. "Neo4j" is a trademark of Neo4j, Inc.; it is used
> here only to describe the software this operator manages. You must hold your
> own valid Neo4j Enterprise license to run Neo4j Enterprise.
>
> **Support & stability.** You're welcome to test, use, fork, and contribute.
> Support is **best-effort via [GitHub issues](https://github.com/priyolahiri/neo4j-kubernetes-operator/issues)** —
> there is no official or commercial support, and no SLA. APIs and behaviour
> **may change between releases without notice**. Validate independently before
> relying on it. Issues, pull requests, and contributions are welcome.

> [!TIP]
> 📖 **Documentation site**: [priyolahiri.github.io/neo4j-kubernetes-operator](https://priyolahiri.github.io/neo4j-kubernetes-operator/) — searchable, versioned, with a release dropdown. Every link in this README also resolves on the docs site.

## Requirements

- **Neo4j**: Enterprise 5.26 LTS or any CalVer release (2025.x+)
- **Kubernetes**: 1.32+
- **cert-manager** 1.20+ — optional, only for TLS-enabled deployments

## Quick Start

Install the operator via Helm:

```bash
helm repo add neo4j-operator https://priyolahiri.github.io/neo4j-kubernetes-operator/charts
helm repo update
helm install neo4j-operator neo4j-operator/neo4j-operator \
  --namespace neo4j-operator-system --create-namespace
```

Prefer plain `kubectl`? Each release attaches a single apply-able bundle
(CRDs + RBAC + Deployment) as a **Release asset** — it's not a file in the repo,
download it from the `releases/download/<tag>/` URL:

```bash
kubectl apply -f https://github.com/priyolahiri/neo4j-kubernetes-operator/releases/download/v1.13.0/neo4j-kubernetes-operator-complete.yaml
```

See the [Installation guide](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/user_guide/installation/#method-3-quick-install-from-github-release) for CRDs-only and latest-version variants.

Create admin credentials and deploy your first instance:

```bash
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password=your-secure-password

# Single-node standalone (dev)
kubectl apply -f https://raw.githubusercontent.com/priyolahiri/neo4j-kubernetes-operator/main/examples/standalone/single-node-standalone.yaml

# OR a minimal cluster (prod)
kubectl apply -f https://raw.githubusercontent.com/priyolahiri/neo4j-kubernetes-operator/main/examples/clusters/minimal-cluster.yaml
```

> **License acceptance is required.** Every cluster/standalone must set
> `spec.acceptLicenseAgreement` to `"yes"` (you hold a Neo4j Enterprise
> commercial license) or `"eval"` (30-day evaluation) — the operator refuses to
> deploy otherwise and never accepts the license on your behalf. The examples
> above use `"eval"`; set `"yes"` for a licensed production deployment.

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

For OCI registry installs, custom Kustomize, OpenShift OLM, and CRD setup details, see the [Installation guide](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/user_guide/installation/).

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

Each has examples under [`examples/`](examples/) and a dedicated guide on the [docs site](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/).

## Documentation

| Topic | Link |
|---|---|
| Getting started | [Quickstart](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/user_guide/getting_started/) |
| Installation methods | [Installation](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/user_guide/installation/) |
| Clustering, topology, fault tolerance | [Clustering](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/user_guide/clustering/) |
| TLS, authentication, RBAC, audit | [Security](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/user_guide/security/) |
| Backup & restore | [Backup & Restore](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/user_guide/guides/backup_restore/) |
| User & role management | [User & Role Management](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/user_guide/user_role_management/) |
| Property sharding | [Property Sharding](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/user_guide/property_sharding/) |
| Monitoring & metrics | [Monitoring](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/user_guide/guides/monitoring/) |
| Migration & upgrades | [Migration Guide](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/user_guide/migration_guide/) |
| Troubleshooting | [Troubleshooting](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/user_guide/guides/troubleshooting/) |
| API reference | [API docs](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/api_reference/) |

## Contributing

This project uses **Kind** for development, testing, and CI workflows.

```bash
git clone https://github.com/priyolahiri/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator
make dev-cluster        # Create local Kind cluster
make operator-setup     # Deploy operator
make test-unit          # Run unit tests
```

See [CONTRIBUTING.md](CONTRIBUTING.md) and the [developer guide](https://priyolahiri.github.io/neo4j-kubernetes-operator/main/developer_guide/development/) for the full inner-loop workflow, test layout, and PR process.
