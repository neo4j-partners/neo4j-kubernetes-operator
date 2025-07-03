# Setting up Envtest for Unit Tests

The unit tests for the Neo4j Kubernetes Operator require envtest binaries to be installed. Here's how to set them up:

## Quick Setup

1. Install the setup-envtest tool:
   ```bash
   make envtest
   ```

2. Download the required Kubernetes binaries:
   ```bash
   ./bin/setup-envtest use 1.31.0 --bin-dir ./bin -p path
   ```

3. Run the unit tests with the proper environment variable:
   ```bash
   KUBEBUILDER_ASSETS="$(pwd)/bin/k8s/1.31.0-darwin-arm64" make test-unit
   ```

## Alternative: Run Unit Tests Directly

To run only the unit tests (excluding integration tests that require a running cluster):

```bash
KUBEBUILDER_ASSETS="$(pwd)/bin/k8s/1.31.0-darwin-arm64" go test -timeout=10m -race -v \
  $(go list ./... | grep -v /test/webhooks | grep -v /test/integration | grep -v /test/e2e)
```

## Note on Test Types

- **Unit Tests**: Run with envtest, no cluster required
- **Integration Tests** (`test/integration/`): Require a Kubernetes cluster
- **Webhook TLS Tests** (`test/webhooks/`): Require a running webhook server
- **E2E Tests** (`test/e2e/`): Require full operator deployment

The error "fork/exec /usr/local/kubebuilder/bin/etcd: no such file or directory" occurs when envtest binaries are not installed. The setup steps above resolve this issue.
