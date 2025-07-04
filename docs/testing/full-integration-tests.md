# Full Integration Tests (E2E)

This document describes how to run the full end-to-end integration tests with the Neo4j operator deployed.

## Overview

The full integration tests workflow deploys the complete Neo4j operator stack and runs all integration tests, including those that require actual cluster reconciliation. This is more comprehensive than the standard CI integration tests which only run lightweight validation tests.

## Running the Tests

### Via GitHub UI

1. Go to the **Actions** tab in the GitHub repository
2. Select **"Full Integration Tests (E2E)"** from the workflow list
3. Click **"Run workflow"**
4. Configure the optional parameters:
   - **Operator image tag**: Specify a custom image tag (default: uses current commit)
   - **Neo4j version**: Neo4j version to test against (default: `5.26-enterprise`)
   - **Timeout minutes**: Maximum test duration (default: `60` minutes)
5. Click **"Run workflow"**

### Via GitHub CLI

```bash
# Run with defaults
gh workflow run integration-e2e.yml

# Run with custom parameters
gh workflow run integration-e2e.yml \
  -f operator-image-tag="v0.2.0" \
  -f neo4j-version="5.27-enterprise" \
  -f timeout-minutes="90"
```

### Via API

```bash
curl -X POST \
  -H "Authorization: token $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github.v3+json" \
  https://api.github.com/repos/$OWNER/$REPO/actions/workflows/integration-e2e.yml/dispatches \
  -d '{
    "ref": "main",
    "inputs": {
      "operator-image-tag": "v0.2.0",
      "neo4j-version": "5.26-enterprise",
      "timeout-minutes": "60"
    }
  }'
```

## What the Workflow Does

### 1. Environment Setup
- Creates a Kind Kubernetes cluster
- Installs kubectl and necessary tools
- Sets up Go environment

### 2. Operator Deployment
- Builds the Neo4j operator Docker image
- Loads the image into the Kind cluster
- Deploys the operator with full RBAC and webhooks
- Waits for the operator to be ready

### 3. Test Execution
- Runs all integration tests (including those requiring operator)
- Tests that were previously skipped now run:
  - Cluster lifecycle tests
  - Multi-cluster deployment tests
  - Auto-scaling tests
  - Plugin management tests
  - Query monitoring tests

### 4. Artifacts Collection
- Collects detailed cluster logs
- Saves test results and coverage reports
- Captures operator logs for debugging

## Expected Results

When the operator is properly deployed, you should see:

- ✅ **All 9 tests run** (instead of 2 passed + 7 skipped)
- ✅ **No tests skipped** due to missing operator
- ✅ **End-to-end validation** of operator functionality

## Troubleshooting

### Operator Not Starting

Check the operator logs in the workflow output:
```bash
kubectl logs -n neo4j-operator-system -l control-plane=controller-manager
```

### Tests Still Skipping

If tests are still being skipped, verify the operator deployment:
```bash
kubectl get deployment -n neo4j-operator-system
kubectl get pods -n neo4j-operator-system
```

### Timeout Issues

For complex tests, you may need to increase the timeout:
- Use the `timeout-minutes` input parameter
- Default is 60 minutes, consider 90+ for comprehensive testing

## Comparison with Standard CI

| Aspect | Standard CI | Full E2E Workflow |
|--------|-------------|-------------------|
| **Trigger** | Automatic (push/PR) | Manual (on-demand) |
| **Operator** | ❌ Not deployed | ✅ Fully deployed |
| **Tests Run** | 2 (lightweight only) | 9 (all tests) |
| **Duration** | ~5 minutes | ~15-30 minutes |
| **Purpose** | Quick validation | Comprehensive testing |

## When to Use

- **Before major releases** to validate full functionality
- **After operator changes** to ensure end-to-end compatibility
- **For debugging** integration test failures
- **During development** of new features requiring operator functionality

This workflow complements the standard CI by providing comprehensive validation when needed while keeping the regular CI fast and efficient.
