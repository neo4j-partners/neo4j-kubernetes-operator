# Neo4j Operator Modes - Quick Reference Card

## Operator Modes Overview

| Feature | Production Mode | Development Mode |
|---------|----------------|------------------|
| **Default** | ✅ Yes (`--mode=production`) | ❌ Requires `--mode=dev` |
| **Startup Time** | 5-10 seconds | 1-3 seconds |
| **Memory Usage** | 128-512MB | 50-200MB |
| **Ports** | Metrics: 8080, Health: 8081 | Metrics: 8082, Health: 8083 |
| **Leader Election** | ✅ Enabled | ❌ Disabled |
| **Security** | Full hardening | Relaxed |
| **Debugging** | Limited | Full (pprof: 6060) |
| **Hot Reload** | ❌ No | ✅ Yes |
| **Cache Strategy** | OnDemand | OnDemand/None |

## Cache Strategies

| Strategy | Startup | Memory | API Load | Best For |
|----------|---------|--------|----------|----------|
| **standard** | 10-15s | High | Low | Large stable clusters |
| **lazy** | 5-8s | Medium | Medium | RBAC environments |
| **selective** | 3-5s | Low-Med | Medium | Resource-constrained |
| **on-demand** | 2-3s | Low→Med | Medium | **Default choice** |
| **none** | <1s | Minimal | High | Development/testing |

## Essential Commands

### Production Deployment
```bash
# Deploy to cluster
make deploy-prod

# Manual deployment
kubectl apply -k config/overlays/prod

# Custom production config
./manager --mode=production --leader-elect=true --cache-strategy=lazy
```

### Development
```bash
# In-cluster development (REQUIRED)
make deploy-dev

# Watch development logs
kubectl logs -f -n neo4j-operator-dev deployment/neo4j-operator-controller-manager

# Access debug endpoints via port-forward
kubectl port-forward -n neo4j-operator-dev deployment/neo4j-operator-controller-manager 6060:6060
```

**⚠️ CRITICAL:** Never run the operator outside the cluster. In-cluster deployment is required for proper DNS resolution and Neo4j cluster formation.

### Common Flags
```bash
--mode=production|dev              # Set operator mode
--cache-strategy=standard|lazy|selective|on-demand|none
--controllers=cluster,standalone,database,backup,restore,plugin
--leader-elect=true|false          # Enable leader election
--ultra-fast                       # No-cache mode
--skip-cache-wait                  # Skip cache sync
--zap-log-level=debug|info|error   # Log verbosity
--metrics-bind-address=:8080       # Metrics endpoint
--health-probe-bind-address=:8081  # Health endpoint
```

## Troubleshooting Quick Fixes

| Problem | Solution |
|---------|----------|
| **Slow startup** | `--cache-strategy=on-demand --skip-cache-wait` |
| **High memory** | `--cache-strategy=selective` or `export GOMEMLIMIT=400MiB` |
| **RBAC errors** | `--cache-strategy=lazy` |
| **Need fast iteration** | `--mode=dev --ultra-fast` |
| **Debug performance** | Enable pprof: `curl http://localhost:6060/debug/pprof/heap` |

## Development Workflow

```bash
# 1. Create Kind cluster
make dev-cluster

# 2. Deploy operator in dev mode
make deploy-dev

# 3. Make code changes, then rebuild and redeploy
make docker-build IMG=neo4j-operator:dev
kind load docker-image neo4j-operator:dev --name neo4j-operator-dev
kubectl rollout restart -n neo4j-operator-dev deployment/neo4j-operator-controller-manager

# 4. Test changes
kubectl apply -f examples/clusters/minimal-cluster.yaml

# 5. Watch logs for debugging
kubectl logs -f -n neo4j-operator-dev deployment/neo4j-operator-controller-manager
```

## Cache Strategy Selection

```
Production Environment?
├── Yes
│   ├── Large cluster (>100 nodes)? → --cache-strategy=standard
│   ├── RBAC restrictions? → --cache-strategy=lazy
│   └── Default → --cache-strategy=on-demand
└── No (Development)
    ├── Need fastest startup? → --ultra-fast
    ├── Resource constrained? → --cache-strategy=selective
    └── Default → --cache-strategy=on-demand
```

## Port Reference

| Environment | Metrics | Health | pprof |
|-------------|---------|-----------|-------|
| **Production** | 8080 | 8081 | - |
| **Development** | 8082 | 8083 | 6060 |
| **Local** | Custom | Custom | 6060 |

## Environment Variables

```bash
# Essential
export KUBECONFIG=/path/to/config
export WATCH_NAMESPACE=specific-namespace  # Optional

# Performance tuning
export GOMEMLIMIT=500MiB
export GOMAXPROCS=4
export GOGC=100

# Development
export DEVELOPMENT_MODE=true
export DEBUG=true
```

## Health Checks

```bash
# Check operator health
curl http://localhost:8081/healthz

# Check readiness
curl http://localhost:8081/readyz

# Prometheus metrics
curl http://localhost:8080/metrics

# Development pprof (dev mode only)
curl http://localhost:6060/debug/pprof/
```

## Controller Selection (Dev Mode)

```bash
# Available controllers:
cluster      # Neo4jEnterpriseCluster
standalone   # Neo4jEnterpriseStandalone
database     # Neo4jDatabase
backup       # Neo4jBackup
restore      # Neo4jRestore
plugin       # Neo4jPlugin

# Examples:
--controllers=cluster,database        # Core functionality
--controllers=backup,restore          # Backup operations only
--controllers=cluster                 # Minimal for testing
```

---
**Quick Start:** `make deploy-prod` (production) or `make deploy-dev` (development)
**⚠️ Note:** Always run the operator in-cluster - never locally
**Help:** `kubectl exec deployment/neo4j-operator-controller-manager -- /manager --help` or see [full documentation](../user_guide/operator-modes.md)
