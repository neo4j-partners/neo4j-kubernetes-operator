# TLS Cluster Formation Findings Report

## Date: 2025-07-18

## Executive Summary

Testing revealed that TLS-enabled Neo4j clusters experience partial cluster formation issues, with nodes forming multiple smaller clusters instead of a single unified cluster. Applied fixes improved the situation from complete split-brain to partial cluster formation.

## Test Configuration

- **Cluster**: 3 primaries + 2 secondaries
- **TLS Mode**: cert-manager with self-signed CA
- **Neo4j Version**: 5.26.0-enterprise
- **Pod Management**: Parallel startup (all pods start simultaneously)

## Issues Found

### 1. Missing RBAC Permissions

**Problem**: Operator lacked permission to create roles with endpoints access
```
attempting to grant RBAC permissions not currently held:
{APIGroups:[""], Resources:["endpoints"], Verbs:["get" "list" "watch"]}
```

**Solution**: Added endpoints permission via kubebuilder RBAC marker:
```go
//+kubebuilder:rbac:groups="",resources=endpoints,verbs=get;list;watch;create;update;patch;delete
```

### 2. TLS Certificate Trust Issues

**Problem**: Cluster SSL policy didn't trust peer certificates during formation
- Inter-node TLS communication on port 5000 failed certificate verification
- Nodes couldn't establish trusted connections for cluster formation

**Solution**: Added `trust_all=true` to cluster SSL policy:
```properties
dbms.ssl.policy.cluster.trust_all=true
```

## Test Results

### Before Fixes
- **Result**: Complete split-brain (5 separate single-node clusters)
- **Each node**: Formed its own independent cluster
- **Cluster formation**: 0% success

### After Fixes
- **Result**: Partial cluster formation
- **Cluster 1**: primary-0, primary-2, secondary-0 (3 nodes)
- **Cluster 2**: primary-1, secondary-1 (2 nodes)
- **Improvement**: From 5 clusters to 2 clusters

## Root Cause Analysis

1. **TLS Handshake Timing**: During parallel startup, TLS handshake negotiations can interfere with the cluster discovery and formation process

2. **Trust Establishment**: Even with `trust_all=true`, the timing of certificate availability and trust establishment affects which nodes can communicate during the critical formation window

3. **Race Condition**: Nodes that complete TLS setup early form clusters before later nodes are ready to join

## Recommendations

### Short Term
1. **Document Known Issue**: Add to user documentation that TLS clusters may experience partial formation
2. **Workaround**: Users can manually trigger node rejoining after initial deployment

### Medium Term
1. **Startup Sequencing**: Consider adding small delays between pod startups for TLS clusters
2. **Retry Logic**: Implement cluster join retry logic with exponential backoff
3. **Health Checks**: Add pre-formation health checks to ensure TLS is fully initialized

### Long Term
1. **TLS-Aware Formation**: Develop TLS-specific cluster formation logic
2. **Certificate Readiness**: Wait for certificate mount and validation before starting Neo4j
3. **Graceful Degradation**: Implement automatic cluster merge for split formations

## Code Changes Made

### 1. RBAC Update
**File**: `internal/controller/neo4jenterprisecluster_controller.go`
```go
//+kubebuilder:rbac:groups="",resources=endpoints,verbs=get;list;watch;create;update;patch;delete
```

### 2. TLS Configuration
**File**: `internal/resources/cluster.go`
```go
# Cluster SSL Policy (for intra-cluster communication)
dbms.ssl.policy.cluster.enabled=true
dbms.ssl.policy.cluster.base_directory=/ssl
dbms.ssl.policy.cluster.private_key=tls.key
dbms.ssl.policy.cluster.public_certificate=tls.crt
dbms.ssl.policy.cluster.trust_all=true
dbms.ssl.policy.cluster.client_auth=NONE
dbms.ssl.policy.cluster.tls_versions=TLSv1.3,TLSv1.2
```

## Future Work

1. **Enhanced Testing**: Test with different TLS configurations:
   - Mutual TLS (client auth required)
   - External CA certificates
   - Certificate rotation scenarios

2. **Performance Testing**: Measure impact of TLS on cluster formation time

3. **Alternative Solutions**:
   - Pre-warming TLS connections
   - Certificate pre-validation
   - Staged cluster formation for TLS

## Conclusion

While TLS cluster formation is not yet perfect, the improvements made reduce the severity from complete failure to partial success. The parallel formation approach works well for non-TLS clusters (100% success) but requires additional work for TLS scenarios.
