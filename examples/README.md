# Neo4j Kubernetes Operator Examples

This directory contains example configurations for deploying Neo4j Enterprise clusters using the Neo4j Kubernetes Operator.

## Prerequisites

Before deploying any examples, ensure you have:

1. **Neo4j Kubernetes Operator installed** in your cluster
2. **cert-manager with ClusterIssuer** (automatically installed in dev/test clusters)
3. **Appropriate storage classes** available in your cluster
4. **Neo4j Enterprise license** (required for all examples)

**Note**: Development and test clusters created with `make dev-cluster` or `make test-cluster` automatically include cert-manager v1.18.2 and a self-signed ClusterIssuer (`ca-cluster-issuer`) for TLS testing.

## Quick Start

### 1. Create Admin Credentials

All examples require an admin secret. Create it first:

```bash
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j \
  --from-literal=password=your-secure-password
```

### 2. Deploy a Cluster

Choose an example and deploy:

```bash
# Single-node for development/testing
kubectl apply -f examples/clusters/single-node.yaml

# Three-node cluster for production (with TLS)
kubectl apply -f examples/clusters/three-node-cluster.yaml

# Three-node cluster for testing (TLS disabled)
kubectl apply -f examples/clusters/three-node-simple.yaml

# Cluster with read replicas for read scaling
kubectl apply -f examples/clusters/cluster-with-read-replicas.yaml

# Kubernetes discovery example (automatic discovery configuration)
kubectl apply -f examples/clusters/k8s-discovery-cluster.yaml

# Two primary nodes (⚠️ NOT recommended for production)
kubectl apply -f examples/clusters/two-primary-cluster.yaml

# Four primary nodes (⚠️ Consider 3+1 read replica instead)
kubectl apply -f examples/clusters/four-primary-cluster.yaml
```

### 3. Access Neo4j

Once deployed, access Neo4j through port forwarding:

```bash
# Port forward to the cluster
kubectl port-forward svc/your-cluster-name-client 7474:7474 7687:7687

# Open Neo4j Browser
open http://localhost:7474
```

## Automatic Kubernetes Discovery

**All clusters automatically use Kubernetes Discovery** for cluster member discovery. The operator handles all configuration automatically:

### What the Operator Creates Automatically

1. **RBAC Resources**:
   - ServiceAccount: `{cluster-name}-discovery`
   - Role: `{cluster-name}-discovery` (with service list permissions)
   - RoleBinding: `{cluster-name}-discovery`

2. **Discovery Services**:
   - Primary headless service: `{cluster-name}-primary-headless`
   - Secondary headless service: `{cluster-name}-secondary-headless` (when needed)

3. **Neo4j Configuration**:
   ```properties
   dbms.cluster.discovery.resolver_type=K8S
   dbms.kubernetes.label_selector=app.kubernetes.io/name=neo4j,app.kubernetes.io/instance={cluster-name},neo4j.com/role=primary
   dbms.kubernetes.discovery.service_port_name=discovery
   ```

### Benefits

- ✅ **Dynamic discovery** - automatic adaptation to scaling
- ✅ **Cloud-native integration** - uses Kubernetes API
- ✅ **Zero configuration** - no manual setup required
- ✅ **Automatic RBAC** - proper security permissions

**No manual discovery configuration needed or supported!** Simply deploy a cluster and the operator handles everything. Any manual discovery settings in `spec.config` are automatically overridden to ensure consistent Kubernetes discovery.

## Example Configurations

### `clusters/single-node.yaml`

- **Use case**: Development, testing, small workloads that may need to scale
- **Topology**: 1 primary, 0 secondaries (scalable to multi-node)
- **Mode**: Unified clustering (all deployments use clustering infrastructure)
- **TLS**: Disabled for simplicity
- **Resources**: 2Gi RAM, 500m CPU

### `clusters/three-node-cluster.yaml`

- **Use case**: Production, high availability
- **Topology**: 3 primaries, 0 secondaries
- **Mode**: Multi-node clustered deployment
- **TLS**: cert-manager enabled
- **Resources**: 4Gi RAM, 1 CPU
- **Features**: Production configuration, monitoring enabled

### `clusters/three-node-simple.yaml`

- **Use case**: Testing, development, environments without cert-manager
- **Topology**: 3 primaries, 0 secondaries
- **Mode**: Clustered with quorum
- **TLS**: Disabled for simplicity
- **Resources**: 2Gi RAM, 500m CPU
- **Features**: Testing configuration, quick deployment

### `clusters/cluster-with-read-replicas.yaml`

- **Use case**: Read-heavy workloads, horizontal scaling
- **Topology**: 3 primaries, 2 secondaries (read replicas)
- **Mode**: Clustered with read scaling
- **TLS**: Disabled for simplicity
- **Resources**: 3Gi RAM, 750m CPU
- **Features**: Optimized for read performance

### `clusters/k8s-discovery-cluster.yaml`

- **Use case**: Production workload showcasing automatic Kubernetes discovery
- **Topology**: 3 primaries, 2 secondaries
- **Mode**: Clustered with automatic discovery
- **TLS**: cert-manager enabled
- **Resources**: 4Gi RAM, 2 CPU
- **Features**: Multi-zone placement, LoadBalancer service, automatic RBAC

## Fault Tolerance Considerations ⚠️

The operator now allows even numbers of primary nodes but issues warnings about reduced fault tolerance. Understanding these implications is crucial for production deployments.

