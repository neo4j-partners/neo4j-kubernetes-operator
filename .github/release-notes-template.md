# Neo4j Kubernetes Operator __TAG__

## Installation

### Quick Install (Complete Operator)
```bash
kubectl apply --server-side -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/__TAG__/neo4j-kubernetes-operator-complete.yaml
```

### CRDs Only
```bash
kubectl apply --server-side -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/__TAG__/neo4j-kubernetes-operator.yaml
```

## Helm

### Helm chart repository (recommended, available from v1.8.0 onwards)

```bash
helm repo add neo4j-operator https://neo4j-partners.github.io/neo4j-kubernetes-operator/charts
helm repo update

helm install neo4j-operator neo4j-operator/neo4j-operator \
  --version __VERSION__ \
  --namespace neo4j-operator-system \
  --create-namespace
```

### OCI registry (all releases)

```bash
helm install neo4j-operator oci://ghcr.io/neo4j-partners/charts/neo4j-operator \
  --version __VERSION__ \
  --namespace neo4j-operator-system \
  --create-namespace
```

## Upgrading

Breaking changes are documented in the [Migration Guide](https://neo4j-partners.github.io/neo4j-kubernetes-operator/user_guide/migration_guide/),
grouped by release. Before deploying this version, check the guide for any
manifest updates required since the release you're currently running.

## Requirements

- Kubernetes 1.32+
- Neo4j Enterprise 5.26+ or CalVer 2025.01.0+
- cert-manager v1.20+ (for TLS)

## Container Images

| Image | Tag |
|-------|-----|
| `ghcr.io/neo4j-partners/neo4j-kubernetes-operator` | `__TAG__` |
| `mcp/neo4j-cypher` | `latest` (official Docker Hub image) |

Images are signed with [Sigstore Cosign](https://docs.sigstore.dev/cosign/overview/) keyless signing.

```bash
cosign verify ghcr.io/neo4j-partners/neo4j-kubernetes-operator:__TAG__ \
  --certificate-identity-regexp='github.com/neo4j-partners' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com'
```

## How this release was validated

Every release must pass, mechanically — a failure in any gate blocks publication:

- **Unit suite + generated-artifact drift gate** — CRDs, RBAC, Helm chart, and the OLM bundle are regenerated and diffed on every PR; a release cannot ship manifests that don't match the code.
- **Core integration suite on both supported Neo4j lines** — Neo4j 5.26 LTS *and* the pinned CalVer release, on every runtime-affecting PR.
- **Extended integration suite on the release commit** — the full matrix (clustering, scaling, split-brain recovery, the complete backup/restore matrix, sharding) runs against the exact commit being tagged.
- **Install-confidence gate (blocking, inside the release pipeline)** — a five-leg matrix on a fresh Kubernetes cluster: Helm install (cluster and namespace-scoped RBAC modes), Helm upgrade **from the previous published release** including the CRD refresh, documented-order uninstall with live resources, and the kubectl server-side-apply path. A release that cannot cleanly install, upgrade, or uninstall cannot be published.
- **Signed supply chain** — multi-arch images signed with Sigstore Cosign (verification command above); OLM bundle validated with operator-sdk.

See [Supported Neo4j Versions](https://neo4j-partners.github.io/neo4j-kubernetes-operator/user_guide/version_support/) for what "validated" means per Neo4j line, and [CI/CD & Workflows](https://neo4j-partners.github.io/neo4j-kubernetes-operator/developer_guide/ci_and_workflows/) for the gate machinery.

## Release Assets

| Asset | Description |
|-------|-------------|
| `neo4j-kubernetes-operator-complete.yaml` | Complete operator install (CRDs + RBAC + Deployment) |
| `neo4j-kubernetes-operator.yaml` | CRDs only |

`kubectl apply --server-side` is the recommended apply form for both assets (the largest CRDs exceed client-side apply's last-applied annotation limit).

**Helm users**: apply the CRD asset (`neo4j-kubernetes-operator.yaml`) before `helm upgrade` — Helm never upgrades CRDs.

## Documentation

- [Getting Started Guide](./docs/README.md)
- [API Reference](./docs/api_reference/)
- [Migration Guide](./docs/user_guide/migration_guide.md)

## Bug Reports

Please report issues at [GitHub Issues](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues)
