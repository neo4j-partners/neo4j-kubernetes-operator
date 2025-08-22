# GitHub Actions Workflows

This directory contains streamlined GitHub Actions workflows for the Neo4j Kubernetes Operator with reusable composite actions to eliminate duplication.

## 🔄 Workflows

### 🧪 ci.yml - Main CI Pipeline
**Triggers:** Push to main/develop, Pull Requests, Manual dispatch
**Purpose:** Fast CI feedback with unit tests only

**Jobs:**
1. **Unit Tests** - ✅ Always run on all pushes/PRs (fast, no cluster required)

**Features:**
- ⚡ Extremely fast feedback - unit tests only
- 💾 No resource overhead from Kubernetes clusters
- 🔄 Clean and simple workflow focused on code quality
- 📊 Clear pass/fail status for essential code validation

### 🔬 integration-tests.yml - Integration Tests (On-Demand)
**Triggers:** Manual dispatch only
**Purpose:** Comprehensive integration testing with full operator deployment

**Jobs:**
1. **Integration Tests** - Full integration test suite with deployed operator
2. **Integration Test Summary** - Detailed status reporting with configuration info

**Features:**
- 🎛️ Configurable Neo4j version (default: 5.26-enterprise)
- ⏱️ Configurable timeout (default: 60 minutes)
- 🔄 Choice between full test suite or smoke tests only
- 🏗️ Complete operator build and deployment
- 📦 Comprehensive artifact collection (logs, cluster state)
- 🧹 Automatic cluster cleanup

### 🚀 release.yml - Release Pipeline
**Triggers:** Git tags (v*.*.*), Manual dispatch
**Purpose:** Automated release builds and GitHub releases

**Jobs:**
1. **Validate Release** - Run tests and build validation
2. **Build and Push** - Multi-arch container builds to ghcr.io
3. **Create Release** - GitHub release with manifests and release notes

**Features:**
- Multi-architecture support (amd64, arm64)
- Automated manifest bundling
- Generated release notes
- Container image publishing to GitHub Container Registry

## 🧩 Reusable Actions

### setup-go
**Location:** `.github/actions/setup-go/action.yml`
**Purpose:** Standardized Go environment setup with caching

**Features:**
- Go installation with configurable version
- Optimized Go module caching
- Automatic dependency downloads

### setup-k8s
**Location:** `.github/actions/setup-k8s/action.yml`
**Purpose:** Kubernetes testing environment setup

**Features:**
- Kind cluster creation with configurable version
- kubectl installation
- CRD installation and operator setup
- Cluster readiness verification

### collect-logs
**Location:** `.github/actions/collect-logs/action.yml`
**Purpose:** Comprehensive log collection for debugging

**Features:**
- Cluster state information
- Operator logs
- Event collection
- Automatic artifact upload on failure

## 📋 Environment Variables

The workflows use centralized environment variables for consistency:

```yaml
env:
  GO_VERSION: '1.22'          # Go version for all jobs
  KIND_VERSION: 'v0.20.0'     # Kind version (integration-tests.yml only)
  REGISTRY: ghcr.io           # Container registry (release.yml only)
```

## 🔧 Usage

### Running Tests Locally
```bash
# Unit tests (fast)
make test-unit

# Integration tests (requires cluster)
make test-cluster
make test-integration
make test-cluster-delete
```

### Triggering Workflows

**Unit Tests (ci.yml):** ✅ Run automatically on every push/PR

**Integration Tests (integration-tests.yml) - On-demand only:**
- **Manual Trigger**: Actions → "Integration Tests" → "Run workflow"
- **Configuration Options**:
  - **Neo4j Version**: Choose Neo4j version (default: `5.26-enterprise`)
  - **Timeout**: Set test timeout in minutes (default: 60)
  - **Test Suite**: Choose between full integration tests or smoke tests only
- **Features**: Complete operator deployment testing with comprehensive logging

**Release (release.yml):**
- Push git tag: `git tag v1.0.0 && git push origin v1.0.0`
- Manual: Use GitHub Actions UI with custom tag

## 🎯 Workflow Benefits

### Current Structure
- 3 focused workflow files (ci.yml, integration-tests.yml, release.yml)
- 3 reusable composite actions
- Clear separation of concerns
- Minimal resource usage for primary CI

### Key Benefits
- **Fast CI Feedback**: Unit tests run in < 5 minutes without Kubernetes overhead
- **Resource Efficient**: Integration tests only consume GitHub runner resources when needed
- **Flexible Testing**: Choose between full integration suite or smoke tests
- **Clear Purpose**: Each workflow has a single, well-defined responsibility

### Performance Improvements
- **Faster builds** through optimized caching
- **Reduced redundancy** with shared actions
- **Better maintainability** with centralized configuration
- **Consistent environments** across all jobs

## 🚀 Development Commands

```bash
# Environment setup
make dev-cluster              # Create dev cluster with cert-manager
make manifests generate       # Code generation

# Testing
make test-unit               # Unit tests only
make test-integration        # Integration tests (requires cluster)

# Cleanup
make dev-cluster-clean       # Clean operator resources
make dev-cluster-delete      # Delete dev cluster
```

## 🔍 Troubleshooting

### Workflow Failures
1. Check the **Actions** tab for detailed logs
2. Look for artifacts containing test results and logs
3. Integration test failures will include cluster logs

### Common Issues
- **Integration tests timeout:** Usually cert-manager deployment issues
- **Unit test failures:** Check for Go version compatibility
- **Release failures:** Verify tag format and permissions

### Debugging Commands
```bash
# Check workflow status
gh run list --workflow=ci.yml

# View specific run
gh run view <run-id>

# Download artifacts
gh run download <run-id>
```
