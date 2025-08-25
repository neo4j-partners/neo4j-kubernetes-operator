# Neo4j Kubernetes Operator Modes and Configuration Guide

## Table of Contents
- [Overview](#overview)
- [Operator Modes](#operator-modes)
  - [Production Mode](#production-mode)
  - [Development Mode](#development-mode)
- [Caching Strategies](#caching-strategies)
  - [Understanding Kubernetes Informers](#understanding-kubernetes-informers)
  - [Available Cache Strategies](#available-cache-strategies)
  - [Strategy Comparison](#strategy-comparison)
- [Configuration Options](#configuration-options)
  - [Command Line Flags](#command-line-flags)
  - [Environment Variables](#environment-variables)
- [Deployment Methods](#deployment-methods)
  - [In-Cluster Deployment](#in-cluster-deployment)
- [Performance Tuning](#performance-tuning)
- [Troubleshooting](#troubleshooting)
- [Best Practices](#best-practices)
- [Quick Reference](#quick-reference)

## Overview

The Neo4j Kubernetes Operator supports two distinct operational modes designed for different use cases:

- **Production Mode**: Optimized for stability, security, and observability in production environments
- **Development Mode**: Optimized for rapid iteration, debugging, and local development

Each mode provides different defaults and capabilities while maintaining full functionality for managing Neo4j deployments.

## Operator Modes

### Production Mode

Production mode is the default operational mode, designed for running the operator in production Kubernetes clusters with enterprise-grade requirements.

#### Key Characteristics

| Feature | Configuration |
|---------|--------------|
| **Default Mode** | Yes (when no `--mode` flag specified) |
| **Namespace** | `neo4j-operator-system` |
| **Image** | `ghcr.io/neo4j-labs/neo4j-kubernetes-operator:latest` |
| **Resource Limits** | CPU: 100m-500m, Memory: 64Mi-128Mi |
| **Cache Strategy** | OnDemand (optimized for RBAC) |
| **Security** | Full security context, runAsNonRoot, seccomp profiles |
| **Leader Election** | Enabled for high availability |
| **Metrics Port** | 8080 (secure serving available) |
| **Health Port** | 8081 |
| **Controllers** | All 6 controllers loaded |
| **Log Level** | Info (configurable) |

#### Production Features

**Security Hardening:**
```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  fsGroup: 65532
  seccompProfile:
    type: RuntimeDefault
  capabilities:
    drop: ["ALL"]
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
```

**High Availability:**
- Leader election enabled for multiple operator replicas
- Automatic failover support
- Graceful shutdown handling (10-second termination period)

**Resource Management:**
```yaml
resources:
  requests:
    cpu: 100m
    memory: 64Mi
  limits:
    cpu: 500m
    memory: 128Mi
```

**Observability:**
- Prometheus metrics endpoint with optional TLS
- Structured JSON logging
- Health and readiness probes
- Event recording for audit trails

#### Production Deployment

**Using Kustomize (Recommended):**
```bash
# Deploy with production overlay
kubectl apply -k config/overlays/prod

# Or using make target
make deploy-prod
```

**Direct Deployment:**
```bash
# Install CRDs
kubectl apply -f config/crd/bases/

# Deploy operator
kubectl apply -f config/manager/
```

**Configuration Example:**
```bash
# Start operator with custom production settings
./manager \
  --mode=production \
  --leader-elect=true \
  --metrics-secure=true \
  --cache-strategy=lazy \
  --zap-log-level=info
```

### Development Mode

Development mode is optimized for local development, testing, and debugging with faster startup times and enhanced developer tools.

#### Key Characteristics

| Feature | Configuration |
|---------|--------------|
| **Activation** | `--mode=dev` flag |
| **Namespace** | `neo4j-operator-dev` or current namespace |
| **Image** | `neo4j-operator:dev` |
| **Resource Limits** | None (unlimited in local mode) |
| **Cache Strategy** | OnDemand or NoCache (ultra-fast startup) |
| **Security** | Relaxed for development |
| **Leader Election** | Disabled |
| **Metrics Port** | 8082 |
| **Health Port** | 8083 |
| **Controllers** | Configurable selection |
| **Log Level** | Debug (default) |

#### Development Features

**Fast Iteration:**
- Skip cache synchronization for instant startup
- Lazy informer creation
- Higher API rate limits (QPS: 100, Burst: 200)
- Ultra-fast mode with no caching available

**Debugging Support:**
```bash
# Run with Delve debugger
dlv debug cmd/main.go -- --mode=dev --zap-log-level=debug

# Enable pprof profiling (port 6060)
curl http://localhost:6060/debug/pprof/heap
curl http://localhost:6060/debug/pprof/profile?seconds=30
```

**Hot Reload with Air:**
```bash
# Install Air
go install github.com/cosmtrek/air@latest

# Run with hot reload
air --build.cmd "go build -o tmp/main cmd/main.go" \
    --build.bin "./tmp/main --mode=dev"
```

**Controller Selection:**
```bash
# Load only specific controllers
./manager --mode=dev --controllers=cluster,database

# Available controllers:
# - cluster: Neo4jEnterpriseCluster
# - standalone: Neo4jEnterpriseStandalone
# - database: Neo4jDatabase
# - backup: Neo4jBackup
# - restore: Neo4jRestore
# - plugin: Neo4jPlugin
```

#### Development Deployment

**⚠️ CRITICAL: The operator MUST always run in-cluster, even for development. Running outside the cluster causes DNS resolution failures and cluster formation issues.**

**In-Cluster Development (REQUIRED):**
```bash
# Create development cluster
make dev-cluster

# Deploy operator in dev mode
make deploy-dev

# Watch logs for debugging
kubectl logs -f deployment/neo4j-operator-controller-manager \
  -n neo4j-operator-dev

# Access debugging endpoints via port-forward
kubectl port-forward -n neo4j-operator-dev \
  deployment/neo4j-operator-controller-manager 6060:6060
```

**Development Features Available In-Cluster:**
- Fast startup with optimized cache strategies
- Debug logging enabled by default
- Controller selection via environment variables
- Profiling endpoints accessible via port-forward
- Hot reload through image rebuilds and redeployment

## Caching Strategies

### Understanding Kubernetes Informers

Kubernetes informers are a critical component of controller performance, providing a local cache of cluster resources to reduce API server load. The operator offers multiple caching strategies to balance between startup time, memory usage, and API efficiency.

### Available Cache Strategies

#### 1. Standard Cache

**Description:** Traditional controller-runtime caching with full informer synchronization.

**Characteristics:**
- Full resource caching on startup
- Highest memory usage
- Lowest API server load after initial sync
- Slowest startup time (10-15 seconds)

**Best For:**
- Stable production environments
- Large clusters with many resources
- Scenarios requiring minimal API calls

**Configuration:**
```bash
--cache-strategy=standard
```

#### 2. Lazy Cache

**Description:** Delays informer creation until resources are actually accessed.

**Characteristics:**
- Informers created on first use
- Moderate memory usage
- Balanced startup time (5-8 seconds)
- Good for production with RBAC constraints

**Best For:**
- Production environments with RBAC
- Medium-sized clusters
- Balanced performance requirements

**Configuration:**
```bash
--cache-strategy=lazy
```

**Implementation Details:**
```go
// Only caches essential resources initially
ByObject: {
  Neo4jEnterpriseCluster: {},
  Neo4jEnterpriseStandalone: {},
  Neo4jDatabase: {},
  // Other resources cached on demand
}
```

#### 3. Selective Cache

**Description:** Caches only specific resource types that the operator manages.

**Characteristics:**
- Caches Neo4j CRDs and managed Kubernetes resources
- Reduced memory footprint
- Faster startup (3-5 seconds)
- Efficient for focused operations

**Best For:**
- Resource-constrained environments
- Operators with limited scope
- Development and testing

**Configuration:**
```bash
--cache-strategy=selective
```

**Cached Resources:**
- All Neo4j CRDs (Cluster, Standalone, Database, Backup, Restore, Plugin)
- StatefulSets, Services, ConfigMaps, Secrets managed by operator
- Pods for health monitoring

#### 4. OnDemand Cache (Default)

**Description:** Creates informers only when specific resources are needed.

**Characteristics:**
- Minimal initial caching
- Lowest memory usage at startup
- Fast startup (2-3 seconds)
- Gradual memory increase as resources are accessed

**Best For:**
- Default production choice
- Dynamic environments
- Quick operator restarts

**Configuration:**
```bash
--cache-strategy=on-demand  # Default for both modes
```

#### 5. No Cache

**Description:** Bypasses informer caching entirely, using direct API calls.

**Characteristics:**
- No memory overhead from caching
- Instant startup (<1 second)
- Highest API server load
- Every operation requires API call

**Best For:**
- Ultra-fast development iteration
- Testing and debugging
- Short-lived operator instances

**Configuration:**
```bash
--cache-strategy=none
# Or
--ultra-fast  # Enables no-cache mode
```

### Strategy Comparison

| Strategy | Startup Time | Memory Usage | API Load | Use Case |
|----------|-------------|--------------|----------|----------|
| **Standard** | 10-15s | High (200-500MB) | Low | Large production clusters |
| **Lazy** | 5-8s | Medium (100-300MB) | Medium | Production with RBAC |
| **Selective** | 3-5s | Low-Medium (80-200MB) | Medium | Resource-constrained |
| **OnDemand** | 2-3s | Low initially (50-150MB) | Medium | Default choice |
| **None** | <1s | Minimal (30-50MB) | High | Development/debugging |

### Cache Configuration Examples

**Production with Lazy Cache:**
```bash
./manager --mode=production \
  --cache-strategy=lazy \
  --leader-elect=true
```

**Development with No Cache:**
```bash
./manager --mode=dev \
  --cache-strategy=none \
  --skip-cache-wait
```

**Custom Cache Configuration:**
```go
// In code: Custom cache configuration
cacheOpts := cache.Options{
  SyncPeriod: &syncPeriod,  // How often to resync
  DefaultNamespaces: map[string]cache.Config{
    "neo4j-apps": {},  // Watch specific namespace
  },
  ByObject: map[client.Object]cache.ByObject{
    &neo4jv1alpha1.Neo4jEnterpriseCluster{}: {
      Label: labels.SelectorFromSet(labels.Set{
        "managed-by": "neo4j-operator",
      }),
    },
  },
}
```

## Configuration Options

### Command Line Flags

#### Core Flags

| Flag | Description | Default | Modes |
|------|-------------|---------|-------|
| `--mode` | Operator mode: production or dev | `production` | Both |
| `--cache-strategy` | Cache strategy (see above) | Auto-selected | Both |
| `--leader-elect` | Enable leader election | `false` | Production |
| `--metrics-bind-address` | Metrics endpoint address | Mode-dependent | Both |
| `--health-probe-bind-address` | Health probe address | Mode-dependent | Both |

#### Development Flags

| Flag | Description | Default | Modes |
|------|-------------|---------|-------|
| `--controllers` | Comma-separated controller list | All controllers | Dev only |
| `--skip-cache-wait` | Skip cache synchronization | `false` (true in dev) | Both |
| `--lazy-informers` | Enable lazy informer creation | `false` | Both |
| `--ultra-fast` | No-cache mode for fastest startup | `false` | Both |

#### Logging Flags

| Flag | Description | Default | Modes |
|------|-------------|---------|-------|
| `--zap-log-level` | Log verbosity (debug/info/error) | Mode-dependent | Both |
| `--zap-devel` | Development logging format | `true` | Both |
| `--zap-encoder` | Log encoding (json/console) | Mode-dependent | Both |
| `--zap-stacktrace-level` | Stack trace trigger level | `error` | Both |

### Environment Variables

The operator respects standard Kubernetes controller environment variables:

```bash
# Kubernetes configuration
export KUBECONFIG=/path/to/kubeconfig

# Namespace to watch (empty = all namespaces)
export WATCH_NAMESPACE=neo4j-apps

# Development mode
export DEVELOPMENT_MODE=true

# Metrics configuration
export METRICS_BIND_ADDRESS=:8080
export HEALTH_PROBE_BIND_ADDRESS=:8081

# Performance tuning
export GOMEMLIMIT=500MiB
export GOMAXPROCS=4
```

## Deployment Methods

### In-Cluster Deployment

#### Production Deployment

```bash
# 1. Create namespace
kubectl create namespace neo4j-operator-system

# 2. Install CRDs
kubectl apply -f config/crd/bases/

# 3. Deploy operator
kubectl apply -k config/overlays/prod

# 4. Verify deployment
kubectl get deployment -n neo4j-operator-system
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager
```

#### Development Deployment

```bash
# 1. Create Kind cluster
make dev-cluster

# 2. Build and load image
make docker-build IMG=neo4j-operator:dev
kind load docker-image neo4j-operator:dev --name neo4j-operator-dev

# 3. Deploy operator
make deploy-dev

# 4. Watch logs
kubectl logs -f deployment/neo4j-operator-controller-manager \
  -n neo4j-operator-dev
```

### Development Debugging

**All debugging must be done with the operator running in-cluster. Use the following techniques:**

#### In-Cluster Debugging with Port-Forward

```bash
# Deploy operator in dev mode with debug image
make dev-cluster
make deploy-dev

# Port-forward pprof endpoint for profiling
kubectl port-forward -n neo4j-operator-dev \
  deployment/neo4j-operator-controller-manager 6060:6060

# Access profiling endpoints
curl http://localhost:6060/debug/pprof/heap
curl http://localhost:6060/debug/pprof/profile?seconds=30
```

#### Development Image with Debugging Tools

```dockerfile
# Build debug-enabled operator image
FROM golang:1.24-alpine AS debug
RUN go install github.com/go-delve/delve/cmd/dlv@latest
COPY manager /manager
EXPOSE 40000
CMD ["dlv", "exec", "/manager", "--headless", "--listen=:40000", "--api-version=2", "--", "--mode=dev"]
```

#### Remote Debugging Setup

```bash
# Build and deploy debug image
docker build -t neo4j-operator:debug -f Dockerfile.debug .
kind load docker-image neo4j-operator:debug --name neo4j-operator-dev

# Port-forward Delve port
kubectl port-forward -n neo4j-operator-dev \
  deployment/neo4j-operator-controller-manager 40000:40000

# Connect with IDE or dlv client
dlv connect localhost:40000
```

## Performance Tuning

### Memory Optimization

```bash
# Limit Go memory usage
export GOMEMLIMIT=500MiB

# Tune garbage collection
export GOGC=100  # Default
export GOGC=50   # More aggressive GC, lower memory

# Monitor memory usage
go tool pprof http://localhost:6060/debug/pprof/heap
```

### API Rate Limiting

```go
// Configured per mode
config.QPS = 100    // Queries per second
config.Burst = 200  // Burst capacity

// Custom configuration
restConfig.QPS = 50
restConfig.Burst = 100
restConfig.Timeout = 30 * time.Second
```

### Cache Tuning

```bash
# Fast startup, gradual cache building
--cache-strategy=on-demand --skip-cache-wait

# Minimal memory usage
--cache-strategy=none --ultra-fast

# Balanced for production
--cache-strategy=lazy --sync-period=5m
```

### Concurrent Reconciliation

```go
// Controller configuration
MaxConcurrentReconciles: 3  // Parallel reconciliation
RecoverPanic: true          // Recover from panics
RateLimiter: workqueue.NewItemExponentialFailureRateLimiter(
  5*time.Millisecond,    // Base delay
  1000*time.Second,      // Max delay
)
```

## Troubleshooting

### Common Issues

#### Slow Startup

**Symptoms:** Operator takes >30 seconds to become ready

**Solutions:**
```bash
# Use faster cache strategy
--cache-strategy=on-demand

# Skip cache wait
--skip-cache-wait

# Development mode for testing
--mode=dev --ultra-fast
```

#### High Memory Usage

**Symptoms:** Operator consuming >500MB memory

**Solutions:**
```bash
# Use selective caching
--cache-strategy=selective

# Limit watched namespaces
export WATCH_NAMESPACE=neo4j-apps

# Set memory limits
export GOMEMLIMIT=400MiB
```

#### RBAC Errors

**Symptoms:** "forbidden" errors in logs

**Solutions:**
```bash
# Check RBAC permissions
kubectl auth can-i list neo4jenterpriseclusters --as=system:serviceaccount:neo4j-operator-system:neo4j-operator-controller-manager

# Use lazy cache to avoid pre-caching forbidden resources
--cache-strategy=lazy
```

#### Leader Election Issues

**Symptoms:** Multiple operators competing, frequent restarts

**Solutions:**
```bash
# Ensure unique leader election ID
--leader-election-id=neo4j-operator-unique-id

# Disable for single instance
--leader-elect=false
```

### Debug Techniques

#### Enable Verbose Logging

```bash
# Maximum verbosity
--zap-log-level=debug --zap-devel=true

# Specific component debugging
export DEBUG_CONTROLLER=true
export DEBUG_CACHE=true
```

#### Profile Performance

```bash
# CPU profiling
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof

# Memory profiling
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof heap.prof

# Goroutine analysis
curl http://localhost:6060/debug/pprof/goroutine?debug=2
```

#### Trace Reconciliation

```go
// Add to controller
log.Info("Reconciling",
  "resource", req.NamespacedName,
  "generation", cluster.Generation,
  "resourceVersion", cluster.ResourceVersion,
)
```

## Best Practices

### Production Deployment

1. **Use Production Mode Explicitly**
   ```bash
   --mode=production --leader-elect=true
   ```

2. **Configure Resource Limits**
   ```yaml
   resources:
     requests:
       memory: "128Mi"
       cpu: "100m"
     limits:
       memory: "512Mi"
       cpu: "1000m"
   ```

3. **Enable Monitoring**
   ```yaml
   # ServiceMonitor for Prometheus
   apiVersion: monitoring.coreos.com/v1
   kind: ServiceMonitor
   metadata:
     name: neo4j-operator
   spec:
     selector:
       matchLabels:
         app: neo4j-operator
     endpoints:
     - port: metrics
   ```

4. **Use Appropriate Cache Strategy**
   - Default `on-demand` for most cases
   - `lazy` for RBAC-heavy environments
   - `standard` for stable, large clusters

5. **Implement Health Checks**
   ```yaml
   livenessProbe:
     httpGet:
       path: /healthz
       port: 8081
     periodSeconds: 10
   readinessProbe:
     httpGet:
       path: /readyz
       port: 8081
     periodSeconds: 5
   ```

### Development Workflow

**⚠️ MANDATORY: Always run the operator in-cluster for development. Local execution causes DNS resolution failures and cluster formation issues.**

1. **In-Cluster Development Setup**
   ```bash
   # Create development cluster
   make dev-cluster

   # Deploy operator in dev mode
   make deploy-dev

   # Verify deployment
   kubectl get deployment -n neo4j-operator-dev
   ```

2. **Development Iteration Cycle**
   ```bash
   # 1. Make code changes
   # 2. Build and load new image
   make docker-build IMG=neo4j-operator:dev
   kind load docker-image neo4j-operator:dev --name neo4j-operator-dev

   # 3. Restart deployment with new image
   kubectl rollout restart -n neo4j-operator-dev deployment/neo4j-operator-controller-manager

   # 4. Monitor logs
   kubectl logs -f -n neo4j-operator-dev deployment/neo4j-operator-controller-manager
   ```

3. **In-Cluster Debugging**
   ```bash
   # Enable debug logging
   kubectl patch -n neo4j-operator-dev deployment/neo4j-operator-controller-manager \
     -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--mode=dev","--zap-log-level=debug"]}]}}}}'

   # Access pprof endpoints via port-forward
   kubectl port-forward -n neo4j-operator-dev deployment/neo4j-operator-controller-manager 6060:6060
   curl http://localhost:6060/debug/pprof/heap
   ```

4. **Testing Pattern**
   ```bash
   # Deploy test workloads
   kubectl apply -f examples/clusters/minimal-cluster.yaml

   # Watch cluster formation
   kubectl get neo4jenterprisecluster -w

   # Run integration tests
   make test-integration
   ```

### Security Recommendations

1. **Production Security**
   - Always run as non-root user
   - Enable seccomp profiles
   - Use read-only root filesystem
   - Implement network policies

2. **RBAC Configuration**
   - Use minimal permissions
   - Separate roles for different environments
   - Regular permission audits

3. **Secret Management**
   - Never log sensitive data
   - Use Kubernetes secrets
   - Implement secret rotation

## Quick Reference

### Mode Comparison

| Feature | Production | Development |
|---------|------------|-------------|
| **Default** | ✅ Yes | ❌ No |
| **Startup Time** | 5-10s | 1-3s |
| **Memory Usage** | 128-512MB | 50-200MB |
| **CPU Usage** | 100-500m | Unlimited |
| **Security** | Full | Relaxed |
| **Leader Election** | ✅ Enabled | ❌ Disabled |
| **Hot Reload** | ❌ No | ✅ Yes |
| **Debugging** | Limited | Full |
| **Cache Strategy** | OnDemand | OnDemand/None |
| **Log Level** | Info | Debug |
| **Controllers** | All | Configurable |

### Common Commands

```bash
# Production deployment
make deploy-prod

# Development deployment
make deploy-dev

# Development deployment
make deploy-dev

# Watch development logs
kubectl logs -f -n neo4j-operator-dev deployment/neo4j-operator-controller-manager

# Access debug endpoints
kubectl port-forward -n neo4j-operator-dev deployment/neo4j-operator-controller-manager 6060:6060

# Production with custom cache
./manager --mode=production --cache-strategy=lazy --leader-elect=true

# Profile memory usage
curl http://localhost:6060/debug/pprof/heap > heap.prof && go tool pprof heap.prof

# Check operator version
./manager --version

# View all options
./manager --help
```

### Port Reference

| Mode | Metrics | Health | pprof |
|------|---------|-----------|-------|
| **Production** | 8080 | 8081 | - |
| **Development** | 8082 | 8083 | 6060 |

### Cache Strategy Selection Flow

```
Is this production?
├── Yes
│   ├── Large stable cluster? → Standard
│   ├── RBAC constraints? → Lazy
│   └── Default → OnDemand
└── No (Development)
    ├── Need fastest startup? → None (--ultra-fast)
    ├── Testing specific features? → Selective
    └── Default → OnDemand
```

## Additional Resources

- [Kubernetes Controller Best Practices](https://kubernetes.io/docs/concepts/architecture/controller/)
- [controller-runtime Documentation](https://pkg.go.dev/sigs.k8s.io/controller-runtime)
- [Neo4j Operator Architecture](./architecture.md)
- [Troubleshooting Guide](./troubleshooting.md)
- [Performance Tuning Guide](./guides/performance.md)

---

*Last Updated: January 2025*
*Version: 1.0.0*
