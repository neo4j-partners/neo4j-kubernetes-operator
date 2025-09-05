# Neo4j Kubernetes Operator Examples

This directory contains example configurations for deploying Neo4j Enterprise clusters using the Neo4j Kubernetes Operator.

## Prerequisites

Before deploying any examples, ensure you have:

1. **Neo4j Kubernetes Operator installed** in your cluster:
   ```bash
   # Standard deployment (uses local images)
   make deploy-dev   # or make deploy-prod

   # Registry-based deployment (requires ghcr.io access)
   make deploy-prod-registry
   ```
2. **cert-manager v1.18+ with ClusterIssuer** (automatically installed in dev/test clusters)
3. **Appropriate storage classes** available in your cluster
4. **Neo4j Enterprise Edition** (evaluation license acceptable for testing)

**Note**: Development and test clusters created with `make dev-cluster` or `make test-cluster` automatically include cert-manager v1.18.2 and a self-signed ClusterIssuer (`ca-cluster-issuer`) for TLS testing. The operator works with Neo4j Enterprise 5.26+ and 2025.x versions.

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
# Minimal cluster (2 servers for high availability)
kubectl apply -f examples/clusters/minimal-cluster.yaml

# Three-server cluster for production (with TLS)
kubectl apply -f examples/clusters/three-node-cluster.yaml

# Three-server cluster for testing (TLS disabled)
kubectl apply -f examples/clusters/three-node-simple.yaml

# Multi-server cluster for production (with TLS and advanced features)
kubectl apply -f examples/clusters/multi-server-cluster.yaml

# Multi-zone deployment with topology placement
kubectl apply -f examples/clusters/topology-placement-cluster.yaml

# Six-server cluster for large deployments
kubectl apply -f examples/clusters/six-server-cluster.yaml

# Two-server cluster (minimum for high availability)
kubectl apply -f examples/clusters/two-server-cluster.yaml
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
   - Discovery service: `{cluster-name}-discovery` (ClusterIP with `neo4j.com/clustering=true` label)
   - Headless service: `{cluster-name}-headless` (for pod-to-pod communication)

3. **Neo4j Configuration**:
   ```properties
   dbms.cluster.discovery.resolver_type=K8S
   dbms.kubernetes.label_selector=neo4j.com/clustering=true
   dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery
   dbms.cluster.discovery.version=V2_ONLY
   ```

### Benefits

- ✅ **Dynamic discovery** - automatic adaptation to scaling
- ✅ **Cloud-native integration** - uses Kubernetes API
- ✅ **Zero configuration** - no manual setup required
- ✅ **Automatic RBAC** - proper security permissions

**No manual discovery configuration needed or supported!** Simply deploy a cluster and the operator handles everything. Any manual discovery settings in `spec.config` are automatically overridden to ensure consistent Kubernetes discovery.

## Example Configurations

### `clusters/minimal-cluster.yaml`

- **Use case**: Development, testing, minimum high availability
- **Topology**: 2 servers (minimum for clustering)
- **Mode**: Server-based clustering (servers self-organize)
- **TLS**: Disabled for simplicity
- **Resources**: 2Gi RAM, 500m CPU

### `clusters/three-node-cluster.yaml`

- **Use case**: Production, high availability
- **Topology**: 3 servers (optimal fault tolerance)
- **Mode**: Server-based clustering with TLS
- **TLS**: cert-manager enabled
- **Resources**: 4Gi RAM, 1 CPU
- **Features**: Production configuration, monitoring enabled

### `clusters/three-node-simple.yaml`

- **Use case**: Testing, development, environments without cert-manager
- **Topology**: 3 servers (optimal fault tolerance)
- **Mode**: Server-based clustering, TLS disabled
- **TLS**: Disabled for simplicity
- **Resources**: 2Gi RAM, 500m CPU
- **Features**: Testing configuration, quick deployment

### `clusters/cluster-with-read-replicas.yaml`

- **Use case**: Read-heavy workloads, horizontal scaling
- **Topology**: 5 servers (can host databases with read replicas)
- **Mode**: Server-based clustering for flexible database topologies
- **TLS**: Disabled for simplicity
- **Resources**: 3Gi RAM, 750m CPU
- **Features**: Optimized for read performance

### `clusters/multi-server-cluster.yaml`

- **Use case**: Production workload with advanced features
- **Topology**: 5 servers (automatic role organization)
- **Mode**: Server-based clustering with automatic discovery
- **TLS**: cert-manager enabled
- **Resources**: 4Gi RAM, 2 CPU
- **Features**: LoadBalancer service, automatic RBAC, production config

### `clusters/topology-placement-cluster.yaml`

- **Use case**: Multi-zone production deployment with placement constraints
- **Topology**: 3 servers with topology spread constraints
- **Mode**: Server-based clustering with anti-affinity rules
- **TLS**: cert-manager enabled
- **Resources**: 4Gi RAM, 2 CPU
- **Features**: Zone distribution, topology constraints, fault tolerance

## Fault Tolerance Considerations ⚠️

The operator now allows even numbers of primary nodes but issues warnings about reduced fault tolerance. Understanding these implications is crucial for production deployments.

### Server Configuration Recommendations

| Configuration | Fault Tolerance | Use Case | Recommendation |
|---------------|----------------|----------|----------------|
| 2 Servers | None | Development/Testing | ✅ Minimum for clustering |
| 3 Servers | ✅ 1 node failure | Production | ✅ **Recommended minimum** |
| 4 Servers | ⚠️ 1 node failure (same as 3) | - | ⚠️ Consider 3 or 5 instead |
| 5 Servers | ✅ 2 node failures | High availability | ✅ Mission-critical |
| 6 Servers | ⚠️ 2 node failures (same as 5) | - | ⚠️ Consider 5 or 7 instead |
| 7+ Servers | ✅ 3+ node failures | Maximum availability | ✅ Extreme requirements |

### Operator Warnings

When deploying with even numbers of servers, the operator will emit warnings:

```
Warning: Even number of servers (4) may reduce fault tolerance.
For optimal cluster quorum, consider using an odd number (3, 5, or 7) of servers.
```

### Best Practices

1. **Use odd numbers** of servers for production
2. **5 servers minimum** for property sharding deployments (3+ for standard clusters)
3. **Scale with databases**, not excessive servers
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

| Use Case | Servers | Database Topologies | Notes |
|----------|---------|-------------------|-------|
| Development | 2 | Simple databases | Minimum for clustering |
| Testing | 2-3 | Various topologies | Test different configurations |
| Small Production | 3 | 1-2 primaries, 0-1 secondaries | Minimal HA cluster |
| Large Production | 5-7 | Multiple databases with different topologies | Flexible infrastructure |
| Read-Heavy | 5+ | Databases with read replicas | Horizontal read scaling |

## Deployment Behavior

### Cluster Formation Process

Neo4j clusters use parallel pod startup with coordinated formation:

1. **Parallel Startup**: All server pods start simultaneously for faster deployment
2. **Discovery Phase**: Servers discover each other via Kubernetes service discovery
3. **Self-Organization**: Servers automatically form cluster and assign roles as needed
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
5. **Cluster formation slow**: All server pods start in parallel - expect 2-3 minutes total formation time
6. **Server pods not ready**: Check resource availability and network connectivity between pods

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
