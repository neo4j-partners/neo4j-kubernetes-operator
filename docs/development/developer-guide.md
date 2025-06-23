# Neo4j Operator Developer Guide

This comprehensive guide covers all aspects of developing the Neo4j Enterprise Operator for Kubernetes, from initial setup to advanced development workflows.

## 🏗️ Core Features for Development

### Topology-Aware Placement

The operator supports automatic distribution of Neo4j primaries across availability zones:

- **Automatic zone discovery** - Discovers available zones in the cluster
- **Smart constraint generation** - Creates appropriate Kubernetes constraints
- **Validation and monitoring** - Ensures proper distribution
- **Flexible configuration** - Supports both hard and soft constraints

**Development workflow:**

```bash
# Test topology features locally
make dev-start-multizone     # Start multi-zone kind cluster
kubectl apply -f config/samples/topology-aware-cluster.yaml
./scripts/validate-topology.sh my-cluster
```

**Key files for topology development:**

- `api/v1alpha1/neo4jenterprisecluster_types.go` - API definitions
- `internal/controller/topology_scheduler.go` - Core scheduling logic
- `internal/controller/neo4jenterprisecluster_controller.go` - Controller integration
- `docs/topology-aware-placement.md` - Complete feature documentation

## 🚀 Quick Start

### Prerequisites

