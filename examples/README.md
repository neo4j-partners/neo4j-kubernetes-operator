# Neo4j Kubernetes Operator Examples

This directory contains example configurations for deploying Neo4j Enterprise clusters using the Neo4j Kubernetes Operator.

## Prerequisites

Before deploying any examples, ensure you have:

1. **Neo4j Kubernetes Operator installed** in your cluster
2. **cert-manager installed** (if using TLS examples)
3. **Appropriate storage classes** available in your cluster
4. **Neo4j Enterprise license** (required for all examples)

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
```

### 3. Access Neo4j

Once deployed, access Neo4j through port forwarding:

```bash
# Port forward to the cluster
kubectl port-forward svc/your-cluster-name-client 7474:7474 7687:7687

# Open Neo4j Browser
open http://localhost:7474
```

## Example Configurations

### `clusters/single-node.yaml`

- **Use case**: Development, testing, small workloads
- **Topology**: 1 primary, 0 secondaries
- **Mode**: Single-node (`dbms.mode=SINGLE`)
- **TLS**: Disabled for simplicity
- **Resources**: 2Gi RAM, 500m CPU

### `clusters/three-node-cluster.yaml`

- **Use case**: Production, high availability
- **Topology**: 3 primaries, 0 secondaries
- **Mode**: Clustered with quorum
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

For production, enable TLS with cert-manager:

```yaml
tls:
  mode: cert-manager
  issuerRef:
    name: your-cluster-issuer
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
| Development | 1 | 0 | Single-node mode |
| Testing | 1 | 0 | Single-node mode |
| Small Production | 3 | 0 | Minimal HA cluster |
| Large Production | 3 | 0-5 | Add replicas for read scaling |
| Read-Heavy | 3 | 2+ | Horizontal read scaling |

## Deployment Behavior

### Cluster Formation Process

Multi-node Neo4j clusters follow a specific startup sequence:

1. **Bootstrap Pod (ordinal 0)**: Starts first and forms the initial cluster
2. **Joining Pods (ordinal 1+)**: Start sequentially after previous pod is ready
3. **Readiness Timing**: Each pod takes 1-2 minutes to become ready
4. **Total Time**: 3-node cluster typically takes 3-5 minutes to fully deploy

### Expected Timeline

| Pod | Status | Timing |
|-----|--------|--------|
| pod-0 | Bootstrap, forms cluster | 0-2 minutes |
| pod-1 | Joins cluster | 2-4 minutes |
| pod-2 | Joins cluster | 4-6 minutes |

**Note**: This is normal StatefulSet behavior - pods start sequentially for data consistency.

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

## Support

For more information, see:
- [User Guide](../docs/user_guide/getting_started.md)
- [Configuration Reference](../docs/user_guide/configuration.md)
- [Troubleshooting Guide](../docs/user_guide/guides/troubleshooting.md)
