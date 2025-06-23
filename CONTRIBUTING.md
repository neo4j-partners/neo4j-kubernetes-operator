# Contributing to Neo4j Enterprise Operator

Welcome to the Neo4j Enterprise Operator project! We're excited to have you contribute. This guide will help you get started with development.

## Prerequisites

Before contributing, ensure you have the following tools installed:

- **Go 1.21+**: [Installation Guide](https://golang.org/doc/install)
- **Docker**: [Installation Guide](https://docs.docker.com/get-docker/)
- **kubectl**: [Installation Guide](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- **kind**: [Installation Guide](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- **make**: Usually pre-installed on Unix systems
- **git**: [Installation Guide](https://git-scm.com/book/en/v2/Getting-Started-Installing-Git)

## Quick Start

1. **Fork and Clone the Repository**

   ```bash
   git clone https://github.com/your-username/neo4j-kubernetes-operator.git

cd neo4j-kubernetes-operator

   ```

2. **Set Up Development Environment**

   ```bash
   make setup-dev
   ```

3. **Install Pre-commit Hooks**

   ```bash
   make install-hooks
   ```

4. **Run Tests**

   ```bash
   make test
   ```

5. **Start Local Development**

   ```bash
   make dev-cluster
   make dev-run
   ```

## Development Workflow

### Setting Up Your Environment

1. **Install all dependencies**:

   ```bash
   make setup-dev
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

2. **Make your changes** and ensure they follow our coding standards:

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
   make dev-run
   ```

### Testing Your Changes

We have several levels of testing:

- **Unit Tests**: `make test`
- **Integration Tests**: `make test-integration`
- **E2E Tests**: `make test-e2e`
- **All Tests**: `make test-all`

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

## Development Tools

### Pre-commit Hooks

We use pre-commit hooks to ensure code quality:

- **go fmt**: Code formatting
- **go vet**: Static analysis
- **golangci-lint**: Advanced linting
- **goimports**: Import organization
- **Tests**: Unit tests on changed files

### VS Code Integration

Install the Go extension and use the provided `.vscode/settings.json` for optimal development experience.

### Debugging

Use `make debug` to run the operator with delve debugger support.

## Project Structure

```text
├── api/                    # API type definitions
├── cmd/                    # Main application entry point
├── config/                 # Kubernetes manifests and configuration
├── internal/               # Internal packages
│   ├── controller/         # Controller implementations
│   ├── resources/          # Resource builders
│   ├── neo4j/             # Neo4j client and utilities
│   └── webhooks/          # Admission webhooks
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
