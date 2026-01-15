# Neo4j Operator Modes - Quick Reference Card

## Mode Defaults (Binary)

| Setting | Production | Development |
|---------|------------|-------------|
| `--mode` | `production` | `dev` |
| Metrics bind | `:8080` | `:8082` |
| Health bind | `:8081` | `:8083` |
| Cache strategy | `on-demand` | `on-demand` |
| `--skip-cache-wait` | `false` | `true` (auto) |

CLI flags override these defaults.

## Helm Behavior

- `developmentMode: true` adds `--mode=dev` and `--zap-devel=true`.
- `metrics.enabled` controls `--metrics-bind-address` (`0` disables). Port comes from `metrics.service.port`.
- `--health-probe-bind-address=:8081` is always set by the chart.
- `leaderElection.enabled: true` passes `--leader-elect=true`.

## Scope

- `operatorMode: cluster|namespace|namespaces`.
- `watchNamespaces` is used only when `operatorMode=namespaces`.
- Non-Helm deployments use `WATCH_NAMESPACE=team-a,team-b` (empty = all namespaces).

## Essential Commands

### Production
```bash
make deploy-prod
kubectl apply -k config/overlays/prod
```

### Development (In-Cluster Only, Kind)
```bash
make dev-cluster
make deploy-dev
kubectl logs -f -n neo4j-operator-dev deployment/neo4j-operator-controller-manager
```

## Common Flags
```bash
--mode=production|dev
--leader-elect=true|false
--metrics-bind-address=:8080
--metrics-secure=true
--health-probe-bind-address=:8081
--cache-strategy=standard|lazy|selective|on-demand|none
--ultra-fast
--skip-cache-wait
--controllers=cluster,standalone,database,backup,restore,plugin,shardeddatabase
--zap-log-level=debug|info|warn|error|dpanic|panic|fatal
```

## Cache Strategies

| Strategy | Behavior | When to Use |
|----------|----------|-------------|
| `standard` | Default controller-runtime cache for types used by controllers. | Large/stable clusters. |
| `lazy` | Caches essential Neo4j CRDs and uses a longer resync in production. | RBAC-restricted or large clusters. |
| `selective` | Caches a reduced set of Neo4j CRDs. | Resource-constrained environments. |
| `on-demand` | Caches essential Neo4j CRDs; other types use the direct client. | Default choice. |
| `none` | Direct API client; skips caching entirely. | Fast dev iteration only. |

## Controller Selection (Dev Mode)

```bash
# Available controllers:
cluster      # Neo4jEnterpriseCluster
standalone   # Neo4jEnterpriseStandalone
database     # Neo4jDatabase
backup       # Neo4jBackup
restore      # Neo4jRestore
plugin       # Neo4jPlugin
shardeddatabase  # Neo4jShardedDatabase

# Examples:
--controllers=cluster,database
--controllers=backup,restore
--controllers=cluster
```

The default dev list does not include `shardeddatabase`; add it explicitly when needed.

## Environment Variables

```bash
export WATCH_NAMESPACE=team-a,team-b
export GOMEMLIMIT=500MiB
export GOMAXPROCS=4
```

## Health and Metrics (Example with Production Defaults)

```bash
curl http://localhost:8081/healthz
curl http://localhost:8081/readyz
curl http://localhost:8080/metrics
```

## Quick Start

`make deploy-prod` (production) or `make deploy-dev` (development)

Full guide: `docs/user_guide/operator-modes.md`
