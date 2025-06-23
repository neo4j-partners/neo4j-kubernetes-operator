# Auto-scaling Guide for Neo4j Enterprise

## Overview

The Neo4j Kubernetes Operator provides intelligent auto-scaling capabilities for both primary and secondary nodes, allowing your cluster to automatically adapt to changing workloads while maintaining performance and cost efficiency.

## 🎯 Auto-scaling Features

### Core Capabilities

- **Primary Node Scaling**: Maintains odd numbers for quorum requirements
- **Secondary Node Scaling**: Scales based on read workload demands
- **Multi-metric Scaling**: CPU, memory, query latency, connection count, and custom metrics
- **Zone-aware Scaling**: Distributes replicas across availability zones
- **Quorum Protection**: Prevents scaling that would break cluster quorum
- **Predictive Scaling**: ML-based workload prediction (Enterprise feature)

## 🏗️ Basic Auto-scaling Configuration

### Simple CPU-based Scaling

```yaml
# basic-autoscaling.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-autoscaling
spec:
  topology:
    primaries: 3
    secondaries: 2

  autoScaling:
    enabled: true

    primaries:
      enabled: true
      minReplicas: 3
      maxReplicas: 7
      metrics:
      - type: "cpu"
        target: "70"
        weight: "1.0"

    secondaries:
      enabled: true
      minReplicas: 1
      maxReplicas: 10
      metrics:
      - type: "cpu"
        target: "60"
        weight: "1.0"

  # Other configuration...
  image:
    repo: "neo4j"
    tag: "5.15-enterprise"

  storage:
    className: "gp3"
    size: "500Gi"
```

## 🔧 Advanced Multi-metric Scaling

### Production-ready Configuration

```yaml
# advanced-autoscaling.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-production-autoscaling
spec:
  topology:
    primaries: 3
    secondaries: 2
    enforceDistribution: true
    availabilityZones:
      - "us-west-2a"
      - "us-west-2b"
      - "us-west-2c"

  autoScaling:
    enabled: true

    # Primary nodes auto-scaling
    primaries:
      enabled: true
      minReplicas: 3  # Must be odd for quorum
      maxReplicas: 7  # Must be odd for quorum

      # Multiple scaling metrics
      metrics:
      - type: "cpu"
        target: "70"
        weight: "1.0"
      - type: "memory"
        target: "80"
        weight: "0.8"
      - type: "query_latency"
        target: "100ms"
        weight: "1.2"
        source:
          type: "neo4j"
          neo4j:
            cypherQuery: "CALL dbms.queryJournal() YIELD elapsedTimeMillis RETURN avg(elapsedTimeMillis)"
            metricName: "avg_query_latency"

      # Quorum protection
      quorumProtection:
        enabled: true
        minHealthyPrimaries: 2
        healthCheck:
          interval: "30s"
          timeout: "10s"
          failureThreshold: 3

    # Secondary nodes auto-scaling
    secondaries:
      enabled: true
      minReplicas: 1
      maxReplicas: 20

      # Read-optimized metrics
      metrics:
      - type: "cpu"
        target: "60"
        weight: "1.0"
      - type: "connection_count"
        target: "100"
        weight: "1.5"
        source:
          type: "neo4j"
          neo4j:
            jmxBean: "org.neo4j:instance=kernel#0,name=Bolt"
            metricName: "ConnectionsOpened"
      - type: "throughput"
        target: "1000"
        weight: "1.0"
        source:
          type: "prometheus"
          prometheus:
            serverUrl: "http://prometheus:9090"
            query: "rate(neo4j_bolt_messages_received_total[5m])"
            interval: "30s"

      # Zone-aware scaling
      zoneAware:
        enabled: true
        minReplicasPerZone: 1
        maxZoneSkew: 2
        zonePreference:
          - "us-west-2a"
          - "us-west-2b"
          - "us-west-2c"

    # Global scaling behavior
    behavior:
      scaleUp:
        stabilizationWindow: "60s"
        policies:
        - type: "Pods"
          value: 2
          period: "60s"
        - type: "Percent"
          value: 50
          period: "60s"
        selectPolicy: "Max"

      scaleDown:
        stabilizationWindow: "300s"  # 5 minutes
        policies:
        - type: "Pods"
          value: 1
          period: "60s"
        selectPolicy: "Min"

      # Coordination between primary and secondary scaling
      coordination:
        enabled: true
        primaryPriority: 8
        secondaryPriority: 5
        scalingDelay: "30s"

    # Advanced features
    advanced:
      predictive:
        enabled: true
        historicalWindow: "7d"
        predictionHorizon: "1h"
        confidenceThreshold: "0.8"

      customAlgorithms:
      - name: "workload-predictor"
        type: "webhook"
        webhook:
          url: "http://ml-service:8080/predict"
          method: "POST"
          timeout: "30s"
          headers:
            Content-Type: "application/json"
        config:
          model: "neo4j-workload-v1"
          threshold: "0.75"
```

