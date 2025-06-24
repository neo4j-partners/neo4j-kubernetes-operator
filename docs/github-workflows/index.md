# GitHub Workflows Documentation

This directory contains comprehensive documentation for the GitHub workflows used in the Neo4j Kubernetes Operator project.

## Overview

The Neo4j Kubernetes Operator uses several GitHub workflows for CI/CD, testing, and deployment. These workflows are designed with reliability, security, and maintainability in mind.

## Documentation Index

### 📋 [README.md](README.md)
Comprehensive guide covering all aspects of the GitHub workflows:
- Workflow overview and architecture
- Cluster setup and connectivity
- Required secrets and environment variables
- Workflow dependencies and relationships
- Fail-fast mechanisms
- Troubleshooting guide
- Security considerations
- Best practices

### 🔧 [workflow-improvements-summary.md](workflow-improvements-summary.md)
Detailed summary of recent improvements made to the workflows:
- Enhanced cluster creation and configuration
- Comprehensive connectivity checks
- Fail-fast mechanisms implementation
- Improved secrets management
- New reusable tools and scripts
- Testing procedures
- Future enhancement plans

## Workflow Files

The actual workflow files are located in `.github/workflows/`:

- **`ci.yml`** - Main continuous integration pipeline with enhanced testing
- **`static-analysis.yml`** - Code quality and security checks with race detection
- **`openshift-certification.yml`** - OpenShift compatibility testing with comprehensive test suite
- **`cluster-test.yml`** - Reusable cluster testing workflow
- **`test-validation.yml`** - Dedicated test validation suite with race detection

## Recent Improvements

### Test Infrastructure Enhancements
- **Data Race Fixes**: Resolved Gomega registration data race issues across test suites
- **Race Detection**: Added comprehensive race detection to all unit tests
- **Controller Improvements**: Enhanced backup controller with cloud storage support
- **Test Organization**: Improved test categorization (cluster vs non-cluster tests)
- **Error Handling**: Better error handling and cleanup procedures

### Workflow Enhancements
- **Conditional Test Execution**: Tests run based on cluster availability
- **Comprehensive Coverage**: Added dedicated test validation workflow
- **Better Reporting**: Enhanced test results and coverage reporting
- **Timeout Management**: Improved timeout handling for long-running tests

## Quick Start

### For Developers
1. Read the [README.md](README.md) for workflow overview
2. Review [workflow-improvements-summary.md](workflow-improvements-summary.md) for recent changes
3. Check the troubleshooting section for common issues

### For Contributors
1. Understand the workflow architecture from [README.md](README.md)
2. Review security considerations and best practices
3. Test workflows locally using the provided scripts

### For Maintainers
1. Review the workflow dependencies and relationships
2. Understand the fail-fast mechanisms and error handling
3. Monitor workflow performance and reliability metrics

## Test Categories

### Unit Tests (No Cluster Required)
- Controller tests with race detection
- Webhook validation tests
- Security coordinator tests
- Neo4j client tests
- Backup and restore functionality tests

### Integration Tests (Cluster Required)
- Cluster lifecycle management
- Enterprise features validation
- Failure scenario handling
- Multi-cluster operations

## Related Resources

- **Cluster Configuration**: `hack/kind-config.yaml`
- **Validation Scripts**: `scripts/validate-cluster.sh`
- **Makefile Targets**: See `Makefile` for cluster validation targets
- **Development Setup**: `hack/setup-dev.sh`

## Support

For issues related to GitHub workflows:
1. Check the troubleshooting section in [README.md](README.md)
2. Review the workflow logs for specific error messages
3. Test workflows locally using the provided validation scripts
4. Open an issue with detailed error information and context