- **Go 1.21+** - [Install Go](https://golang.org/doc/install)
- **Docker** - [Install Docker](https://docs.docker.com/get-docker/)
- **kubectl** - [Install kubectl](https://kubernetes.io/docs/tasks/tools/)
- **kind** - For local Kubernetes clusters
- **Git** - Version control

### One-Command Setup

```bash
# Initialize complete development environment
make dev-init
```

This command will:

- ✅ Install all development tools
- ✅ Create a kind Kubernetes cluster
- ✅ Setup git hooks and pre-commit checks
- ✅ Configure IDE settings
- ✅ Start monitoring stack

### Development Dashboard

```bash
# View current development environment status
make dev-dashboard
```

This provides:

- Real-time status of cluster, services, and tools
- Quick access to common development tasks
- Environment health checks

## 🏗️ Development Environment

### Development Modes

#### 1. Normal Development Mode

```bash
make dev-start
```

#### 2. Debug Mode

```bash
make dev-debug
```

#### 3. Hot Reload Mode

```bash
# Terminal 1: Start services
make dev-services

# Terminal 2: Start operator with hot reload
HOT_RELOAD=true make dev-start
```

#### 4. Tilt Development Mode

```bash
tilt up
```

### Development Services

#### Start Core Services

```bash
# Start Neo4j, Prometheus, Grafana
make dev-services

# Access points:
# - Neo4j Browser: http://localhost:7474 (neo4j/password)
# - Prometheus: http://localhost:9090
# - Grafana: http://localhost:3000 (admin/admin)
```

#### Start Neo4j Cluster

```bash
# Start 3-node Neo4j Enterprise cluster
make dev-services-cluster

# Access cluster nodes:
# - Core 1: http://localhost:7475
# - Core 2: http://localhost:7476
# - Core 3: http://localhost:7477
```

#### Start Monitoring Stack

```bash
make dev-services-monitoring
```

## 🧪 Testing Framework

### Comprehensive Testing

```bash
# Run all tests with reports
make test-all-comprehensive

# Quick test suite
make test-quick-comprehensive

# Specific test types
make test-unit-comprehensive
make test-integration-comprehensive
make test-e2e-comprehensive
make test-ginkgo
```

### Test Configuration

Set environment variables for test customization:

```bash
# Enable parallel testing
export PARALLEL=true

export PERF_DURATION=10m
export PERF_CONCURRENT=20

# Run comprehensive tests
make test-all-comprehensive
```

### Test Reports

```bash
# Generate comprehensive test report
make test-report

# View test summary
make test-summary
```

## 🔍 Code Quality & Analysis

### Comprehensive Quality Analysis

```bash
# Run complete quality analysis
make quality

# Quick quality checks
make quality-quick

# Specific analyses
make quality-format    # Code formatting
make quality-lint      # Linting
make quality-security  # Security analysis
make quality-deps      # Dependency analysis
make quality-metrics   # Code metrics
```

### Code Formatting

```bash
# Advanced code formatting
make quality-format
```

This runs:

- `go fmt` - Standard Go formatting
- `gofumpt` - Enhanced formatting
- `gci` - Import organization
- `golines` - Line length formatting

### Security Analysis

```bash
make quality-security
```

This runs:

- `gosec` - Go security checker
- `govulncheck` - Vulnerability scanning
- `gitleaks` - Secret detection
- License compatibility checks

### Quality Reports

```bash
# Generate and open quality report
make quality-report
```

## 🎯 Development Tools & Features

### 🔧 Comprehensive Testing Framework

- **Unit Tests** - Fast, isolated tests
- **Integration Tests** - Controller integration testing
- **E2E Tests** - End-to-end workflow testing
- **Ginkgo BDD Tests** - Behavior-driven development
- **Topology Testing** - Validate placement across zones

```bash
make test-all-comprehensive  # Run all tests
make test-quick-comprehensive # Quick test suite
make test-report            # Generate HTML test report
make test-topology          # Test topology-aware placement
```

### 🔍 Code Quality Analysis

- **Advanced Linting** - golangci-lint, staticcheck, errcheck
- **Security Scanning** - gosec, govulncheck, gitleaks
- **Code Formatting** - gofmt, gofumpt, goimports, golines
- **Dependency Analysis** - License checking, vulnerability scanning
- **Code Metrics** - Complexity, coverage, performance

```bash
make quality                # Complete quality analysis
make quality-quick          # Quick quality checks
make quality-report         # Generate HTML quality report
```

### 🐳 Development Services

- **Neo4j Enterprise** - Single instance and cluster modes
- **Prometheus** - Metrics collection and monitoring
- **Grafana** - Dashboards and visualization
- **Redis** - Caching for development
- **MinIO** - S3-compatible storage testing
- **LocalStack** - AWS services simulation
- **Jaeger** - Distributed tracing

```bash
make dev-services           # Start core services
make dev-services-cluster   # Start Neo4j cluster
make dev-services-monitoring # Start monitoring stack
```

### 🔥 Hot Reload & Debug

- **Air** - Hot reload for Go applications
- **Delve** - Go debugger integration
- **Tilt** - Kubernetes development environment
- **VS Code** - Complete IDE integration

```bash
make dev-start              # Normal development mode
make dev-debug              # Debug mode with delve
HOT_RELOAD=true make dev-start # Hot reload mode
tilt up                     # Tilt development mode
```

### 📊 Performance Profiling

- **CPU Profiling** - Identify performance bottlenecks
- **Memory Profiling** - Track memory usage and leaks
- **Execution Tracing** - Detailed execution analysis
- **Blocking Profiling** - Find blocking operations
- **Mutex Profiling** - Analyze lock contention

```bash
make profile-trace          # Execution tracing
```

### 🔐 Security & Compliance

- **Pre-commit hooks** - Automated security checks
- **Secret detection** - Prevent credential leaks
- **Vulnerability scanning** - Dependency security
- **License compliance** - Legal compliance checking
- **RBAC validation** - Kubernetes security

### 📝 VS Code Integration

- **Code snippets** - Neo4j operator-specific snippets
- **Debug configurations** - Pre-configured debugging
- **Task definitions** - Integrated build and test tasks
- **Extension recommendations** - Curated extension list
- **IntelliSense** - Enhanced code completion

### 🎯 Git Workflow Enhancement

- **Conventional commits** - Standardized commit messages
- **Pre-commit hooks** - Automated quality checks
- **Commit linting** - Enforce commit standards
- **Changelog generation** - Automated release notes

### Development Modes

#### 1. Normal Development Mode

```bash
make dev-start
```

#### 2. Debug Mode

```bash
make dev-debug
```

#### 3. Hot Reload Mode

```bash
# Terminal 1: Start services
make dev-services

# Terminal 2: Start operator with hot reload
HOT_RELOAD=true make dev-start
```

#### 4. Tilt Development Mode

```bash
tilt up
```

## 🔄 Git Workflow

### Git Hooks

```bash
# Install pre-commit hooks
make git-hooks-install

# Update hooks
make git-hooks-update

# Run hooks manually
make git-hooks-run
```

### Commit Standards

We use [Conventional Commits](https://www.conventionalcommits.org/):

```bash
# Examples of good commit messages
git commit -m "feat: add cluster scaling functionality"
git commit -m "fix: resolve memory leak in reconciler"
git commit -m "docs: update operator deployment guide"
git commit -m "test: add integration tests for backup controller"
```

### Pre-commit Checks

The following checks run automatically:

- Go formatting and imports
- Linting with golangci-lint
- Unit tests
- Security scanning
- Commit message validation
- YAML validation
- Markdown linting

## 📖 Documentation

### Generate Documentation

```bash
# API documentation
make docs-api

# CRD documentation
make docs-crd

# Serve documentation locally
make docs-serve
```

### Documentation Structure

```
docs/
├── api-reference.md          # Generated API docs
├── development/
│   ├── developer-guide.md    # This guide
│   ├── architecture.md       # Architecture overview
│   └── testing-guide.md      # Testing guidelines
├── user/
│   ├── quickstart.md         # User quick start
│   └── examples/             # Usage examples
└── operations/
    ├── deployment.md         # Deployment guide
    └── troubleshooting.md    # Troubleshooting
```

## 🔍 Debugging

### Local Debugging

#### With VS Code

1. Set breakpoints in your code
2. Press F5 or go to Run and Debug
3. Select "Launch Neo4j Operator"

#### With Delve CLI

```bash
# Build with debug symbols
go build -gcflags="all=-N -l" -o bin/manager cmd/main.go

# Start debugger
dlv exec bin/manager -- --zap-devel=true --leader-elect=false
```

### Remote Debugging

#### Port Forward to Running Pod

```bash
# Forward debug port
kubectl port-forward deployment/neo4j-operator-controller-manager 2345:2345

# Connect debugger
dlv connect localhost:2345
```

### Debug Information

#### View Operator Logs

```bash
make logs-follow
```

#### Check Health

```bash
make health-check
```

#### Scrape Metrics

```bash
make metrics-scrape
```

## 🏗️ Advanced Development

### Custom Controllers

When adding new controllers:

1. **Generate CRD Types**

   ```bash
   kubebuilder create api --group neo4j --version v1alpha1 --kind MyResource
   ```

2. **Update RBAC**

   ```bash
   make manifests
   ```

3. **Add Tests**

   ```bash
   # Unit tests
   touch internal/controller/myresource_controller_test.go

   # Integration tests
   touch test/integration/myresource_test.go
   ```

4. **Add Webhooks** (if needed)

   ```bash
   kubebuilder create webhook --group neo4j --version v1alpha1 --kind MyResource
   ```

### Custom Metrics

Add custom Prometheus metrics:

```go
import "github.com/prometheus/client_golang/prometheus"

var (
    clusterReconcileTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "neo4j_cluster_reconcile_total",
            Help: "Total number of cluster reconciliations",
        },
        []string{"cluster", "namespace", "result"},
    )
)
```

### Performance Optimization

#### Controller Optimization

- Use field selectors in watches
- Implement proper resource caching
- Use exponential backoff for retries
- Batch operations when possible

#### Memory Management

```bash
# Monitor memory usage
make profile-mem

# Check for memory leaks
go test -tags=goleak ./...
```

## 🚨 Troubleshooting

### Common Issues

#### 1. Build Failures

```bash
# Clean and rebuild
make clean
go mod download
go mod tidy
make build
```

#### 2. Test Failures

```bash
# Clean test cache
go clean -testcache

# Run specific test
go test -v ./internal/controller/... -run TestSpecificFunction
```

#### 3. Kind Cluster Issues

```bash
# Reset cluster
make dev-cluster-delete
make dev-cluster
```

#### 4. Docker Issues

```bash
# Clean Docker system
make dev-services-clean
docker system prune -af
```

### Getting Help

1. **Check the logs**: `make dev-logs`
2. **Review the dashboard**: `make dev-dashboard`
3. **Run diagnostics**: `make health-check`
4. **Check issues**: [GitHub Issues](https://github.com/neo4j-labs/neo4j-kubernetes-operator/issues)

## 🎯 Best Practices

### Code Organization

```
internal/
├── controller/          # Controllers
├── resources/          # Resource builders
├── neo4j/             # Neo4j client
├── metrics/           # Custom metrics
└── webhooks/          # Admission webhooks
```

### Testing Strategy

1. **Unit Tests** - Test individual functions
2. **Integration Tests** - Test controller integration
3. **E2E Tests** - Test complete workflows

### Security Considerations

1. **RBAC** - Use least privilege principle
2. **Secrets** - Never log sensitive data
3. **Network** - Use network policies
4. **Images** - Use minimal base images
5. **Scanning** - Regular security scans

### Performance Guidelines

1. **Controllers** - Efficient reconciliation loops
2. **Resources** - Proper resource limits
3. **Caching** - Smart caching strategies
4. **Metrics** - Monitor performance metrics
5. **Profiling** - Regular performance profiling

## 📚 Additional Resources

- [Kubernetes Operator Development Guide](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
- [controller-runtime Documentation](https://pkg.go.dev/sigs.k8s.io/controller-runtime)
- [Neo4j Driver Documentation](https://neo4j.com/docs/go-manual/current/)
- [Prometheus Operator Guide](https://prometheus-operator.dev/)

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Run quality checks: `make quality`
6. Submit a pull request

### Contribution Checklist

- [ ] Code follows Go conventions
- [ ] Tests added for new functionality
- [ ] Documentation updated
- [ ] Commit messages follow conventional format
- [ ] Pre-commit hooks pass
- [ ] CI/CD pipeline passes

---

For more detailed information, see the specific guides in the `docs/` directory or run `make help-dev` for a quick reference.
