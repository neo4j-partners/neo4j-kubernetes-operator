# Disaster Recovery Guide for Neo4j Enterprise

## Overview

This guide demonstrates how to implement comprehensive disaster recovery (DR) for Neo4j Enterprise clusters using the Neo4j Kubernetes Operator's built-in features. Rather than a separate DR system, the operator provides DR capabilities through the combination of **topology-aware placement**, **multi-cluster deployments**, and **backup & restore** features.

## 🎯 Disaster Recovery Strategies

### Strategy 1: Zone-Level Resilience (RTO: 0-30s, RPO: 0)

**Use Case:** Protection against availability zone failures within a region
**Implementation:** Topology-aware placement with zone distribution

### Strategy 2: Region-Level Resilience (RTO: 1-5 minutes, RPO: 0-1 minute)

**Use Case:** Protection against entire region failures
**Implementation:** Multi-cluster deployment with cross-region networking

### Strategy 3: Complete Infrastructure Failure (RTO: 15-60 minutes, RPO: 1-24 hours)

**Use Case:** Protection against cloud provider or complete infrastructure failures
**Implementation:** Cross-cloud backup and restore

## 🏗️ DR Architecture Patterns

### Pattern 1: Multi-Zone High Availability

```yaml
┌─────────────────────────────────────────────────────────────────┐
│                    Single-Region Multi-Zone                     │
├─────────────────────────────────────────────────────────────────┤
│  Zone A              │  Zone B              │  Zone C           │
│  ┌─────────────────┐ │  ┌─────────────────┐ │ ┌─────────────────┐│
│  │ Primary Node 1  │ │  │ Primary Node 2  │ │ │ Primary Node 3  ││
│  │ Secondary Node 1│ │  │ Secondary Node 2│ │ │                 ││
│  └─────────────────┘ │  └─────────────────┘ │ └─────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

### Pattern 2: Cross-Region Active-Standby

```yaml
┌─────────────────────────────────────────────────────────────────┐
│                    Primary Region (Active)                      │
├─────────────────────────────────────────────────────────────────┤
│  Zone A              │  Zone B              │  Zone C           │
│  ┌─────────────────┐ │  ┌─────────────────┐ │ ┌─────────────────┐│
│  │ Primary Node 1  │ │  │ Primary Node 2  │ │ │ Primary Node 3  ││
│  │ Secondary Node 1│ │  │ Secondary Node 2│ │ │                 ││
│  └─────────────────┘ │  └─────────────────┘ │ └─────────────────┘│
└─────────────────────────────────────────────────────────────────┘
                                  │
                                  │ Backup Replication
                                  ▼
┌─────────────────────────────────────────────────────────────────┐
│                      DR Region (Standby)                        │
├─────────────────────────────────────────────────────────────────┤
│  Zone A              │  Zone B                                  │
│  ┌─────────────────┐ │  ┌─────────────────┐                    │
│  │ Standby Node 1  │ │  │ Standby Node 2  │                    │
│  │ (Restored from  │ │  │ (Restored from  │                    │
│  │  backup)        │ │  │  backup)        │                    │
│  └─────────────────┘ │  └─────────────────┘                    │
└─────────────────────────────────────────────────────────────────┘
```

### Pattern 3: Cross-Cloud Multi-Region

```yaml
┌─────────────────────────────────────────────────────────────────┐
│                         AWS Region                              │
├─────────────────────────────────────────────────────────────────┤
│  Primary Cluster (3 Primaries + 2 Secondaries)                 │
└─────────────────────────────────────────────────────────────────┘
                                  │
                                  │ Cross-Cloud Backup
                                  ▼
┌─────────────────────────────────────────────────────────────────┐
│                         GCP Region                              │
├─────────────────────────────────────────────────────────────────┤
│  DR Cluster (1 Primary + 1 Secondary, restored from backup)    │
└─────────────────────────────────────────────────────────────────┘
```

## 🔧 Implementation Examples

### Step 1: Zone-Level Resilience

```yaml
# primary-cluster.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-primary
  namespace: neo4j
