# Testing Guide

This guide explains the comprehensive testing strategy for the Neo4j Enterprise Operator, covering unit tests, integration tests, and end-to-end testing practices.

## Testing Strategy Overview

The operator uses a multi-layered testing approach:

- **Unit Tests**: Fast tests for individual functions and components
- **Integration Tests**: Full workflow testing with Kubernetes API server
- **End-to-End Tests**: Real cluster testing with Kind clusters
- **Performance Tests**: Reconciliation efficiency and resource usage validation

## Test Infrastructure

### Testing Framework
- **Ginkgo/Gomega**: BDD-style testing framework for integration tests
- **Envtest**: Kubernetes API server for integration testing
- **Kind**: Kubernetes in Docker for real cluster testing
- **Go Testing**: Standard Go testing for unit tests

### Test Environments
- **Development**: `neo4j-operator-dev` Kind cluster
- **Testing**: `neo4j-operator-test` Kind cluster
- **CI/CD**: Automated testing in GitHub Actions

## Unit Tests

Unit tests are fast, require no Kubernetes cluster, and test individual functions and components.

### Running Unit Tests

```bash
# Run all unit tests (no cluster required)
make test-unit

# Run specific package tests
go test ./internal/controller -v
go test ./internal/validation -v
go test ./api/v1alpha1 -v

# Run specific test functions
go test ./internal/controller -run TestGetStatefulSetName -v
go test ./internal/validation -run TestTopologyValidator -v
```

### Unit Test Structure

Unit tests are located alongside the code they test:

```
internal/controller/
├── neo4jenterprisecluster_controller.go
├── neo4jenterprisecluster_controller_test.go
├── plugin_controller.go
├── plugin_controller_unit_test.go        # Unexported method tests
└── plugin_controller_test.go             # Integration-style tests
```

### Writing Unit Tests

```go
func TestGetStatefulSetName(t *testing.T) {
    r := &Neo4jPluginReconciler{}

    tests := []struct {
        name       string
        deployment *DeploymentInfo
        expected   string
    }{
        {
            name: "cluster deployment",
            deployment: &DeploymentInfo{
                Type: "cluster",
                Name: "my-cluster",
            },
            expected: "my-cluster-server",
        },
        // Add more test cases...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := r.getStatefulSetName(tt.deployment)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

## Integration Tests

Integration tests use envtest to provide a real Kubernetes API server without requiring a full cluster.

### Test Cluster Management

```bash
# Create test cluster with cert-manager
make test-cluster

# Clean operator resources (keep cluster running)
make test-cluster-clean

# Reset cluster (delete and recreate)
make test-cluster-reset

# Delete test cluster entirely
make test-cluster-delete

# Complete test environment cleanup
make test-destroy
```

### Running Integration Tests

```bash
# Full integration test suite
make test-integration

# Run tests for CI (assumes cluster exists)
make test-integration-ci

