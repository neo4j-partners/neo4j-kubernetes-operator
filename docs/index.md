# Neo4j Kubernetes Operator

The declarative way to run Neo4j Enterprise on Kubernetes.

A Kubernetes operator that manages Neo4j Enterprise clusters, databases, users,
roles, backups, and plugins through Custom Resource Definitions. Every aspect of
a Neo4j deployment — from cluster topology to privilege grants — is expressed as
YAML and reconciled to its declared state.

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: production
spec:
  topology:
    servers: 3
  tls:
    mode: cert-manager
    issuerRef: { name: ca-cluster-issuer, kind: ClusterIssuer }
  monitoring:
    enabled: true
```

The manifest above provisions a three-server Neo4j Enterprise cluster with
cert-manager-issued TLS, RAFT-based primary election, and Prometheus metrics
enabled.

---

## Custom Resource Definitions

| Resource | Purpose |
|---|---|
| `Neo4jEnterpriseCluster` / `Neo4jEnterpriseStandalone` | Cluster and single-node deployments |
| `Neo4jDatabase` | Database lifecycle with topology, seed URIs (S3, GCS, Azure), and point-in-time recovery |
| `Neo4jUser`, `Neo4jRole`, `Neo4jRoleBinding` | Declarative user, role, and privilege management with drift reconciliation |
| `Neo4jAuthRule` | Attribute-based access control (ABAC) — claims-to-roles mapping at OIDC authentication time (Neo4j 2026.03+) |
| `Neo4jPlugin` | Plugin installation for APOC, GDS, Bloom, GenAI, N10s, and GraphQL with automatic security configuration |
| `Neo4jBackup`, `Neo4jRestore` | FULL, DIFF, and AUTO backup types with point-in-time recovery |
| `Neo4jShardedDatabase` | Property sharding for horizontal scale (GA in Neo4j 2025.12 and later CalVer releases) |

[Browse the full API reference →](api_reference/neo4jenterprisecluster.md)

---

## Capabilities

**Drift reconciliation.** The operator continuously reconciles live Neo4j state
against the declared specification. Out-of-band changes — privilege revokes,
configuration edits, role drops — are detected and corrected on the next
reconciliation loop.

**Controller-side validation.** All validation runs inline within reconciliation
rather than via admission webhooks, eliminating webhook configuration overhead
and avoiding cluster-wide failure modes when the validator is unavailable.

**Live observability.** Operator status fields surface Neo4j cluster state
sourced from `SHOW SERVERS`, `SHOW DATABASES`, `SHOW USERS`, and
`SHOW ROLES` queries. Server health, database topology, user counts, and
privilege drift are visible via `kubectl describe`.

**Multi-version support.** Neo4j 5.26 LTS (the final SemVer release) and the
CalVer release line — 2025.x, 2026.x, and onward — are both supported, with
automatic version detection for Cypher 25 syntax and version-appropriate
configuration keys.

---

## Installation

```bash
helm repo add neo4j https://priyolahiri.github.io/neo4j-kubernetes-operator/charts
helm install neo4j-operator neo4j/neo4j-operator \
  --namespace neo4j-operator-system --create-namespace
```

The operator is also published as an OCI artifact at
`oci://ghcr.io/priyolahiri/charts/neo4j-operator`.

For a step-by-step walkthrough, see [Getting Started](user_guide/getting_started.md).

---

## Documentation

| Section | Contents |
|---|---|
| [Getting Started](user_guide/getting_started.md) | Installation and first cluster deployment |
| [User Guide](user_guide/installation.md) | Configuration, TLS, networking, plugin management |
| [User & Role Management](user_guide/user_role_management.md) | Declarative RBAC end-to-end |
| [Backup & Restore](user_guide/guides/backup_restore.md) | Backup strategies and disaster recovery |
| [GitOps Integration](gitops/README.md) | ArgoCD and Flux integration patterns |
| [API Reference](api_reference/neo4jenterprisecluster.md) | Complete CRD field documentation |
| [Architecture](developer_guide/architecture.md) | Operator internals and design |
| [Contributing](developer_guide/contributing.md) | Development environment and contribution workflow |

---

## Project Status

This operator is alpha software, maintained in a personal capacity by a single
contributor at Neo4j. It is not an official Neo4j product and is not supported
by Neo4j, Inc. APIs and behaviour may change between releases. Independent
validation is required before production use.

Issues and contributions are welcome at
[github.com/priyolahiri/neo4j-kubernetes-operator](https://github.com/priyolahiri/neo4j-kubernetes-operator/issues).
