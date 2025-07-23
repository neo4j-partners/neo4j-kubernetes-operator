# Troubleshooting Guide

This guide provides comprehensive troubleshooting information for the Neo4j Kubernetes Operator, covering both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone` deployments.

## Quick Reference

### Diagnostic Commands

```bash
# Check deployment status
kubectl get neo4jenterprisecluster
kubectl get neo4jenterprisestandalone

# View detailed information
kubectl describe neo4jenterprisecluster <cluster-name>
kubectl describe neo4jenterprisestandalone <standalone-name>

# Check pod status
kubectl get pods -l app.kubernetes.io/name=neo4j
kubectl logs -l app.kubernetes.io/name=neo4j

# Check events
kubectl get events --sort-by=.metadata.creationTimestamp

# Check operator logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager
```

### Common Port Forwarding Commands

```bash
# For clusters
kubectl port-forward svc/<cluster-name>-client 7474:7474 7687:7687

# For standalone deployments
kubectl port-forward svc/<standalone-name>-service 7474:7474 7687:7687
```

## Common Issues and Solutions

### 1. Split-Brain Scenarios

#### Problem: Cluster nodes form multiple independent clusters
This is most common with TLS-enabled clusters where nodes fail to join during initial formation.

**Quick Check**:
```bash
# Check each node's view of the cluster
for i in 0 1 2; do
  kubectl exec <cluster>-primary-$i -- cypher-shell -u neo4j -p <password> "SHOW SERVERS" | wc -l
done
```

**Solution**: See the comprehensive [Split-Brain Recovery Guide](../troubleshooting/split-brain-recovery.md) or use the [Quick Reference](../quick-reference/split-brain-recovery-quick-ref.md).

**Quick Fix**:
```bash
# Restart minority cluster nodes
kubectl delete pod <cluster>-primary-1 <cluster>-secondary-1
```

### 2. Test Environment Issues

#### Problem: Integration tests failing with namespace termination issues
Test namespaces get stuck in "Terminating" state due to resources with finalizers.

**Solution**: Ensure proper cleanup in test code:
```go
// Always remove finalizers before deletion
if len(resource.GetFinalizers()) > 0 {
    resource.SetFinalizers([]string{})
    _ = k8sClient.Update(ctx, resource)
}
_ = k8sClient.Delete(ctx, resource)
```

#### Problem: Backup sidecar test timeout
Test waits for wrong readiness field on standalone deployments.

**Solution**: Check the correct status field:
```go
// For standalone deployments
return standalone.Status.Ready  // NOT Status.Conditions

// Correct pod label selector
client.MatchingLabels{"app": standalone.Name}
```

#### Problem: Operator not deployed in test cluster
Integration tests fail because operator is not running.

**Solution**: Deploy operator before running tests:
```bash
kubectl config use-context kind-neo4j-operator-test
make deploy IMG=neo4j-operator:dev
make test-integration
```

### 3. Deployment Validation Errors

#### Problem: Single-Node Cluster Not Allowed
```
Error: Neo4jEnterpriseCluster requires minimum cluster topology: either 1 primary + 1 secondary, or multiple primaries. For single-node deployments, use Neo4jEnterpriseStandalone instead
```

**Solution**: Use the correct CRD for your deployment type:

**For development/testing** (single-node):
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseStandalone
metadata:
  name: dev-neo4j
spec:
  image:
    repo: neo4j
    tag: "5.26-enterprise"
  storage:
    className: standard
    size: "10Gi"
```

**For production** (minimum cluster):
```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: prod-cluster
spec:
  topology:
    primaries: 1
    secondaries: 1  # Minimum required
  image:
    repo: neo4j
    tag: "5.26-enterprise"
  storage:
    className: standard
    size: "10Gi"
```

#### Problem: Invalid Neo4j Version
```
Error: Neo4j version 5.25.0 is not supported. Minimum required version is 5.26.0
```

**Solution**: Update to a supported version:
```yaml
spec:
  image:
    tag: "5.26-enterprise"  # or later
```

**Supported versions:**
- **Semver**: 5.26.0, 5.26.1, 5.27.0, 6.0.0+
- **Calver**: 2025.01.0, 2025.06.1, 2026.01.0+

### 2. Pod Startup Issues

