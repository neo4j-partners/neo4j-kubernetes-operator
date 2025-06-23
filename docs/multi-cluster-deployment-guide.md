# Multi-Cluster Deployment Guide for Neo4j Enterprise

## Overview

The Neo4j Enterprise Operator supports **multi-cluster deployments**, enabling you to deploy Neo4j clusters across multiple Kubernetes clusters for enhanced availability, disaster recovery, and geographic distribution. This guide covers everything from basic setup to advanced cross-region networking configurations.

## 🎯 Use Cases

### 1. **Geographic Distribution**

Deploy Neo4j closer to your users across different regions:

```
US-East Region: [Primary-1, Primary-2, Secondary-1, Secondary-2]
EU-West Region: [Primary-3, Secondary-3, Secondary-4]
APAC Region:    [Secondary-5, Secondary-6]
```

### 2. **Disaster Recovery**

Maintain active clusters in multiple regions for automatic failover:

```
Primary Region:  [3 Primaries, 2 Secondaries] ← Active workload
DR Region:       [1 Primary, 1 Secondary]     ← Standby for failover
```

### 3. **Compliance & Data Sovereignty**

Keep data within specific geographic boundaries while maintaining global access:

```
EU Cluster:  [Customer data for EU users]
US Cluster:  [Customer data for US users]
Global:      [Shared reference data]
```

## 🏗️ Architecture Patterns

### Pattern 1: Active-Active Multi-Region

```yaml
┌─────────────────────────────────────────────────────────────────┐
│                    Multi-Cluster Architecture                   │
├─────────────────────────────────────────────────────────────────┤
│  US-East Cluster          │  EU-West Cluster    │ APAC Cluster  │
│  ┌─────────────────────┐  │  ┌─────────────────┐│┌─────────────┐ │
│  │ Primary-1 (Leader)  │  │  │ Primary-2       │││ Primary-3   │ │
│  │ Primary-2           │  │  │ Secondary-1     │││ Secondary-1 │ │
│  │ Secondary-1         │  │  │ Secondary-2     │││ Secondary-2 │ │
│  └─────────────────────┘  │  └─────────────────┘│└─────────────┘ │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Cross-Cluster Networking                     │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐         │
│  │ Submariner  │    │ Istio Mesh  │    │ Cilium      │         │
│  │ VPN Mesh    │    │ Service     │    │ Cluster     │         │
│  │             │    │ Discovery   │    │ Mesh        │         │
│  └─────────────┘    └─────────────┘    └─────────────┘         │
└─────────────────────────────────────────────────────────────────┘
```

### Pattern 2: Primary-DR (Active-Standby)

```yaml
┌─────────────────────────────────────────────────────────────────┐
│                    Primary-DR Architecture                      │
├─────────────────────────────────────────────────────────────────┤
│  Primary Region (US-East)     │  DR Region (US-West)            │
│  ┌─────────────────────────┐  │  ┌─────────────────────────┐    │
│  │ Primary-1 (Leader)      │  │  │ Primary-3 (Standby)     │    │
│  │ Primary-2               │  │  │ Secondary-1 (Standby)   │    │
│  │ Secondary-1             │  │  │                         │    │
│  │ Secondary-2             │  │  │ ← Automated Failover    │    │
│  └─────────────────────────┘  │  └─────────────────────────┘    │
│  ↓ Continuous Replication     │  ↑ Backup Restore               │
└─────────────────────────────────────────────────────────────────┘
```

## 📋 Prerequisites

### 1. **Multiple Kubernetes Clusters**

- At least 2 Kubernetes clusters (1.20+)
- Neo4j Operator installed on each cluster
- Sufficient resources for Neo4j workloads

### 2. **Network Connectivity**

Choose one of the following networking solutions:

#### Option A: Submariner (Recommended)

```bash
# Install Submariner on both clusters
subctl deploy-broker --kubeconfig cluster1-config.yaml
subctl join --kubeconfig cluster1-config.yaml broker-info.subm
subctl join --kubeconfig cluster2-config.yaml broker-info.subm
```

#### Option B: Istio Multi-Cluster

```bash
# Install Istio with multi-cluster support
istioctl install --set values.pilot.env.EXTERNAL_ISTIOD=true
istioctl create-remote-secret --name=cluster1 | kubectl apply -f -
```

#### Option C: Manual VPC Peering

