# Neo4j Enterprise Clustering

This document describes how to configure and manage Neo4j Enterprise clusters using the Neo4j Kubernetes Operator.

## Overview

The Neo4j Kubernetes Operator supports Neo4j 5.26+ Enterprise clustering with multiple discovery mechanisms and advanced features like auto-scaling, read replicas, and multi-zone deployments.

## Cluster Architecture

A Neo4j Enterprise cluster consists of:

- **Primary nodes**: Core cluster members that participate in consensus and handle write operations
- **Secondary nodes**: Read replicas that handle read operations and provide high availability
- **Discovery service**: Enables cluster members to find each other
- **Routing service**: Routes client connections to appropriate cluster members

## Discovery Methods

The operator automatically uses **Kubernetes Discovery** (recommended) for all clusters as described in the [Neo4j Operations Manual](https://neo4j.com/docs/operations-manual/current/clustering/setup/discovery/).

### How Neo4j Kubernetes Discovery Works

The operator implements Neo4j's Kubernetes discovery mechanism with the following architecture:

1. **Discovery Service**: A dedicated ClusterIP service labeled with `neo4j.com/clustering=true`
2. **Service Discovery**: Neo4j queries the Kubernetes API to find services matching the label selector
3. **Endpoint Resolution**: Neo4j uses the discovered service to query endpoints and find individual pod IPs
4. **Cluster Formation**: Pods connect directly to each other using their resolved IPs

**Important**: When checking logs, you'll see Neo4j report discovering a service hostname like:
```
Resolved endpoints... to '[my-cluster-discovery.default.svc.cluster.local:5000]'
```
This is **expected behavior**. Neo4j discovers the service first, then internally queries its endpoints to get pod IPs.

### RBAC Requirements

The discovery mechanism requires specific permissions:
- **Services**: To discover the labeled service
- **Endpoints**: To resolve individual pod IPs from the service

The operator automatically creates these permissions in the discovery role.

### Kubernetes Discovery (Automatic)

**The operator automatically configures Kubernetes API-based discovery for all clusters.** This provides:

- **Dynamic service discovery** via Kubernetes API
- **Automatic adaptation** to scaling operations
- **Native cloud-native integration**
- **Reduced operator complexity**

**No manual configuration required** - the operator automatically:

1. **Creates RBAC resources**:
   - ServiceAccount: `{cluster-name}-discovery`
   - Role: `{cluster-name}-discovery` (with permissions to list services and endpoints)
   - RoleBinding: `{cluster-name}-discovery`

2. **Creates role-specific services**:
   - Primary headless service: `{cluster-name}-primary-headless`
   - Secondary headless service: `{cluster-name}-secondary-headless` (when secondaries > 0)

3. **Configures Neo4j automatically** with version-specific settings:

   **For Neo4j 5.26+ (SemVer) - CRITICAL V2_ONLY Configuration**:
   ```properties
   dbms.cluster.discovery.resolver_type=K8S
   dbms.kubernetes.label_selector=neo4j.com/cluster={cluster-name},neo4j.com/clustering=true
   dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery
   dbms.cluster.discovery.version=V2_ONLY
   ```

   **For Neo4j 2025.x+ (CalVer)**:
   ```properties
   dbms.cluster.discovery.resolver_type=K8S
   dbms.kubernetes.label_selector=neo4j.com/cluster={cluster-name},neo4j.com/clustering=true
   dbms.kubernetes.discovery.service_port_name=tcp-discovery
   # V2_ONLY is default in 2025.x, no explicit setting needed
   ```

   > **⚠️ CRITICAL**: The operator uses `tcp-discovery` port (5000) for V2_ONLY discovery mode, not `tcp-tx` port (6000).
   > V2_ONLY mode disables the discovery port (6000) and only uses the cluster communication port (5000).
   > This is essential for proper cluster formation in Neo4j 5.26+ and 2025.x.

### Discovery Method Enforcement

**The operator enforces Kubernetes discovery for all clusters.** Any manual discovery configuration in `spec.config` is automatically overridden during cluster startup to ensure:

- ✅ **Consistent behavior** across all deployments
- ✅ **Cloud-native integration** with Kubernetes API
- ✅ **Automatic scaling support** without manual endpoint management
- ✅ **Simplified operations** with zero discovery configuration

**Note**: While you can specify discovery settings in `spec.config`, the operator will always override them with Kubernetes discovery during pod startup to maintain operational consistency.

## Cluster Formation

The operator uses a **unified cluster formation approach** that ensures all nodes coordinate properly during startup, eliminating the timing issues that can cause separate cluster formation.

### Cluster Formation Strategy

The operator uses a unified clustering approach for all deployments:

#### Unified Cluster Formation
- All deployments use Neo4j's clustering infrastructure (even single-node)
- Automatic handling of discovery configuration based on Neo4j version
- Coordinated startup ensures data consistency

#### Parallel Pod Management
- Uses **parallel pod startup** for faster cluster formation
- All pods start simultaneously and coordinate during bootstrap
- Prevents split-brain scenarios through proper coordination

### Cluster Formation Strategy

The operator uses a unified approach where all primaries must be present for initial cluster formation:

| Cluster Size | Formation Requirement | Rationale |
|--------------|----------------------|-----------|
| 1 primary | 1 node required | Single-node cluster |
| 2 primaries | 2 nodes required | Prevents split-brain |
| 3+ primaries | All nodes required | Ensures consistent initial state |

This approach ensures that clusters form with a complete and consistent initial membership.

### Cluster Formation Process

1. **Resource Creation**: The operator creates all Kubernetes resources (StatefulSets, Services, RBAC)
2. **Parallel Pod Startup**: All pods start simultaneously (not sequentially)
3. **Discovery Phase**: Pods discover each other via Kubernetes service discovery
4. **Coordination Phase**: All pods wait for complete membership before forming cluster
5. **Service Ready**: Cluster accepts connections after successful formation

### Important Considerations

- **Complete Membership**: All configured primary nodes must be available for initial cluster formation
- **Startup Time**: Cluster formation typically completes within 2-3 minutes
- **Pod Readiness**: Pods are marked ready only after successful cluster formation
- **Scaling**: After initial formation, clusters can be scaled following Neo4j's online scaling procedures

## Basic Cluster Configuration

### Simple 3-Node Cluster

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: simple-cluster
spec:
  edition: enterprise
  image:
    repo: neo4j
    tag: 5.26-enterprise
  topology:
    primaries: 3
    secondaries: 0
  storage:
    className: standard
    size: 10Gi
  # Kubernetes discovery is automatically configured by the operator
  # No manual discovery configuration needed!
```

**Note**: The operator automatically handles all clustering configuration including:
- Kubernetes discovery setup
- RBAC resource creation
- Service creation for cluster communication
- Neo4j configuration for optimal clustering

### Using kubectl Commands

```bash
# Check cluster status
kubectl get neo4jenterprisecluster my-cluster -o yaml

# View cluster details
kubectl describe neo4jenterprisecluster my-cluster

# Check pod status
kubectl get pods -l app.kubernetes.io/instance=my-cluster
```

## Advanced Configuration

### Auto-Scaling

Enable auto-scaling for both primary and secondary nodes:

```yaml
spec:
  autoScaling:
    enabled: true
    primaries:
      enabled: true
      minReplicas: 3
      maxReplicas: 7
      allowQuorumBreak: false
      quorumProtection:
        enabled: true
        minHealthyPrimaries: 2
      metrics:
        - type: cpu
          target: "70%"
          weight: "1.0"
        - type: memory
          target: "80%"
          weight: "1.0"
    secondaries:
      enabled: true
      minReplicas: 2
      maxReplicas: 10
      zoneAware:
        enabled: true
        minReplicasPerZone: 1
        maxZoneSkew: 2
```

### Multi-Zone Deployment

Configure topology spread and anti-affinity for high availability:

```yaml
spec:
  topology:
    primaries: 3
    secondaries: 2
    placement:
      antiAffinity:
        enabled: true
        topologyKey: topology.kubernetes.io/zone
        type: preferredDuringSchedulingIgnoredDuringExecution
      topologySpread:
        enabled: true
        topologyKey: topology.kubernetes.io/zone
        maxSkew: 1
        whenUnsatisfiable: DoNotSchedule
```

### TLS Configuration

Enable TLS encryption for cluster communication:

```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: neo4j-cluster-issuer
      kind: ClusterIssuer
    duration: 8760h  # 1 year
    renewBefore: 720h  # 30 days
```

## Port Configuration

The operator uses the following default ports:

- **Bolt**: 7687 (client connections)
- **HTTP**: 7474 (HTTP API)
- **HTTPS**: 7473 (HTTPS API)
- **Cluster**: 5000 (cluster communication)
- **Discovery**: 6000 (discovery service)
- **Routing**: 7688 (routing service)
- **Raft**: 7000 (consensus protocol)

You can customize these ports in the cluster configuration:

```yaml
spec:
  config:
    server.cluster.listen_address: 0.0.0.0:5000
    server.discovery.listen_address: 0.0.0.0:6000
    server.routing.listen_address: 0.0.0.0:7688
```

## Health Monitoring

The operator provides comprehensive health monitoring:

```bash
# Check cluster health
kubectl get neo4jenterprisecluster my-cluster -o jsonpath='{.status.phase}'

# Get cluster status
kubectl describe neo4jenterprisecluster my-cluster

# View cluster logs
kubectl logs -l app.kubernetes.io/instance=my-cluster
```

## Scaling Operations

### Scale Up/Down

```bash
# Scale primaries by editing the resource
kubectl patch neo4jenterprisecluster my-cluster --type='merge' -p='{"spec":{"topology":{"primaries":5}}}'

# Scale secondaries
kubectl patch neo4jenterprisecluster my-cluster --type='merge' -p='{"spec":{"topology":{"secondaries":4}}}'

# Or edit the resource directly
kubectl edit neo4jenterprisecluster my-cluster
```

### Rolling Upgrades

```yaml
spec:
  upgradeStrategy:
    strategy: RollingUpgrade
    preUpgradeHealthCheck: true
    postUpgradeHealthCheck: true
    maxUnavailableDuringUpgrade: 1
    upgradeTimeout: 30m
    healthCheckTimeout: 5m
    stabilizationTimeout: 3m
    autoPauseOnFailure: true
```

## Troubleshooting

### Common Issues

1. **Cluster Formation Fails**
   - Check that all required ports are open
   - Verify DNS resolution for headless service
   - Check RBAC resources were created: `kubectl get serviceaccount,role,rolebinding -l neo4j.com/cluster={cluster-name}`

2. **Discovery Issues**
   - Verify discovery service exists: `kubectl get service {cluster-name}-discovery`
   - Check discovery service has clustering label: `kubectl get service {cluster-name}-discovery -o jsonpath='{.metadata.labels.neo4j\.com/clustering}'`
   - Verify discovery role has endpoints permission: `kubectl get role {cluster-name}-discovery -o yaml | grep endpoints`
   - Check discovery logs show service hostname (this is EXPECTED): `kubectl logs {cluster-name}-primary-0 | grep "Resolved endpoints"`
   - Verify pod has correct ServiceAccount: `kubectl get pod {cluster-name}-primary-0 -o jsonpath='{.spec.serviceAccountName}'`

   **Note**: Neo4j's K8s discovery returns service hostnames (e.g., `{cluster-name}-discovery.default.svc.cluster.local:5000`) in logs. This is expected behavior - Neo4j internally queries the service endpoints to discover individual pods.

3. **Quorum Loss**
   - Check primary node health
   - Verify minimum cluster size configuration
   - Review auto-scaling settings

### Debug Commands

```bash
# Check cluster member status
kubectl exec -it my-cluster-primary-0 -- cypher-shell -u neo4j -p password "CALL dbms.cluster.overview()"

# Check discovery service
kubectl exec -it my-cluster-primary-0 -- cypher-shell -u neo4j -p password "CALL dbms.cluster.discovery()"

# View cluster logs
kubectl logs my-cluster-primary-0 -c neo4j
```

## Best Practices

1. **Use odd numbers of primaries (3, 5, 7)** for optimal fault tolerance. Even numbers are allowed but generate warnings about potential split-brain scenarios.
2. **Enable auto-scaling** for production workloads
3. **Use multi-zone deployment** for high availability
4. **Configure proper resource limits** based on workload
5. **Enable TLS** for secure cluster communication
6. **Monitor cluster health** regularly
7. **Use rolling upgrades** for zero-downtime updates
8. **Trust automatic discovery** - the operator handles all Kubernetes discovery configuration

## Migration from Neo4j 4.x

When migrating from Neo4j 4.x clustering to 5.x:

1. **Remove all manual discovery configuration** - the operator handles discovery automatically
2. **Update from `causal_clustering.*` to `dbms.cluster.*`** - but discovery settings are managed by operator
3. **Update port configurations** for new routing service if customized
4. **Remove static endpoint lists** - no longer needed with Kubernetes discovery
5. **Test cluster formation** in staging environment first

**Important**: The operator automatically handles all cluster discovery. Remove any manual discovery configuration from your cluster specifications.

## References

- [Neo4j Operations Manual - Clustering](https://neo4j.com/docs/operations-manual/current/clustering/)
- [Neo4j Operations Manual - Discovery](https://neo4j.com/docs/operations-manual/current/clustering/setup/discovery/)
- [Kubernetes StatefulSets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
