# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the Neo4j Enterprise Operator for Kubernetes, which manages Neo4j Enterprise clusters (v5.26+) in Kubernetes environments. Built with Kubebuilder framework.

## Architecture

**Key Components:**
- **CRDs**: Neo4jEnterpriseCluster, Neo4jBackup/Restore, Neo4jDatabase, Neo4jUser/Role/Grant, Neo4jPlugin
- **Controllers**: Enterprise cluster controller with autoscaling and security coordinator
- **Webhooks**: Validation webhooks integrated with cert-manager
- **CLI Plugin**: kubectl-neo4j for cluster management

**Directory Structure:**
- `api/v1alpha1/` - CRD type definitions
- `internal/controller/` - Reconciliation logic
- `internal/resources/` - K8s resource builders
- `internal/neo4j/` - Neo4j client implementation
- `test/` - Unit, integration, and e2e tests

## Essential Commands

### Build & Development
```bash
make build                 # Build operator binary
make docker-build         # Build container image
make manifests            # Generate CRDs and RBAC
make generate             # Generate DeepCopy methods
make dev-run              # Run operator locally (outside cluster)
make dev-cluster          # Create Kind development cluster
make operator-setup       # Deploy operator with webhooks to cluster
```

### Testing
```bash
# Quick tests (no cluster required)
make test-unit            # Unit tests only
make test-webhooks        # Webhook validation tests with envtest

# Webhook-specific tests
make test-webhooks-tls           # Test webhook TLS in dev cluster
make test-webhooks-integration   # Full webhook integration tests
make test-webhook-cert-rotation  # Test certificate rotation

# Integration tests (requires cluster)
make test-integration     # Full integration suite
make test-with-operator   # Tests with automatic operator setup

# Full test suite
make test                 # All tests with coverage report

# Run specific test
go test ./internal/controller -run TestClusterReconciler
ginkgo run -focus "should create backup" ./test/integration
ginkgo run -focus "webhook TLS" ./test/webhooks
```

### Code Quality
```bash
make fmt                  # Format code with gofmt
make lint                 # Run golangci-lint (strict mode)
make lint-lenient        # Run with relaxed rules for CI
make vet                  # Run go vet
make security            # Run gosec security scan
```

### Debugging & Troubleshooting
```bash
# View operator logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager

# Check webhook certificates
kubectl get certificate -n neo4j-operator

# Validate CRDs
kubectl explain neo4jenterprisecluster.spec

# Test webhook locally
make webhook-test
```

## Testing Strategy

The project uses Ginkgo/Gomega for BDD-style testing:

1. **Unit Tests** (`test/unit/`) - Test individual components without K8s
2. **Webhook Tests** (`test/webhooks/`) - Validate admission webhooks
3. **Integration Tests** (`test/integration/`) - Test with real K8s cluster
4. **E2E Tests** (`test/e2e/`) - Full deployment scenarios

Always run `make test-unit` before committing. Integration tests require a cluster with cert-manager.

## Important Development Notes

1. **Webhook Development**: Webhooks require cert-manager. Use `make operator-setup` for local testing.
2. **Controller Testing**: Use envtest for controller tests without real cluster.
3. **Neo4j Client**: The operator communicates with Neo4j via Bolt protocol (internal/neo4j/client.go).
4. **Security**: All Neo4j auth is handled by SecurityCoordinator - never bypass it.

## Common Development Tasks

### Adding a New Controller
1. Create types in `api/v1alpha1/`
2. Run `make generate manifests`
3. Implement controller in `internal/controller/`
4. Add tests in `test/unit/controllers/`
5. Update RBAC in `config/rbac/role.yaml`

### Testing Webhooks Locally

#### Quick Webhook Testing
```bash
# Test webhooks without cluster (unit tests with envtest)
make test-webhooks

# Test webhook TLS configuration in development cluster
make test-webhooks-tls

# Full webhook integration tests with TLS
make test-webhooks-integration

# Test certificate rotation
make test-webhook-cert-rotation
```

#### Development Webhook Setup
```bash
# Create development cluster with enhanced self-signed certificates
make dev-cluster
make deploy KUSTOMIZE_DIR=config/dev  # Uses development config with CA hierarchy

# Test webhook functionality
./hack/test-webhooks.sh

# Apply sample resources to test validation
kubectl apply -f config/samples/neo4j_v1alpha1_neo4jenterprisecluster.yaml --dry-run=server
kubectl apply -f test/fixtures/invalid-cluster.yaml --dry-run=server  # Should fail
```

#### Webhook TLS Strategy
- **Development**: Use self-signed certificates with proper CA hierarchy (`config/dev/`)
- **Testing**: Automated cert-manager setup with Kind clusters
- **Production**: Can use LetsEncrypt for Neo4j clusters, self-signed for webhooks
- **CI/CD**: Comprehensive webhook testing in GitHub Actions

### Debugging Failed Reconciliation
```bash
# Check controller logs
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager -f

# Check events
kubectl describe neo4jenterprisecluster <name>

# Enable debug logging
make dev-run ARGS="--zap-log-level=debug"
```

## CI/CD Workflow

GitHub Actions runs:
1. Fast feedback tests (unit, lint, security)
2. Integration tests with webhooks
3. E2E tests with full cluster deployment
4. Multi-arch container builds

PRs must pass all checks. Use conventional commits (feat:, fix:, docs:).


## Reports

All reports that Claude generates should go into the reports directory. The reports can be reviewed by Claude to determine changes that were made.
