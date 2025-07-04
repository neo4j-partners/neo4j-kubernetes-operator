# Backup and Restore Troubleshooting Guide

This guide provides comprehensive troubleshooting information for Neo4j backup and restore operations using the Kubernetes operator. It covers common issues, diagnostic steps, and solutions for various backup and restore scenarios.

## Prerequisites

Before troubleshooting, ensure you have:
- Neo4j Enterprise cluster running version **5.26.0+** (semver) or **2025.01.0+** (calver)
- Appropriate RBAC permissions for backup/restore operations
- Access to cluster logs and events
- Understanding of your storage backend configuration

## Quick Diagnostic Commands

### General Status Check
```bash
# Check backup resource status
kubectl get neo4jbackups
kubectl get neo4jrestores

# View detailed resource information
kubectl describe neo4jbackup <backup-name>
kubectl describe neo4jrestore <restore-name>

# Check events
kubectl get events --sort-by=.metadata.creationTimestamp

# View operator logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager
```

### Job and Pod Status
```bash
# List backup/restore jobs
kubectl get jobs -l app.kubernetes.io/component=backup
kubectl get jobs -l app.kubernetes.io/component=restore

# Check job logs
kubectl logs job/<backup-name>-backup
kubectl logs job/<restore-name>-restore

# Check pod status and logs
kubectl get pods -l app.kubernetes.io/component=backup
kubectl logs <backup-pod-name>
```

## Common Issues and Solutions

### 1. Version Compatibility Issues

#### Problem: Neo4j Version Not Supported
```
Error: Neo4j version 5.25.0 is not supported. Minimum required version is 5.26.0
```

**Diagnosis:**
```bash
# Check cluster image version
kubectl get neo4jenterprisecluster <cluster-name> -o jsonpath='{.spec.image.tag}'

# Check backup/restore resource events
kubectl describe neo4jbackup <backup-name>
```

**Solutions:**
1. **Update Neo4j Version:**
   ```yaml
   spec:
     image:
       tag: "5.26.0-enterprise"  # or later version
   ```

2. **Verify Supported Versions:**
   - **Semver**: 5.26.0, 5.26.1, 5.27.0, 6.0.0+
   - **Calver**: 2025.01.0, 2025.06.1, 2026.01.0+

#### Problem: Invalid Version Format
```
Error: invalid Neo4j version format: latest. Expected semver (5.26+) or calver (2025.01+)
```

**Solution:**
Use specific version tags instead of `latest`:
```yaml
spec:
  image:
    tag: "5.26.0-enterprise"
```

### 2. Storage Backend Issues

#### Problem: S3 Access Denied
```
Error: AccessDenied: Access Denied
```

**Diagnosis:**
```bash
# Check AWS credentials
kubectl get secret aws-credentials -o yaml

# Verify IAM permissions
aws sts get-caller-identity
aws s3 ls s3://your-backup-bucket/
```

**Solutions:**
1. **Verify IAM Permissions:**
   ```json
   {
     "Version": "2012-10-17",
     "Statement": [
       {
         "Effect": "Allow",
         "Action": [
           "s3:GetObject",
           "s3:PutObject",
           "s3:DeleteObject",
           "s3:ListBucket"
         ],
         "Resource": [
           "arn:aws:s3:::your-backup-bucket",
           "arn:aws:s3:::your-backup-bucket/*"
         ]
       }
     ]
   }
   ```

2. **Update Service Account Annotations:**
   ```yaml
   metadata:
     annotations:
       eks.amazonaws.com/role-arn: arn:aws:iam::ACCOUNT:role/neo4j-backup-role
   ```

3. **Check Secret Format:**
   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: aws-credentials
   data:
     AWS_ACCESS_KEY_ID: <base64-key>
     AWS_SECRET_ACCESS_KEY: <base64-secret>
   ```

#### Problem: GCS Permission Denied
```
Error: 403 Forbidden: Permission denied
```

**Solutions:**
1. **Verify Service Account Key:**
   ```bash
   # Check service account secret
   kubectl get secret gcs-credentials -o yaml

   # Test GCS access
   gsutil ls gs://your-backup-bucket/
   ```

2. **Required GCS Permissions:**
   - `storage.objects.create`
   - `storage.objects.delete`
   - `storage.objects.get`
   - `storage.objects.list`
   - `storage.buckets.get`

#### Problem: Azure Storage Authentication Failed
```
Error: AuthenticationFailed: Server failed to authenticate the request
```

**Solutions:**
1. **Check Storage Account Key:**
   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: azure-credentials
   data:
     AZURE_STORAGE_ACCOUNT: <base64-account-name>
     AZURE_STORAGE_KEY: <base64-storage-key>
   ```