# Run specific test suites
ginkgo run -focus "Neo4jEnterpriseCluster" ./test/integration
ginkgo run -focus "should create backup" ./test/integration
ginkgo run -focus "Plugin Installation" ./test/integration
```

### Integration Test Structure

Integration tests are located in `test/integration/` and follow consistent patterns:

```go
var _ = Describe("Neo4jPlugin Integration Tests", func() {
    const (
        timeout  = time.Second * 300  // 5-minute timeout for CI
        interval = time.Second * 5
    )

    Context("Plugin Installation on Cluster", func() {
        It("Should install APOC plugin on Neo4jEnterpriseCluster", func() {
            ctx := context.Background()
            namespace := createUniqueNamespace()

            By("Creating namespace")
            Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

            By("Creating admin secret")
            // Create required secrets...

            By("Creating Neo4jEnterpriseCluster")
            cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
                ObjectMeta: metav1.ObjectMeta{
                    Name:      "plugin-test-cluster",
                    Namespace: namespace.Name,
                },
                Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
                    Image: neo4jv1alpha1.ImageSpec{
                        Repo: "neo4j",
                        Tag:  "5.26.0-enterprise",
                    },
                    Topology: neo4jv1alpha1.TopologyConfiguration{
                        Servers: 2,
                    },
                    // Resource constraints for CI compatibility
                    Resources: &corev1.ResourceRequirements{
                        Requests: corev1.ResourceList{
                            corev1.ResourceCPU:    resource.MustParse("100m"),
                            corev1.ResourceMemory: resource.MustParse("1.5Gi"),
                        },
                        Limits: corev1.ResourceList{
                            corev1.ResourceCPU:    resource.MustParse("500m"),
                            corev1.ResourceMemory: resource.MustParse("1.5Gi"),
                        },
                    },
                    Storage: neo4jv1alpha1.StorageSpec{
                        Size:      "1Gi",
                        ClassName: "standard",
                    },
                },
            }
            Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())

            By("Waiting for cluster to be ready")
            Eventually(func() string {
                currentCluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
                err := k8sClient.Get(ctx, types.NamespacedName{
                    Name:      "plugin-test-cluster",
                    Namespace: namespace.Name,
                }, currentCluster)
                if err != nil {
                    return ""
                }
                return currentCluster.Status.Phase
            }, timeout, interval).Should(Equal("Ready"))

            // Continue with plugin testing...
        })
    })
})
```

### Current Architecture Testing (August 2025)

#### Server-Based Architecture Tests

Tests verify the new server-based architecture:

```go
By("Verifying server StatefulSet exists with correct name")
serverSts := &appsv1.StatefulSet{}
Eventually(func() error {
    return k8sClient.Get(ctx, types.NamespacedName{
        Name:      clusterName + "-server",  // Server-based naming
        Namespace: namespace.Name,
    }, serverSts)
}, timeout, interval).Should(Succeed())
Expect(*serverSts.Spec.Replicas).To(Equal(int32(2)))
```

#### Centralized Backup Testing

Tests verify centralized backup architecture:

```go
By("Verifying centralized backup StatefulSet")
backupSts := &appsv1.StatefulSet{}
Eventually(func() error {
    return k8sClient.Get(ctx, types.NamespacedName{
        Name:      clusterName + "-backup",  // Centralized backup
        Namespace: namespace.Name,
    }, backupSts)
}, timeout, interval).Should(Succeed())
Expect(*backupSts.Spec.Replicas).To(Equal(int32(1)))  // Single backup pod
```

#### Dual Deployment Support Testing

Tests verify both cluster and standalone support:

```go
Context("Plugin Installation on Standalone", func() {
    It("Should install GDS plugin on Neo4jEnterpriseStandalone", func() {
        // Test standalone deployment with plugin installation
        standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{
            ObjectMeta: metav1.ObjectMeta{
                Name:      standaloneName,
                Namespace: namespace.Name,
            },
            Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
                Image: neo4jv1alpha1.ImageSpec{
                    Repo: "neo4j",
                    Tag:  "5.26.0-enterprise",
                },
                // Standalone-specific configuration...
            },
        }
        // Test plugin installation on standalone...
    })
})
```

### Test Configuration Guidelines

#### Resource Requirements for CI
All integration tests use minimal resources to avoid CI scheduling issues:

```yaml
resources:
  requests:
    cpu: "100m"           # Minimal CPU for CI compatibility
    memory: "1.5Gi"       # Required for Neo4j Enterprise database operations
  limits:
    cpu: "500m"           # Reasonable limit for testing
    memory: "1.5Gi"       # Neo4j Enterprise minimum for database operations
```

#### Storage Configuration
```yaml
storage:
  size: "1Gi"            # Minimal size for testing
  className: "standard"  # Default storage class in Kind
```

#### Timeout Configuration
```go
const (
    timeout  = time.Second * 300  // 5-minute timeout for CI environments
    interval = time.Second * 5    // Check every 5 seconds
)
```

## Resource Cleanup Patterns

### Critical Cleanup Requirements

Proper resource cleanup is **critical** to prevent namespace termination issues:

#### 1. Always Remove Finalizers Before Deletion

```go
By("Cleaning up")
if resource != nil {
    // Remove finalizers first
    if len(resource.GetFinalizers()) > 0 {
        resource.SetFinalizers([]string{})
        Expect(k8sClient.Update(ctx, resource)).Should(Succeed())
    }
    // Then delete the resource
    Expect(k8sClient.Delete(ctx, resource)).Should(Succeed())
}
```

#### 2. Handle All Resource Types

Clean up all resources that might have finalizers:

```go
// Neo4j resources
Expect(k8sClient.Delete(ctx, cluster)).Should(Succeed())
Expect(k8sClient.Delete(ctx, standalone)).Should(Succeed())
Expect(k8sClient.Delete(ctx, plugin)).Should(Succeed())
Expect(k8sClient.Delete(ctx, database)).Should(Succeed())
Expect(k8sClient.Delete(ctx, backup)).Should(Succeed())

