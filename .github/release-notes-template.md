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