```bash
# AWS VPC Peering example
aws ec2 create-vpc-peering-connection \
  --vpc-id vpc-12345678 \
  --peer-vpc-id vpc-87654321 \
  --peer-region us-west-2
```

### 3. **Storage Configuration**

- Shared storage for backups (S3, GCS, Azure Blob)
- Cross-region replication enabled

## 🚀 Quick Start

### Step 1: Configure the Primary Cluster with Topology Awareness

```yaml
# primary-cluster.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-primary
  namespace: neo4j
spec:
  # Multi-cluster configuration
  multiCluster:
    enabled: true

    # Cross-cluster networking
    networking:
      type: "cilium"
      cilium:
        clusterMesh:
          enabled: true
          clusterId: 1
        encryption:
          enabled: true
          type: "wireguard"

      dns:
        enabled: true
        zone: "neo4j.local"

    # Multi-cluster topology
    topology:
      clusters:
      - name: "us-east-primary"
        region: "us-east-1"
        endpoint: "neo4j-primary.neo4j.svc.cluster.local:7687"
        nodeAllocation:
          primaries: 3  # Odd number for quorum
          secondaries: 2
      - name: "us-west-dr"
        region: "us-west-2"
        endpoint: "neo4j-dr.neo4j.svc.cluster.local:7687"
        nodeAllocation:
          primaries: 1
          secondaries: 1

      primaryCluster: "us-east-primary"
      strategy: "active-passive"

  # ⚡ TOPOLOGY-AWARE CONFIGURATION FOR MAXIMUM RESILIENCE
  topology:
    primaries: 3  # Always use odd numbers for proper quorum
    secondaries: 2
    enforceDistribution: true  # Critical for disaster resilience

    # Specify availability zones for strict distribution
    availabilityZones:
      - "us-east-1a"
      - "us-east-1b"
      - "us-east-1c"

    # Advanced placement configuration
    placement:
      # Topology spread ensures even distribution across zones
      topologySpread:
        enabled: true
        topologyKey: "topology.kubernetes.io/zone"
        maxSkew: 1  # No more than 1 pod difference between zones
        whenUnsatisfiable: "DoNotSchedule"  # Hard requirement
        minDomains: 3  # Require at least 3 zones

      # Anti-affinity prevents co-location
      antiAffinity:
        enabled: true
        type: "required"  # Hard anti-affinity
        topologyKey: "topology.kubernetes.io/zone"

      # Additional node selection for resilience
      nodeSelector:
        node-type: "database"
        kubernetes.io/arch: "amd64"

      requiredDuringScheduling: true  # Hard constraints

  # Shared storage for cross-cluster backups
  storage:
    className: "gp3"
    size: "100Gi"

  # Cross-cluster backup configuration
  backups:
    defaultStorage:
      type: "s3"
      bucket: "neo4j-multicluster-backups"
```

### Step 2: Configure the DR Cluster with Topology Awareness

```yaml
# dr-cluster.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-dr
  namespace: neo4j
spec:
  # Multi-cluster configuration
  multiCluster:
    enabled: true

    # Multi-cluster topology
    topology:
      clusters:
      - name: "us-west-dr"
        region: "us-west-2"
        endpoint: "neo4j-dr.neo4j.svc.cluster.local:7687"
        nodeAllocation:
          primaries: 3
          secondaries: 2

      primaryCluster: "us-east-primary"
      strategy: "active-passive"

    # Cross-cluster networking
    networking:
      type: "cilium"
      cilium:
        clusterMesh:
          enabled: true
          clusterId: 1
        encryption:
          enabled: true
          type: "wireguard"

      dns:
        enabled: true
        zone: "neo4j.local"

    # Cross-cluster coordination
    coordination:
      leaderElection:
        enabled: true
        leaseDuration: "15s"
      failoverCoordination:
        enabled: true
        timeout: "5m"
        healthCheck:
          interval: "30s"
          failureThreshold: 3

  # ⚡ TOPOLOGY-AWARE DR CLUSTER CONFIGURATION
  topology:
    primaries: 3  # Odd number for proper quorum even in DR
    secondaries: 2
    enforceDistribution: true  # Critical for zone-level resilience

    # DR region availability zones
    availabilityZones:
      - "us-west-2a"
      - "us-west-2b"
      - "us-west-2c"

    # Same placement strategy as primary for consistency
    placement:
      topologySpread:
        enabled: true
        topologyKey: "topology.kubernetes.io/zone"
        maxSkew: 1
        whenUnsatisfiable: "DoNotSchedule"
        minDomains: 3

      antiAffinity:
        enabled: true
        type: "required"
        topologyKey: "topology.kubernetes.io/zone"

      nodeSelector:
        node-type: "database"
        kubernetes.io/arch: "amd64"

      requiredDuringScheduling: true

  # Storage configuration
  storage:
    className: "gp3"
    size: "100Gi"

  # Backup configuration (same bucket, different region)
  backups:
    defaultStorage:
      type: "s3"
      bucket: "neo4j-multicluster-backups"
```

