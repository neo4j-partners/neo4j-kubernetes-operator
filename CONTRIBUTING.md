# Contributing to Neo4j Enterprise Operator

Welcome to the Neo4j Enterprise Operator project! We're excited to have you contribute. This guide will help you get started with development.

## Prerequisites

> **⚠️ IMPORTANT: Kind is Required**
> This project **exclusively uses Kind (Kubernetes in Docker)** for development and testing. All development workflows, testing, and CI emulation depend on Kind clusters. You cannot contribute without Kind installed.

Before contributing, ensure you have the following tools installed:

### Required Tools

- **Go 1.21+**: [Installation Guide](https://golang.org/doc/install)
- **Docker**: [Installation Guide](https://docs.docker.com/get-docker/)
- **kubectl**: [Installation Guide](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- **Kind (Kubernetes in Docker)**: **REQUIRED** - See installation instructions below
- **make**: Usually pre-installed on Unix systems
- **git**: [Installation Guide](https://git-scm.com/book/en/v2/Getting-Started-Installing-Git)

### Kind Installation Instructions

Kind is **mandatory** for all development workflows. Choose your platform:

#### macOS (using Homebrew - Recommended)
```bash
# Install Kind via Homebrew
brew install kind

# Verify installation
kind version
```

#### Linux (using Go or Binary)
```bash
# Option 1: Install via Go (if you have Go installed)
go install sigs.k8s.io/kind@latest

# Option 2: Download binary directly
# For AMD64 / x86_64
[ $(uname -m) = x86_64 ] && curl -Lo ./kind https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64
# For ARM64
[ $(uname -m) = aarch64 ] && curl -Lo ./kind https://kind.sigs.k8s.io/dl/latest/kind-linux-arm64
chmod +x ./kind
sudo mv ./kind /usr/local/bin/kind

# Verify installation
kind version
```

#### Windows (using Chocolatey or Scoop)
```bash
# Using Chocolatey
choco install kind

# Using Scoop
scoop install kind

# Verify installation
kind version
```

#### Why Kind is Required

- **Development Clusters**: All local development uses Kind clusters (`make dev-cluster`)
- **Testing Infrastructure**: Integration and E2E tests require Kind (`make test-integration`)
- **CI Emulation**: The `make test-ci-local` target depends on Kind clusters
- **No Alternatives**: We do **not** support minikube, k3s, or other local Kubernetes solutions

### Verify Prerequisites

Once you have all tools installed, verify your setup:

```bash
# Check all required tools
go version          # Should show Go 1.21+
docker version      # Should show Docker info
kubectl version     # Should show kubectl client
kind version        # Should show Kind version
make --version      # Should show GNU Make
git --version       # Should show Git version

# Test Kind functionality
kind create cluster --name test-setup
kind delete cluster --name test-setup
```

## Quick Start

1. **Fork and Clone the Repository**

   ```bash
   git clone https://github.com/your-username/neo4j-kubernetes-operator.git

cd neo4j-kubernetes-operator

   ```

2. **Set Up Development Environment**

   ```bash
   make dev-cluster
   ```

3. **Install Dependencies**

   ```bash
   make manifests generate
   ```

4. **Run Tests**

   ```bash
   make test
   ```

5. **Start Local Development**

   ```bash
   make dev-cluster
   make operator-setup  # Deploy operator in development cluster
   ```

## Development Workflow

### Setting Up Your Environment

1. **Install all dependencies**:

   ```bash
   make manifests generate
   ```

2. **Create a development cluster**:

   ```bash
   make dev-cluster
   ```

3. **Install CRDs**:

   ```bash
   make install
   ```

### Making Changes

1. **Create a feature branch**:

   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make your changes** and ensure they follow our coding standards (optional but recommended):

   ```bash
   make fmt lint
   ```

3. **Run tests**:

   ```bash
   make test
   make test-integration
   ```

4. **Test your changes locally**:

   ```bash
   make operator-setup  # Deploy operator to test your changes
   ```

### Testing Your Changes

We have several levels of testing:

- **Unit Tests**: `make test-unit`
- **Integration Tests**: `make test-integration` (auto-creates cluster and deploys operator)
- **All Tests**: `make test`
- **Coverage Report**: `make test-coverage`

#### Integration Test Best Practices

When writing integration tests, **proper resource cleanup is MANDATORY** to prevent CI failures:

1. **Always include AfterEach blocks** with comprehensive cleanup
2. **Remove finalizers** before deleting resources to ensure actual deletion
3. **Call cleanupCustomResourcesInNamespace()** to clean up all related resources
4. **Don't rely on test suite cleanup alone** - implement active cleanup

Example pattern:
```go
AfterEach(func() {
    if cluster != nil {
        // Remove finalizers and delete
        if len(cluster.GetFinalizers()) > 0 {
            cluster.SetFinalizers([]string{})
            _ = k8sClient.Update(ctx, cluster)
        }
        _ = k8sClient.Delete(ctx, cluster)
    }
    // Clean up namespace resources
    cleanupCustomResourcesInNamespace(testNamespace)
})
```

See CLAUDE.md for detailed integration test patterns and common pitfalls to avoid.

#### CI Workflow Emulation (Added 2025-08-22)

Before submitting PRs, test your changes with CI-identical resource constraints:

```bash
# Emulate complete CI workflow with debug logging
make test-ci-local
```

**Why Use CI Emulation:**
- **Prevent CI Failures**: Test with same memory constraints as GitHub Actions (512Mi vs 1.5Gi local)
- **Debug Information**: Comprehensive logs for troubleshooting (`logs/ci-local-*.log`)
- **Complete Testing**: Full workflow from unit tests to integration cleanup
- **Resource Validation**: Ensures your changes work in memory-constrained environments

**When to Use:**
- Before pushing changes that affect resource requirements
- When debugging failed CI runs
- Before submitting PRs with test modifications
- When adding new integration tests

**Generated Debug Files:**
- `logs/ci-local-unit.log` - Unit test execution logs
- `logs/ci-local-integration.log` - Integration test and cluster setup logs
- `logs/ci-local-cleanup.log` - Environment cleanup logs

**Example Usage:**
```bash
# Test changes before pushing
make test-ci-local

# Check for memory-related issues
grep -E "(memory|Memory|OOM)" logs/ci-local-integration.log

# Verify CI readiness
echo "If this passes, CI should pass too ✅"
```

### Code Generation

When you modify API types, run:

```bash
make generate manifests
```

### Submitting Changes

1. **Commit your changes** with a clear message:

   ```bash
   git add .
   git commit -m "feat: add new feature description"
   ```

2. **Push to your fork**:

   ```bash
   git push origin feature/your-feature-name
   ```

3. **Create a Pull Request** with:
   - Clear title and description
   - Reference to any related issues
   - Screenshots for UI changes
   - Test results

### CI/CD Workflow (Updated 2025-08-22)

Our CI pipeline has been optimized for faster feedback and resource efficiency:

#### Automatic Testing
- **✅ Unit Tests**: Run automatically on every push and PR
- **⚡ Fast Feedback**: Unit tests provide immediate feedback without cluster overhead

#### Optional Integration Tests (On-demand)
Integration tests are now **optional** and only run when explicitly requested to save CI resources:

**How to trigger integration tests:**

1. **Manual Trigger** (Recommended for testing):
   ```
   Go to Actions → CI → "Run workflow" → Check "Run integration tests"
   ```

2. **PR Label** (For PRs requiring integration testing):
   ```
   Add "run-integration-tests" label to your pull request
   ```

3. **Commit Message** (For specific commits):
   ```bash
   git commit -m "feat: add new cluster feature [run-integration]"
   ```

**Why this change?**
- **Resource Efficiency**: Integration tests require significant memory (~7GB) and time (10+ minutes)
- **Faster PRs**: Most changes only need unit tests for validation
- **On-demand**: Run integration tests only when needed for cluster/integration changes

**When to run integration tests:**
- Changes to controllers or cluster logic
- New integration test additions
- Before important releases
- When troubleshooting CI-specific issues

## Development Tools

### Code Quality Tools (Optional)

The following tools are available for local development to maintain code quality:

- **go fmt**: Code formatting (`make fmt`)
- **go vet**: Static analysis (`make vet`)
- **golangci-lint**: Advanced linting (`make lint`)
- **goimports**: Import organization
- **Security scanning**: (`make security`)

**Note:** Code quality checks are not enforced by CI but are recommended for local development.

### VS Code Integration

Install the Go extension and use the provided `.vscode/settings.json` for optimal development experience.

### Debugging

Use `make operator-setup` to deploy the operator in your development cluster for testing and debugging.

## Project Structure

```text
├── api/                    # API type definitions
├── cmd/                    # Main application entry point
├── config/                 # Kubernetes manifests and configuration
├── internal/               # Internal packages
│   ├── controller/         # Controller implementations
│   ├── resources/          # Resource builders
│   ├── neo4j/             # Neo4j client and utilities
│   └── validation/        # Input validation logic
├── test/                   # Test files
├── hack/                   # Development scripts
└── docs/                   # Documentation
```

## Coding Standards

### Go Style

- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `gofmt` and `goimports`
- Follow the [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)

### Kubernetes Conventions

- Follow [Kubernetes API Conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
- Use proper status conditions
- Implement proper RBAC

### Git Commit Messages

We follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` for new features
- `fix:` for bug fixes
- `docs:` for documentation changes
- `test:` for test additions/changes
- `refactor:` for code refactoring
- `chore:` for maintenance tasks

## Getting Help

- **Slack**: Join our [Neo4j Community Slack](https://neo4j.com/slack/)
- **Issues**: Check existing [GitHub Issues](https://github.com/neo4j-labs/neo4j-kubernetes-operator/issues)
- **Discussions**: Start a [GitHub Discussion](https://github.com/neo4j-labs/neo4j-kubernetes-operator/discussions)

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