## 📊 Custom Metrics and Monitoring

### Prometheus-based Custom Metrics

```yaml
# prometheus-metrics-scaling.yaml
spec:
  autoScaling:
    enabled: true
    secondaries:
      metrics:
      - type: "custom"
        target: "50"
        weight: "1.0"
        customQuery: "avg(rate(neo4j_page_cache_hits_total[5m]))"
        source:
          type: "prometheus"
          prometheus:
            serverUrl: "http://prometheus:9090"
            query: "avg(rate(neo4j_page_cache_hits_total[5m]))"
            interval: "30s"

      - type: "custom"
        target: "80"
        weight: "1.2"
        customQuery: "neo4j_store_size_total / neo4j_store_size_limit"
        source:
          type: "prometheus"
          prometheus:
            serverUrl: "http://prometheus:9090"
            query: "neo4j_store_size_total / neo4j_store_size_limit * 100"
```

### Neo4j-specific Metrics

```yaml
# neo4j-metrics-scaling.yaml
spec:
  autoScaling:
    enabled: true
    primaries:
      metrics:
      - type: "custom"
        target: "1000"
        weight: "1.0"
        source:
          type: "neo4j"
          neo4j:
            cypherQuery: |
              CALL dbms.cluster.overview()
              YIELD role, addresses, databases
              WHERE role = 'LEADER'
              RETURN count(*) as leader_count
            metricName: "leader_availability"

      - type: "custom"
        target: "95"
        weight: "1.5"
        source:
          type: "neo4j"
          neo4j:
            jmxBean: "org.neo4j:instance=kernel#0,name=HighAvailability"
            metricName: "SlaveUpdatePullerLastUpdateTime"
```

## 🎛️ Scaling Policies and Behaviors

### Conservative Scaling (Production)

```yaml
# conservative-scaling.yaml
spec:
  autoScaling:
    behavior:
      scaleUp:
        stabilizationWindow: "180s"  # 3 minutes
        policies:
        - type: "Pods"
          value: 1
          period: "180s"
        selectPolicy: "Min"

      scaleDown:
        stabilizationWindow: "600s"  # 10 minutes
        policies:
        - type: "Pods"
          value: 1
          period: "300s"
        selectPolicy: "Min"
```

### Aggressive Scaling (Development/Testing)

```yaml
# aggressive-scaling.yaml
spec:
  autoScaling:
    behavior:
      scaleUp:
        stabilizationWindow: "30s"
        policies:
        - type: "Percent"
          value: 100
          period: "30s"
        selectPolicy: "Max"

      scaleDown:
        stabilizationWindow: "60s"
        policies:
        - type: "Percent"
          value: 50
          period: "60s"
        selectPolicy: "Max"
```

## 🔍 Monitoring and Observability

### Auto-scaling Metrics Dashboard

```yaml
# autoscaling-monitoring.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: autoscaling-grafana-dashboard
data:
  dashboard.json: |
    {
      "dashboard": {
        "title": "Neo4j Auto-scaling Dashboard",
        "panels": [
          {
            "title": "Cluster Size Over Time",
            "type": "graph",
            "targets": [
              {
                "expr": "neo4j_cluster_primaries_total",
                "legendFormat": "Primaries"
              },
              {
                "expr": "neo4j_cluster_secondaries_total",
                "legendFormat": "Secondaries"
              }
            ]
          },
          {
            "title": "Scaling Events",
            "type": "table",
            "targets": [
              {
                "expr": "increase(neo4j_autoscaling_events_total[1h])",
                "legendFormat": "{{type}} - {{reason}}"
              }
            ]
          },
          {
            "title": "Resource Utilization",
            "type": "graph",
            "targets": [
              {
                "expr": "avg(neo4j_cpu_utilization_percent)",
                "legendFormat": "CPU %"
              },
              {
                "expr": "avg(neo4j_memory_utilization_percent)",
                "legendFormat": "Memory %"
              }
            ]
          }
        ]
      }
    }
```

### Prometheus Alerts for Auto-scaling

