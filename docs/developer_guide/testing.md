# Testing

This guide explains how to run the test suite for the Neo4j Enterprise Operator. The project has a comprehensive testing strategy with unit, integration, and end-to-end (E2E) tests.

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

## E2E Tests

End-to-end tests run against a real `kind` cluster and test the full lifecycle of the operator and its resources. To run the E2E tests, you first need to create a test environment:

```bash
make test-env-setup
```

Then, you can run the E2E tests:

```bash
make test-e2e
```

## Test Runner Script

The `scripts/run-tests.sh` script provides a unified way to run all types of tests with various options. You can use this script to run specific test suites, enable coverage, and more.