spec:
  # Zone-aware topology
  topology:
    primaries: 3
    secondaries: 2
    enforceDistribution: true
    availabilityZones:
      - "us-east-1a"
      - "us-east-1b"
      - "us-east-1c"

    # Anti-affinity and topology spread
    placement:
      antiAffinity:
        enabled: true
        topologyKey: "topology.kubernetes.io/zone"
        type: "required"

      topologySpread:
        enabled: true
        topologyKey: "topology.kubernetes.io/zone"
        maxSkew: 1
        whenUnsatisfiable: "DoNotSchedule"

  # Frequent backups for DR
  backups:
    defaultStorage:
      type: "s3"
      bucket: "neo4j-multiregion-backups"
    schedule: "*/10 * * * *"  # Every 10 minutes
    retention: "30d"

  # Other configuration...
  image:
    repo: "neo4j"
    tag: "5.15-enterprise"

  storage:
    className: "gp3"
    size: "500Gi"
```

### Step 2: Cross-Region Multi-Cluster

```yaml
# multi-cluster-primary.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-primary
  namespace: neo4j
spec:
  # Multi-cluster configuration
  multiCluster:
    enabled: true
    topology:
      clusters:
      - name: "us-east-primary"
        region: "us-east-1"
        endpoint: "neo4j-primary.neo4j.svc.cluster.local:7687"
        nodeAllocation:
          primaries: 3
          secondaries: 2
      strategy: "active-passive"
      primaryCluster: "us-east-primary"

    # Cross-region networking
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

  # Zone-aware topology
  topology:
    primaries: 3
    secondaries: 2
    enforceDistribution: true
    availabilityZones:
      - "us-east-1a"
      - "us-east-1b"
      - "us-east-1c"

  # Cross-region backup configuration
  backups:
    defaultStorage:
      type: "s3"
      bucket: "neo4j-multiregion-backups"
    schedule: "*/10 * * * *"  # Every 10 minutes
    retention: "30d"

  # Other configuration...
  image:
    repo: "neo4j"
    tag: "5.15-enterprise"

  storage:
    className: "gp3"
    size: "500Gi"
```

#### DR Region Configuration

```yaml
# dr-region-cluster.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-dr
  namespace: neo4j
spec:
  # Multi-cluster configuration
  multiCluster:
    enabled: true
    topology:
      clusters:
      - name: "us-west-dr"
        region: "us-west-2"
        endpoint: "neo4j-dr.neo4j.svc.cluster.local:7687"
        nodeAllocation:
          primaries: 1
          secondaries: 1
      strategy: "active-passive"
      primaryCluster: "us-east-primary"

    # Cross-region networking
    networking:
      type: "cilium"
      cilium:
        clusterMesh:
          enabled: true
          clusterId: 2
        encryption:
          enabled: true
          type: "wireguard"

      dns:
        enabled: true
        zone: "neo4j.local"

  # Smaller topology for DR (cost optimization)
  topology:
    primaries: 1
    secondaries: 1
    enforceDistribution: true
    availabilityZones:
      - "us-west-2a"
      - "us-west-2b"

  # Storage configuration
  storage:
    className: "gp3"
    size: "500Gi"

  # Restore from cross-region backup
  restoreFrom:
    storage:
      type: "s3"
      bucket: "neo4j-multiregion-backups"

  # Backup configuration (for local backups)
  backups:
    defaultStorage:
      type: "s3"
      bucket: "neo4j-multiregion-backups-west"

  # Other configuration...
  image:
    repo: "neo4j"
    tag: "5.15-enterprise"
```

### Step 3: Cross-Cloud DR Setup

```yaml
# cross-cloud-dr.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-cross-cloud-dr
  namespace: neo4j