### Step 3: Deploy the Clusters

```bash
# Deploy primary cluster
kubectl apply -f primary-cluster.yaml --kubeconfig primary-cluster-config.yaml

# Deploy DR cluster
kubectl apply -f dr-cluster.yaml --kubeconfig dr-cluster-config.yaml

# Verify multi-cluster connectivity
kubectl get neo4jenterprisecluster -o wide --all-namespaces
```

## 🎯 Multi-Cluster + Topology Awareness: Maximum Resilience

### Understanding the Combined Approach

When you combine **multi-cluster deployments** with **topology-aware placement**, you create a defense-in-depth strategy that protects against multiple failure scenarios simultaneously:

```yaml
┌─────────────────────────────────────────────────────────────────┐
│                    LAYERED RESILIENCE STRATEGY                  │
├─────────────────────────────────────────────────────────────────┤
│  Layer 1: Zone-Level Protection (Topology Awareness)           │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │ Primary Region (us-east-1)                                  ││
│  │ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐            ││
│  │ │ Zone A      │ │ Zone B      │ │ Zone C      │            ││
│  │ │ Primary-1   │ │ Primary-2   │ │ Primary-3   │            ││
│  │ │ Secondary-1 │ │ Secondary-2 │ │             │            ││
│  │ └─────────────┘ └─────────────┘ └─────────────┘            ││
│  └─────────────────────────────────────────────────────────────┘│
│                              │                                  │
│  Layer 2: Region-Level Protection (Multi-Cluster)              │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │ DR Region (us-west-2)                                       ││
│  │ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐            ││
│  │ │ Zone A      │ │ Zone B      │ │ Zone C      │            ││
│  │ │ Primary-4   │ │ Primary-5   │ │ Primary-6   │            ││
│  │ │ Secondary-3 │ │ Secondary-4 │ │             │            ││
│  │ └─────────────┘ └─────────────┘ └─────────────┘            ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

### Failure Scenarios and Protection

| Failure Type | Without Topology | With Topology Only | With Multi-Cluster + Topology |
|--------------|------------------|-------------------|-------------------------------|
| **Single Pod** | ⚠️ Possible quorum loss | ✅ Automatic failover | ✅ Automatic failover |
| **Single Zone** | ❌ Likely downtime | ✅ Continues running | ✅ Continues running |
| **Multiple Zones** | ❌ Definite downtime | ❌ Possible downtime | ✅ Fails over to DR region |
| **Entire Region** | ❌ Complete outage | ❌ Complete outage | ✅ Automatic DR activation |
| **Cloud Provider** | ❌ Complete outage | ❌ Complete outage | ⚠️ Manual intervention needed |

### Example: Production-Grade Configuration

Here's a complete example showing how to configure maximum resilience:

```yaml
# production-resilient-cluster.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-production
  namespace: neo4j
  labels:
    environment: production
    resilience-tier: maximum