#### Problem: Pods Stuck in Pending State
```bash
# Check pod events
kubectl describe pod <pod-name>

# Common causes:
# - Insufficient resources
# - Storage issues
# - Image pull issues
```

**Solutions:**

1. **Check Resource Availability:**
   ```bash
   kubectl describe nodes
   kubectl get pv
   ```

2. **Verify Storage Class:**
   ```bash
   kubectl get storageclass
   kubectl describe storageclass <storage-class-name>
   ```

3. **Check Image Pull:**
   ```bash
   kubectl describe pod <pod-name> | grep -A 5 "Events:"
   ```

#### Problem: Pods Crashing (CrashLoopBackOff)
```bash
# Check pod logs
kubectl logs <pod-name> --previous
```

**Common causes and solutions:**

1. **Memory Issues:**
   ```yaml
   spec:
     resources:
       requests:
         memory: "2Gi"
       limits:
         memory: "4Gi"
   ```

2. **Configuration Issues:**
   ```bash
   # Check ConfigMap
   kubectl get configmap <cluster-name>-config -o yaml
   ```

3. **License Issues:**
   ```bash
   # Check license secret
   kubectl get secret <license-secret> -o yaml
   ```

### 3. Connectivity Issues

#### Problem: Cannot Connect to Neo4j
```bash
# Test connectivity
kubectl port-forward svc/<service-name> 7474:7474 7687:7687
curl http://localhost:7474

# Check service
kubectl get svc -l app.kubernetes.io/name=neo4j
kubectl describe svc <service-name>
```

**Solutions:**

1. **Check Service Configuration:**
   ```yaml
   # For clusters
   service: <cluster-name>-client

   # For standalone
   service: <standalone-name>-service
   ```

2. **Verify Network Policies:**
   ```bash
   kubectl get networkpolicies
   kubectl describe networkpolicy <policy-name>
   ```

3. **Check TLS Configuration:**
   ```bash
   # For TLS-enabled deployments
   kubectl get certificates
   kubectl describe certificate <cert-name>
   ```

### 4. Cluster-Specific Issues

#### Problem: Cluster Formation Fails
```bash
# Check cluster status
kubectl get neo4jenterprisecluster <cluster-name> -o yaml

# Check individual pod logs
kubectl logs <cluster-name>-0
kubectl logs <cluster-name>-1
```

**Solutions:**

1. **🔧 CRITICAL FIX: V2_ONLY Discovery Configuration**

   **Issue**: Neo4j 5.26+ and 2025.x use V2_ONLY discovery mode which disables the discovery port (6000) and only uses the cluster port (5000).

   **Verification**: Check that the operator is using the correct configuration:
   ```bash
   # Check ConfigMap for correct discovery configuration
   kubectl get configmap <cluster-name>-config -o yaml | grep -A 5 -B 5 "tcp-discovery"

   # Should show (Neo4j 5.26+):
   # dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery
   # dbms.cluster.discovery.version=V2_ONLY

   # Should show (Neo4j 2025.x):
   # dbms.kubernetes.discovery.service_port_name=tcp-discovery
   # (V2_ONLY is default, not explicitly set)
   ```

   **Fix**: Ensure operator version includes the V2_ONLY discovery fix. If using older version, upgrade to latest.

2. **Verify Cluster Topology:**
   ```bash
   # Ensure minimum topology requirements
   kubectl get neo4jenterprisecluster <cluster-name> -o jsonpath='{.spec.topology}'
   ```

3. **Check Inter-Pod Communication:**
   ```bash
   # Test DNS resolution to headless service
   kubectl exec -it <pod-name> -- nslookup <cluster-name>-headless

   # Test cluster port connectivity (5000)
   kubectl exec -it <pod-name> -- timeout 2 bash -c "</dev/tcp/localhost/5000"
   ```

4. **Verify Discovery Labels:**
   ```bash
   # Check that only headless service has clustering label
   kubectl get svc -l neo4j.com/cluster=<cluster-name> -o yaml | grep -A 3 -B 3 "neo4j.com/clustering"
   ```

#### Problem: Scaling Issues
```bash
# Check scaling validation
kubectl get events | grep -i scale
```

**Solutions:**

