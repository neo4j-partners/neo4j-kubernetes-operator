# Testing

This guide explains how to run the test suite for the Neo4j Enterprise Operator. The project has a comprehensive testing strategy with unit and integration tests.

## Unit Tests

Unit tests are located alongside the code they test and do not require a Kubernetes cluster. To run the unit tests, use the following command:

```bash
make test-unit
```

## Integration Tests

Integration tests use the `envtest` library to test the controllers against a real Kubernetes API server without needing a full cluster. To run the integration tests, use the following command:

```bash
make test-integration
```

## Test Clusters

For integration tests, you'll need a Kubernetes cluster. The project provides convenient targets for managing test clusters:

```bash
# Create a test cluster
make test-cluster

# Run integration tests
make test-integration

# Clean up test cluster resources
make test-cluster-clean

# Delete the test cluster entirely
make test-cluster-delete
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