```yaml
# autoscaling-alerts.yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: neo4j-autoscaling-alerts
spec:
  groups:
  - name: neo4j.autoscaling
    rules:
    - alert: AutoscalingDisabled
      expr: neo4j_autoscaling_enabled == 0
      for: 5m
      labels:
        severity: warning
      annotations:
        summary: "Neo4j auto-scaling is disabled"
        description: "Auto-scaling has been disabled for cluster {{ $labels.cluster }}"

    - alert: MaxReplicasReached
      expr: neo4j_cluster_replicas_current >= neo4j_autoscaling_max_replicas
      for: 2m
      labels:
        severity: warning
      annotations:
        summary: "Neo4j cluster at maximum replicas"
        description: "Cluster {{ $labels.cluster }} has reached maximum replicas"

    - alert: ScalingEventsFailed
      expr: increase(neo4j_autoscaling_events_failed_total[10m]) > 3
      for: 1m
      labels:
        severity: critical
      annotations:
        summary: "Multiple auto-scaling events failed"
        description: "{{ $value }} scaling events failed in the last 10 minutes"

    - alert: QuorumAtRisk
      expr: neo4j_cluster_primaries_healthy < 2
      for: 30s
      labels:
        severity: critical
      annotations:
        summary: "Neo4j cluster quorum at risk"
        description: "Only {{ $value }} healthy primaries remaining"
```

## 🧪 Testing Auto-scaling

### Load Testing Script

```bash
#!/bin/bash
# autoscaling-load-test.sh

echo "🧪 Starting Neo4j Auto-scaling Load Test"

# Configuration
CLUSTER_ENDPOINT="neo4j-autoscaling-client:7687"
TEST_DURATION="600"  # 10 minutes
CONCURRENT_CONNECTIONS="50"

# Step 1: Record initial cluster size
echo "📊 Recording initial cluster size"
INITIAL_PRIMARIES=$(kubectl get neo4jenterprisecluster neo4j-autoscaling -o jsonpath='{.status.replicas.primaries}')
INITIAL_SECONDARIES=$(kubectl get neo4jenterprisecluster neo4j-autoscaling -o jsonpath='{.status.replicas.secondaries}')

echo "Initial cluster size: ${INITIAL_PRIMARIES} primaries, ${INITIAL_SECONDARIES} secondaries"

# Step 2: Generate load
echo "🚀 Generating load for ${TEST_DURATION} seconds"
for i in $(seq 1 $CONCURRENT_CONNECTIONS); do
  kubectl run load-test-$i --rm -i --image=neo4j:latest --restart=Never -- \
    cypher-shell -a "bolt://${CLUSTER_ENDPOINT}" -u neo4j -p "$NEO4J_PASSWORD" \
    --format plain \
    "UNWIND range(1, 1000) as i
     CREATE (n:LoadTest {id: i, timestamp: timestamp()})
     WITH n
     MATCH (m:LoadTest) WHERE m.id < n.id
     CREATE (n)-[:CONNECTED_TO]->(m)" &
done

# Step 3: Monitor scaling events
echo "👀 Monitoring scaling events"
kubectl get events --watch --field-selector reason=ScalingUp,reason=ScalingDown &
EVENTS_PID=$!

# Step 4: Wait for test duration
sleep $TEST_DURATION

# Step 5: Stop monitoring and cleanup
kill $EVENTS_PID 2>/dev/null || true

# Step 6: Record final cluster size
echo "📊 Recording final cluster size"
FINAL_PRIMARIES=$(kubectl get neo4jenterprisecluster neo4j-autoscaling -o jsonpath='{.status.replicas.primaries}')
FINAL_SECONDARIES=$(kubectl get neo4jenterprisecluster neo4j-autoscaling -o jsonpath='{.status.replicas.secondaries}')

echo "Final cluster size: ${FINAL_PRIMARIES} primaries, ${FINAL_SECONDARIES} secondaries"

# Step 7: Cleanup test data
echo "🧹 Cleaning up test data"
kubectl run cleanup --rm -i --tty --image=neo4j:latest --restart=Never -- \
  cypher-shell -a "bolt://${CLUSTER_ENDPOINT}" -u neo4j -p "$NEO4J_PASSWORD" \
  "MATCH (n:LoadTest) DETACH DELETE n"

echo "🎉 Load test completed!"
echo "Scaling results:"
echo "  Primaries: ${INITIAL_PRIMARIES} → ${FINAL_PRIMARIES}"
echo "  Secondaries: ${INITIAL_SECONDARIES} → ${FINAL_SECONDARIES}"
```

### Scaling Verification