1. **Verify Minimum Topology:**
   ```yaml
   # Scaling cannot violate minimum requirements
   spec:
     topology:
       primaries: 1
       secondaries: 1  # Cannot scale below this
   ```

2. **Check Resource Limits:**
   ```yaml
   spec:
     resources:
       requests:
         cpu: "500m"
         memory: "2Gi"
   ```

### 5. Standalone-Specific Issues

#### Problem: Standalone Pod Won't Start
```bash
# Check standalone status
kubectl get neo4jenterprisestandalone <standalone-name> -o yaml

# Check pod events
kubectl describe pod <standalone-name>-0
```

**Solutions:**

1. **Check Standalone Configuration:**
   ```yaml
   # Uses unified clustering infrastructure (Neo4j 5.26+)
   # No manual configuration needed for single-node operation
   ```

2. **Verify Storage Configuration:**
   ```yaml
   spec:
     storage:
       className: standard
       size: "10Gi"
   ```

#### Problem: Migration from Cluster to Standalone
```bash
# Create backup first
kubectl apply -f backup.yaml

# Deploy standalone
kubectl apply -f standalone.yaml

# Restore data
kubectl apply -f restore.yaml
```

### 6. Performance Issues

#### Problem: Slow Query Performance
```bash
# Check resource usage
kubectl top pods
kubectl top nodes

# Check Neo4j metrics
kubectl port-forward svc/<service-name> 7474:7474
# Access http://localhost:7474/metrics
```

**Solutions:**

1. **Adjust Memory Settings:**
   ```yaml
   spec:
     config:
       server.memory.heap.initial_size: "2G"
       server.memory.heap.max_size: "4G"
       server.memory.pagecache.size: "2G"
   ```

2. **Enable Query Logging:**
   ```yaml
   spec:
     config:
       dbms.logs.query.enabled: "true"
       dbms.logs.query.threshold: "1s"
   ```

3. **Check Storage Performance:**
   ```bash
   # Test storage I/O
   kubectl exec -it <pod-name> -- dd if=/dev/zero of=/data/test bs=1M count=1000
   ```

### 7. Storage Issues

#### Problem: PVC Issues
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
       className: fast-ssd  # Ensure this exists
       size: "50Gi"
   ```

2. **Check Node Storage:**
   ```bash
   kubectl describe nodes
   df -h  # On nodes
   ```

#### Problem: Data Corruption
```bash
# Check Neo4j consistency
kubectl exec -it <pod-name> -- neo4j-admin check-consistency
```

**Solutions:**

1. **Run Consistency Check:**
   ```bash
   kubectl exec -it <pod-name> -- neo4j-admin check-consistency --database=neo4j
   ```

2. **Restore from Backup:**
   ```bash
   kubectl apply -f restore-from-backup.yaml
   ```

### 8. Backup and Restore Issues

#### Problem: Backup failing with permission denied
Backup jobs fail with "permission denied" or "cannot exec into pod" errors.

**Solution**: The operator now automatically creates RBAC resources. If you're upgrading:
```bash
# Ensure operator has latest permissions
kubectl apply -f https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/neo4j-kubernetes-operator.yaml

# Check operator has pods/exec and pods/log permissions
kubectl describe clusterrole neo4j-operator-manager-role | grep -E "pods/exec|pods/log"
```

**Note**: Starting with the latest version, the operator automatically creates:
- Service accounts for backup jobs
- Roles with `pods/exec` and `pods/log` permissions
- Role bindings for secure backup execution

#### Problem: Backup path not found
Neo4j 5.26+ requires backup destination path to exist.

**Solution**: The operator's backup sidecar automatically creates paths. Check sidecar is running:
```bash
# Check backup sidecar is present
kubectl get pod <neo4j-pod> -o yaml | grep backup-sidecar

# Check sidecar logs
kubectl logs <neo4j-pod> -c backup-sidecar
```

### 9. Security Issues

#### Problem: Authentication Failures
```bash
# Check auth secret
kubectl get secret <auth-secret> -o yaml

# Check Neo4j auth logs
kubectl logs <pod-name> | grep -i auth
```

**Solutions:**

1. **Verify Admin Secret:**
   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: neo4j-admin-secret
   data:
     username: bmVvNGo=  # base64 encoded
     password: cGFzc3dvcmQ=  # base64 encoded
   ```