// Kubernetes resources (usually auto-cleaned by owner references)
// PVCs, Services, StatefulSets are cleaned automatically
```

#### 3. Use Helper Functions

```go
// Helper function to create unique namespace
func createUniqueNamespace() *corev1.Namespace {
    return &corev1.Namespace{
        ObjectMeta: metav1.ObjectMeta{
            Name: fmt.Sprintf("test-%d", time.Now().UnixNano()),
        },
    }
}
```

### Test Suite Cleanup Helpers

The integration test suite provides cleanup utilities:

```go
// Clean up all custom resources in namespace
cleanupCustomResourcesInNamespace(namespace)

// Force remove finalizers if needed
forceRemoveFinalizers(resource)
```

## Testing Best Practices

### Resource Naming Patterns

Test resources should use predictable naming:

```go
// Cluster naming
clusterName := "test-cluster-" + unique-suffix

// Expected StatefulSet names (server-based architecture)
expectedServerSts := clusterName + "-server"
expectedBackupSts := clusterName + "-backup"

// Standalone naming
standaloneName := "test-standalone-" + unique-suffix
expectedStandaloneSts := standaloneName  // No suffix for standalone
```

### Memory Requirements

**Critical for Neo4j Enterprise**: Tests must allocate sufficient memory:

```go
Resources: &corev1.ResourceRequirements{
    Requests: corev1.ResourceList{
        corev1.ResourceMemory: resource.MustParse("1.5Gi"),  // MINIMUM for Enterprise
    },
    Limits: corev1.ResourceList{
        corev1.ResourceMemory: resource.MustParse("1.5Gi"),  // Prevent OOMKill
    },
},
```

**Why 1.5Gi is required**:
- Neo4j Enterprise needs minimum memory for database operations
- Lower values cause Out of Memory kills (exit code 137)
- Database creation and topology operations fail with insufficient memory

### Waiting Patterns

#### Cluster Readiness (Condition-Based)

```go
Eventually(func() string {
    cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{}
    err := k8sClient.Get(ctx, clusterKey, cluster)
    if err != nil {
        return ""
    }
    return cluster.Status.Phase
}, timeout, interval).Should(Equal("Ready"))
```

#### Standalone Readiness (Boolean-Based)

```go
Eventually(func() bool {
    standalone := &neo4jv1alpha1.Neo4jEnterpriseStandalone{}
    err := k8sClient.Get(ctx, standaloneKey, standalone)
    if err != nil {
        return false
    }
    return standalone.Status.Ready
}, timeout, interval).Should(BeTrue())
```

#### Neo4j Cluster Formation Verification

```go
By("Verifying Neo4j cluster formation")
Eventually(func() error {
    // Connect to first server and check cluster status
    return exec.Command("kubectl", "exec",
        clusterName+"-server-0", "--",
        "cypher-shell", "-u", "neo4j", "-p", password,
        "SHOW SERVERS").Run()
}, timeout, interval).Should(Succeed())
```

## Performance Testing

### Reconciliation Efficiency Tests

```go
It("Should maintain efficient reconciliation rates", func() {
    // Monitor reconciliation frequency
    // Verify <100 reconciliations per minute under normal conditions
})
```

### Resource Usage Tests

```go
It("Should use optimal resource patterns", func() {
    // Verify centralized backup uses <30% resources of sidecar approach
    // Check server-based StatefulSet efficiency
})
```

## CI/CD Testing

### GitHub Actions Integration

Tests run automatically in CI with:
- **Parallel Execution**: Multiple test suites run concurrently
- **Resource Constraints**: CI-optimized resource limits
- **Timeout Handling**: Extended timeouts for image pull delays
- **Cleanup Automation**: Automatic test environment cleanup

### CI-Specific Configuration

```bash
# Environment variables for CI
export CI=true
export KUBEBUILDER_ASSETS="$(pwd)/bin/k8s/1.31.0-linux-amd64"
export KUBECONFIG=~/.kube/config
```

## Troubleshooting Test Failures

### Common Test Issues

#### 1. Namespace Stuck in Terminating

**Symptoms**: Test namespaces remain in "Terminating" state indefinitely

**Diagnosis**:
```bash
# Check for resources with finalizers
kubectl get all,neo4jenterpriseclusters,neo4jenterprisestandalones,neo4jplugins -n <namespace> -o yaml | grep -A5 finalizers