2. **Verify Container Permissions:**
   ```bash
   # Test Azure CLI access
   az storage blob list --container-name your-container --account-name your-account
   ```

#### Problem: PVC Storage Issues
```
Error: pod has unbound immediate PersistentVolumeClaims
```

**Diagnosis:**
```bash
# Check PVC status
kubectl get pvc
kubectl describe pvc <pvc-name>

# Check storage class
kubectl get storageclass
```

**Solutions:**
1. **Verify Storage Class:**
   ```yaml
   spec:
     storage:
       type: pvc
       pvc:
         name: backup-storage
         size: 100Gi
         storageClass: fast-ssd  # Ensure this exists
   ```

2. **Check Available Storage:**
   ```bash
   # List nodes and storage
   kubectl describe nodes
   kubectl get pv
   ```

### 3. Backup Operation Issues

#### Problem: Backup Job Fails to Start
```
Status: Failed
Message: Failed to create backup job: pods "backup-job-xyz" is forbidden
```

**Diagnosis:**
```bash
# Check RBAC permissions
kubectl auth can-i create jobs --as=system:serviceaccount:neo4j:neo4j-operator

# Check service account
kubectl get serviceaccount neo4j-operator -o yaml
```

**Solutions:**
1. **Verify RBAC:**
   ```yaml
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRole
   metadata:
     name: neo4j-backup-role
   rules:
   - apiGroups: ["batch"]
     resources: ["jobs", "cronjobs"]
     verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
   ```

2. **Check Service Account Binding:**
   ```yaml
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRoleBinding
   metadata:
     name: neo4j-backup-binding
   subjects:
   - kind: ServiceAccount
     name: neo4j-operator
     namespace: neo4j
   roleRef:
     kind: ClusterRole
     name: neo4j-backup-role
     apiGroup: rbac.authorization.k8s.io
   ```

#### Problem: Backup Times Out
```
Status: Failed
Message: Backup job timed out after 2h0m0s
```

**Solutions:**
1. **Increase Timeout:**
   ```yaml
   spec:
     timeout: "4h"  # Increase timeout for large databases
   ```

2. **Check Resource Limits:**
   ```yaml
   spec:
     options:
       additionalArgs:
         - "--parallel-recovery"
         - "--temp-path=/tmp/backup"
   ```

3. **Monitor Disk I/O:**
   ```bash
   # Check node resources
   kubectl top nodes
   kubectl top pods
   ```

#### Problem: Backup Verification Fails
```
Status: Failed
Message: Backup verification failed: inconsistent data detected
```

**Solutions:**
1. **Check Database Consistency:**
   ```cypher
   // Connect to Neo4j and run
   CALL dbms.checkConsistency()
   ```

2. **Disable Verification Temporarily:**
   ```yaml
   spec:
     options:
       verify: false  # Disable for problematic databases
   ```

3. **Use Force Flag:**
   ```yaml
   spec:
     force: true
   ```

### 4. Restore Operation Issues

#### Problem: Target Cluster Not Ready
```
Status: Waiting
Message: Target cluster is not ready
```

**Diagnosis:**
```bash
# Check cluster status
kubectl get neo4jenterprisecluster <cluster-name>
kubectl describe neo4jenterprisecluster <cluster-name>

# Check pod status
kubectl get pods -l app.kubernetes.io/instance=<cluster-name>
```

**Solutions:**
1. **Wait for Cluster Readiness:**
   ```bash
   # Monitor cluster status
   kubectl get neo4jenterprisecluster <cluster-name> -w
   ```

2. **Check Cluster Configuration:**
   ```yaml
   # Ensure cluster has proper resources
   spec:
     resources:
       requests:
         memory: "2Gi"
         cpu: "1"
   ```