2. **Check Password Policy:**
   ```yaml
   spec:
     auth:
       passwordPolicy:
         minLength: 8
         requireUppercase: true
   ```

#### Problem: TLS Certificate Issues
```bash
# Check certificate status
kubectl get certificates
kubectl describe certificate <cert-name>

# Check cert-manager logs
kubectl logs -n cert-manager deployment/cert-manager
```

**Solutions:**

1. **Verify Issuer:**
   ```yaml
   spec:
     tls:
       mode: cert-manager
       issuerRef:
         name: ca-cluster-issuer
         kind: ClusterIssuer
   ```

2. **Check Certificate Details:**
   ```bash
   kubectl get secret <tls-secret> -o yaml
   ```

3. **TLS Cluster Formation Issues:**

   TLS-enabled clusters are prone to split-brain during initial formation. If you see partial cluster formation:

   ```bash
   # Check for split clusters
   kubectl exec <cluster>-primary-0 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
   kubectl exec <cluster>-primary-1 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"
   ```

   **Prevention:**
   ```yaml
   spec:
     config:
       # Increase discovery timeouts for TLS clusters
       dbms.cluster.discovery.v2.initial_timeout: "10s"
       dbms.cluster.discovery.v2.retry_timeout: "20s"
       # Note: Do NOT override dbms.cluster.raft.membership.join_timeout
       # The operator sets it to 10m which is optimal
   ```

   See [Split-Brain Recovery Guide](../troubleshooting/split-brain-recovery.md) for detailed recovery procedures.

## Advanced Troubleshooting

### Debug Mode

Enable debug logging in the operator:
```bash
kubectl patch deployment neo4j-operator-controller-manager \
  -n neo4j-operator \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--zap-log-level=debug"]}]}}}}'
```

### Resource Monitoring

Monitor resource usage:
```bash
# Watch resource usage
watch kubectl top pods
watch kubectl top nodes

# Check resource limits
kubectl describe limitrange
kubectl describe resourcequota
```

### Network Debugging

Test network connectivity:
```bash
# DNS resolution
kubectl exec -it <pod-name> -- nslookup <service-name>

# Port connectivity
kubectl exec -it <pod-name> -- telnet <service-name> 7687

# Network policies
kubectl get networkpolicies --all-namespaces
```

## Collecting Diagnostic Information

Use this script to collect comprehensive diagnostic information:

```bash
#!/bin/bash
# neo4j-debug.sh - Collect diagnostic information

echo "=== Neo4j Kubernetes Operator Diagnostic Report ==="
echo "Generated: $(date)"
echo

echo "=== Cluster Resources ==="
kubectl get neo4jenterprisecluster
echo

echo "=== Standalone Resources ==="
kubectl get neo4jenterprisestandalone
echo

echo "=== Pods ==="
kubectl get pods -l app.kubernetes.io/name=neo4j
echo

echo "=== Services ==="
kubectl get svc -l app.kubernetes.io/name=neo4j
echo

echo "=== PVCs ==="
kubectl get pvc -l app.kubernetes.io/name=neo4j
echo

echo "=== ConfigMaps ==="
kubectl get configmap -l app.kubernetes.io/name=neo4j
echo

echo "=== Secrets ==="
kubectl get secret -l app.kubernetes.io/name=neo4j
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

echo "=== Node Resources ==="
kubectl describe nodes | grep -A 5 "Allocated resources:"
```

## Getting Help

### Support Resources

- **Documentation**: [User Guide](../getting_started.md)
- **API Reference**: [Neo4jEnterpriseCluster](../../api_reference/neo4jenterprisecluster.md), [Neo4jEnterpriseStandalone](../../api_reference/neo4jenterprisestandalone.md)
- **Migration Guide**: [Migration Guide](../migration_guide.md)
- **Community**: [Neo4j Community Forum](https://community.neo4j.com/)
- **Issues**: [GitHub Issues](https://github.com/neo4j-labs/neo4j-kubernetes-operator/issues)

### When to Contact Support

Contact support when:
- Data corruption is suspected
- Cluster formation consistently fails
- Performance is significantly degraded
- Security incidents occur
- Migration issues cannot be resolved

Always provide the diagnostic report and specific error messages when contacting support.