spec:
  # 🌐 MULTI-CLUSTER CONFIGURATION
  multiCluster:
    enabled: true

    # Define the complete multi-cluster topology
    topology:
      clusters:
      - name: "production-primary"
        region: "us-east-1"
        endpoint: "neo4j-production.neo4j.svc.cluster.local:7687"
        nodeAllocation:
          primaries: 3
          secondaries: 3
      - name: "production-dr"
        region: "us-west-2"
        endpoint: "neo4j-dr.neo4j.svc.cluster.local:7687"
        nodeAllocation:
          primaries: 3
          secondaries: 2
      - name: "production-backup"
        region: "eu-west-1"
        endpoint: "neo4j-backup.neo4j.svc.cluster.local:7687"
        nodeAllocation:
          primaries: 1
          secondaries: 1

      primaryCluster: "production-primary"
      strategy: "active-passive"

    # Cross-cluster networking
    networking:
      type: "cilium"
      cilium:
        clusterMesh:
          enabled: true
          clusterId: 1
        encryption:
          enabled: true
          type: "wireguard"

      dns:
        enabled: true
        zone: "neo4j.local"

    # Cross-cluster coordination
    coordination:
      leaderElection:
        enabled: true
        leaseDuration: "15s"
        renewDeadline: "10s"
        retryPeriod: "2s"

      stateSynchronization:
        enabled: true
        interval: "30s"
        conflictResolution: "last_writer_wins"

      failoverCoordination:
        enabled: true
        timeout: "5m"
        healthCheck:
          interval: "15s"
          failureThreshold: 3
          successThreshold: 1

  # ⚡ TOPOLOGY-AWARE CONFIGURATION
  topology:
    primaries: 3
    secondaries: 3
    enforceDistribution: true

    # Strict zone requirements
    availabilityZones:
      - "us-east-1a"
      - "us-east-1b"
      - "us-east-1c"

    placement:
      # Zone-level distribution
      topologySpread:
        enabled: true
        topologyKey: "topology.kubernetes.io/zone"
        maxSkew: 1
        whenUnsatisfiable: "DoNotSchedule"
        minDomains: 3

      # Anti-affinity
      antiAffinity:
        enabled: true
        type: "required"
        topologyKey: "topology.kubernetes.io/zone"

      # Node selection
      nodeSelector:
        node-type: "database"
        kubernetes.io/arch: "amd64"

      requiredDuringScheduling: true

  # 💾 RESILIENT STORAGE CONFIGURATION
  storage:
    className: "gp3"
    size: "500Gi"

  # 🔄 COMPREHENSIVE BACKUP STRATEGY
  backups:
    defaultStorage:
      type: "s3"
      bucket: "neo4j-production-backups"

  # Other configuration...
  image:
    repo: "neo4j"
    tag: "5.15-enterprise"
      threshold: 0.8
    - name: "cross_cluster_latency"
      query: "neo4j_cross_cluster_latency_seconds"
      threshold: 0.1

    # Critical alerts
    alerts:
    - name: "zone-imbalance"
      condition: "zone_distribution_health < 0.8"
      severity: "warning"
    - name: "cross-cluster-partition"
      condition: "cross_cluster_latency > 1.0"
      severity: "critical"

  # 🔒 SECURITY FOR MULTI-CLUSTER
  security:
    tls:
      enabled: true
      mutual: true
      crossCluster: true

    authentication:
      provider: "ldap"
      crossClusterSync: true

    networkPolicies:
      enabled: true
      crossClusterTraffic: "encrypted"
```

### Validation and Testing

After deploying your resilient configuration, validate it works:

```bash
#!/bin/bash
# resilience-validation.sh

echo "🧪 Testing Multi-Cluster + Topology Resilience"

# 1. Verify zone distribution
echo "📍 Checking zone distribution..."
kubectl get pods -l app.kubernetes.io/name=neo4j -o wide | \
  awk '{print $1, $7}' | grep -v NAME | \
  while read pod node; do
    zone=$(kubectl get node $node -o jsonpath='{.metadata.labels.topology\.kubernetes\.io/zone}')
    echo "$pod -> $zone"
  done | sort -k2

# 2. Test zone failure simulation
echo "💥 Simulating zone failure..."
kubectl cordon -l topology.kubernetes.io/zone=us-east-1a
kubectl delete pods -l app.kubernetes.io/name=neo4j --field-selector spec.nodeName=$(kubectl get nodes -l topology.kubernetes.io/zone=us-east-1a -o name | head -1 | cut -d/ -f2)

# Wait and check cluster health
sleep 60
kubectl exec neo4j-production-0 -- cypher-shell -u neo4j -p "$NEO4J_PASSWORD" \
  "CALL dbms.cluster.overview() YIELD role, addresses RETURN role, count(*) as count"

# 3. Test cross-cluster connectivity
echo "🌐 Testing cross-cluster connectivity..."
kubectl --context=primary-cluster exec neo4j-production-0 -- \
  nc -zv neo4j-dr.neo4j.svc.clusterset.local 7687

# 4. Restore zone
echo "🔄 Restoring zone..."
kubectl uncordon -l topology.kubernetes.io/zone=us-east-1a

