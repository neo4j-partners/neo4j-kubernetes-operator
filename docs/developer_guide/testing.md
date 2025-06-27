# Testing

This guide explains how to run the test suite for the Neo4j Enterprise Operator.

## Unit Tests

To run the unit tests, use the following command:

```bash
make test-unit
```

## Integration Tests

To run the integration tests, you first need to create a test environment:

```bash
make test-env-setup
```

Then, you can run the integration tests:

```bash
make test-integration
```

## E2E Tests

To run the end-to-end tests, you first need to create a test environment:

```bash
make test-env-setup
```

Then, you can run the E2E tests:

```bash
make test-e2e
```