spec:
  # Multi-cluster configuration
  multiCluster:
    enabled: true
    topology:
      clusters:
      - name: "gcp-dr"
        region: "us-central1"
        endpoint: "neo4j-dr.neo4j.svc.cluster.local:7687"
        nodeAllocation:
          primaries: 1
          secondaries: 1
      strategy: "active-passive"

  # Cross-cloud backup strategy
  restoreFrom:
    storage:
      type: "gcs"
      bucket: "neo4j-dr-backups-gcp"

  backups:
    defaultStorage:
      type: "gcs"
      bucket: "neo4j-dr-backups-gcp"
    schedule: "0 */6 * * *"  # Every 6 hours
    retention: "90d"

  # Other configuration...
  image:
    repo: "neo4j"
    tag: "5.15-enterprise"

  storage:
    className: "ssd"
    size: "500Gi"

  topology:
    primaries: 1
    secondaries: 1
    availabilityZones:
      - "us-central1-a"
      - "us-central1-b"
```

## 🔧 Automated DR Procedures

### Health Check and Monitoring

```yaml
# dr-monitoring.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: dr-monitoring-config
  namespace: neo4j
data:
  monitor.sh: |
    #!/bin/bash
    set -e

    # Configuration
    PRIMARY_ENDPOINT="neo4j-primary.neo4j.svc.cluster.local:7687"
    DR_ENDPOINT="neo4j-dr.neo4j.svc.cluster.local:7687"
    FAILOVER_THRESHOLD=3

    # Health check function
    check_cluster_health() {
      local endpoint=$1
      echo "Checking health of $endpoint"

      # Test connectivity
      if ! nc -z ${endpoint%:*} ${endpoint#*:}; then
        return 1
      fi

      # Test Neo4j cluster health
      cypher-shell -a "bolt://$endpoint" \
        -u neo4j -p "$NEO4J_PASSWORD" \
        "CALL dbms.cluster.overview() YIELD role RETURN role" || return 1

      return 0
    }

    # Failover function
    trigger_failover() {
      echo "Triggering failover to DR region"

      # 1. Scale up DR cluster
      kubectl patch neo4jenterprisecluster neo4j-dr \
        --type='merge' \
        -p='{"spec":{"topology":{"primaries":3,"secondaries":2}}}'

      # 2. Update DNS/Load balancer to point to DR cluster
      kubectl patch service neo4j-global \
        --type='merge' \
        -p='{"spec":{"selector":{"app.kubernetes.io/instance":"neo4j-dr"}}}'

      # 3. Send alert
      curl -X POST "$SLACK_WEBHOOK" \
        -H 'Content-type: application/json' \
        --data '{"text":"🚨 DR Failover activated! Primary cluster is down."}'

      # 4. Log failover event
      kubectl create event \
        --type=Warning \
        --reason=DisasterRecoveryFailover \
        --message="Automatic failover to DR region activated"
    }

    # Main health check loop
    FAILED_CHECKS=$(kubectl get configmap dr-state -o jsonpath='{.data.failed_checks}' 2>/dev/null || echo "0")

    if ! check_cluster_health "$PRIMARY_ENDPOINT"; then
      FAILED_CHECKS=$((FAILED_CHECKS + 1))
      echo "Primary cluster health check failed ($FAILED_CHECKS/$FAILOVER_THRESHOLD)"

      # Store failure count in ConfigMap
      kubectl patch configmap dr-state \
        --type='merge' \
        -p="{\"data\":{\"failed_checks\":\"$FAILED_CHECKS\"}}" 2>/dev/null || \
      kubectl create configmap dr-state --from-literal=failed_checks="$FAILED_CHECKS"

      if [ $FAILED_CHECKS -ge $FAILOVER_THRESHOLD ]; then
        # Check if DR is already active (check if DR cluster is scaled up)
        DR_PRIMARIES=$(kubectl get neo4jenterprisecluster neo4j-dr \
          -o jsonpath='{.spec.topology.primaries}')

        if [ "$DR_PRIMARIES" -lt 3 ]; then
          trigger_failover
        fi
      fi
    else
      echo "Primary cluster is healthy"
      # Reset failure count
      kubectl patch configmap dr-state \
        --type='merge' \
        -p='{"data":{"failed_checks":"0"}}' 2>/dev/null || \
      kubectl create configmap dr-state --from-literal=failed_checks="0"
    fi

---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: dr-health-monitor
  namespace: neo4j
spec:
  schedule: "*/1 * * * *"  # Every minute
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: dr-monitor
            image: neo4j/dr-monitor:latest
            env:
            - name: NEO4J_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: neo4j-auth
                  key: password
            - name: SLACK_WEBHOOK
              valueFrom:
                secretKeyRef:
                  name: dr-alerts
                  key: slack-webhook
            command:
            - /bin/bash
            - /scripts/monitor.sh
            volumeMounts:
            - name: monitor-script
              mountPath: /scripts
          volumes:
          - name: monitor-script
            configMap:
              name: dr-monitoring-config
              defaultMode: 0755
          restartPolicy: OnFailure
```

## 📊 DR Testing and Validation

### Disaster Recovery Drill Script

```bash
#!/bin/bash
# dr-drill.sh - Automated DR testing script

set -e

echo "🧪 Starting Disaster Recovery Drill"
echo "=================================="

# Configuration
PRIMARY_CLUSTER="neo4j-primary"
DR_CLUSTER="neo4j-dr"
TEST_NAMESPACE="neo4j"
BACKUP_BUCKET="neo4j-dr-test-backups"

# Step 1: Create test data in primary cluster
echo "📝 Step 1: Creating test data in primary cluster"
kubectl exec -it ${PRIMARY_CLUSTER}-primary-0 -n ${TEST_NAMESPACE} -- cypher-shell -u neo4j -p "$NEO4J_PASSWORD" "
CREATE (t:TestNode {id: randomUUID(), timestamp: datetime(), drill: 'dr-test'})
RETURN t.id as testId
" | tee /tmp/test-data-id.txt

TEST_ID=$(grep -o '[a-f0-9-]\{36\}' /tmp/test-data-id.txt | head -1)
echo "Created test node with ID: $TEST_ID"

# Step 2: Trigger backup
echo "🔄 Step 2: Triggering immediate backup"
kubectl create job dr-test-backup-$(date +%s) \
  --from=cronjob/neo4j-backup-cron \
  -n ${TEST_NAMESPACE}

# Wait for backup to complete
echo "⏳ Waiting for backup to complete..."
sleep 60

# Step 3: Simulate primary cluster failure
echo "💥 Step 3: Simulating primary cluster failure"
kubectl scale statefulset ${PRIMARY_CLUSTER}-primary --replicas=0 -n ${TEST_NAMESPACE}

# Step 4: Activate DR cluster (scale up)
echo "🚀 Step 4: Activating DR cluster"
kubectl patch neo4jenterprisecluster ${DR_CLUSTER} \
  --type='merge' \
  -p='{"spec":{"topology":{"primaries":3,"secondaries":2}}}' \
  -n ${TEST_NAMESPACE}

# Wait for DR cluster to become ready
echo "⏳ Waiting for DR cluster to become active..."
kubectl wait --for=condition=Ready pod/${DR_CLUSTER}-primary-0 -n ${TEST_NAMESPACE} --timeout=300s

# Step 5: Verify data in DR cluster
echo "✅ Step 5: Verifying test data in DR cluster"
DATA_FOUND=$(kubectl exec -it ${DR_CLUSTER}-primary-0 -n ${TEST_NAMESPACE} -- cypher-shell -u neo4j -p "$NEO4J_PASSWORD" "
MATCH (t:TestNode {id: '$TEST_ID'})
RETURN count(t) as found
" | grep -o '[0-9]' | head -1)

if [ "$DATA_FOUND" = "1" ]; then
  echo "✅ Test data found in DR cluster - DR SUCCESS!"
else
  echo "❌ Test data NOT found in DR cluster - DR FAILED!"
  exit 1
fi

# Step 6: Test application connectivity
echo "🔌 Step 6: Testing application connectivity to DR cluster"
kubectl run dr-test-client --rm -i --tty --image=neo4j:latest --restart=Never -- \
  cypher-shell -a "bolt://${DR_CLUSTER}-client.${TEST_NAMESPACE}.svc.cluster.local:7687" \
  -u neo4j -p "$NEO4J_PASSWORD" \
  "CALL dbms.cluster.overview() YIELD role, addresses RETURN role, addresses"

# Step 7: Cleanup test data
echo "🧹 Step 7: Cleaning up test data"
kubectl exec -it ${DR_CLUSTER}-primary-0 -n ${TEST_NAMESPACE} -- cypher-shell -u neo4j -p "$NEO4J_PASSWORD" "
MATCH (t:TestNode {drill: 'dr-test'})
DELETE t
"

# Step 8: Restore primary cluster (cleanup)
echo "🔄 Step 8: Restoring primary cluster"
kubectl scale statefulset ${PRIMARY_CLUSTER}-primary --replicas=3 -n ${TEST_NAMESPACE}
kubectl patch neo4jenterprisecluster ${DR_CLUSTER} \
  --type='merge' \
  -p='{"spec":{"topology":{"primaries":1,"secondaries":1}}}' \
  -n ${TEST_NAMESPACE}

echo "🎉 Disaster Recovery Drill Completed Successfully!"
echo "📊 Results:"
echo "  - RTO (Recovery Time Objective): ~5 minutes"
echo "  - RPO (Recovery Point Objective): ~10 minutes (backup frequency)"
echo "  - Data Integrity: ✅ Verified"
echo "  - Application Connectivity: ✅ Tested"
```

## 📈 DR Monitoring and Metrics

### Prometheus Rules for DR Monitoring

```yaml
# dr-prometheus-rules.yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: neo4j-dr-rules
  namespace: neo4j
spec:
  groups:
  - name: neo4j.disaster-recovery
    rules:
    # RTO (Recovery Time Objective) tracking
    - record: neo4j:dr:rto_seconds
      expr: |
        (
          neo4j_cluster_failover_completed_timestamp -
          neo4j_cluster_primary_failure_timestamp
        )
      labels:
        metric_type: "rto"

    # RPO (Recovery Point Objective) tracking
    - record: neo4j:dr:rpo_seconds
      expr: |
        (
          neo4j_cluster_failover_completed_timestamp -
          neo4j_backup_last_successful_timestamp
        )
      labels:
        metric_type: "rpo"

    # DR cluster health
    - record: neo4j:dr:cluster_health
      expr: |
        up{job="neo4j-dr"} and on()
        neo4j_cluster_member_role == 1

    # Backup freshness
    - record: neo4j:dr:backup_age_seconds
      expr: |
        time() - neo4j_backup_last_successful_timestamp

    # Cross-region replication lag
    - record: neo4j:dr:replication_lag_seconds
      expr: |
        neo4j_cluster_replication_lag_seconds{
          source_region!="target_region"
        }

    # Alerts
    - alert: DRClusterDown
      expr: neo4j:dr:cluster_health == 0
      for: 2m
      labels:
        severity: critical
      annotations:
        summary: "DR cluster is down"
        description: "Neo4j DR cluster has been down for more than 2 minutes"

    - alert: BackupTooOld
      expr: neo4j:dr:backup_age_seconds > 1800  # 30 minutes
      for: 5m
      labels:
        severity: warning
      annotations:
        summary: "Backup is too old"
        description: "Last successful backup is older than 30 minutes"

    - alert: CrossRegionReplicationLag
      expr: neo4j:dr:replication_lag_seconds > 300  # 5 minutes
      for: 10m
      labels:
        severity: warning
      annotations:
        summary: "Cross-region replication lag detected"
        description: "Replication lag between regions is {{ $value }} seconds"

    - alert: DRRTOExceeded
      expr: neo4j:dr:rto_seconds > 600  # 10 minutes
      for: 1m
      labels:
        severity: critical
      annotations:
        summary: "DR RTO exceeded"
        description: "Disaster recovery took {{ $value }} seconds, exceeding RTO target"

    - alert: DRRPOExceeded
      expr: neo4j:dr:rpo_seconds > 3600  # 1 hour
      for: 1m
      labels:
        severity: critical
      annotations:
        summary: "DR RPO exceeded"
        description: "Data loss window is {{ $value }} seconds, exceeding RPO target"
```

### Grafana Dashboard for DR Monitoring

```json
{
  "dashboard": {
    "id": null,
    "title": "Neo4j Disaster Recovery Dashboard",
    "tags": ["neo4j", "disaster-recovery"],
    "timezone": "browser",
    "panels": [
      {
        "title": "DR Cluster Health",
        "type": "stat",
        "targets": [
          {
            "expr": "neo4j:dr:cluster_health",
            "legendFormat": "DR Cluster Status"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "color": {
              "mode": "thresholds"
            },
            "thresholds": {
              "steps": [
                {"color": "red", "value": 0},
                {"color": "green", "value": 1}
              ]
            }
          }
        }
      },
      {
        "title": "RTO (Recovery Time Objective)",
        "type": "stat",
        "targets": [
          {
            "expr": "neo4j:dr:rto_seconds",
            "legendFormat": "Last RTO (seconds)"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "unit": "s",
            "thresholds": {
              "steps": [
                {"color": "green", "value": 0},
                {"color": "yellow", "value": 300},
                {"color": "red", "value": 600}
              ]
            }
          }
        }
      },
      {
        "title": "RPO (Recovery Point Objective)",
        "type": "stat",
        "targets": [
          {
            "expr": "neo4j:dr:rpo_seconds",
            "legendFormat": "Last RPO (seconds)"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "unit": "s",
            "thresholds": {
              "steps": [
                {"color": "green", "value": 0},
                {"color": "yellow", "value": 1800},
                {"color": "red", "value": 3600}
              ]
            }
          }
        }
      },
      {
        "title": "Backup Age",
        "type": "graph",
        "targets": [
          {
            "expr": "neo4j:dr:backup_age_seconds",
            "legendFormat": "Backup Age"
          }
        ],
        "yAxes": [
          {
            "unit": "s",
            "label": "Age (seconds)"
          }
        ]
      },
      {
        "title": "Cross-Region Replication Lag",
        "type": "graph",
        "targets": [
          {
            "expr": "neo4j:dr:replication_lag_seconds",
            "legendFormat": "Replication Lag"
          }
        ],
        "yAxes": [
          {
            "unit": "s",
            "label": "Lag (seconds)"
          }
        ]
      }
    ],
    "time": {
      "from": "now-6h",
      "to": "now"
    },
    "refresh": "30s"
  }
}
```

## 🛠️ Failover Procedures

### Manual Failover Process

```bash
#!/bin/bash
# manual-failover.sh - Manual DR failover procedure

echo "🚨 Starting Manual Disaster Recovery Failover"
echo "============================================="

# Configuration
PRIMARY_CLUSTER="neo4j-primary"
DR_CLUSTER="neo4j-dr"
NAMESPACE="neo4j"

# Step 1: Verify primary cluster is down
echo "🔍 Step 1: Verifying primary cluster status"
if kubectl get pods -l app.kubernetes.io/instance=${PRIMARY_CLUSTER} -n ${NAMESPACE} | grep -q Running; then
  echo "⚠️  WARNING: Primary cluster appears to be running!"
  echo "Are you sure you want to proceed with failover? (y/N)"
  read -r response
  if [[ ! "$response" =~ ^[Yy]$ ]]; then
    echo "Failover cancelled."
    exit 1
  fi
fi

# Step 2: Scale up DR cluster
echo "🚀 Step 2: Scaling up DR cluster"
kubectl patch neo4jenterprisecluster ${DR_CLUSTER} \
  --type='merge' \
  -p='{"spec":{"topology":{"primaries":3,"secondaries":2}}}' \
  -n ${NAMESPACE}

# Step 3: Wait for DR cluster to be ready
echo "⏳ Step 3: Waiting for DR cluster to be ready"
kubectl wait --for=condition=Ready \
  pod/${DR_CLUSTER}-primary-0 \
  -n ${NAMESPACE} \
  --timeout=600s

# Step 4: Update service endpoints
echo "🔄 Step 4: Updating service endpoints"
kubectl patch service neo4j-global \
  --type='merge' \
  -p='{"spec":{"selector":{"app.kubernetes.io/instance":"'${DR_CLUSTER}'"}}}' \
  -n ${NAMESPACE}

# Step 5: Verify DR cluster health
echo "✅ Step 5: Verifying DR cluster health"
kubectl exec -it ${DR_CLUSTER}-primary-0 -n ${NAMESPACE} -- \
  cypher-shell -u neo4j -p "$NEO4J_PASSWORD" \
  "CALL dbms.cluster.overview() YIELD role, addresses RETURN role, addresses"

echo "🎉 Manual failover completed successfully!"
echo "📊 DR cluster is now active and serving traffic"
```

### Failback Procedure

```bash
#!/bin/bash
# failback.sh - Failback to primary region

echo "🔄 Starting Failback to Primary Region"
echo "======================================"

# Configuration
PRIMARY_CLUSTER="neo4j-primary"
DR_CLUSTER="neo4j-dr"
NAMESPACE="neo4j"

# Step 1: Ensure primary region is healthy
echo "🔍 Step 1: Verifying primary region health"
kubectl get nodes -l topology.kubernetes.io/region=us-east-1

# Step 2: Create backup from DR cluster
echo "💾 Step 2: Creating backup from DR cluster"
kubectl create job failback-backup-$(date +%s) \
  --from=cronjob/neo4j-backup-cron \
  -n ${NAMESPACE}

# Wait for backup
echo "⏳ Waiting for backup to complete..."
sleep 120

# Step 3: Restore primary cluster from backup
echo "🔄 Step 3: Restoring primary cluster"
kubectl patch neo4jenterprisecluster ${PRIMARY_CLUSTER} \
  --type='merge' \
  -p='{"spec":{"restoreFrom":{"storage":{"type":"s3","bucket":"neo4j-multiregion-backups"}}}}' \
  -n ${NAMESPACE}

# Step 4: Scale up primary cluster
kubectl scale statefulset ${PRIMARY_CLUSTER}-primary --replicas=3 -n ${NAMESPACE}

# Step 5: Wait for primary cluster to be ready
echo "⏳ Step 5: Waiting for primary cluster to be ready"
kubectl wait --for=condition=Ready \
  pod/${PRIMARY_CLUSTER}-primary-0 \
  -n ${NAMESPACE} \
  --timeout=600s

# Step 6: Switch traffic back to primary
echo "🔄 Step 6: Switching traffic back to primary"
kubectl patch service neo4j-global \
  --type='merge' \
  -p='{"spec":{"selector":{"app.kubernetes.io/instance":"'${PRIMARY_CLUSTER}'"}}}' \
  -n ${NAMESPACE}

# Step 7: Scale down DR cluster
echo "📉 Step 7: Scaling down DR cluster"
kubectl patch neo4jenterprisecluster ${DR_CLUSTER} \
  --type='merge' \
  -p='{"spec":{"topology":{"primaries":1,"secondaries":1}}}' \
  -n ${NAMESPACE}

echo "🎉 Failback completed successfully!"
echo "📊 Primary cluster is now active and DR cluster is in standby mode"
```

## 🧪 DR Testing Scenarios

### Scenario 1: Zone Failure Test

```bash
#!/bin/bash
# zone-failure-test.sh

echo "🧪 Testing Zone Failure Resilience"

# Simulate zone failure by cordoning nodes
kubectl cordon $(kubectl get nodes -l topology.kubernetes.io/zone=us-east-1a -o name)

# Verify cluster remains healthy
kubectl get pods -l app.kubernetes.io/instance=neo4j-primary -o wide

# Test application connectivity
kubectl run test-client --rm -i --tty --image=neo4j:latest --restart=Never -- \
  cypher-shell -a "bolt://neo4j-primary-client:7687" -u neo4j -p "$NEO4J_PASSWORD" \
  "RETURN 'Zone failure test successful' as result"

# Cleanup
kubectl uncordon $(kubectl get nodes -l topology.kubernetes.io/zone=us-east-1a -o name)
```

### Scenario 2: Region Failure Test

```bash
#!/bin/bash
# region-failure-test.sh

echo "🧪 Testing Region Failure and DR Activation"

# Simulate region failure by scaling down primary cluster
kubectl scale statefulset neo4j-primary-primary --replicas=0

# Trigger DR activation
kubectl patch neo4jenterprisecluster neo4j-dr \
  --type='merge' \
  -p='{"spec":{"topology":{"primaries":3,"secondaries":2}}}'

# Wait and test
sleep 300
kubectl run test-client --rm -i --tty --image=neo4j:latest --restart=Never -- \
  cypher-shell -a "bolt://neo4j-dr-client:7687" -u neo4j -p "$NEO4J_PASSWORD" \
  "RETURN 'Region failure test successful' as result"
```

## 📋 DR Runbooks

### Runbook 1: Complete Primary Region Failure

**Scenario:** Primary region (us-east-1) is completely unavailable

**Detection:**

- Multiple alerts firing: PrimaryClusterDown, RegionUnavailable
- Application unable to connect to primary endpoints
- AWS/GCP console shows region-wide issues

**Response:**

1. **Immediate (0-5 minutes):**

   ```bash
   # Activate DR cluster
   kubectl patch neo4jenterprisecluster neo4j-dr \
     --type='merge' \
     -p='{"spec":{"topology":{"primaries":3,"secondaries":2}}}'
   ```

2. **Short-term (5-30 minutes):**

   ```bash
   # Update DNS/Load balancer
   kubectl patch service neo4j-global \
     --type='merge' \
     -p='{"spec":{"selector":{"app.kubernetes.io/instance":"neo4j-dr"}}}'

   # Notify stakeholders
   curl -X POST "$SLACK_WEBHOOK" -d '{"text":"🚨 DR activated for primary region failure"}'
   ```

3. **Recovery (when primary region returns):**

   ```bash
   # Follow failback procedure
   ./failback.sh
   ```

### Runbook 2: Data Corruption in Primary

**Scenario:** Data corruption detected in primary cluster

**Detection:**

- Data integrity checks failing
- Application reporting inconsistent data
- Database consistency alerts

**Response:**

1. **Immediate:**

   ```bash
   # Stop writes to primary cluster
   kubectl patch neo4jenterprisecluster neo4j-primary \
     --type='merge' \
     -p='{"spec":{"config":{"dbms.read_only":"true"}}}'
   ```

2. **Assessment:**

   ```bash
   # Check backup integrity
   kubectl exec neo4j-primary-primary-0 -- \
     neo4j-admin check-consistency --database=neo4j
   ```

3. **Recovery:**

   ```bash
   # Restore from known good backup
   kubectl patch neo4jenterprisecluster neo4j-primary \
     --type='merge' \
     -p='{"spec":{"restoreFrom":{"pointInTime":"2024-01-01T12:00:00Z"}}}'
   ```

## 📚 Best Practices

### 1. **Design for Failure**

- Assume any component can fail at any time
- Design for multiple failure scenarios simultaneously
- Test failure scenarios regularly

### 2. **Automate Everything**

- Automated health checks and monitoring
- Automated failover procedures
- Automated recovery and failback

### 3. **Monitor Continuously**

- Real-time health monitoring
- SLA tracking (RTO/RPO)
- Performance impact monitoring

### 4. **Test Regularly**

- Monthly DR drills
- Quarterly full disaster simulations
- Annual cross-cloud failover tests

### 5. **Document Procedures**

- Step-by-step runbooks
- Contact information and escalation paths
- Post-incident review procedures

## 📚 Related Documentation

- [Multi-Cluster Deployment Guide](./multi-cluster-deployment-guide.md)
- [Topology-Aware Placement](./topology-aware-placement.md)
- [Backup & Restore Guide](./backup-restore-guide.md)
- [Performance Guide](./performance-guide.md)

## 🤝 Support

For disaster recovery support:

- Review the troubleshooting procedures
- Test your DR setup with provided drill scripts
- Monitor RTO/RPO metrics continuously
- Contact support with specific DR scenarios and requirements