### Primary Node Recommendations

| Configuration | Fault Tolerance | Use Case | Recommendation |
|---------------|----------------|----------|----------------|
| 1 Primary | None | Development | ✅ Development only |
| 2 Primaries | ⚠️ Limited | Cost-constrained | ❌ Avoid for production |
| 3 Primaries | ✅ 1 node failure | Production | ✅ **Recommended minimum** |
| 4 Primaries | ⚠️ 1 node failure (same as 3) | - | ❌ Use 3+1 read replica |
| 5 Primaries | ✅ 2 node failures | High availability | ✅ Mission-critical |
| 6 Primaries | ⚠️ 2 node failures (same as 5) | - | ❌ Use 5+1 read replica |
| 7 Primaries | ✅ 3 node failures | Maximum availability | ✅ Extreme requirements |

### Operator Warnings

When deploying with even numbers of primaries, the operator will emit warnings:

```
Warning: Even number of primary nodes (2) reduces fault tolerance.
In a split-brain scenario, the cluster may become unavailable.
Consider using an odd number (3, 5, or 7) for optimal fault tolerance.
```

### Best Practices

1. **Use odd numbers** of primary nodes for production
2. **3 primaries minimum** for any production deployment
3. **Scale reads with secondaries**, not excessive primaries
4. **Monitor cluster health** continuously
5. **Test failover scenarios** regularly

For detailed fault tolerance analysis, see: [Fault Tolerance Guide](../docs/user_guide/guides/fault_tolerance.md)

## Customization Guide

### Storage

Update the storage configuration for your environment:

```yaml
storage:
  className: your-storage-class  # e.g., gp2, standard, fast-ssd
  size: "50Gi"                  # Adjust based on data requirements
```

### Resources

Adjust resource allocation based on your workload:

```yaml
resources:
  requests:
    memory: "4Gi"    # Initial allocation
    cpu: "1"
  limits:
    memory: "8Gi"    # Maximum allocation
    cpu: "4"
```

### TLS Configuration

For development/testing, use the automatically configured self-signed issuer:

```yaml
tls:
  mode: cert-manager
  issuerRef:
    name: ca-cluster-issuer  # Self-signed issuer for development
    kind: ClusterIssuer
```

For production, replace with your own ClusterIssuer:

```yaml
tls:
  mode: cert-manager
  issuerRef:
    name: letsencrypt-prod   # Your production issuer
    kind: ClusterIssuer
```

### Custom Configuration

Add Neo4j-specific settings:

```yaml
config:
  dbms.logs.query.enabled: "INFO"
  dbms.transaction.timeout: "60s"
  metrics.enabled: "true"
```

## Topology Guidelines

| Use Case | Primaries | Secondaries | Notes |
|----------|-----------|-------------|-------|
| Development | 1 | 0 | Single-primary cluster (scalable) |
| Testing | 1 | 0 | Single-primary cluster (scalable) |
| Small Production | 3 | 0 | Minimal HA cluster |
| Large Production | 3 | 0-5 | Add replicas for read scaling |
| Read-Heavy | 3 | 2+ | Horizontal read scaling |

## Deployment Behavior

### Cluster Formation Process

Neo4j clusters use parallel pod startup with coordinated formation:

1. **Parallel Startup**: All pods start simultaneously for faster deployment
2. **Discovery Phase**: Pods discover each other via Kubernetes service discovery
3. **Coordination**: All primary nodes coordinate to form initial cluster membership
4. **Total Time**: Typical cluster formation completes in 2-3 minutes

### Expected Timeline

| Phase | Activity | Timing |
|-------|----------|--------|
| Resource Creation | StatefulSets, Services, ConfigMaps | 0-30 seconds |
| Pod Startup | All pods start in parallel | 30-60 seconds |
| Cluster Formation | Coordination and membership | 1-3 minutes |

**Note**: The operator uses parallel pod management for efficient cluster formation while maintaining data consistency.

## Troubleshooting

### Common Issues

1. **Pod stuck in Pending**: Check storage class and PVC binding
2. **License errors**: Verify `NEO4J_ACCEPT_LICENSE_AGREEMENT=yes`
3. **TLS issues**: Ensure cert-manager and issuer are configured
4. **Memory issues**: Increase resource limits if pods are OOMKilled
5. **Cluster formation slow**: Multi-node clusters start pods sequentially - expect 1-2 minutes between pods
6. **Second/third pod not ready**: Wait for StatefulSet sequential startup; check readiness probes

### Useful Commands

```bash
# Check cluster status
kubectl get neo4jenterprisecluster

# View cluster details
kubectl describe neo4jenterprisecluster your-cluster-name

# Check pod logs
kubectl logs -l neo4j.com/cluster=your-cluster-name

# Check operator logs
kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager
```

## Directory Structure

- **`clusters/`** - Production-ready cluster configurations with various topologies
- **`standalone/`** - Single-node Neo4j deployments for development
- **`backup-restore/`** - Backup and restore operation examples
- **`database/`** - Database creation and management examples
- **`end-to-end/`** - Complete deployment scenarios for production use
- **`testing/`** - Test configurations used for operator development and validation

## Support

For more information, see:
- [User Guide](../docs/user_guide/getting_started.md)
- [Configuration Reference](../docs/user_guide/configuration.md)
- [Troubleshooting Guide](../docs/user_guide/guides/troubleshooting.md)
