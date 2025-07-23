# README.md Updates Summary

**Date**: 2025-07-23

## Changes Made

### 1. Added Alpha Software Warning
Added a prominent warning box after the main description:
```markdown
> ⚠️ **ALPHA SOFTWARE WARNING**: This operator is currently in **alpha stage**. There may be breaking changes at any time due to ongoing development. For production use or evaluation, please use the [latest stable release](https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest) rather than the main branch code.
```

### 2. Removed Autoscaling Reference
- Removed the line mentioning "Auto-scaling: Horizontal Pod Autoscaler (HPA) integration with intelligent scaling logic" from the Operations & Automation section
- This aligns with the autoscaling functionality removal completed earlier

### 3. Updated Example Commands
Changed quick start examples to use release versions instead of main branch:
- For single-node deployment: Added curl command to download from releases
- For clustered deployment: Added curl command to download from releases
- For experts section: Added git clone command using latest release tag

### 4. Updated cert-manager Version Information
- Changed from "Version 1.5+" to "Version 1.5+ (tested with v1.18.2, required for TLS/SSL features)"
- This provides more accurate information about the tested version

## Verification

All statements in the README were verified for accuracy:
- ✅ Minimum cluster topology requirements (1 primary + 1 secondary OR 2+ primaries) - confirmed in topology_validator.go
- ✅ Support for Neo4j 5.26+ and 2025.x versions
- ✅ Kubernetes 1.21+ requirement
- ✅ Go 1.21+ for development
- ✅ Features list aligns with current implementation
- ✅ Performance metrics (99.8% API call reduction)
- ✅ All documentation links point to existing files

## Recommendations for Users

1. **For Production Use**: Always use the latest stable release, not the main branch
2. **For Development**: Clone the specific release tag to ensure consistency
3. **Breaking Changes**: Monitor release notes carefully as the operator is in alpha stage
4. **Examples**: Use versioned examples from releases rather than main branch URLs
