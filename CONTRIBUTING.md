# Contributing to Neo4j Enterprise Operator

Welcome to the Neo4j Enterprise Operator project! This guide covers everything you need to get a productive development environment running.

## Prerequisites

> **Kind is Required** -- This project exclusively uses Kind (Kubernetes in Docker) for development, testing, and CI. No alternatives (minikube, k3s) are supported.

### Required Tools

| Tool | Version | Install |
|------|---------|---------|
| Go | 1.25+ | [golang.org/doc/install](https://golang.org/doc/install) |
| Docker | Latest | [docs.docker.com/get-docker](https://docs.docker.com/get-docker/) |
| kubectl | Latest | [kubernetes.io/docs/tasks/tools](https://kubernetes.io/docs/tasks/tools/install-kubectl/) |
| Kind | 0.27.0+ | `brew install kind` (macOS) or [kind.sigs.k8s.io](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) |
| make | Any | Pre-installed on macOS/Linux |
| git | Any | [git-scm.com](https://git-scm.com/book/en/v2/Getting-Started-Installing-Git) |

### Optional Tools

| Tool | Purpose | Install |
|------|---------|---------|
| [Tilt](https://tilt.dev/) | Live-reload dev loop (~5s rebuilds) | `brew install tilt` |
| [watchexec](https://github.com/watchexec/watchexec) | File-watcher for `make dev-watch` | `brew install watchexec` |
| [mise](https://mise.jdx.dev/) | Pin exact tool versions from `.tool-versions` | `curl https://mise.jdx.dev/install.sh \| sh` |
| [pre-commit](https://pre-commit.com/) | Git hook framework | `brew install pre-commit` |

### Automated Prerequisite Check

```bash
make check-prereqs
```

This verifies all required tools are installed and Docker is running, with actionable error messages for anything missing.

### Reproducible Tool Versions (Optional)

The project includes a `.tool-versions` file that pins the exact tool versions used in CI:

```bash
# Install mise (one-time)
curl https://mise.jdx.dev/install.sh | sh

# Install all pinned versions (Go 1.25, Kind 0.27.0, kubectl 1.31.0, etc.)
mise install
```

This guarantees version parity between your local machine and CI.

## Quick Start

```bash
# 1. Clone
git clone https://github.com/your-username/neo4j-kubernetes-operator.git
cd neo4j-kubernetes-operator

# 2. Bootstrap complete dev environment (Kind cluster + cert-manager + operator)
make dev-up

# 3. Verify everything works
make smoke-test

# 4. Start developing (pick one)
make deploy-dev-local           # Manual rebuild after changes
make dev-watch                  # Auto-rebuild on file save
tilt up                         # Live-reload with web dashboard (recommended)

# 5. Tear down when done
make dev-down
```

`make dev-up` takes ~3 minutes on first run. Subsequent runs are faster since it reuses an existing cluster.

### Alternative: GitHub Codespaces (Zero Local Setup)

The project includes a `.devcontainer/` configuration. On GitHub, click **Code > Codespaces > New codespace** to get a fully configured environment with Go, Docker-in-Docker, kubectl, Helm, and Kind pre-installed. Once the container starts, run `make dev-up` to bootstrap.

Ports 7474 (Neo4j Browser) and 7687 (Bolt) are auto-forwarded.

## Development Workflow

### Setting Up Your Environment

The fastest path is `make dev-up`. For manual control:

```bash
make dev-cluster        # Create Kind cluster + install cert-manager
make manifests generate # Generate CRDs and DeepCopy methods
make install            # Install CRDs into the cluster
make deploy-dev-local   # Build operator image, load into Kind, deploy
```

### Inner Dev Loop

Three options, from simplest to most powerful:

#### Option A: Manual Rebuild (no extra tools)

```bash
# After making changes:
make deploy-dev-local   # Full rebuild + redeploy (~60s)
```

#### Option B: File Watcher (requires watchexec or fswatch)

```bash
make dev-watch
```

Watches `api/`, `internal/`, and `cmd/` for `.go` and `.yaml` changes. On each save, automatically runs `make manifests generate build deploy-dev-local`. Install a watcher first:

```bash
brew install watchexec   # recommended
# or
brew install fswatch     # alternative
```

#### Option C: Tilt (recommended for regular contributors)

```bash
brew install tilt   # one-time
tilt up             # start the dev loop
```

Tilt provides:
- **Auto CRD regeneration** when `api/` files change
- **Cross-compile + image build + Kind load** on every Go source change (~5s)
- **Web dashboard** showing resource status, logs, and build times
- **Unit test runner** (manual trigger from the Tilt UI)

The Tiltfile uses `custom_build` to compile a Linux binary locally and package it into a minimal Alpine image, then loads it into Kind. The architecture is auto-detected (works on both amd64 and arm64).

```bash
tilt up --stream   # terminal-only mode (no browser)
tilt down          # stop and clean up
tilt ci            # run once and exit (useful for validation)
```

### Making Changes

1. **Create a feature branch**:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make your changes** and run code quality checks:
   ```bash
   make fmt lint
   ```

3. **Run tests**:
   ```bash
   make test-unit                           # Fast unit tests (~30s)
   make test-one TEST="should create"       # Single integration test
   make test-integration                    # Full integration suite (~30min)
   ```

4. **Verify end-to-end**:
   ```bash
   make smoke-test   # Deploys a standalone instance and waits for Ready
   ```

5. **If you modified API types or RBAC markers** (`api/v1beta1/`, `internal/controller/`):
   ```bash
   # Single command — regenerates everything: DeepCopy, CRDs, RBAC,
   # kustomize lists, helm chart CRDs + ClusterRole + ArtifactHub
   # annotation, OperatorHub bundle.  Also lints the chart and verifies
   # OperatorHub CSV coverage.
   make ship-prep
   ```
   For an iterative loop (no bundle build), `make sync-all` is faster.
   CI's `check-drift` job will fail the PR if any generated file is out
   of date, so always run this before pushing.

   If you're adding a *new* CRD, also add a description case to
   `scripts/helm-sync-artifacthub-crds.sh` — the script exits non-zero
   if any CRD lacks a description.

   If using Tilt, individual `make manifests generate` happen
   automatically; the packaging syncs (`make ship-prep`) still need to
   run before commit.

## Testing

### Test Levels

| Command | What It Does | Cluster Required | Typical Duration |
|---------|-------------|-----------------|-----------------|
| `make test-unit` | Unit tests with envtest | No | ~30s |
| `make test-one TEST="..."` | Single integration test by name | Yes | 1-5min |
| `make smoke-test` | Deploy standalone, verify Ready | Yes | ~3min |
| `make test-integration` | Full integration suite | Auto-created | ~30min |
| `make test-ci-local` | Emulate full CI workflow | Auto-created | ~45min |
| `make test-coverage` | Unit tests with coverage report | No | ~1min |

### Running a Single Integration Test

```bash
# Run tests matching a description substring
make test-one TEST="should create standalone"
make test-one TEST="backup"
make test-one TEST="version detection"
```

This wraps Ginkgo's `--focus` flag. It auto-detects which Kind cluster (dev or test) to use.

### Smoke Test

```bash
make smoke-test
```

Deploys a minimal Neo4j Enterprise standalone instance (`hack/smoke-test-standalone.yaml`) with 1.5Gi memory, waits for the `Ready` condition (up to 5 minutes), then cleans up. Useful as a quick end-to-end sanity check after changes.

### Integration Test Best Practices

**Resource cleanup is mandatory** in every test. Tests that leak resources cause cascading failures in CI.

```go
AfterEach(func() {
    if cluster != nil {
        // Remove finalizers before deletion
        if len(cluster.GetFinalizers()) > 0 {
            cluster.SetFinalizers([]string{})
            _ = k8sClient.Update(ctx, cluster)
        }
        _ = k8sClient.Delete(ctx, cluster)
        cluster = nil
    }
    if testNamespace != "" {
        cleanupCustomResourcesInNamespace(testNamespace)
    }
})
```

Key rules:
- Always remove finalizers before deleting custom resources
- Use `cleanupCustomResourcesInNamespace()` in every `AfterEach`
- Use 300-second timeouts for all integration tests
- Memory requests must be >= 1.5Gi (Neo4j Enterprise minimum)

See `CLAUDE.md` for the full regression prevention checklist.

### CI Workflow Emulation

Before submitting a PR that touches controllers or tests, validate with CI-identical constraints:

```bash
make test-ci-local
```

This runs unit tests and integration tests with CI environment variables (`CI=true`, 512Mi memory limits). Debug logs are saved to:
- `logs/ci-local-unit.log`
- `logs/ci-local-integration.log`
- `logs/ci-local-cleanup.log`

## CI/CD

### Automatic (Every Push/PR)
- **Unit tests** run on every push and pull request

### On-Demand (Integration Tests)

Integration tests are opt-in to save CI resources (~7GB RAM, 10+ minutes):

| Trigger | How |
|---------|-----|
| PR label | Add `run-integration-tests` label |
| Commit message | Include `[run-integration]` in message |
| Manual | Actions > CI > Run workflow > Check "Run integration tests" |

**When to trigger integration tests:**
- Changes to controllers, resources, or cluster logic
- New or modified integration tests
- Before important releases

## Make Target Reference

Run `make help` for the most common targets, or `make help-all` for the complete list.

### Getting Started
| Target | Description |
|--------|-------------|
| `make dev-up` | Bootstrap complete dev environment (cluster + operator) |
| `make dev-down` | Tear down the complete dev environment |
| `make check-prereqs` | Verify all required tools are installed |
| `make deploy-dev-local` | Build and deploy operator to Kind |

### Dev Loop
| Target | Description |
|--------|-------------|
| `make dev-watch` | Auto-rebuild on file changes (requires watchexec/fswatch) |
| `make operator-logs` | Follow operator logs |
| `make operator-status` | Show operator deployment status |

### Testing
| Target | Description |
|--------|-------------|
| `make test-unit` | Unit tests (no cluster needed) |
| `make test-one TEST="..."` | Run a single integration test by name |
| `make smoke-test` | Deploy standalone and verify Ready state |
| `make test-integration` | Full integration suite |
| `make test-ci-local` | Emulate CI workflow locally |
| `make test-coverage` | Generate coverage report |

### Build
| Target | Description |
|--------|-------------|
| `make build` | Build operator binary |
| `make docker-build` | Build container image |
| `make manifests` | Generate CRDs and RBAC |
| `make generate` | Generate DeepCopy methods |

### Code Quality
| Target | Description |
|--------|-------------|
| `make fmt` | Run go fmt |
| `make vet` | Run go vet |
| `make lint` | Run golangci-lint |
| `make security` | Run gosec security scanner |
| `make tidy` | Tidy go modules |

### Cluster Management
| Target | Description |
|--------|-------------|
| `make dev-cluster` | Create Kind dev cluster |
| `make dev-cluster-reset` | Delete and recreate dev cluster |
| `make dev-cluster-delete` | Delete dev cluster |
| `make test-cluster` | Create Kind test cluster |

## Project Structure

```text
.
├── api/v1beta1/           # CRD type definitions
├── cmd/                    # Operator entrypoint
├── config/                 # Kustomize manifests
│   ├── crd/bases/          # Generated CRD YAML
│   ├── overlays/dev/       # Dev deployment (neo4j-operator-dev namespace)
│   ├── overlays/prod/      # Prod deployment (neo4j-operator-system namespace)
│   └── overlays/integration-test/
├── internal/
│   ├── controller/         # Reconciliation logic
│   ├── resources/          # Kubernetes resource builders
│   ├── neo4j/              # Neo4j Bolt client
│   └── validation/         # Inline validation (no webhooks)
├── test/
│   ├── integration/        # Ginkgo integration tests
│   ├── fixtures/           # Test data
│   └── testutil/           # Test helpers
├── charts/neo4j-operator/  # Helm chart
├── hack/                   # Dev scripts and configs
│   ├── kind-config.yaml    # Kind cluster configuration
│   ├── setup-dev.sh        # Full dev environment setup
│   ├── Dockerfile.tilt     # Lightweight image for Tilt builds
│   └── smoke-test-standalone.yaml  # Manifest for smoke-test target
├── scripts/                # CI and operational scripts
├── examples/               # 67+ example manifests by use case
├── .devcontainer/          # GitHub Codespaces / VS Code Dev Container
├── .tool-versions          # Pinned tool versions for mise/asdf
├── Tiltfile                # Tilt live-reload configuration
├── Makefile                # Build, test, deploy targets
└── CLAUDE.md               # AI assistant guidance and project reference
```

## Coding Standards

### Go Style

- Follow [Effective Go](https://golang.org/doc/effective_go.html) and [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- Use `gofmt` and `goimports`
- Run `make fmt lint` before committing

### Kubernetes Conventions

- Follow [Kubernetes API Conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
- All validation is inline in controllers (`internal/validation/`), never via webhooks
- Use `retry.RetryOnConflict` for resource updates
- Use structured event constants from `internal/controller/events.go`

### Git Commit Messages

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add new feature description
fix: resolve issue with cluster formation
docs: update contributing guide
test: add integration test for backup
refactor: simplify reconciliation loop
chore: update dependency versions
```

Pre-commit hooks (if installed) validate commit message format automatically.

### Pre-commit Hooks (Optional)

```bash
# Install pre-commit framework
brew install pre-commit

# Install hooks for this repo
pre-commit install
pre-commit install --hook-type commit-msg
```

Hooks run on each commit: go fmt, goimports, go mod tidy, golangci-lint (lenient, soft-fail), staticcheck, gitleaks (secret detection), and commitizen (message format).

## Submitting Changes

1. **Commit** with a conventional commit message
2. **Push** to your fork
3. **Create a Pull Request** with:
   - Clear title and description
   - Reference to related issues
   - Test results or CI confirmation
4. **Trigger integration tests** if your change touches controllers or cluster logic (add the `run-integration-tests` label)

## Debugging

```bash
# Operator logs
make operator-logs

# Describe a custom resource
kubectl describe neo4jenterprisecluster <name>

# Check pod events
kubectl get events --sort-by='.lastTimestamp' -A

# Enable debug logging
kubectl patch -n neo4j-operator-dev deployment/neo4j-operator-controller-manager \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--mode=dev","--zap-log-level=debug"]}]}}}}'

# OOM troubleshooting
kubectl describe pod <pod-name> | grep -E "(OOMKilled|Memory|Exit.*137)"
```

## Getting Help

- **Issues**: [GitHub Issues](https://github.com/priyolahiri/neo4j-kubernetes-operator/issues)
- **Discussions**: [GitHub Discussions](https://github.com/priyolahiri/neo4j-kubernetes-operator/discussions)
- **Slack**: [Neo4j Community Slack](https://neo4j.com/slack/)

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
