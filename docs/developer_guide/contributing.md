# Contributing Guide

We welcome contributions to the Neo4j Enterprise Operator! This guide provides comprehensive instructions for contributing code, documentation, and improvements to make the operator better for everyone.

## 🚀 Quick Start for Contributors

### Prerequisites

- **Go**: Version 1.21+ for development
- **Docker**: Container runtime for building images
- **kubectl**: Kubernetes CLI tool
- **kind**: Kubernetes in Docker for local clusters
- **git**: Version control

### 1. Repository Setup

```bash
# Fork the repository on GitHub, then clone your fork
git clone https://github.com/<your-username>/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator

# Add upstream remote for pulling updates
git remote add upstream https://github.com/neo4j-labs/neo4j-kubernetes-operator.git

# Create a feature branch
git checkout -b feature/my-awesome-feature
```

### 2. Development Environment Setup

```bash
# Generate CRDs and Go code
make manifests generate

# Create development Kind cluster with cert-manager
make dev-cluster

# Start the operator locally for development
make dev-run
```

**Benefits of this setup**:
- **Fast iteration**: No container rebuilds needed
- **Debug support**: Full debugger capabilities
- **Live reloading**: Operator restarts when code changes
- **Direct logs**: Console output for immediate feedback

### 3. Make Your Changes

The operator follows a well-defined architecture. Key areas for contributions:

**Controllers** (`internal/controller/`):
- Neo4jEnterpriseCluster controller for clustered deployments
- Neo4jEnterpriseStandalone controller for single-node deployments
- Database, Plugin, Backup, and Restore controllers

**Custom Resources** (`api/v1alpha1/`):
- CRD type definitions for all Neo4j resources
- Validation tags and documentation

**Resource Builders** (`internal/resources/`):
- Kubernetes resource generation logic
- ConfigMap, Service, and StatefulSet builders

**Validation Framework** (`internal/validation/`):
- Input validation and recommendations
- Error handling and user guidance

### 4. Testing Your Changes

```bash
# Run unit tests (no cluster required)
make test-unit

# Run integration tests (requires test cluster)
make test-integration

# Run specific controller tests
go test ./internal/controller -run TestClusterReconciler -v

# Test with example deployments
kubectl apply -f examples/clusters/minimal-cluster.yaml
kubectl apply -f examples/standalone/single-node-standalone.yaml
```

### 5. Submit Your Contribution

```bash
# Run code quality checks
make fmt lint vet

# Commit using conventional commits
git add .
git commit -m "feat: add server role constraints for topology optimization"

# Push and create pull request
git push origin feature/my-awesome-feature
# Create PR via GitHub UI
```

## 📝 Development Guidelines

### Code Organization

**Follow the established patterns**:

1. **Controller Pattern**: Use the standard Kubernetes controller pattern with proper reconciliation
2. **Builder Pattern**: Use resource builders in `internal/resources/` for clean separation
3. **Validation Framework**: Add validation in `internal/validation/` with clear error messages
4. **Testing Strategy**: Write unit, integration, and manual tests for new features

### Current Architecture (August 2025)

Understand the current architecture before making changes:

#### Server-Based Architecture
- **Clusters**: Use `{cluster-name}-server` StatefulSet with self-organizing servers
- **Standalone**: Use `{standalone-name}` StatefulSet (single replica)
- **Centralized Backup**: Single `{cluster-name}-backup-0` StatefulSet per cluster

#### Dual Deployment Support
- **Neo4jEnterpriseCluster**: High-availability clustered deployments (2+ servers)
- **Neo4jEnterpriseStandalone**: Single-node deployments for development/testing
- **Plugin System**: Supports both deployment types with automatic detection

### Code Style Guidelines

#### Go Code Standards
```bash
# Format code
make fmt

# Run linter (strict mode for contributions)
make lint

# Run go vet
make vet

# Security scan
make security
```