```bash
#!/bin/bash
# verify-autoscaling.sh

echo "✅ Verifying Auto-scaling Configuration"

# Check if auto-scaling is enabled
AUTOSCALING_ENABLED=$(kubectl get neo4jenterprisecluster neo4j-autoscaling -o jsonpath='{.spec.autoScaling.enabled}')
if [ "$AUTOSCALING_ENABLED" = "true" ]; then
  echo "✅ Auto-scaling is enabled"
else
  echo "❌ Auto-scaling is disabled"
  exit 1
fi

# Check primary scaling configuration
PRIMARY_MIN=$(kubectl get neo4jenterprisecluster neo4j-autoscaling -o jsonpath='{.spec.autoScaling.primaries.minReplicas}')
PRIMARY_MAX=$(kubectl get neo4jenterprisecluster neo4j-autoscaling -o jsonpath='{.spec.autoScaling.primaries.maxReplicas}')

if [ $((PRIMARY_MIN % 2)) -eq 1 ] && [ $((PRIMARY_MAX % 2)) -eq 1 ]; then
  echo "✅ Primary replica counts are odd (quorum safe): min=$PRIMARY_MIN, max=$PRIMARY_MAX"
else
  echo "❌ Primary replica counts must be odd for quorum: min=$PRIMARY_MIN, max=$PRIMARY_MAX"
  exit 1
fi

# Check metrics configuration
METRICS_COUNT=$(kubectl get neo4jenterprisecluster neo4j-autoscaling -o jsonpath='{.spec.autoScaling.primaries.metrics}' | jq length)
if [ "$METRICS_COUNT" -gt 0 ]; then
  echo "✅ Scaling metrics configured: $METRICS_COUNT metrics"
else
  echo "❌ No scaling metrics configured"
  exit 1
fi

echo "🎉 Auto-scaling configuration is valid!"
```

## 🛠️ Troubleshooting

### Common Issues

#### Issue: Scaling Not Triggering

```bash
# Check metrics availability
kubectl top pods -l app.kubernetes.io/name=neo4j

# Check HPA status (if using Kubernetes HPA integration)
kubectl describe hpa neo4j-autoscaling-hpa

# Check operator logs
kubectl logs -f deployment/neo4j-operator-controller-manager -n neo4j-operator-system | grep -i scaling
```

#### Issue: Quorum Broken During Scaling

```bash
# Check cluster health
kubectl exec neo4j-autoscaling-primary-0 -- cypher-shell -u neo4j -p "$NEO4J_PASSWORD" \
  "CALL dbms.cluster.overview() YIELD role, addresses RETURN role, count(*)"

# Emergency quorum repair (if needed)
kubectl patch neo4jenterprisecluster neo4j-autoscaling \
  --type='merge' \
  -p='{"spec":{"autoScaling":{"primaries":{"allowQuorumBreak":true}}}}'
```

#### Issue: Scaling Too Aggressive

```bash
# Increase stabilization windows
kubectl patch neo4jenterprisecluster neo4j-autoscaling \
  --type='merge' \
  -p='{"spec":{"autoScaling":{"behavior":{"scaleUp":{"stabilizationWindow":"300s"},"scaleDown":{"stabilizationWindow":"600s"}}}}}'
```

### Performance Tuning

#### Metric Collection Optimization

```yaml
# Optimized metric collection
spec:
  autoScaling:
    primaries:
      metrics:
      - type: "cpu"
        target: "70"
        source:
          type: "kubernetes"  # Faster than Prometheus for basic metrics
      - type: "custom"
        target: "100"
        source:
          type: "prometheus"
          prometheus:
            serverUrl: "http://prometheus:9090"
            query: "avg(neo4j_active_transactions)"
            interval: "60s"  # Reduce frequency for expensive queries
```

## 📚 Best Practices

### 1. **Start Conservative**

- Begin with longer stabilization windows
- Use smaller scaling increments
- Monitor closely before increasing aggressiveness

### 2. **Quorum Safety**

- Always maintain odd numbers of primaries
- Enable quorum protection
- Set appropriate minimum healthy primary thresholds

### 3. **Multi-metric Approach**

- Don't rely on CPU alone
- Include application-specific metrics
- Weight metrics appropriately

### 4. **Zone Awareness**

- Enable zone-aware scaling for high availability
- Set appropriate zone distribution policies
- Consider zone capacity limitations

### 5. **Testing and Validation**

- Test scaling behavior under realistic loads
- Validate scaling triggers and thresholds
- Monitor resource utilization patterns

## 📚 Related Documentation

- [Performance Guide](./performance-guide.md)
- [Topology-Aware Placement](./topology-aware-placement.md)
- [Multi-Cluster Deployment Guide](./multi-cluster-deployment-guide.md)
- [Query Monitoring Guide](./query-monitoring-guide.md)

## 🤝 Support

For auto-scaling support:

- Monitor scaling events and metrics
- Test thoroughly in non-production environments
- Adjust policies based on actual workload patterns
- Contact support for advanced configuration guidance