#### Problem: Database Already Exists
```
Status: Failed
Message: database myapp already exists. Use replaceExisting option or force flag
```

**Solutions:**
1. **Use Replace Existing:**
   ```yaml
   spec:
     options:
       replaceExisting: true
   ```

2. **Use Force Flag:**
   ```yaml
   spec:
     force: true
   ```

3. **Drop Database First:**
   ```cypher
   // Connect to Neo4j and run
   DROP DATABASE myapp IF EXISTS
   ```

#### Problem: PITR Transaction Log Issues
```
Status: Failed
Message: transaction log validation failed: missing log segment
```

**Diagnosis:**
```bash
# Check transaction log storage
kubectl describe neo4jrestore <restore-name>

# Verify log storage accessibility
aws s3 ls s3://transaction-logs/production/logs/
```

**Solutions:**
1. **Check Log Retention:**
   ```yaml
   spec:
     source:
       pitr:
         logRetention: "14d"  # Increase retention period
   ```

2. **Disable Log Validation:**
   ```yaml
   spec:
     source:
       pitr:
         validateLogIntegrity: false
   ```

3. **Use Different Recovery Point:**
   ```yaml
   spec:
     source:
       pointInTime: "2025-01-04T10:00:00Z"  # Earlier time
   ```

### 5. Networking and Connectivity Issues

#### Problem: Cannot Connect to Neo4j During Restore
```
Status: Failed
Message: failed to create Neo4j client: connection refused
```

**Diagnosis:**
```bash
# Check Neo4j service
kubectl get svc -l app.kubernetes.io/instance=<cluster-name>

# Test connectivity
kubectl port-forward svc/<cluster-name>-client 7687:7687
neo4j-client -u neo4j -p password bolt://localhost:7687
```

**Solutions:**
1. **Check Service Configuration:**
   ```yaml
   # Ensure service is properly exposed
   spec:
     services:
       neo4j:
         enabled: true
         type: ClusterIP
   ```

2. **Verify Network Policies:**
   ```bash
   kubectl get networkpolicies
   kubectl describe networkpolicy <policy-name>
   ```

3. **Check Firewall Rules:**
   ```bash
   # Ensure port 7687 is accessible
   telnet <cluster-ip> 7687
   ```

### 6. Resource and Performance Issues

#### Problem: Out of Memory During Backup
```
Status: Failed
Message: backup job killed due to memory limit
```

**Solutions:**
1. **Increase Job Resources:**
   ```yaml
   # Add to backup job template (requires operator modification)
   resources:
     requests:
       memory: "4Gi"
       cpu: "2"
     limits:
       memory: "8Gi"
       cpu: "4"
   ```

2. **Use Incremental Backup:**
   ```yaml
   spec:
     options:
       additionalArgs:
         - "--incremental"
   ```

3. **Optimize Backup Path:**
   ```yaml
   spec:
     options:
       additionalArgs:
         - "--temp-path=/tmp/backup"
         - "--parallel-recovery"
   ```

#### Problem: Slow Backup Performance
```
Status: Running (for extended time)
```

**Solutions:**
1. **Enable Compression:**
   ```yaml
   spec:
     options:
       compress: true
   ```

2. **Use Parallel Processing:**
   ```yaml
   spec:
     options:
       additionalArgs:
         - "--parallel-recovery"
   ```

3. **Check Storage Performance:**
   ```bash
   # Test storage I/O
   kubectl exec -it <backup-pod> -- dd if=/dev/zero of=/backup/test bs=1M count=1000
   ```

### 7. Hook Execution Issues

#### Problem: Pre-restore Hook Fails
```
Status: Failed
Message: Pre-restore hooks failed: hook job failed
```

**Diagnosis:**
```bash
# Check hook job status
kubectl get jobs -l app.kubernetes.io/component=pre-restore

# Check hook job logs
kubectl logs job/<restore-name>-pre-restore-hook
```

**Solutions:**
1. **Increase Hook Timeout:**
   ```yaml
   spec:
     options:
       preRestore:
         job:
           timeout: "30m"  # Increase timeout
   ```

