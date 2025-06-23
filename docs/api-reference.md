# API Reference

This document provides comprehensive API reference for the Neo4j Enterprise Operator Custom Resources.

## Table of Contents

- [Overview](#overview)
- [Common Types](#common-types)
- [Neo4jEnterpriseCluster](#neo4jenterprisecluster)
- [Neo4jDatabase](#neo4jdatabase)
- [Neo4jBackup](#neo4jbackup)
- [Neo4jRestore](#neo4jrestore)
- [Neo4jUser](#neo4juser)
- [Neo4jRole](#neo4jrole)
- [Neo4jGrant](#neo4jgrant)
- [Status Conditions](#status-conditions)
- [Examples](#examples)

## Overview

The Neo4j Enterprise Operator extends the Kubernetes API with custom resources for managing Neo4j database clusters and related resources. All resources follow Kubernetes conventions and implement the standard `spec-status` pattern.

### API Groups

- **Group**: `neo4j.neo4j.com`
- **Version**: `v1alpha1`
- **Kind Prefix**: `Neo4j`

### Resource Categories

1. **Cluster Management**: Neo4jEnterpriseCluster, Neo4jDatabase
2. **Data Protection**: Neo4jBackup, Neo4jRestore
3. **Access Control**: Neo4jUser, Neo4jRole, Neo4jGrant

## Common Types

### ObjectMeta

All resources inherit standard Kubernetes metadata:

```yaml
metadata:
  name: string          # Resource name
  namespace: string     # Kubernetes namespace
  labels: {}           # Key-value labels
  annotations: {}      # Key-value annotations
  finalizers: []       # Finalizer strings
```

### ResourceRequirements

Standard Kubernetes resource requirements:

```yaml
resources:
  requests:
    memory: "2Gi"
    cpu: "1"
  limits:
    memory: "4Gi"
    cpu: "2"
```

### CommonStatus

All resources implement common status fields:

```yaml
status:
  conditions: []        # Status conditions array
  phase: string        # Current phase
  observedGeneration: int64  # Last observed generation
```

## Neo4jEnterpriseCluster

Manages Neo4j Enterprise clusters with causal clustering support.

### API Version

- **Group**: `neo4j.neo4j.com`
- **Version**: `v1alpha1`
- **Kind**: `Neo4jEnterpriseCluster`

### Specification

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: cluster-name
  namespace: neo4j-system
spec:
  # Core cluster configuration
  coreServers: 3                    # Number of core servers (required)
  readReplicas: 2                   # Number of read replicas (optional)

  # Neo4j configuration
  image:
    repository: "neo4j"             # Container image repository
    tag: "5.26-enterprise"          # Image tag
    pullPolicy: "IfNotPresent"      # Image pull policy
    pullSecrets: []                 # Image pull secrets

  # Resource allocation
  resources:
    requests:
      memory: "2Gi"
      cpu: "1"
    limits:
      memory: "4Gi"
      cpu: "2"

  # Storage configuration
  storage:
    dataVolumeSize: "10Gi"          # Data volume size
    logsVolumeSize: "1Gi"           # Logs volume size
    storageClassName: "fast-ssd"     # Storage class
    accessModes: ["ReadWriteOnce"]   # Volume access modes

  # Network configuration
  networking:
    discovery:
      advertised_address: "CLUSTER_IP"  # Discovery address type
    ports:
      http: 7474                    # HTTP port
      https: 7473                   # HTTPS port
      bolt: 7687                    # Bolt port
      cluster: 5000                 # Cluster communication port
      raft: 7000                    # Raft port
      transaction: 6000             # Transaction port

  # Security configuration
  security:
    authEnabled: true               # Enable authentication
    tlsEnabled: true                # Enable TLS
    certificateSecretName: "neo4j-certs"  # TLS certificate secret
    adminPasswordSecretName: "neo4j-admin"  # Admin password secret

  # Backup configuration
  backup:
    enabled: true                   # Enable backups
    schedule: "0 2 * * *"          # Backup schedule (cron)
    retentionPolicy: "7d"          # Backup retention
    storageBackend:
      type: "s3"                   # Backend type (s3, gcs, azure)
      secretName: "backup-credentials"  # Backend credentials
      config:
        bucket: "neo4j-backups"
        region: "us-west-2"

  # Monitoring configuration
  monitoring:
    enabled: true                   # Enable monitoring
    metricsPort: 2004              # Metrics port
    prometheusEnabled: true        # Prometheus integration
```

### Status

```yaml
status:
  # Overall cluster status
  phase: "Ready|Pending|Failed"
  conditions:
  - type: "Ready"
    status: "True"
    reason: "ClusterReady"
    message: "All cluster nodes are ready"

  # Cluster topology
  coreMembers:
  - name: "cluster-core-0"
    role: "LEADER|FOLLOWER"
    address: "10.0.0.1:5000"
    status: "ONLINE|OFFLINE"
  - name: "cluster-core-1"
    role: "FOLLOWER"
    address: "10.0.0.2:5000"
    status: "ONLINE"

  readReplicas:
  - name: "cluster-replica-0"
    address: "10.0.0.3:5000"
    status: "ONLINE"

  # Service endpoints
  endpoints:
    bolt: "neo4j://cluster.neo4j-system.svc.cluster.local:7687"
    http: "http://cluster.neo4j-system.svc.cluster.local:7474"
    https: "https://cluster.neo4j-system.svc.cluster.local:7473"

  # Resource status
  observedGeneration: 1
  currentReplicas: 5
  readyReplicas: 5
```

## Neo4jDatabase

Manages individual databases within Neo4j clusters.

### API Version

- **Group**: `neo4j.neo4j.com`
- **Version**: `v1alpha1`
- **Kind**: `Neo4jDatabase`

### Specification

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: database-name
  namespace: neo4j-system
spec:
  # Target cluster
  clusterRef:
    name: "neo4j-cluster"           # Neo4jEnterpriseCluster name
    namespace: "neo4j-system"       # Optional, defaults to current namespace

  # Database configuration
  name: "analytics"                 # Database name in Neo4j
  type: "standard"                  # Database type

  # Initial data
  initialData:
    cypherScript: |
      CREATE (n:Person {name: 'Alice'});
      CREATE (m:Person {name: 'Bob'});
      CREATE (n)-[:KNOWS]->(m);

  # Access control
  defaultAccess: "read"             # Default access level

  # Configuration options
  options:
    txLogRetention: "7 days"
    dbms.memory.heap.initial_size: "512m"
    dbms.memory.heap.max_size: "1g"
```

### Status

```yaml
status:
  phase: "Ready|Creating|Failed"
  conditions:
  - type: "Ready"
    status: "True"
    reason: "DatabaseReady"
    message: "Database is ready for use"

  # Database information
  name: "analytics"
  status: "online"
  role: "primary"

  # Access information
  address: "neo4j://cluster.neo4j-system.svc.cluster.local:7687/analytics"

  observedGeneration: 1
```

## Neo4jBackup

Manages Neo4j database backups.

### API Version

- **Group**: `neo4j.neo4j.com`
- **Version**: `v1alpha1`
- **Kind**: `Neo4jBackup`

### Specification

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: backup-name
  namespace: neo4j-system
spec:
  # Target database
  database:
    clusterRef:
      name: "neo4j-cluster"
      namespace: "neo4j-system"
    name: "analytics"               # Database name, optional for full backup

  # Backup configuration
  schedule: "0 2 * * *"            # Cron schedule
  type: "full"                     # full|incremental

  # Storage configuration
  storage:
    type: "s3"                     # s3|gcs|azure|pvc
    secretName: "backup-credentials"
    config:
      bucket: "neo4j-backups"
      path: "/cluster-backups"
      region: "us-west-2"

  # Retention policy
  retention:
    keepLast: 7                    # Keep last N backups
    keepDaily: 30                  # Keep daily backups for N days
    keepWeekly: 12                 # Keep weekly backups for N weeks
    keepMonthly: 6                 # Keep monthly backups for N months

  # Backup options
  options:
    compression: true              # Enable compression
    encryption: true               # Enable encryption
    consistencyCheck: true         # Run consistency check
```

### Status

```yaml
status:
  phase: "Ready|Running|Failed"
  conditions:
  - type: "Ready"
    status: "True"
    reason: "BackupReady"
    message: "Backup configuration is ready"

  # Last backup information
  lastBackup:
    startTime: "2024-01-15T02:00:00Z"
    completionTime: "2024-01-15T02:15:00Z"
    size: "1.2GB"
    location: "s3://neo4j-backups/cluster-backups/2024-01-15_02-00-00.backup"
    status: "Completed"

  # Next scheduled backup
  nextBackup: "2024-01-16T02:00:00Z"

  observedGeneration: 1
```

## Neo4jRestore

Manages Neo4j database restore operations.

### API Version

- **Group**: `neo4j.neo4j.com`
- **Version**: `v1alpha1`
- **Kind**: `Neo4jRestore`

### Specification

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jRestore
metadata:
  name: restore-name
  namespace: neo4j-system
spec:
  # Target database
  database:
    clusterRef:
      name: "neo4j-cluster"
      namespace: "neo4j-system"
    name: "analytics"               # Target database name

  # Source backup
  source:
    backupRef:
      name: "daily-backup"          # Reference to Neo4jBackup
    # OR direct backup location
    location: "s3://neo4j-backups/cluster-backups/2024-01-15_02-00-00.backup"

  # Restore options
  options:
    replaceExisting: true           # Replace existing database
    force: false                    # Force restore even if database exists
    consistencyCheck: true          # Run consistency check after restore
```

### Status

```yaml
status:
  phase: "Pending|Running|Completed|Failed"
  conditions:
  - type: "Completed"
    status: "True"
    reason: "RestoreCompleted"
    message: "Database restored successfully"

  # Restore progress
  startTime: "2024-01-15T10:00:00Z"
  completionTime: "2024-01-15T10:30:00Z"

  # Restore information
  sourceBackup: "s3://neo4j-backups/cluster-backups/2024-01-15_02-00-00.backup"
  targetDatabase: "analytics"

  observedGeneration: 1
```

## Status Conditions

All resources implement standard Kubernetes condition types:

### Common Condition Types

| Type | Description |
|------|-------------|
| `Ready` | Resource is ready for use |
| `Progressing` | Operation is in progress |
| `Degraded` | Resource is running but with reduced functionality |
| `Available` | Resource is available for requests |

### Condition Status Values

| Status | Description |
|--------|-------------|
| `True` | Condition is satisfied |
| `False` | Condition is not satisfied |
| `Unknown` | Condition status cannot be determined |

### Example Condition

```yaml
conditions:
- type: "Ready"
  status: "True"
  lastTransitionTime: "2024-01-15T10:00:00Z"
  reason: "ClusterReady"
  message: "All cluster nodes are healthy and ready"
```

## Examples

### Basic Cluster

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: basic-cluster
  namespace: default
spec:
  image:
    repository: "neo4j"
    tag: "5.26-enterprise"
  coreServers: 3
  readReplicas: 1
  storage:
    dataVolumeSize: "10Gi"
    storageClassName: "standard"
  security:
    authEnabled: true
    adminPasswordSecretName: "neo4j-admin"
```

### Production Cluster with Backups

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: production-cluster
  namespace: neo4j-production
spec:
  image:
    repository: "neo4j"
    tag: "5.26-enterprise"
  coreServers: 5
  readReplicas: 3

  resources:
    requests:
      memory: "4Gi"
      cpu: "2"
    limits:
      memory: "8Gi"
      cpu: "4"

  storage:
    dataVolumeSize: "100Gi"
    logsVolumeSize: "10Gi"
    storageClassName: "fast-ssd"

  security:
    authEnabled: true
    tlsEnabled: true
    certificateSecretName: "neo4j-tls"
    adminPasswordSecretName: "neo4j-admin"

  backup:
    enabled: true
    schedule: "0 2 * * *"
    retentionPolicy: "30d"
    storageBackend:
      type: "s3"
      secretName: "backup-credentials"
      config:
        bucket: "neo4j-prod-backups"
        region: "us-west-2"

  monitoring:
    enabled: true
    prometheusEnabled: true
```

### Database with Initial Data

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: analytics-db
  namespace: neo4j-production
spec:
  clusterRef:
    name: "production-cluster"
  name: "analytics"
  type: "standard"

  initialData:
    cypherScript: |
      // Create indexes
      CREATE INDEX person_name IF NOT EXISTS FOR (p:Person) ON (p.name);
      CREATE INDEX company_name IF NOT EXISTS FOR (c:Company) ON (c.name);

      // Create initial data
      CREATE (alice:Person {name: 'Alice', role: 'Data Scientist'});
      CREATE (bob:Person {name: 'Bob', role: 'Engineer'});
      CREATE (neo4j:Company {name: 'Neo4j', industry: 'Graph Technology'});

      // Create relationships
      CREATE (alice)-[:WORKS_FOR]->(neo4j);
      CREATE (bob)-[:WORKS_FOR]->(neo4j);
      CREATE (alice)-[:COLLABORATES_WITH]->(bob);

  options:
    txLogRetention: "7 days"
    dbms.memory.heap.initial_size: "1g"
    dbms.memory.heap.max_size: "2g"
```

### Automated Backup Configuration

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jBackup
metadata:
  name: daily-full-backup
  namespace: neo4j-production
spec:
  database:
    clusterRef:
      name: "production-cluster"
    # Omit name for full cluster backup

  schedule: "0 2 * * *"  # Daily at 2 AM
  type: "full"

  storage:
    type: "s3"
    secretName: "backup-credentials"
    config:
      bucket: "neo4j-prod-backups"
      path: "/daily-full"
      region: "us-west-2"

  retention:
    keepLast: 7
    keepDaily: 30
    keepWeekly: 12
    keepMonthly: 6

  options:
    compression: true
    encryption: true
    consistencyCheck: true
```
