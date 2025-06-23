# Testing Guide

This guide covers testing best practices and methodologies for the Neo4j Enterprise Operator for Kubernetes.

## Table of Contents

- [Testing Philosophy](#testing-philosophy)
- [Test Types](#test-types)
- [Running Tests](#running-tests)
- [Writing Tests](#writing-tests)
- [Best Practices](#best-practices)

## Testing Philosophy

Our testing approach follows the testing pyramid:

1. **Unit Tests (70%)** - Fast, isolated, focused on individual components
2. **Integration Tests (20%)** - Test component interactions
3. **End-to-End Tests (10%)** - Full system validation

### Test Principles

- **Fast Feedback** - Tests should run quickly and provide immediate feedback
- **Reliable** - Tests should be deterministic and not flaky
- **Maintainable** - Tests should be easy to understand and modify
- **Comprehensive** - Critical paths must be covered
- **Isolated** - Tests should not depend on external systems when possible

## Test Types

### 1. Unit Tests

Test individual functions, methods, and components in isolation.

**Location**: `*_test.go` files alongside source code
**Framework**: Go's built-in testing + testify assertions
**Speed**: < 1 second per test

```go
func TestReconcileNeo4jDatabase(t *testing.T) {
    tests := []struct {
        name     string
        database *v1alpha1.Neo4jDatabase
        want     ctrl.Result
        wantErr  bool
    }{
        {
            name: "successful reconciliation",
            database: &v1alpha1.Neo4jDatabase{
                ObjectMeta: metav1.ObjectMeta{
                    Name:      "test-db",
                    Namespace: "default",
                },
                Spec: v1alpha1.Neo4jDatabaseSpec{
                    // ... spec fields
                },
            },
            want:    ctrl.Result{},
            wantErr: false,
        },
        // ... more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ... test implementation
        })
    }
}
```

### 2. Integration Tests

Test controller interactions with the Kubernetes API and other components.

**Location**: `test/integration/`
**Framework**: Ginkgo + Gomega + envtest
**Environment**: Kubernetes control plane (etcd + API server)

```go
var _ = Describe("Neo4jDatabase Controller", func() {
    Context("When creating a Neo4jDatabase", func() {
        It("Should create the database successfully", func() {
            ctx := context.Background()

            database := &v1alpha1.Neo4jDatabase{
                ObjectMeta: metav1.ObjectMeta{
                    Name:      "test-database",
                    Namespace: "default",
                },
                Spec: v1alpha1.Neo4jDatabaseSpec{
                    // ... spec
                },
            }

            Expect(k8sClient.Create(ctx, database)).To(Succeed())

            Eventually(func() bool {
                err := k8sClient.Get(ctx, client.ObjectKey{
                    Name:      "test-database",
                    Namespace: "default",
                }, database)
                return err == nil && database.Status.Phase == "Ready"
            }, timeout, interval).Should(BeTrue())
        })
    })
})
```

### 3. End-to-End Tests

Test complete workflows in a real Kubernetes cluster.

**Location**: `test/e2e/`
**Framework**: Ginkgo + Gomega
**Environment**: Real Kubernetes cluster (kind, minikube, etc.)

## Running Tests

### Quick Test Commands

```bash
# Run all unit tests
make test

# Run unit tests with coverage
make coverage

# Run integration tests
make test-integration

# Run e2e tests
make test-e2e

# Run all tests
make test-all
```

### Comprehensive Test Suite

```bash
# Run complete test suite with reports
make test-all-comprehensive

# Quick comprehensive tests (subset)
make test-quick-comprehensive

# Generate HTML test report
make test-report
```

## Writing Tests

### Unit Test Guidelines

1. **Test Structure**: Use table-driven tests for multiple scenarios
2. **Isolation**: Mock external dependencies
3. **Assertions**: Use testify for clear assertions
4. **Setup/Teardown**: Use test fixtures for complex setup

```go
func TestControllerFunction(t *testing.T) {
    // Arrange
    ctrl := gomock.NewController(t)
    defer ctrl.Finish()

    mockClient := mocks.NewMockClient(ctrl)
    // ... setup mocks

    // Act
    result, err := functionUnderTest(mockClient, input)

    // Assert
    assert.NoError(t, err)
    assert.Equal(t, expected, result)
}
```

## Best Practices

### Test Organization

1. **Group Related Tests**: Use test suites and sub-tests
2. **Clear Test Names**: Names should describe what is being tested
3. **Independent Tests**: Tests should not depend on each other
4. **Consistent Setup**: Use common setup/teardown patterns

### Test Data Management

1. **Test Fixtures**: Use fixtures for complex test data
2. **Factory Functions**: Create factory functions for test objects
3. **Data Isolation**: Each test should use unique data
4. **Cleanup**: Always clean up test data

### Troubleshooting

#### Common Issues

1. **Flaky Tests**: Use retry logic and proper synchronization
2. **Resource Conflicts**: Ensure proper cleanup and unique naming
3. **Timeout Issues**: Adjust timeouts for slow environments

#### Debugging Tests

```bash
# Run tests with verbose output
go test -v ./...

# Run specific test
go test -run TestSpecificFunction

# Run tests with race detection
go test -race ./...
```

## Resources

- [Go Testing Documentation](https://golang.org/pkg/testing/)
- [Ginkgo Testing Framework](https://onsi.github.io/ginkgo/)
- [Gomega Matcher Library](https://onsi.github.io/gomega/)
- [Controller Runtime Testing](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest)

---

For more information, see the [Developer Guide](developer-guide.md) or run `make help-dev` for available test commands.