2. **Fix Hook Script:**
   ```yaml
   spec:
     options:
       preRestore:
         job:
           template:
             container:
               command: ["/bin/sh"]
               args: ["-c", "set -e; /scripts/pre-restore.sh"]  # Add error handling
   ```

#### Problem: Cypher Hook Execution Fails
```
Status: Failed
Message: failed to execute Cypher statement: syntax error
```

**Solutions:**
1. **Validate Cypher Syntax:**
   ```yaml
   spec:
     options:
       postRestore:
         cypherStatements:
           - "CALL db.awaitIndexes(600)"  # Add timeout
           - "MATCH (n:User) WHERE n.created IS NULL SET n.created = datetime()"
   ```

2. **Check Database State:**
   ```cypher
   // Verify database is accessible
   CALL db.ping()
   ```

## Advanced Troubleshooting

### Debug Mode

Enable debug logging in the operator:
```bash
# Restart operator with debug logging
kubectl patch deployment neo4j-operator-controller-manager \
  -n neo4j-operator \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--zap-log-level=debug"]}]}}}}'
```

### Resource Monitoring

Monitor resource usage during operations:
```bash
# Watch resource usage
watch kubectl top pods
watch kubectl top nodes

# Monitor storage usage
kubectl exec -it <backup-pod> -- df -h
```

### Network Debugging

Test network connectivity:
```bash
# DNS resolution
kubectl exec -it <backup-pod> -- nslookup <cluster-name>-client

# Port connectivity
kubectl exec -it <backup-pod> -- telnet <cluster-name>-client 7687

# Network policies
kubectl get networkpolicies --all-namespaces
```

## Prevention and Best Practices

### Monitoring Setup

1. **Set up Alerts:**
   ```yaml
   # Prometheus alert for backup failures
   - alert: BackupFailed
     expr: increase(neo4j_backup_failures_total[1h]) > 0
     for: 5m
     annotations:
       summary: "Neo4j backup failed"
   ```

2. **Regular Health Checks:**
   ```bash
   # Weekly backup validation
   kubectl get neo4jbackups -o json | jq '.items[] | select(.status.phase != "Completed")'
   ```

### Capacity Planning

1. **Storage Monitoring:**
   ```bash
   # Monitor backup storage growth
   kubectl get pvc -o jsonpath='{.items[*].status.capacity.storage}'
   ```

2. **Performance Baselines:**
   ```bash
   # Establish backup performance baselines
   kubectl get neo4jbackup -o jsonpath='{.items[*].status.stats.duration}'
   ```

### Regular Testing

1. **Backup Validation:**
   ```bash
   # Monthly restore tests
   kubectl apply -f test-restore.yaml
   ```

2. **Disaster Recovery Drills:**
   ```bash
   # Quarterly DR tests
   kubectl apply -f disaster-recovery-test.yaml
   ```

## Getting Help

### Collecting Diagnostic Information

```bash
#!/bin/bash
# backup-restore-debug.sh - Collect diagnostic information

echo "=== Neo4j Backup/Restore Diagnostic Report ==="
echo "Generated: $(date)"
echo

echo "=== Cluster Information ==="
kubectl get neo4jenterpriseclusters
echo

echo "=== Backup Resources ==="
kubectl get neo4jbackups
echo

echo "=== Restore Resources ==="
kubectl get neo4jrestores
echo

echo "=== Recent Events ==="
kubectl get events --sort-by=.metadata.creationTimestamp | tail -20
echo

echo "=== Operator Logs (last 100 lines) ==="
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager --tail=100
echo

echo "=== Storage Classes ==="
kubectl get storageclass
echo

echo "=== PVCs ==="
kubectl get pvc
```

### Support Resources

- **Documentation**: [Backup and Restore Guide](backup_restore.md)
- **API Reference**: [Neo4jBackup](../../api_reference/neo4jbackup.md), [Neo4jRestore](../../api_reference/neo4jrestore.md)
- **Community**: Neo4j Community Forum
- **Enterprise Support**: Neo4j Support Portal

### When to Contact Support

Contact support when:
- Data corruption is suspected
- Backup/restore operations consistently fail
- Performance is significantly degraded
- Security incidents occur
- Complex PITR scenarios need assistance

Provide the diagnostic report and specific error messages when contacting support.
