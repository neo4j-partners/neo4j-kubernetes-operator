# Neo4j Kubernetes Operator Modes and Scope Guide

## Table of Contents
- [Overview](#overview)
- [Quick Start (Beginner)](#quick-start-beginner)
- [Operator Modes](#operator-modes)
  - [Production Mode](#production-mode)
  - [Development Mode (In-Cluster Only)](#development-mode-in-cluster-only)
- [Scope and RBAC](#scope-and-rbac)
  - [Cluster Scope](#cluster-scope)
  - [Namespace Scope](#namespace-scope)
  - [Multi-Namespace Scope](#multi-namespace-scope)
- [Helm Configuration](#helm-configuration)
- [Non-Helm Configuration](#non-helm-configuration)
- [Cache Strategies](#cache-strategies)
- [Controller Selection (Dev Mode)](#controller-selection-dev-mode)
- [Logging and Metrics](#logging-and-metrics)
- [Troubleshooting](#troubleshooting)
- [Quick Reference](#quick-reference)
- [Additional Resources](#additional-resources)

## Overview

The operator has two separate axes of configuration:

- **Operational mode**: production vs development (affects defaults, logging, and cache behavior).
- **Watch scope**: cluster-wide, single namespace, or a fixed list of namespaces (affects RBAC and where CRs are reconciled).

Choose both explicitly so your deployment matches your environment and security model.

## Quick Start (Beginner)

**1) Choose a scope**

- **Cluster scope** (default): one operator manages Neo4j in any namespace.
- **Namespace scope**: one operator manages Neo4j only in its own namespace.
- **Multi-namespace**: one operator manages a fixed list of namespaces.

**2) Install with Helm (recommended)**

```bash
# Cluster scope (default)
helm install neo4j-operator oci://ghcr.io/neo4j-partners/charts/neo4j-operator \
  --namespace neo4j-operator-system \
  --create-namespace

# Namespace scope
helm install neo4j-operator oci://ghcr.io/neo4j-partners/charts/neo4j-operator \
  --namespace team-a \
  --create-namespace \
  --set operatorMode=namespace

# Multi-namespace scope
helm install neo4j-operator oci://ghcr.io/neo4j-partners/charts/neo4j-operator \
  --namespace neo4j-operator-system \
  --create-namespace \
  --set operatorMode=namespaces \
  --set watchNamespaces={team-a,team-b}
```

**3) Development? Use Kind and in-cluster only.**

Use `make dev-cluster` and `make deploy-dev`. Do not run the operator on your host.

**4) Install without Helm (Kustomize or kubectl)**

```bash
# Install CRDs
kubectl apply -f config/crd/bases/

# Production overlay
kubectl apply -k config/overlays/prod

# Development overlay (Kind only)
kubectl apply -k config/overlays/dev
```

## Operator Modes

### Production Mode

Production mode is the default when `--mode` is not set. Use it for real workloads.
Explicit flags and Helm values always override the mode defaults.

**Key behavior (binary defaults):**
- **Mode**: `production`
- **Metrics bind**: `:8080` (unless overridden)
- **Health bind**: `:8081` (unless overridden)
- **Cache strategy**: `on-demand` (unless overridden)
- **Leader election**: off by default (recommended for HA)
- **Controllers loaded**: cluster, standalone, database, backup, restore, plugin, shardeddatabase

**Helm defaults that matter:**
- `leaderElection.enabled: true` (passes `--leader-elect=true`)
- `logLevel: info`
- `metrics.enabled: true` with port `8080`
- `--health-probe-bind-address=:8081` is always set by the chart

### Development Mode (In-Cluster Only)

Development mode is optimized for faster iteration, but it **must run in-cluster**. Use Kind for dev and test clusters.

**How to enable:**
- **CLI/Kustomize**: add `--mode=dev`
- **Helm**: set `developmentMode: true`

**Default behavior in dev mode:**
- **Metrics bind**: `:8082` (unless overridden)
- **Health bind**: `:8083` (unless overridden)
- **Cache strategy**: `on-demand` (or `none` if `--ultra-fast`)
- **skip-cache-wait**: auto-enabled if not explicitly set
- **API rate limits**: QPS 100, Burst 200

Helm always sets `--metrics-bind-address` and `--health-probe-bind-address`, so the dev-mode defaults above only apply when you run the binary directly or override those values.

**In-cluster dev workflow (Kind only):**
```bash
make dev-cluster
make docker-build IMG=neo4j-operator:dev
kind load docker-image neo4j-operator:dev --name neo4j-operator-dev
make deploy-dev
kubectl logs -f -n neo4j-operator-dev deployment/neo4j-operator-controller-manager
```

Note: `config/overlays/dev` sets the dev image, namespace, and `--mode=dev`. If you customize via Kustomize, keep `--mode=dev` in the manager args or use Helm `developmentMode: true`.

## Scope and RBAC

Scope determines where the operator watches CRs, and RBAC determines what it can read/write.

### Cluster Scope

- **RBAC**: ClusterRole + ClusterRoleBinding
- **Use when**: One operator should manage Neo4j in any namespace.
- **Helm**: `operatorMode: cluster` (default)
- **Non-Helm**: leave `WATCH_NAMESPACE` unset/empty.

### Namespace Scope

- **RBAC**: Role + RoleBinding in the operator namespace
- **Use when**: Strict separation per team/namespace.
- **Helm**: `operatorMode: namespace`
- **Non-Helm**: set `WATCH_NAMESPACE=<namespace>`.
- **Adding namespaces**: deploy another operator in that namespace or switch to multi-namespace/cluster scope.

### Multi-Namespace Scope

- **RBAC**: ClusterRole + ClusterRoleBinding
- **Use when**: One operator should manage a fixed list of namespaces.
- **Helm**:
  ```yaml
  operatorMode: namespaces
  watchNamespaces:
    - team-a
    - team-b
  ```
- **Non-Helm**: set `WATCH_NAMESPACE=team-a,team-b` (comma-separated).
- **Adding namespaces**: update `watchNamespaces` (or `WATCH_NAMESPACE`) and upgrade/redeploy the operator.

Note: backup workflows create per-namespace RBAC (ServiceAccount/Role/RoleBinding) as needed for backup jobs.

## Helm Configuration

Key values for modes and scope in `charts/neo4j-operator/values.yaml`:

```yaml
# Scope
operatorMode: cluster        # cluster | namespace | namespaces
watchNamespaces: []          # used only for operatorMode=namespaces

# Mode
developmentMode: false
logLevel: info

# Metrics and leader election
metrics:
  enabled: true
  service:
    port: 8080
leaderElection:
  enabled: true
```

Examples:

```bash
# Development mode + cluster scope
helm upgrade --install neo4j-operator oci://ghcr.io/neo4j-partners/charts/neo4j-operator \
  --namespace neo4j-operator-dev \
  --create-namespace \
  --set developmentMode=true \
  --set logLevel=debug

# Multi-namespace scope
helm upgrade --install neo4j-operator oci://ghcr.io/neo4j-partners/charts/neo4j-operator \
  --namespace neo4j-operator-system \
  --create-namespace \
  --set operatorMode=namespaces \
  --set watchNamespaces={team-a,team-b}
```

## Non-Helm Configuration

If you deploy with Kustomize or raw manifests, configure scope and mode explicitly:

**Recommended Kustomize targets:**
- `config/overlays/prod` (production)
- `config/overlays/dev` (development, Kind only)
- `config/overlays/namespace-scoped` (single namespace)

**Scope (env var):**
```yaml
- name: WATCH_NAMESPACE
  value: team-a,team-b
```

**Mode (args):**
```yaml
args:
  - --mode=dev
```

Reference overlays:
- `config/overlays/prod`
- `config/overlays/dev`
- `config/overlays/namespace-scoped`

## Cache Strategies

Cache strategy controls how aggressively the operator caches resources via controller-runtime.

| Strategy | Behavior | When to Use |
|----------|----------|-------------|
| `standard` | Default controller-runtime cache for types used by controllers. | Large/stable clusters. |
| `lazy` | Caches essential Neo4j CRDs and uses a longer resync in production. | RBAC-restricted or large clusters. |
| `selective` | Caches a reduced set of Neo4j CRDs. | Resource-constrained environments. |
| `on-demand` | Caches essential Neo4j CRDs; other types use the direct client. | Default choice. |
| `none` | Direct API client; skips caching entirely. | Fast dev iteration only. |

Set via:

```bash
--cache-strategy=on-demand
--cache-strategy=none
--ultra-fast           # equivalent to none and enables skip-cache-wait
--skip-cache-wait      # readiness does not wait for cache sync
```

Note: `--lazy-informers` is accepted but currently does not change behavior; prefer `--cache-strategy`.

## Controller Selection (Dev Mode)

In development mode, you can load only a subset of controllers:

```bash
--controllers=cluster,standalone,database
```

Valid controller names:
- `cluster`
- `standalone`
- `database`
- `backup`
- `restore`
- `plugin`
- `shardeddatabase`

The default dev list does **not** include `shardeddatabase`; add it explicitly when needed.

## Logging and Metrics

**Logging (zap):**
- `--zap-log-level=debug|info|warn|error|dpanic|panic|fatal`
- `--zap-devel=true|false`
- `--zap-encoder=json|console`
- `--zap-stacktrace-level=debug|info|warn|error|dpanic|panic|fatal`

**Operator metrics endpoint:**
- `--metrics-bind-address=:8080` (set to `0` to disable)
- `--metrics-secure=true` (TLS, production mode only)

Helm sets `--metrics-bind-address` based on `metrics.enabled` and `metrics.service.port`.

## Troubleshooting

**Operator sees no CRs:**
- Verify `WATCH_NAMESPACE` (empty for cluster scope).
- Confirm `operatorMode` in Helm matches your intent.

**RBAC forbidden errors:**
- Cluster scope or multi-namespace requires ClusterRole/ClusterRoleBinding.
- Namespace scope requires Role/RoleBinding in the operator namespace.
- Validate with `kubectl auth can-i` using the operator ServiceAccount.

**Metrics not reachable:**
- Ensure `metrics.enabled: true` or `--metrics-bind-address` is not `0`.
- Confirm the Service port matches the bind address.

## Quick Reference

**Defaults by mode (when running `/manager` directly):**

| Setting | Production | Development |
|---------|------------|-------------|
| `--mode` | `production` | `dev` |
| Metrics bind | `:8080` | `:8082` |
| Health bind | `:8081` | `:8083` |
| Cache strategy | `on-demand` | `on-demand` |
| `--skip-cache-wait` | `false` | `true` (auto) |

**Common flags:**

```bash
--mode=production|dev
--leader-elect=true|false
--metrics-bind-address=:8080
--health-probe-bind-address=:8081
--cache-strategy=standard|lazy|selective|on-demand|none
--ultra-fast
--skip-cache-wait
--controllers=cluster,standalone,database,backup,restore,plugin,shardeddatabase
```

**Scope environment variable:**

```bash
WATCH_NAMESPACE=team-a,team-b
```

## Additional Resources

- `docs/developer_guide/architecture.md`
- `docs/developer_guide/development.md`
- `docs/user_guide/guides/monitoring.md`
- `docs/user_guide/guides/performance.md`
- `docs/user_guide/guides/troubleshooting.md`
