# Neo4j Kubernetes Operator __TAG__

## Installation

### Quick Install (Complete Operator)
```bash
kubectl apply -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/__TAG__/neo4j-kubernetes-operator-complete.yaml
```

### CRDs Only
```bash
kubectl apply -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/__TAG__/neo4j-kubernetes-operator.yaml
```

## Helm (OCI)

```bash
helm install neo4j-operator oci://ghcr.io/neo4j-partners/charts/neo4j-operator \
  --version __VERSION__ \
  --namespace neo4j-operator-system \
  --create-namespace
```

## Breaking Changes

> **Upgrading from any previous version requires manifest updates.**
> See the [Migration Guide](./docs/user_guide/migration_guide.md) for step-by-step instructions.

### API Version: v1alpha1 to v1beta1

All CRDs graduated from `neo4j.neo4j.com/v1alpha1` to `neo4j.neo4j.com/v1beta1`.
Every manifest must be updated:

```bash
# Batch update all manifests
find /path/to/manifests -name '*.yaml' -exec \
  sed -i 's|neo4j.neo4j.com/v1alpha1|neo4j.neo4j.com/v1beta1|g' {} +
```

### Bolt TLS Enforcement

When TLS is enabled, `server.bolt.tls_level` is now `REQUIRED` (was `OPTIONAL`).
Plain `bolt://` connections are rejected — use `bolt+s://` or `bolt+ssc://`.

### Deprecated Config Key

`dbms.logs.query.enabled` — use `db.logs.query.enabled` instead.

### Cumulative changes from v1.6.0-alpha

If upgrading from v1.5.0-alpha or earlier, also apply these v1.6.0-alpha changes:
- `Neo4jRestore`: `spec.targetCluster` changed to `spec.clusterRef`
- `AuthSpec`: `provider`/`secretRef` removed — use `authenticationProviders`/`authorizationProviders`
- Standalone: `spec.route` changed to `spec.service.route`; `spec.persistence` changed to `spec.storage.retentionPolicy`
- TrustStore/Kerberos: `secretRef` changed to `name`
- Backup encryption: `ChaCha20` changed to `ChaCha20Poly1305`

**[Full Migration Guide](./docs/user_guide/migration_guide.md)**

## Requirements

- Kubernetes 1.30+
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
| `operatorhub-bundle-__VERSION__.tar.gz` | OperatorHub bundle |

## Documentation

- [Getting Started Guide](./docs/README.md)
- [API Reference](./docs/api_reference/)
- [Migration Guide](./docs/user_guide/migration_guide.md)

## Bug Reports

Please report issues at [GitHub Issues](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues)