#### Best Practices
1. **Error Handling**: Always handle errors gracefully with proper context
2. **Logging**: Use structured logging with appropriate log levels
3. **Resource Management**: Use `controllerutil.CreateOrUpdate` with retry logic
4. **Finalizers**: Implement proper cleanup with finalizer handling
5. **Status Updates**: Update resource status to reflect current state
6. **Validation**: Add comprehensive validation for user inputs

### Testing Requirements

#### Unit Tests
- **Location**: Alongside source code (`*_test.go`)
- **Coverage**: Aim for >80% coverage on controller logic
- **Patterns**: Use table-driven tests for multiple scenarios

```go
func TestGetStatefulSetName(t *testing.T) {
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
            result := getStatefulSetName(tt.deployment)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

#### Integration Tests
- **Location**: `test/integration/`
- **Framework**: Ginkgo/Gomega BDD-style testing
- **Requirements**: Use 5-minute timeout and proper cleanup
- **Resources**: Minimal CPU (100m), adequate memory (1.5Gi for Neo4j Enterprise)

```go
var _ = Describe("Neo4jPlugin Integration Tests", func() {
    const (
        timeout  = time.Second * 300  // 5-minute timeout for CI
        interval = time.Second * 5
    )

    Context("Plugin Installation", func() {
        It("Should install APOC plugin on cluster", func() {
            // Test implementation with proper cleanup
        })
    })
})
```

### Documentation Requirements

#### Code Documentation
- **Public Functions**: Document all exported functions and types
- **Complex Logic**: Explain non-obvious code with comments
- **API Changes**: Update relevant API documentation

#### User Documentation
For user-facing features, update:
- **Examples**: Add example configurations in `examples/`
- **User Guide**: Update relevant user guide sections
- **API Reference**: Update CRD documentation if API changes

## 🔄 Development Workflow

### Branch Strategy

1. **Feature Branches**: Create feature branches from `main`
2. **Naming Convention**: `feature/short-description` or `fix/issue-description`
3. **Single Purpose**: One feature or fix per branch
4. **Regular Updates**: Keep branches updated with upstream changes

### Pull Request Process

#### Before Submitting
1. **Code Quality**: Run `make fmt lint vet` locally
2. **Tests**: Ensure all tests pass with `make test`
3. **Documentation**: Update relevant documentation
4. **Examples**: Add or update examples if needed

#### PR Description Template
```markdown
## Description
Brief description of changes and motivation.

## Type of Change
- [ ] Bug fix (non-breaking change that fixes an issue)
- [ ] New feature (non-breaking change that adds functionality)
- [ ] Breaking change (fix or feature that causes existing functionality to change)
- [ ] Documentation update

## Testing
- [ ] Unit tests added/updated
- [ ] Integration tests added/updated
- [ ] Manual testing completed
- [ ] Examples tested

## Checklist
- [ ] Code follows project style guidelines
- [ ] Self-review completed
- [ ] Documentation updated
- [ ] Tests added for new functionality
- [ ] All tests pass
```

#### Review Process
1. **Automated Checks**: CI/CD runs automated tests and linting
2. **Code Review**: Maintainers review code for quality and design
3. **Integration Testing**: Changes tested against integration suite
4. **Documentation Review**: Documentation changes reviewed for accuracy

### Git Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/) specification:

```bash
# Feature additions
git commit -m "feat: add server role constraints for topology optimization"

# Bug fixes
git commit -m "fix: resolve resource version conflict during cluster formation"

# Documentation updates
git commit -m "docs: update contributing guide with current architecture"

