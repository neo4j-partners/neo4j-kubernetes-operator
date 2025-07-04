# GitHub Workflows

This directory contains GitHub Actions workflows for the Neo4j Kubernetes Operator.

## Workflows

### 🔄 ci.yml - Main CI Pipeline
**Triggers:** Push to main/develop, Pull Requests
**Purpose:** Complete CI pipeline with unit tests and conditional integration tests

**Jobs:**
1. **Unit Tests** - Fast unit tests (no cluster required)
2. **Integration Tests** - Full integration tests (requires `integration-tests` label on PR)

**Features:**
- Fast feedback with unit tests first
- Integration tests only run on main branch pushes or labeled PRs
- Coverage reporting to Codecov
- Artifact collection for debugging

### 🧪 unit-tests.yml - Standalone Unit Tests
**Triggers:** Push to main/develop, Pull Requests
**Purpose:** Fast unit test execution for quick feedback

**Features:**
- Go 1.22 support
- Module caching for faster builds
- Coverage reporting
- Artifact collection

### 🔗 integration-tests.yml - Standalone Integration Tests
**Triggers:** Push to main, Pull Requests to main, Manual dispatch
**Purpose:** Full integration testing with Kind cluster

**Features:**
- Kind cluster setup with kubectl
- CRD installation and operator setup
- 45-minute timeout for comprehensive testing
- Detailed failure logging and artifact collection
- Automatic cluster cleanup


## Usage

### For Contributors

**Standard Development:**
```bash
# Workflows automatically run on:
git push origin feature-branch  # Triggers unit tests
git push origin main           # Triggers full CI pipeline
```

**Integration Testing:**
```bash
# To run integration tests on a PR, add the label:
gh pr edit --add-label "integration-tests"

# Or trigger manually:
gh workflow run integration-tests.yml
```

### For Maintainers

**Manual Workflow Triggers:**
```bash
# Run integration tests manually
gh workflow run integration-tests.yml

# Check workflow status
gh run list --workflow=ci.yml
```

## Workflow Strategy

### Fast Feedback
- **Unit tests** run on every push/PR (2-3 minutes)
- **Integration tests** only run when needed to save resources

### Comprehensive Coverage
- **Integration tests** ensure compatibility across scenarios
- **Coverage reporting** tracks test effectiveness

### Resource Efficiency
- **Conditional integration tests** - only on main or labeled PRs
- **Caching** for Go modules and builds
- **Parallel jobs** where possible
- **Automatic cleanup** to prevent resource leaks

## Configuration

### Required Secrets
- `CODECOV_TOKEN` - For coverage reporting (optional)

### Environment Variables
- `GO_VERSION: '1.22'` - Go version used across all workflows
- `ENVTEST_K8S_VERSION` - Kubernetes version for testing

### Makefile Targets Used
- `make test-unit` - Unit tests
- `make test-integration` - Integration tests
- `make test-cluster` - Create Kind cluster
- `make test-cluster-delete` - Cleanup Kind cluster
- `make manifests generate` - Code generation

## Troubleshooting

### Common Issues

**Integration tests failing:**
- Check cluster logs in workflow artifacts
- Verify CRDs are installed correctly
- Check for resource constraints in Kind cluster

**Unit tests failing:**
- Check for missing dependencies
- Verify envtest setup
- Check for race conditions

**Workflow not triggering:**
- Verify branch protection rules
- Check workflow file syntax
- Ensure proper GitHub permissions

### Debugging

1. **Check workflow logs** in GitHub Actions tab
2. **Download artifacts** for detailed logs and coverage
3. **Use manual dispatch** to test specific scenarios

## Maintenance

### Adding New Tests
1. Add unit tests - they'll run automatically in existing workflows
2. Add integration tests - they'll run in integration workflows
3. Update this README if new workflow features are added

### Updating Dependencies
1. Update Go version in all workflows consistently
2. Update action versions (setup-go, checkout, etc.)
3. Update Kubernetes versions for testing compatibility

### Performance Optimization
1. Monitor workflow execution times
2. Optimize caching strategies
3. Consider workflow parallelization opportunities