echo "✅ Resilience validation complete"
```

## 🔧 Advanced Configuration

### Cross-Region Networking Options

#### 1. Submariner Configuration

```yaml
spec:
  multiCluster:
    networking:
      type: "submariner"
      submariner:
        # Submariner-specific configuration
        brokerNamespace: "submariner-broker"
        cableDriver: "libreswan"  # or "wireguard", "vxlan"
        natEnabled: true

        # Service discovery
        serviceDiscovery:
          enabled: true
          clustersetDomain: "clusterset.local"

        # Network configuration
        clusterCIDR: "10.244.0.0/16"
        serviceCIDR: "10.96.0.0/12"
        globalCIDR: "192.168.0.0/16"
```

#### 2. Istio Multi-Cluster Configuration

```yaml
spec:
  multiCluster:
    networking:
      type: "istio"
      istio:
        # Istio multi-cluster configuration
        multiCluster:
          networks:
            network1:
              endpoints:
              - fromRegistry: "cluster1"

        # Cross-cluster service discovery
        serviceDiscovery:
          enabled: true
          discoverySelectors:
          - matchLabels:
              app.kubernetes.io/name: "neo4j"

        # Gateway configuration
        gateways:
        - name: "neo4j-gateway"
          servers:
          - port:
              number: 7687
              name: "bolt"
              protocol: "TCP"
            hosts:
            - "neo4j.global"
```

#### 3. Manual VPC Peering Configuration

```yaml
spec:
  multiCluster:
    networking:
      type: "manual"
      manual:
        # Manual network configuration
        peerClusters:
        - name: "us-west-dr"
          endpoint: "10.1.0.100:7687"  # Direct IP
          region: "us-west-2"

        # Security configuration
        tls:
          enabled: true
          mutual: true
          certificateSource: "cert-manager"

        # Load balancer configuration
        loadBalancer:
          type: "aws-nlb"  # or "gcp-ilb", "azure-lb"
          annotations:
            service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
```

### Cross-Cluster Coordination

The operator provides built-in coordination features through the `coordination` field:

```yaml
spec:
  multiCluster:
    coordination:
      leaderElection:
        enabled: true
        leaseDuration: "15s"
        renewDeadline: "10s"
        retryPeriod: "2s"

      stateSynchronization:
        enabled: true
        interval: "30s"
        conflictResolution: "last_writer_wins"

      failoverCoordination:
        enabled: true
        timeout: "5m"
        healthCheck:
          interval: "30s"
          timeout: "10s"
          failureThreshold: 3
          successThreshold: 1
```

## 🌐 Networking Patterns

### Pattern 1: Service Mesh (Istio)

```yaml
# Istio VirtualService for cross-cluster routing
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: neo4j-multicluster
spec:
  hosts:
  - neo4j-global
  gateways:
  - neo4j-gateway
  http:
  - match:
    - headers:
        region:
          exact: "us-east"
    route:
    - destination:
        host: neo4j-primary.neo4j.svc.cluster.local
  - match:
    - headers:
        region:
          exact: "us-west"
    route:
    - destination:
        host: neo4j-dr.neo4j.svc.cluster.local
  - route:  # Default route
    - destination:
        host: neo4j-primary.neo4j.svc.cluster.local
      weight: 80
    - destination:
        host: neo4j-dr.neo4j.svc.cluster.local
      weight: 20
```

### Pattern 2: DNS-Based Routing

```yaml
# CoreDNS configuration for multi-cluster
apiVersion: v1
kind: ConfigMap
metadata:
  name: coredns-custom
  namespace: kube-system
data:
  neo4j.server: |
    neo4j.global {
      forward . 10.1.0.10 10.2.0.10  # Primary and DR cluster DNS
      policy random
    }

    neo4j-primary.global {
      forward . 10.1.0.10  # Primary cluster only
    }

    neo4j-dr.global {
      forward . 10.2.0.10  # DR cluster only
    }
```

## 📊 Monitoring Multi-Cluster Deployments

### Prometheus Configuration

```yaml
# Multi-cluster monitoring
apiVersion: v1
kind: ConfigMap
metadata:
  name: prometheus-multicluster
