# Testing

This guide explains how to run the test suite for the Neo4j Enterprise Operator. The project has a comprehensive testing strategy with unit and integration tests.

## Unit Tests

Unit tests are located alongside the code they test and do not require a Kubernetes cluster. To run the unit tests, use the following command:

```bash
make test-unit
```

## Integration Tests

Integration tests require a Kubernetes cluster with the operator deployed. The tests use Ginkgo/Gomega for BDD-style testing.

### Prerequisites

- Kind cluster with cert-manager installed
- Neo4j operator deployed to the cluster
- Proper RBAC permissions (automatically handled by the operator)

### Running Integration Tests

```bash
# Create a test cluster with cert-manager
make test-cluster

# Deploy the operator to the test cluster
kubectl config use-context kind-neo4j-operator-test
make deploy IMG=neo4j-operator:dev

# Run integration tests
make test-integration
```

### Test Cluster Management

The project provides convenient targets for managing test clusters:

```bash
# Create a test cluster
make test-cluster

# Clean up test cluster resources (keeps cluster running)
make test-cluster-clean

# Reset test cluster (delete and recreate)
make test-cluster-reset

# Delete the test cluster entirely
make test-cluster-delete
```

## Writing Integration Tests

### Test Structure

Integration tests follow a consistent pattern using Ginkgo's BDD syntax:

```go
var _ = Describe("Feature", func() {
    Context("When condition", func() {
        var (
            testNamespace string
            resource      *neo4jv1alpha1.Neo4jEnterpriseCluster
        )

        BeforeEach(func() {
            // Setup test namespace
            testNamespace = createTestNamespace("feature")
            // Create test resources
        })

        AfterEach(func() {
            // IMPORTANT: Clean up resources with finalizer removal
            if resource != nil {
                if len(resource.GetFinalizers()) > 0 {
                    resource.SetFinalizers([]string{})
                    _ = k8sClient.Update(ctx, resource)
                }
                _ = k8sClient.Delete(ctx, resource)
            }
        })

        It("Should do something", func() {
            // Test implementation
        })
    })
})
```

### Resource Cleanup Best Practices

Proper resource cleanup is critical to prevent namespace termination issues:

1. **Always remove finalizers before deletion**:
   ```go
   if len(resource.GetFinalizers()) > 0 {
       resource.SetFinalizers([]string{})
       _ = k8sClient.Update(ctx, resource)
   }
   ```

2. **Use the test suite's cleanup helpers**:
   ```go
   // The integration suite provides cleanup functions
   cleanupCustomResourcesInNamespace(namespace)
   ```

3. **Handle all resource types in cleanup**:
   - Neo4jEnterpriseCluster
   - Neo4jEnterpriseStandalone
   - Neo4jBackup/Restore
   - Secrets, ConfigMaps, PVCs

### Common Test Patterns

#### Waiting for Resources

```go
// Wait for cluster to be ready
Eventually(func() bool {
    err := k8sClient.Get(ctx, clusterKey, &cluster)
    if err != nil {
        return false
    }
    for _, condition := range cluster.Status.Conditions {
        if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
            return true
        }
    }
    return false
}, timeout, interval).Should(BeTrue())
```

#### Checking Standalone Readiness

```go
// Note: Standalone resources use Status.Ready, not conditions
Eventually(func() bool {
    var standalone neo4jv1alpha1.Neo4jEnterpriseStandalone
    if err := k8sClient.Get(ctx, standaloneKey, &standalone); err != nil {
        return false
    }
    return standalone.Status.Ready
}, timeout, interval).Should(BeTrue())
```

## All Tests

To run the complete test suite:

```bash
make test
```

## Test Coverage

To generate test coverage reports:

```bash
make test-coverage
```

## Troubleshooting Test Failures

### Stuck Namespaces

If test namespaces get stuck in "Terminating" state:

1. Check for resources with finalizers:
   ```bash
   kubectl get all,neo4jenterpriseclusters,neo4jenterprisestandalones -n <namespace> -o yaml | grep -A5 finalizers
   ```

2. Force cleanup if needed:
   ```bash
   make test-cluster-clean
   ```

### Test Timeouts

If tests are timing out:

1. Check if the operator is running:
   ```bash
   kubectl get pods -n neo4j-operator-system
   ```

2. Check operator logs:
   ```bash
   kubectl logs -n neo4j-operator-system deployment/neo4j-operator-controller-manager
   ```

3. Ensure cert-manager is installed (required for TLS tests):
   ```bash
   kubectl get pods -n cert-manager
   ```
