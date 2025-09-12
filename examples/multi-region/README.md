# Multi-Region Deployment Examples

This directory contains examples demonstrating how to deploy Neo4j clusters across multiple regions, zones, and data centers using the Neo4j Kubernetes Operator's multi-region features.

## Features Demonstrated

### Phase 1 Features (Implemented)

1. **Server Tagging**: Label servers with tags for routing and load balancing
2. **Zone/Region Awareness**: Deploy servers in specific zones/regions
3. **Load Balancing Policies**: Configure routing policies using Neo4j DSL
4. **Anti-Affinity Rules**: Ensure servers are distributed across failure domains
5. **Disaster Recovery Monitoring**: Track server distribution and cluster health

## Example Files

### 1. Basic Multi-Zone Cluster (`basic-multi-zone-cluster.yaml`)

A simple example showing a 6-server cluster distributed across availability zones with soft anti-affinity.

**Key Features:**
- Zone-based anti-affinity
- Basic resource allocation
- Load balancer service configuration

**Use Case:** Development or testing environments requiring basic high availability.

### 2. Advanced Server Groups (`advanced-server-groups.yaml`)

Demonstrates enterprise-grade multi-region deployment with server groups.

**Key Features:**
- Three regions (US East, US West, EU West)
- Different resources per region
- Server role hints (PRIMARY_PREFERRED, SECONDARY_PREFERRED)
- Named routing policies
- TLS configuration

**Use Case:** Production deployments requiring geographic distribution and disaster recovery.

### 3. Disaster Recovery Setup (`disaster-recovery-setup.yaml`)

Optimized configuration for disaster recovery scenarios.

**Key Features:**
- Primary production region with backup regions
- DR-specific routing policies
- Automated backup configuration
- Catchup strategy optimization

**Use Case:** Mission-critical deployments requiring robust disaster recovery capabilities.

### 4. Standalone with Tags (`standalone-with-tags.yaml`)

Shows how to deploy standalone instances with multi-region features.

**Key Features:**
- Server tags for standalone instances
- Placement configuration
- Zone/region specification
- Anti-affinity for standalone deployments

**Use Case:** Edge deployments or single-node instances that need routing integration.

### 5. Routing Policies (`routing-policies.yaml`)

Comprehensive demonstration of routing policy configurations.

**Key Features:**
- 8 different routing policy examples
- Database-specific routing
- Tiered routing strategies
- User-defined catchup strategies

**Use Case:** Reference implementation for complex routing requirements.

## Deployment Guide

### Prerequisites

1. **Kubernetes Cluster**: Multi-zone or multi-region Kubernetes cluster
2. **Node Labels**: Nodes must be labeled with zones/regions:
   ```bash
   kubectl label nodes <node-name> topology.kubernetes.io/zone=us-east-1a
   kubectl label nodes <node-name> topology.kubernetes.io/region=us-east-1
   ```
3. **Storage Classes**: Regional storage classes must be available
4. **Network Connectivity**: Low-latency network between regions (< 100ms RTT)

### Basic Deployment

1. Create namespace:
   ```bash
   kubectl create namespace neo4j
   ```

2. Deploy a basic multi-zone cluster:
   ```bash
   kubectl apply -f basic-multi-zone-cluster.yaml
   ```

3. Verify server distribution:
   ```bash
   kubectl get pods -n neo4j -o wide
   ```

### Advanced Deployment

1. Deploy with server groups:
   ```bash
   kubectl apply -f advanced-server-groups.yaml
   ```

2. Check server tags:
   ```bash
   kubectl exec -n neo4j global-cluster-server-0 -- \
     cypher-shell -u neo4j -p <password> "CALL dbms.cluster.overview()"
   ```

3. Test routing policies:
   ```bash
   # Connect using specific routing policy
   neo4j+s://global-cluster-client.neo4j:7687?policy=read-only
   ```

## Configuration Reference

### Server Groups

```yaml
serverGroups:
  - name: <group-name>
    count: <number-of-servers>
    serverTags: [<tag1>, <tag2>]
    nodeSelector:
      <label-key>: <label-value>
    roleHint: <PRIMARY_PREFERRED|SECONDARY_PREFERRED|NONE>
    resources:
      requests:
        cpu: <cpu-request>
        memory: <memory-request>
```

### Anti-Affinity

```yaml
antiAffinity:
  type: <zone|region|node>
  required: <true|false>  # Hard vs soft anti-affinity
  weight: <1-100>         # Weight for soft anti-affinity
```

### Routing Policies

```yaml
routing:
  loadBalancingPolicy: server_policies
  policies:
    <policy-name>: |
      tags(<tag-list>)->min(<count>);
      tags(<tag-list>);
      all();
  defaultPolicy: <policy-name>
  databasePolicies:
    - database: <db-name>
      policy: <policy-name>
```

## Monitoring and Validation

### Check Server Distribution

```bash
kubectl get pods -n neo4j -o custom-columns=\
NAME:.metadata.name,NODE:.spec.nodeName,ZONE:.spec.nodeSelector.'topology\.kubernetes\.io/zone'
```

### Verify Cluster Formation

```bash
kubectl exec -n neo4j <cluster>-server-0 -- \
  cypher-shell -u neo4j -p <password> "SHOW SERVERS"
```

### Monitor Routing

```bash
kubectl exec -n neo4j <cluster>-server-0 -- \
  cypher-shell -u neo4j -p <password> "SHOW ROUTING"
```

## Troubleshooting

### Common Issues

1. **Pods not scheduling**: Check node labels and anti-affinity rules
2. **Cluster not forming**: Verify network connectivity between zones/regions
3. **Routing policies not working**: Check server tags are correctly applied
4. **High latency**: Review catchup strategy and cross-region network

### Debug Commands

```bash
# Check pod events
kubectl describe pod -n neo4j <pod-name>

# View operator logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager

# Check server tags in Neo4j config
kubectl exec -n neo4j <pod> -- cat /tmp/neo4j-config/neo4j.conf | grep server.tags

# Test network connectivity
kubectl exec -n neo4j <pod> -- ping <other-pod>.neo4j.svc.cluster.local
```

## Best Practices

1. **Use Odd Number of Regions**: Deploy in 3, 5, or 7 regions for proper quorum
2. **Balance Resources**: Allocate more resources to primary regions
3. **Test Failover**: Regularly test disaster recovery procedures
4. **Monitor Latency**: Track cross-region replication lag
5. **Backup Strategy**: Implement region-specific backup policies

## Additional Resources

- [Neo4j Multi-Region Documentation](https://neo4j.com/docs/operations-manual/current/clustering/multi-data-center/)
- [Kubernetes Topology Spread Constraints](https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/)
- [Neo4j Routing Policies](https://neo4j.com/docs/operations-manual/current/clustering/routing/)
