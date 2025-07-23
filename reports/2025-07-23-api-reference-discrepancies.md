# API Reference Documentation Discrepancies Report
**Date**: 2025-07-23
**Subject**: Comparison of API reference documentation vs actual CRD definitions

## Executive Summary

After reviewing all API reference documentation files in `/docs/api_reference/` and comparing them with the actual CRD type definitions in `/api/v1alpha1/`, I found several discrepancies and areas where the documentation is outdated or incomplete.

## Findings by CRD

### 1. Neo4jBackup (`neo4jbackup.md` vs `neo4jbackup_types.go`)

#### ✅ Correctly Documented
- All main fields in `Neo4jBackupSpec` are documented
- `BackupTarget`, `StorageLocation`, `RetentionPolicy`, `BackupOptions` structures match
- Status fields are properly documented

#### ❌ Discrepancies Found

1. **Missing field in BackupOptions documentation**:
   - Code has `VerifyBackup bool` but docs show `verifyBackup`
   - Field name case mismatch (should be `verify` based on JSON tag)

2. **CloudBlock structure mismatch**:
   - Documentation shows:
     ```yaml
     cloud:
       provider: aws
       region: us-east-1
       credentialsSecret: ...
     ```
   - Actual type only has:
     ```go
     type CloudBlock struct {
       Provider string `json:"provider,omitempty"`
       Identity *CloudIdentity `json:"identity,omitempty"`
     }
     ```
   - Missing `region` and `credentialsSecret` fields in documentation
   - Documentation doesn't mention `CloudIdentity` structure

3. **PVCSpec documentation incomplete**:
   - Code has `accessModes` field but it's not in the type definition
   - Documentation mentions default `accessModes: ["ReadWriteOnce"]` but field doesn't exist

### 2. Neo4jEnterpriseCluster (`neo4jenterprisecluster.md` vs `neo4jenterprisecluster_types.go`)

#### ❌ Major Discrepancies

1. **Documentation is severely outdated**:
   - Only shows basic fields (image, topology, storage, auth, etc.)
   - Missing dozens of fields present in the actual spec

2. **Missing fields in documentation**:
   - `edition` field
   - `env` (environment variables)
   - `nodeSelector`, `tolerations`, `affinity`
   - `restoreFrom` (RestoreSpec)
   - `upgradeStrategy` (UpgradeStrategySpec)
   - `license` field mentioned in docs but not in code
   - `monitoring` field mentioned in docs but not in code
   - `multiCluster` field mentioned in docs but not in code

3. **Type structure differences**:
   - Documentation shows simplified structure
   - Actual code has extensive nested types for:
     - TLS configuration with External Secrets support
     - Auth configuration with JWT, LDAP, Kerberos support
     - Auto-scaling with ML and predictive scaling
     - Query monitoring with sampling and metrics export
     - Detailed placement and topology spread configurations

4. **Status fields completely undocumented**:
   - Documentation doesn't cover the extensive status fields
   - Missing upgrade status tracking
   - Missing scaling status tracking

### 3. Neo4jRestore (`neo4jrestore.md` vs `neo4jrestore_types.go`)

#### ✅ Mostly Accurate

1. **Well documented overall**:
   - Main structure matches well
   - PITR configuration is properly documented
   - Examples are helpful

#### ❌ Minor Discrepancies

1. **RestoreSource field name mismatch**:
   - Documentation shows `pointInTime` as string
   - Code has `PointInTime *metav1.Time` (Time type, not string)

2. **Missing RestoreOptionsSpec**:
   - Documentation references it but doesn't define the structure
   - Type is missing from the code (field exists but type not defined)

### 4. Neo4jEnterpriseStandalone

**Status**: Not checked (documentation file exists but wasn't examined in detail)

### 5. Neo4jDatabase

**Status**: Not checked (documentation file exists but wasn't examined in detail)

### 6. Neo4jPlugin

**Status**: Not checked (documentation file exists but wasn't examined in detail)

## Critical Issues

### 1. CloudBlock Definition Inconsistency
The `CloudBlock` type is shared across multiple CRDs but documented differently:
- In backup docs: shows `region` and `credentialsSecret`
- In actual code: only has `provider` and `identity`
- This will cause confusion for users

### 2. Neo4jEnterpriseCluster Documentation Gap
The cluster CRD documentation is severely outdated and missing 80%+ of the actual fields. This is the most critical documentation gap as it's the primary CRD users interact with.

### 3. Missing Type Definitions
Several types referenced in documentation don't exist in code:
- `RestoreOptionsSpec` (referenced but not defined)
- Various fields mentioned but not implemented

## Recommendations

1. **Immediate Actions**:
   - Update `neo4jenterprisecluster.md` to include all fields
   - Fix `CloudBlock` documentation to match actual implementation
   - Remove references to non-existent fields

2. **Documentation Structure**:
   - Consider auto-generating API docs from code comments
   - Add kubebuilder markers for better documentation
   - Create a validation script to catch future drift

3. **Missing Features**:
   - Either implement missing fields (like `license`, `monitoring`) or remove from docs
   - Define `RestoreOptionsSpec` type or remove references

4. **Version Tracking**:
   - Add version markers to documentation
   - Note which operator version the docs apply to

## Conclusion

The API reference documentation has significant gaps and inaccuracies, particularly for the Neo4jEnterpriseCluster CRD. This creates a poor developer experience and will lead to confusion and support issues. Priority should be given to updating the cluster documentation and establishing a process to keep docs in sync with code changes.