# Breaking changes
git commit -m "feat!: change topology field structure for server-based architecture"
```

**Format**: `<type>[optional scope]: <description>`

**Types**:
- `feat`: New features
- `fix`: Bug fixes
- `docs`: Documentation changes
- `style`: Code style changes (no logic changes)
- `refactor`: Code refactoring
- `test`: Test additions or modifications
- `chore`: Maintenance tasks

## 🐛 Bug Reports and Feature Requests

### Reporting Bugs

When reporting bugs, include:

1. **Environment Information**:
   - Kubernetes version
   - Neo4j version
   - Operator version
   - Cloud provider (if applicable)

2. **Reproduction Steps**:
   - Minimal example that reproduces the issue
   - Expected vs actual behavior
   - Error messages and logs

3. **Relevant Resources**:
   - YAML configurations (sanitized)
   - Operator logs
   - Neo4j logs (if applicable)

### Feature Requests

1. **Use Case**: Describe the problem you're trying to solve
2. **Proposed Solution**: Suggest how the feature might work
3. **Alternatives**: Consider alternative solutions
4. **Impact**: Describe who would benefit from this feature

## 🏗️ Advanced Development

### Adding New CRDs

When adding new Custom Resource Definitions:

1. **Define Types** (`api/v1alpha1/`):
   ```bash
   # Create new CRD type file
   touch api/v1alpha1/mynewresource_types.go
   ```

2. **Create Controller** (`internal/controller/`):
   ```bash
   # Generate controller scaffold
   kubebuilder create api --group neo4j --version v1alpha1 --kind MyNewResource
   ```

3. **Add Validation** (`internal/validation/`):
   - Implement validation logic
   - Add to validation framework

4. **Update RBAC** (`config/rbac/`):
   ```go
   // Add RBAC markers to controller
   //+kubebuilder:rbac:groups=neo4j.neo4j.com,resources=mynewresources,verbs=get;list;watch;create;update;patch;delete
   ```

5. **Generate Code**:
   ```bash
   make manifests generate
   ```

6. **Add Tests**:
   - Unit tests for controller logic
   - Integration tests for full workflow
   - Example configurations

### Performance Considerations

**Controller Optimization**:
- Use `client.Reader` for read-only operations
- Implement proper caching strategies
- Minimize API calls with efficient resource queries
- Use `controllerutil.CreateOrUpdate` for idempotent operations

**Resource Management**:
- Set appropriate resource requests/limits
- Use owner references for automatic cleanup
- Implement proper finalizer handling

### Debugging Tips

#### Local Debugging
```bash
# Run operator with debug logging
make dev-run ARGS="--zap-log-level=debug"

# Check operator logs
kubectl logs -l app.kubernetes.io/name=neo4j-operator -f

# Examine resource status
kubectl describe neo4jenterprisecluster my-cluster
```

#### VS Code Debug Configuration
```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Launch Operator",
      "type": "go",
      "request": "launch",
      "mode": "debug",
      "program": "${workspaceFolder}/cmd/main.go",
      "args": ["--zap-log-level=debug"],
      "env": {
        "KUBECONFIG": "${env:HOME}/.kube/config"
      }
    }
  ]
}
```

## 🤝 Community Guidelines

### Code of Conduct

We follow the [Contributor Covenant Code of Conduct](https://www.contributor-covenant.org/). Please be respectful and inclusive in all interactions.

### Getting Help

- **GitHub Discussions**: For questions and community interaction
- **GitHub Issues**: For bug reports and feature requests
- **Code Review**: Ask questions during the PR review process
- **Documentation**: Check existing documentation first

### Recognition

Contributors are recognized in:
- Release notes for significant contributions
- GitHub contributors list
- Project documentation acknowledgments

## 📚 Resources

### Learning Resources
- **[Kubebuilder Book](https://book.kubebuilder.io/)**: Controller development guide
- **[Kubernetes API Conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)**
- **[Neo4j Documentation](https://neo4j.com/docs/)**: Neo4j Enterprise features

### Project Resources
- **[Architecture Guide](architecture.md)**: System design and components
- **[Development Guide](development.md)**: Local development setup
- **[Testing Guide](testing.md)**: Testing strategy and patterns
- **[User Guides](../user_guide/)**: User-facing documentation

Thank you for contributing to the Neo4j Enterprise Operator! Your contributions help make Neo4j deployments easier and more reliable for everyone.
