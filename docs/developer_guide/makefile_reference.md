# Makefile Reference Guide

This comprehensive reference covers all Make targets available in the Neo4j Kubernetes Operator project, organized by purpose and workflow.

## Table of Contents

- [Quick Start](#quick-start)
- [Target Categories](#target-categories)
- [General Targets](#general-targets)
- [Code Generation](#code-generation)
- [Packaging & Sync](#packaging-sync)
- [Testing](#testing)
- [Build & Images](#build-images)
- [Deployment](#deployment)
- [Development Environment](#development-environment)
- [Dependencies](#dependencies)
- [Code Quality](#code-quality)
- [Environment Variables](#environment-variables)
- [Workflows](#workflows)

## Quick Start

**Essential commands for getting started:**

```bash
# Get help with all available targets
make help

# Create development environment
make dev-cluster          # Create Kind cluster
make operator-setup       # Deploy operator

# Run tests
make test-unit            # Fast unit tests (no cluster)
make test-integration     # Full integration tests (auto-creates cluster)

# Build and deploy
make build                # Build operator binary
make docker-build         # Build container image
make deploy-dev           # Deploy to development namespace

# Pre-release / sync (after touching API types or +kubebuilder markers)
make sync-all             # Regenerate all artifacts (CRDs, RBAC, helm chart, kustomize lists)
make ship-prep            # sync-all + bundle + helm lint + CSV coverage check
make check-drift          # CI gate: fails if any generated file is stale
make bundle-release       # Bundle + real createdAt: timestamp (release workflow only)
make install-hooks        # One-time: install pre-commit hooks (runs check-drift before commit)
```

## Target Categories

The Makefile is organized into logical categories:

| Category | Purpose | Key Targets |
|----------|---------|-------------|
| **General** | Help and basic tasks | `help`, `all` |
| **Code Generation** | Generate Kubernetes manifests | `manifests`, `generate` |
| **Packaging & Sync** | Keep Helm chart, kustomize, OperatorHub bundle in sync with sources | `sync-all`, `ship-prep`, `check-drift` |
| **Testing** | Unit and integration testing | `test-unit`, `test-integration` |
| **Build & Images** | Build binaries and containers | `build`, `docker-build` |
| **Deployment** | Install and deploy operator | `install`, `deploy-dev`, `deploy-prod` |
| **Development Environment** | Local development setup | `dev-cluster`, `operator-setup` |
| **Dependencies** | Download and manage tools | `kustomize`, `controller-gen`, `yq` |
| **Code Quality** | Linting, formatting, security | `fmt`, `lint`, `security` |

## General Targets

### `make help`
**Description**: Display comprehensive help with all available targets
**Usage**: `make help`
**Dependencies**: None
**Example**:
```bash
make help
```

### `make all`
**Description**: Default target - builds the operator binary
**Usage**: `make all` or just `make`
**Dependencies**: `build`
**Example**:
```bash
make         # Same as 'make all'
make all     # Explicit call
```

## Code Generation

### `make manifests`
**Description**: Generate ClusterRole and CustomResourceDefinition objects from code annotations
**Usage**: `make manifests`
**Dependencies**: `controller-gen`
**Output**: Updates files in `config/crd/bases/` and RBAC manifests
**Example**:
```bash
make manifests
# Generates CRDs from api/v1beta1/*_types.go files
# Updates RBAC from controller annotations
```

### `make generate`
**Description**: Generate DeepCopy, DeepCopyInto, and DeepCopyObject method implementations
**Usage**: `make generate`
**Dependencies**: `controller-gen`
**Output**: Updates `*_deepcopy.go` files
**Example**:
```bash
make generate
# Generates DeepCopy methods for all API types
```

## Packaging & Sync

The operator ships through three channels: kustomize manifests (`config/`), a Helm chart (`charts/neo4j-operator/`), and an OperatorHub bundle (`bundle/`). Several files in those trees are generated from sources of truth elsewhere in the repo. The targets in this section regenerate them and verify they're in sync.

**TL;DR for adding a new CRD:**

```bash
# After editing api/v1beta1/*_types.go and adding +kubebuilder:rbac markers:
make ship-prep    # one command — runs every regeneration step
git add -A
git commit -m "..."
```

### `make sync-all`
**Description**: Run every code-generation and sync step (does not build the bundle).
Chains: `manifests` → `generate` → `sync-kustomize` → `sync-editor-viewer-roles` → `helm-sync-crds` → `helm-sync-rbac` → `helm-sync-artifacthub-crds`.
**Usage**: `make sync-all`
**When to run**: after touching any `api/v1beta1/*_types.go` file, any controller `+kubebuilder:rbac:` marker, or any `config/crd/bases/*.yaml` directly.

### `make ship-prep`
**Description**: Pre-release one-shot. Runs `sync-all` plus `bundle`, `helm-lint`, and `check-csv-coverage`.
**Usage**: `make ship-prep`
**When to run**: before tagging a release. Verifies the Helm chart lints, the OperatorHub bundle validates, and every CRD is registered in the CSV.

### `make check-drift`
**Description**: Regenerate every artifact and `git diff --exit-code` to fail if anything is stale. Used as a CI gate (`.github/workflows/ci.yml`).
**Usage**: `make check-drift`
**Notes**: `make bundle` pins the CSV's `createdAt:` to a stable placeholder (see `PINNED_CREATED_AT` in the Makefile), so two consecutive runs produce byte-identical output and concurrent PRs no longer conflict on the timestamp. The `-I '^    createdAt:'` filter is kept as belt-and-braces in case someone bypasses `make bundle` and calls `operator-sdk generate bundle` directly. If `check-drift` fails locally, run `make sync-all bundle` and commit the result. The same check runs as a `pre-commit` hook locally once you've run `make install-hooks` — see below.

### `make bundle-release`
**Description**: Same as `make bundle`, but stamps the real current UTC timestamp into the CSV's `createdAt:` annotation (overriding the pinned placeholder). Run this from the release workflow before publishing to OperatorHub so the bundle ships with a meaningful `createdAt:` for end-user tooling.
**Usage**: `make bundle-release`
**Notes**: only run from release CI — local invocations leave a real timestamp in `bundle/` that will conflict with every other concurrent PR. If you run it by accident, `make bundle` resets it to the placeholder.

### `make install-hooks`
**Description**: Install git pre-commit hooks (drift gate, fmt, lint, gitleaks, commitizen). Wraps `pre-commit install` and `pre-commit install --hook-type commit-msg`.
**Usage**: `make install-hooks` (one-time per clone)
**Prerequisite**: `pre-commit` on your `PATH` (`pip install pre-commit` or `brew install pre-commit`).
**What the drift hook does**: when staged files match `api/v1beta1/*.go`, `internal/controller/*_controller.go`, `scripts/{helm-sync-*,check-csv-coverage,update-alm-examples}*`, `config/manifests/bases/*.csv.yaml`, `config/{crd,rbac,samples,manifests}/`, `charts/neo4j-operator/`, or `bundle/`, the hook runs `make check-drift`. On stale artifacts the regenerated files are left in your working tree; `git add -u` and recommit.

### `make helm-sync-crds`
**Description**: Copy the kubebuilder-generated CRDs from `config/crd/bases/` into `charts/neo4j-operator/crds/` so the Helm install picks them up.
**Usage**: `make helm-sync-crds`
**Source**: `config/crd/bases/*.yaml`

### `make helm-sync-rbac`
**Description**: Regenerate `charts/neo4j-operator/templates/clusterrole.yaml` from the kubebuilder-generated `config/rbac/role.yaml` so the Helm chart grants exactly the same permissions as the controller's `+kubebuilder:rbac:` markers request.
**Usage**: `make helm-sync-rbac`
**Source**: `config/rbac/role.yaml` (regenerated by `make manifests`)

### `make helm-sync-artifacthub-crds`
**Description**: Regenerate the `artifacthub.io/crds` annotation in `Chart.yaml` from `config/crd/bases/`. Each CRD's user-facing description lives in a curated mapping in [`scripts/helm-sync-artifacthub-crds.sh`](https://github.com/neo4j-partners/neo4j-kubernetes-operator/blob/main/scripts/helm-sync-artifacthub-crds.sh) — the script exits non-zero if a CRD has no description, so adding a new CRD without updating the description map is impossible.
**Usage**: `make helm-sync-artifacthub-crds`

### `make sync-kustomize`
**Description**: Regenerate the `resources:` lists in `config/crd/kustomization.yaml` and `config/samples/kustomization.yaml` from on-disk filenames. Comments and other top-level keys preserved.
**Usage**: `make sync-kustomize`

### `make sync-editor-viewer-roles`
**Description**: Generate per-CRD `editor` and `viewer` ClusterRoles into `config/rbac/<crd>_{editor,viewer}_role.yaml` for every CRD in `config/crd/bases/`, and update `config/rbac/kustomization.yaml`. These are convenience cluster-roles end-users can bind to teams without granting cluster-admin; they're not used by the operator itself.
**Usage**: `make sync-editor-viewer-roles`

### `make check-csv-coverage`
**Description**: Verify that every CRD in `config/crd/bases/` is registered in both the source CSV (`config/manifests/bases/*.csv.yaml`) and the bundle CSV (`bundle/manifests/*.csv.yaml`). Fails non-zero with a list of missing kinds.
**Usage**: `make check-csv-coverage`
**Why**: the CSV's `customresourcedefinitions.owned` list is hand-curated (display names, descriptions need human input) — it's easy to forget to register a new CRD, which would silently ship a broken OperatorHub bundle.

### `make helm-lint`, `make helm-template`, `make helm-package`
Standard helm chart targets. `helm-package` depends on the helm syncs, so packaging never ships stale rules or annotations.

### `make bundle`
**Description**: Generate the OperatorHub bundle (CRDs, CSV, ClusterRoles, metadata) under `bundle/`. Also runs `operator-sdk bundle validate`.
**Usage**: `make bundle`
**Notes**: invokes `manifests` first by default. Set `SKIP_MANIFESTS=true` in CI to reuse pre-generated CRDs from a prior job.

### `make yq`
**Description**: Install yq (required by the sync scripts) into `bin/`. Pinned via `YQ_VERSION` in the Makefile.
**Usage**: `make yq`
**Notes**: invoked automatically by every sync target — you rarely call it directly.

### `make fmt`
**Description**: Format Go code using `go fmt`
**Usage**: `make fmt`
**Dependencies**: None
**Example**:
```bash
make fmt
# Formats all Go files in the project
```

### `make vet`
**Description**: Run `go vet` static analysis
**Usage**: `make vet`
**Dependencies**: None
**Example**:
```bash
make vet
# Reports suspicious constructs
```

### `make lint`
**Description**: Run golangci-lint with strict settings
**Usage**: `make lint`
**Dependencies**: `golangci-lint`
**Example**:
```bash
make lint
# Runs comprehensive linting checks
```

### `make lint-lenient`
**Description**: Run golangci-lint with relaxed settings (CI-friendly)
**Usage**: `make lint-lenient`
**Dependencies**: `golangci-lint`
**Example**:
```bash
make lint-lenient
# More permissive linting for CI environments
```

## Testing

> **Important**: All testing requires Kind (Kubernetes in Docker). Install Kind before running tests.

### Unit Testing

#### `make test-unit`
**Description**: Run fast unit tests without requiring a Kubernetes cluster
**Usage**: `make test-unit`
**Dependencies**: `manifests`, `generate`, `fmt`, `vet`, `envtest`
**Duration**: ~30 seconds
**Example**:
```bash
make test-unit
# ✅ Fast tests for controller logic
# ✅ No cluster setup required
# ✅ Includes coverage reporting
```

#### `make test-coverage`
**Description**: Generate detailed coverage report
**Usage**: `make test-coverage`
**Dependencies**: `manifests generate fmt vet envtest` (auto-resolved)
**Env**: `KUBEBUILDER_ASSETS` is set automatically from `ENVTEST_K8S_VERSION` — the controller suite needs it to start envtest. Running `go test -coverprofile ./...` directly without `make` requires you to export it yourself.
**Output**: `coverage/coverage.html`
**Example**:
```bash
make test-coverage
# Generates HTML coverage report
# Opens coverage/coverage.html in browser
```

### Integration Testing

#### `make test-integration`
**Description**: Run comprehensive integration tests with real Kubernetes API
**Usage**: `make test-integration`
**Dependencies**: `manifests generate test-cluster ginkgo kustomize` — the `manifests`/`generate` prereqs guarantee CRDs and RBAC deployed to the test cluster match the current controller source. Skipping `manifests` (e.g., by calling kustomize directly) silently deploys stale schemas.
**Duration**: ~10-15 minutes
**Features**:
- Auto-creates test cluster if needed
- Deploys operator automatically
- Tests real Neo4j deployments
- Includes plugin testing
- Cleanup handled automatically

**Example**:
```bash
make test-integration
# 🔄 Creates neo4j-operator-test cluster
# 📦 Builds and deploys operator
# 🧪 Runs full test suite
# 🧹 Automatic cleanup
```

#### `make test-integration-ci`
**Description**: Run essential integration tests optimized for CI environments
**Usage**: `make test-integration-ci`
**Dependencies**: Existing test cluster and deployed operator
**Duration**: ~5-8 minutes
**Features**:
- Assumes cluster and operator already deployed
- Skips resource-intensive tests
- Focuses on core functionality
- Optimized for CI resource constraints

**Example**:
```bash
make test-integration-ci
# 🚀 CI-optimized test suite
# ⚡ Essential tests only
# 💾 Reduces resource usage
```

#### `make test-integration-ci-full`
**Description**: Run complete integration test suite in CI environment
**Usage**: `make test-integration-ci-full`
**Dependencies**: Existing test cluster and deployed operator
**Duration**: ~15-20 minutes
**⚠️ **Warning**: May cause resource exhaustion in CI

**Example**:
```bash
make test-integration-ci-full
# ⚠️  Full test suite - use with caution in CI
# 🔋 High resource consumption
```

### Test Environment Management

#### `make test-setup`
**Description**: Setup test environment and prepare for testing
**Usage**: `make test-setup`
**Dependencies**: Test environment scripts
**Example**:
```bash
make test-setup
# Prepares test environment
# Creates necessary directories
# Sets up test configurations
```

#### `make test-cleanup`
**Description**: Clean up test environment artifacts while keeping cluster
**Usage**: `make test-cleanup`
**Features**:
- Removes test results and logs
- Cleans coverage files
- Preserves cluster for reuse
**Example**:
```bash
make test-cleanup
# Removes test-results/ coverage/ logs/ tmp/
# Cleans test-output.log and coverage files
# Keeps cluster running
```

#### `make test-cluster`
**Description**: Create dedicated Kind cluster for testing
**Usage**: `make test-cluster`
**Dependencies**: Kind installed
**Cluster Name**: `neo4j-operator-test`
**Variables** (Makefile defaults, overridable via `make VAR=...`):
- `KIND_NODE_IMAGE ?= kindest/node:v1.34.0` — pins the K8s version. Kept in sync with `ENVTEST_K8S_VERSION` so unit tests, dev cluster, and integration tests all run on the same K8s.
- `CERT_MANAGER_VERSION ?= v1.20.0` — pins the cert-manager release.

Both are threaded into `scripts/test-env.sh` via environment variables; the script enforces them as required (no fallback) so direct invocations can't drift from the Makefile defaults.

**Features**:
- Pre-configured with self-signed issuer
- Optimized for testing workloads

**Example**:
```bash
make test-cluster
# Creates neo4j-operator-test cluster
# Installs cert-manager
# Sets up TLS certificates
```

#### `make test-cluster-clean`
**Description**: Remove operator resources from test cluster (keep cluster running)
**Usage**: `make test-cluster-clean`
**Example**:
```bash
make test-cluster-clean
# Removes operator deployment
# Removes test namespaces
# Keeps cluster running
```

#### `make test-cluster-reset`
**Description**: Delete and recreate test cluster
**Usage**: `make test-cluster-reset`
**Dependencies**: `test-cluster-delete`, `test-cluster`
**Example**:
```bash
make test-cluster-reset
# Complete cluster refresh
# Preserves no state
```

#### `make test-cluster-delete`
**Description**: Delete test cluster completely
**Usage**: `make test-cluster-delete`
**Example**:
```bash
make test-cluster-delete
# Removes neo4j-operator-test cluster
```

#### `make test-destroy`
**Description**: Complete test environment cleanup
**Usage**: `make test-destroy`
**Example**:
```bash
make test-destroy
# Removes all test artifacts
# Deletes test cluster
# Cleans temporary files
```

### Comprehensive Testing

#### `make test`
**Description**: Run complete test suite (unit + integration)
**Usage**: `make test`
**Dependencies**: `test-unit`, `test-integration`
**Duration**: ~15-20 minutes
**Example**:
```bash
make test
# ✅ Unit tests
# ✅ Integration tests
# ✅ Full validation
```

#### `make test-ci-local` 🆕
**Description**: Emulate GitHub Actions CI workflow locally with comprehensive debug logging
**Usage**: `make test-ci-local`
**Duration**: ~20-25 minutes
**Features**:
- **Complete CI emulation**: Uses `CI=true GITHUB_ACTIONS=true` environment
- **Resource constraints**: Tests with 512Mi memory limits (same as CI)
- **Debug logging**: Comprehensive logs saved to `logs/` directory
- **Automatic troubleshooting**: Provides debugging commands on failure
- **Self-contained**: Creates, tests, and destroys environment

**Output Files**:
- `logs/ci-local-unit.log` - Unit test output with environment info
- `logs/ci-local-integration.log` - Integration test execution
- `logs/ci-local-cleanup.log` - Environment cleanup

**Example**:
```bash
make test-ci-local
# 🔄 Phase 1: Unit tests with CI environment
# 🔗 Phase 2: Integration tests with CI constraints
# 🧹 Phase 3: Complete cleanup
# 📁 Debug logs in logs/ directory
```

**When to use**:
- Debugging CI failures locally
- Testing resource-constrained scenarios
- Validating changes before CI push
- Reproducing CI-specific issues

## Build & Images

### `make build`
**Description**: Build the operator binary
**Usage**: `make build`
**Dependencies**: `manifests`, `generate`, `fmt`, `vet`
**Output**: `bin/manager`
**Example**:
```bash
make build
# Builds bin/manager executable
# Includes all code generation
```

### `make docker-build`
**Description**: Build Docker image with operator
**Usage**: `make docker-build [IMG=<image-name>]`
**Dependencies**: None
**Default Image**: `controller:latest`
**Example**:
```bash
make docker-build
# Builds controller:latest image

make docker-build IMG=my-operator:v1.0
# Builds custom image name
```

### `make docker-push`
**Description**: Push Docker image to registry
**Usage**: `make docker-push [IMG=<image-name>]`
**Dependencies**: `docker-build`
**Example**:
```bash
make docker-push IMG=ghcr.io/my-org/neo4j-operator:latest
# Pushes image to GitHub Container Registry
```

## Deployment

> **Critical**: The operator **must** run inside the Kubernetes cluster. Running outside the cluster causes DNS resolution failures.

### CRD Management

#### `make install`
**Description**: Install CustomResourceDefinitions into cluster
**Usage**: `make install`
**Dependencies**: `manifests`, `kustomize`
**Example**:
```bash
make install
# Installs all CRDs to current cluster context
```

#### `make uninstall`
**Description**: Remove CRDs from cluster
**Usage**: `make uninstall`
**Dependencies**: `manifests`, `kustomize`
**⚠️ **Warning**: This will delete all Neo4j instances
**Example**:
```bash
make uninstall
# Removes all CRDs and instances
```

### Operator Deployment

#### `make deploy-dev`
**Description**: Deploy operator with development configuration using local images
**Usage**: `make deploy-dev`
**Dependencies**: `deploy-dev-local`
**Namespace**: `neo4j-operator-dev`
**Image**: `neo4j-operator:dev` (built locally)
**Features**:
- Auto-loads image to Kind cluster
- Development-friendly settings
- Enhanced logging

**Example**:
```bash
make deploy-dev
# 🔨 Builds neo4j-operator:dev image
# 📦 Loads to Kind cluster
# 🚀 Deploys to neo4j-operator-dev namespace
```

#### `make deploy-prod`
**Description**: Deploy operator with production configuration using local images
**Usage**: `make deploy-prod`
**Dependencies**: `deploy-prod-local`
**Namespace**: `neo4j-operator-system`
**Image**: `neo4j-operator:latest` (built locally)
**Features**:
- Production-grade settings
- Resource limits applied
- Standard logging levels

**Example**:
```bash
make deploy-prod
# 🔨 Builds neo4j-operator:latest image
# 📦 Loads to Kind cluster
# 🚀 Deploys to neo4j-operator-system namespace
```

#### `make deploy-dev-local`
**Description**: Build and deploy controller with local dev image to Kind cluster
**Usage**: `make deploy-dev-local`
**Dependencies**: `manifests`, `kustomize`, `docker-build`
**Features**:
- Builds `neo4j-operator:dev` image locally
- Auto-detects and loads to available Kind cluster
- Deploys to `neo4j-operator-dev` namespace
**Example**:
```bash
make deploy-dev-local
# Explicit local build and deploy
# Same as deploy-dev but more explicit
```

#### `make deploy-prod-local`
**Description**: Build and deploy controller with local prod image to Kind cluster
**Usage**: `make deploy-prod-local`
**Dependencies**: `manifests`, `kustomize`, `docker-build`
**Features**:
- Builds `neo4j-operator:latest` image locally
- Auto-detects and loads to available Kind cluster
- Deploys to production namespace
**Example**:
```bash
make deploy-prod-local
# Explicit local build and deploy
# Same as deploy-prod but more explicit
```

#### `make deploy-dev-registry`
**Description**: Deploy development configuration using registry image
**Usage**: `make deploy-dev-registry`
**Dependencies**: `manifests`, `kustomize`
**Image Source**: Container registry (requires authentication)
**Example**:
```bash
make deploy-dev-registry
# 📥 Pulls from ghcr.io registry
# 🚀 Deploys dev configuration
```

#### `make deploy-prod-registry`
**Description**: Deploy production configuration using registry image
**Usage**: `make deploy-prod-registry`
**Dependencies**: `manifests`, `kustomize`
**Image Source**: Container registry (requires authentication)
**Example**:
```bash
make deploy-prod-registry
# 📥 Pulls from ghcr.io registry
# 🚀 Deploys production configuration
```

### Deployment Removal

#### `make undeploy-dev`
**Description**: Remove development operator deployment
**Usage**: `make undeploy-dev`
**Example**:
```bash
make undeploy-dev
# Removes development operator
# Keeps CRDs and instances
```

#### `make undeploy-prod`
**Description**: Remove production operator deployment
**Usage**: `make undeploy-prod`
**Example**:
```bash
make undeploy-prod
# Removes production operator
# Keeps CRDs and instances
```

## Development Environment

> **Mandatory**: This project exclusively uses Kind for development. Install Kind before using development targets.

### Cluster Management

#### `make dev-cluster`
**Description**: Create Kind cluster for development
**Usage**: `make dev-cluster`
**Cluster Name**: `neo4j-operator-dev`
**Dependencies**: Kind installed
**Features**:
- Includes cert-manager v1.20.0
- Self-signed ClusterIssuer for TLS
- Development-optimized configuration

**Example**:
```bash
make dev-cluster
# Creates neo4j-operator-dev cluster
# Installs cert-manager
# Sets up development certificates
```

#### `make dev-cluster-clean`
**Description**: Remove operator resources from development cluster
**Usage**: `make dev-cluster-clean`
**Example**:
```bash
make dev-cluster-clean
# Removes operator deployment
# Removes CRDs and instances
# Keeps cluster running
```

#### `make dev-cluster-reset`
**Description**: Reset development cluster (delete and recreate)
**Usage**: `make dev-cluster-reset`
**Dependencies**: `dev-cluster-delete`, `dev-cluster`
**Example**:
```bash
make dev-cluster-reset
# Complete cluster refresh
# Loses all state and data
```

#### `make dev-cluster-delete`
**Description**: Delete development cluster
**Usage**: `make dev-cluster-delete`
**Example**:
```bash
make dev-cluster-delete
# Removes neo4j-operator-dev cluster
```

#### `make dev-cleanup`
**Description**: Clean up development environment completely (keeps cluster)
**Usage**: `make dev-cleanup`
**Dependencies**: Development cleanup script
**Example**:
```bash
make dev-cleanup
# Runs hack/cleanup-dev.sh
# Removes artifacts but keeps cluster
# Useful for resetting state
```

#### `make dev-destroy`
**Description**: Complete development environment destruction
**Usage**: `make dev-destroy`
**Example**:
```bash
make dev-destroy
# Removes all development artifacts
# Deletes development cluster
# Complete cleanup
```

### Operator Management

#### `make operator-setup`
**Description**: Automated operator deployment to available Kind cluster
**Usage**: `make operator-setup`
**Features**:
- **Auto-detection**: Finds available Kind cluster (dev or test)
- **Smart deployment**: Chooses appropriate configuration
- **Status verification**: Confirms successful deployment
- **Error handling**: Provides troubleshooting guidance

**Example**:
```bash
make operator-setup
# 🔍 Detects available Kind cluster
# 📦 Builds and loads appropriate image
# 🚀 Deploys operator
# ✅ Verifies deployment status
```

#### `make operator-setup-interactive`
**Description**: Interactive operator deployment with user prompts
**Usage**: `make operator-setup-interactive`
**Example**:
```bash
make operator-setup-interactive
# 💬 Interactive cluster selection
# 🎛️  Configuration options
# 📋 Detailed status reporting
```

#### `make operator-status`
**Description**: Display comprehensive operator status
**Usage**: `make operator-status`
**Example**:
```bash
make operator-status
# 📊 Operator deployment status
# 🔍 Pod health and logs
# 📈 Resource usage
```

#### `make operator-logs`
**Description**: Follow operator logs in real-time
**Usage**: `make operator-logs`
**Example**:
```bash
make operator-logs
# 📋 Real-time log streaming
# 🔍 Filtered for relevant events
```

### Demo Environment

The demo deploys a TLS-enabled standalone instance and a 3-node TLS-enabled cluster, creates databases with sample data, and demonstrates external access.

#### `make demo-setup`
**Description**: Set up complete demo environment (Kind cluster + cert-manager + operator). Lists existing clusters that will be destroyed and asks for confirmation.
**Usage**: `make demo-setup`

#### `make demo`
**Description**: Run interactive operator demo with full environment setup. Asks for confirmation before destructive steps (destroying existing clusters, deleting previous demo resources). At the end, prompts whether to clean up.
**Usage**: `make demo`
**Includes**: Environment setup (interactive)

#### `make demo-fast`
**Description**: Run automated demo without confirmations (includes setup)
**Usage**: `make demo-fast`
**Includes**: Environment setup (auto-confirmed)

#### `make demo-only`
**Description**: Run fast demo without environment setup (assumes cluster and operator exist)
**Usage**: `make demo-only`

#### `make demo-interactive`
**Description**: Run interactive demo without environment setup (assumes cluster and operator exist)
**Usage**: `make demo-interactive`

#### `make demo-cleanup`
**Description**: Clean up all demo resources (databases, standalone, cluster, admin secret) without running the demo
**Usage**: `make demo-cleanup`

#### Demo script flags

The demo script (`scripts/demo.sh`) accepts these flags:

| Flag | Description |
|------|-------------|
| `--skip-confirmations` | Skip all interactive prompts |
| `--cleanup` | Automatically clean up demo resources after completion |
| `--cleanup-only` | Only clean up resources from a previous demo run |
| `--speed fast\|normal\|slow` | Control demo pacing |
| `--namespace NAMESPACE` | Kubernetes namespace (default: `default`) |
| `--password PASSWORD` | Admin password (default: `demo123456`) |

## Dependencies

The Makefile automatically manages tool dependencies:

### Bundle and Catalog Management

#### `make opm`
**Description**: Download OPM (Operator Package Manager) tool
**Usage**: `make opm`
**Location**: `bin/opm`
**Example**:
```bash
make opm
# Downloads opm for catalog management
```

#### `make bundle`
**Description**: Generate operator bundle manifests
**Usage**: `make bundle [VERSION=<version>]`
**Dependencies**: `manifests`, `kustomize`, `operator-sdk`
**Example**:
```bash
make bundle VERSION=0.1.0
# Generates bundle for version 0.1.0
```

#### `make bundle-build`
**Description**: Build bundle image
**Usage**: `make bundle-build [BUNDLE_IMG=<image>]`
**Example**:
```bash
make bundle-build BUNDLE_IMG=my-bundle:v0.1.0
# Builds bundle container image
```

#### `make bundle-push`
**Description**: Push bundle image to registry
**Usage**: `make bundle-push [BUNDLE_IMG=<image>]`
**Example**:
```bash
make bundle-push BUNDLE_IMG=ghcr.io/org/bundle:v0.1.0
# Pushes bundle to registry
```

#### `make catalog-build`
**Description**: Build a catalog image for OLM
**Usage**: `make catalog-build [CATALOG_IMG=<image>] [BUNDLE_IMGS=<bundles>]`
**Dependencies**: `opm`
**Example**:
```bash
make catalog-build CATALOG_IMG=my-catalog:v1.0
# Builds catalog with bundle images
```

#### `make catalog-push`
**Description**: Push catalog image to registry
**Usage**: `make catalog-push [CATALOG_IMG=<image>]`
**Example**:
```bash
make catalog-push CATALOG_IMG=ghcr.io/my-org/catalog:v1.0
# Pushes catalog to registry
```

### Core Tools

#### `make kustomize`
**Description**: Download kustomize for Kubernetes manifest management
**Version**: v5.4.3
**Location**: `bin/kustomize`

#### `make controller-gen`
**Description**: Download controller-gen for code generation
**Version**: v0.19.0
**Location**: `bin/controller-gen`

#### `make envtest`
**Description**: Download setup-envtest for testing
**Version**: release-0.19
**Location**: `bin/setup-envtest`

#### `make golangci-lint`
**Description**: Download golangci-lint for code quality
**Version**: v1.64.8
**Location**: `bin/golangci-lint`

#### `make ginkgo`
**Description**: Download Ginkgo BDD testing framework
**Version**: v2.29.0
**Location**: `bin/ginkgo`

#### `make operator-sdk`
**Description**: Download Operator SDK for bundle management
**Version**: v1.39.1
**Location**: `bin/operator-sdk`

## Code Quality

### `make security`
**Description**: Run security analysis with gosec
**Usage**: `make security`
**Features**:
- Auto-installs gosec if needed
- Scans for security vulnerabilities
- Reports potential issues

**Example**:
```bash
make security
# 🛡️  Security vulnerability scan
# 📋 Detailed security report
```

### `make tidy`
**Description**: Clean up Go module dependencies
**Usage**: `make tidy`
**Example**:
```bash
make tidy
# 🧹 Removes unused dependencies
# ✅ Verifies module integrity
```

### `make clean`
**Description**: Clean all build artifacts and temporary files
**Usage**: `make clean`
**Removes**:
- `bin/` directory
- `tmp/` directory
- Coverage files
- Build logs
- Test artifacts

**Example**:
```bash
make clean
# 🧹 Complete cleanup
# 🗑️  Removes all build artifacts
```

## Environment Variables

### Image Configuration
- `IMG`: Container image name (default: `controller:latest`)
- `VERSION`: Project version (default: `0.0.1`)
- `CONTAINER_TOOL`: Container tool (default: `docker`)

### Tool Configuration
- `KUBECONFIG`: Kubernetes config file location
- `LOCALBIN`: Local tool installation directory (default: `bin/`)
- `GOBIN`: Go binary installation directory

### Testing Configuration
- `CI`: Enable CI mode (affects resource limits)
- `GITHUB_ACTIONS`: Enable GitHub Actions mode
- `ENVTEST_K8S_VERSION`: Kubernetes version for testing (1.34.0)

### Bundle Configuration
- `CHANNELS`: Bundle channels for OLM
- `DEFAULT_CHANNEL`: Default bundle channel
- `BUNDLE_IMG`: Bundle image name

**Examples**:
```bash
# Custom image build
make docker-build IMG=my-registry/neo4j-operator:v2.0

# CI-mode testing
CI=true make test-unit

# Custom tool location
LOCALBIN=/usr/local/bin make kustomize
```

## Workflows

### Complete Development Setup
```bash
# 1. Create development environment
make dev-cluster           # Create Kind cluster
make operator-setup        # Deploy operator

# 2. Develop and test
make test-unit            # Fast feedback loop
make build                # Build changes
make deploy-dev           # Update deployment

# 3. Comprehensive testing
make test-integration     # Full validation
make test-ci-local        # CI simulation
```

### Production Deployment Workflow
```bash
# 1. Validate code quality
make fmt vet lint         # Code formatting and analysis
make security             # Security scanning

# 2. Test comprehensively
make test                 # Unit + integration tests
make test-coverage        # Verify coverage

# 3. Build and deploy
make docker-build         # Build production image
make deploy-prod          # Production deployment
```

### CI/CD Emulation
```bash
# Reproduce CI failures locally
make test-ci-local        # Complete CI workflow
# Check logs/ci-local-*.log for detailed analysis

# Debug specific CI issues
CI=true GITHUB_ACTIONS=true make test-unit
```

### Testing Workflow
```bash
# Quick development testing
make test-unit            # ~30 seconds

# Comprehensive validation
make test-cluster         # Create test environment
make test-integration     # ~15 minutes
make test-cluster-clean   # Clean resources

# CI preparation
make test-ci-local        # Full CI emulation
```

### Cleanup Workflows
```bash
# Development cleanup
make dev-cluster-clean    # Remove operator only
make dev-destroy          # Complete destruction

# Test cleanup
make test-cluster-clean   # Remove test resources
make test-destroy         # Complete test cleanup

# Complete cleanup
make clean                # Remove build artifacts
```

## Common Patterns

### Error Recovery
```bash
# If cluster is in bad state
make dev-cluster-reset    # or test-cluster-reset
make operator-setup

# If operator is misbehaving
make undeploy-dev         # or undeploy-prod
make deploy-dev           # or deploy-prod
```

### Development Iteration
```bash
# Fast iteration cycle
make test-unit            # Quick validation
make build                # Build changes
make docker-build         # Update image
make deploy-dev           # Deploy changes

# Full validation cycle
make fmt vet              # Code quality
make test                 # Comprehensive testing
make deploy-prod          # Production deployment
```

### Troubleshooting
```bash
# Check operator status
make operator-status      # Deployment overview
make operator-logs        # Real-time logs

# Debug testing issues
make test-ci-local        # Reproduce CI environment
# Check logs/ci-local-*.log

# Clean slate approach
make dev-destroy          # Complete restart
make dev-cluster
make operator-setup
```

---

## Best Practices

1. **Always use Kind**: This project exclusively supports Kind for development
2. **Run tests before commits**: Use `make test-unit` for fast feedback
3. **Test integration changes**: Use `make test-integration` for comprehensive validation
4. **Emulate CI locally**: Use `make test-ci-local` before pushing to CI
5. **Keep dependencies current**: Tools are auto-managed and version-pinned
6. **Use appropriate deployment**: `deploy-dev` for development, `deploy-prod` for production testing
7. **Clean up regularly**: Use cleanup targets to prevent resource exhaustion
8. **Monitor operator logs**: Use `make operator-logs` for real-time debugging

For additional help, run `make help` or consult the [Contributing Guide](contributing.md).