data:
  prometheus.yml: |
    global:
      scrape_interval: 15s

    scrape_configs:
    # Primary cluster metrics
    - job_name: 'neo4j-primary'
      kubernetes_sd_configs:
      - role: pod
        namespaces:
          names: [neo4j]
        kubeconfig_file: /etc/kubeconfig/primary-cluster
      relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_app]
        action: keep
        regex: neo4j
      - source_labels: [__meta_kubernetes_pod_name]
        target_label: cluster
        replacement: 'primary'

    # DR cluster metrics
    - job_name: 'neo4j-dr'
      kubernetes_sd_configs:
      - role: pod
        namespaces:
          names: [neo4j]
        kubeconfig_file: /etc/kubeconfig/dr-cluster
      relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_app]
        action: keep
        regex: neo4j
      - source_labels: [__meta_kubernetes_pod_name]
        target_label: cluster
        replacement: 'dr'
```

### Grafana Dashboard

```json
{
  "dashboard": {
    "title": "Neo4j Multi-Cluster Overview",
    "panels": [
      {
        "title": "Cluster Health",
        "type": "stat",
        "targets": [
          {
            "expr": "up{job=~\"neo4j-.*\"}",
            "legendFormat": "{{cluster}} - {{instance}}"
          }
        ]
      },
      {
        "title": "Cross-Cluster Latency",
        "type": "graph",
        "targets": [
          {
            "expr": "neo4j_cluster_latency_seconds",
            "legendFormat": "{{source_cluster}} -> {{target_cluster}}"
          }
        ]
      },
      {
        "title": "Failover Events",
        "type": "table",
        "targets": [
          {
            "expr": "increase(neo4j_failover_events_total[1h])",
            "legendFormat": "{{cluster}}"
          }
        ]
      }
    ]
  }
}
```

## 🔍 Troubleshooting

### Common Issues

#### 1. **Cross-Cluster Connectivity**

```bash
# Test network connectivity between clusters
kubectl exec -it neo4j-primary-0 -- nc -zv neo4j-dr.neo4j.svc.clusterset.local 7687

# Check Submariner status
subctl show all

# Verify Istio mesh connectivity
istioctl proxy-config cluster neo4j-primary-0.neo4j
```

#### 2. **DNS Resolution**

```bash
# Test DNS resolution
kubectl exec -it neo4j-primary-0 -- nslookup neo4j-dr.neo4j.svc.clusterset.local

# Check CoreDNS configuration
kubectl get configmap coredns -n kube-system -o yaml
```

#### 3. **Certificate Issues**

```bash
# Check TLS certificates
kubectl get certificates -A

# Verify certificate chain
kubectl exec -it neo4j-primary-0 -- openssl s_client -connect neo4j-dr.neo4j.svc.clusterset.local:7687
```

### Debugging Commands

```bash
# Check multi-cluster status
kubectl get neo4jenterprisecluster -o jsonpath='{.items[*].status.multiCluster}'

# View operator logs
kubectl logs -f deployment/neo4j-operator-controller-manager -n neo4j-operator-system

# Check cluster events
kubectl get events --sort-by=.metadata.creationTimestamp -A | grep neo4j

# Verify cross-cluster services
kubectl get services -A | grep neo4j
```

## 🚀 Best Practices

### 1. **Network Security**

- Use mutual TLS for all cross-cluster communication
- Implement network policies to restrict access
- Use service mesh for encrypted communication

### 2. **Resource Planning**

- Plan for network latency between regions
- Size clusters appropriately for failover scenarios
- Monitor cross-cluster bandwidth usage

### 3. **Backup Strategy**

- Implement cross-region backup replication
- Test restore procedures regularly
- Maintain backup retention policies

### 4. **Monitoring**

- Set up comprehensive monitoring across all clusters
- Implement alerting for failover events
- Monitor cross-cluster latency and performance

### 5. **Testing**

- Regularly test failover procedures
- Perform disaster recovery drills
- Validate data consistency across clusters

## 📚 Related Documentation

- [Disaster Recovery Guide](./disaster-recovery-guide.md)
- [Topology-Aware Placement](./topology-aware-placement.md)
- [Backup & Restore Guide](./backup-restore-guide.md)
- [Performance Guide](./performance-guide.md)

## 🤝 Support

For multi-cluster deployment support:

- Check the [troubleshooting section](#troubleshooting)
- Review operator logs for detailed error messages
- Consult the [API reference](./api-reference.md) for configuration options
- Open an issue on GitHub with cluster configurations and logs