# Check for PVCs
kubectl get pvc -n <namespace>
```

**Solutions**:
```bash
# Force cleanup test resources
make test-cluster-clean

# Reset test cluster entirely
make test-cluster-reset

# Manual finalizer removal (if needed)
kubectl patch neo4jenterprisecluster <name> -n <namespace> \
  -p '{"metadata":{"finalizers":[]}}' --type=merge
```

#### 2. Out of Memory (OOMKilled) Failures

**Symptoms**: Pods exit with code 137, "OOMKilled" in pod status

**Diagnosis**:
```bash
# Check pod status
kubectl describe pod <pod-name> | grep -E "(OOMKilled|Memory|Exit.*137)"

# Monitor memory usage
kubectl top pod <pod-name> --containers
```

**Solutions**:
- Increase memory limits to minimum 1.5Gi for Neo4j Enterprise
- Reduce concurrent test execution
- Use minimal storage and CPU allocations

#### 3. Test Timeouts

**Symptoms**: Tests fail with "Timed out after 300s"

**Common Causes**:
- Image pull delays in CI environments
- Insufficient resources for cluster formation
- Missing RBAC permissions

**Solutions**:
```bash
# Check operator status
kubectl get pods -n neo4j-operator

# Check operator logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager

# Verify cert-manager (required for TLS tests)
kubectl get pods -n cert-manager

# Check cluster formation
kubectl get events --sort-by='.firstTimestamp'
```

#### 4. Ginkgo Test Suite Conflicts

**Symptoms**: "Ginkgo does not support rerunning suites" error

**Cause**: Multiple `RunSpecs()` calls in same package

**Solution**: Ensure only one test suite per package:
```go
// Correct: One RunSpecs per package
func TestControllers(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "Controller Suite")
}

// Include all tests in the same suite via Describe blocks
```

## Test Coverage and Quality

### Coverage Targets

```bash
# Generate coverage report
make test-coverage

# View coverage in browser
go tool cover -html=coverage.out
```

### Coverage Goals
- **Unit Tests**: >80% coverage for controller logic
- **Integration Tests**: All major workflows covered
- **E2E Tests**: Critical user journeys verified

### Quality Checks

Integration tests should verify:
- **Resource Creation**: All expected Kubernetes resources created
- **Status Updates**: Proper status conditions and phase transitions
- **Error Handling**: Graceful handling of failure scenarios
- **Resource Cleanup**: Proper finalizer handling and cleanup
- **Performance**: Efficient reconciliation and resource usage

## Writing New Tests

### Adding Unit Tests

1. **Create test file** alongside source code
2. **Follow naming conventions**: `*_test.go` for integration, `*_unit_test.go` for unit tests
3. **Test unexported methods** from within package
4. **Use table-driven tests** for multiple scenarios

### Adding Integration Tests

1. **Add to `test/integration/`** directory
2. **Use Ginkgo BDD style** for readability
3. **Include proper cleanup** with finalizer removal
4. **Set appropriate timeouts** (5 minutes for CI)
5. **Use minimal resources** for CI compatibility
6. **Test both success and failure scenarios**

### Test Documentation

Document test scenarios:
- **Purpose**: What functionality is being tested
- **Setup**: Required resources and configuration
- **Expected Results**: What should happen in success case
- **Cleanup**: How resources are cleaned up
- **CI Considerations**: Any special requirements for CI

This comprehensive testing strategy ensures the Neo4j Enterprise Operator works reliably across different environments and deployment scenarios.
