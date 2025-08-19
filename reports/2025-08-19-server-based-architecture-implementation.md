# Server-Based Architecture Implementation Report

**Date**: 2025-08-19
**Status**: COMPLETED
**Impact**: Major architectural change from primary/secondary StatefulSets to unified server-based deployment

## Overview

This report documents the implementation of the server-based architecture for Neo4j Enterprise clusters, replacing the previous primary/secondary StatefulSet approach with a unified server deployment model and centralized backup system.

## Architecture Change Details

### Before (Legacy - Primary/Secondary StatefulSets)
```yaml
# DEPRECATED APPROACH
topology:
  primaries: 3
  secondaries: 2
```
- Separate `{cluster-name}-primary` and `{cluster-name}-secondary` StatefulSets
- Pre-assigned pod roles at infrastructure level
- Complex topology management and scaling logic
- Backup sidecars in each server pod (expensive)

### After (Current - Server-Based Architecture)
```yaml
# NEW APPROACH
topology:
  servers: 5  # Self-organize into primary/secondary roles
```
- Single `{cluster-name}-server` StatefulSet
- Servers auto-assign roles based on database topology requirements
- Simplified infrastructure, flexible role assignment
- Centralized backup StatefulSet

## Implementation Details

### StatefulSet Architecture
- **Single StatefulSet**: `{cluster-name}-server` with `replicas: N`
- All server pods use identical configuration and auto-discover roles
- **Pod Names**: `{cluster-name}-server-0`, `{cluster-name}-server-1`, `{cluster-name}-server-2`
- Servers self-organize and are role-agnostic at infrastructure level

### Centralized Backup System
- **Architecture**: Single backup StatefulSet per cluster (replaces expensive per-pod sidecars)
- **Resource Efficiency**: 100m CPU/256Mi memory for entire cluster vs N×200m CPU/512Mi per sidecar
- **Connectivity**: Connects to cluster via client service using Bolt protocol
- **Benefits**: No coordination issues, centralized storage, single point of monitoring

### Cluster Formation Process
1. All server pods start simultaneously (`ParallelPodManagement`)
2. First server(s) to start form the initial cluster
3. Additional servers join the existing cluster
4. Databases are created with specified primary/secondary topology
5. Neo4j automatically assigns database hosting to appropriate servers

## Benefits Achieved

1. **Simplified Operations**: Single StatefulSet reduces complexity vs separate primary/secondary StatefulSets
2. **Role Flexibility**: Servers adapt to database needs rather than pre-assigned infrastructure roles
3. **Better Resource Usage**: Servers can host multiple database roles based on actual requirements
4. **Easier Scaling**: Scale server pool independently of database topology requirements
5. **Reduced Configuration**: Identical pod configuration simplifies management
6. **Centralized Backup**: Single backup instance vs expensive N backup sidecars

## Migration Impact

### API Changes
- **Cluster Topology**: Use `servers: N` instead of `primaries: X, secondaries: Y`
- **Database Topology**: Still uses `primaries: X, secondaries: Y` for database allocation
- **Backup Configuration**: Enables centralized backup StatefulSet (not sidecars)

### Operational Changes
- **StatefulSet Names**: Single `{cluster-name}-server` StatefulSet per cluster
- **Pod Names**: Standard StatefulSet naming: `{cluster-name}-server-0`, `{cluster-name}-server-1`
- **Backup Pods**: New `{cluster-name}-backup-0` pod for backup operations
- **Scaling**: Scale by updating `cluster.spec.topology.servers`

## Files Modified

### Core Implementation
- `internal/resources/cluster.go` - Added centralized backup functions, removed backup sidecars
- `internal/controller/neo4jenterprisecluster_controller.go` - Added backup StatefulSet creation

### Tests
- `test/integration/centralized_backup_test.go` - Complete rewrite for centralized approach
- `test/integration/cluster_lifecycle_test.go` - Fixed duplicate function issue

### Documentation
- `CLAUDE.md` - Streamlined to essential information only

## Troubleshooting Commands

### Check Cluster Resources
```bash
# Check cluster StatefulSet
kubectl get statefulset <cluster-name>-server

# Check backup StatefulSet (if backups enabled)
kubectl get statefulset <cluster-name>-backup

# View all cluster pods (servers + backup)
kubectl get pods -l neo4j.com/cluster=<cluster-name>
```

### Verify Cluster Formation
```bash
# Check cluster formation status
kubectl exec <cluster-name>-server-0 -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"

# Check cluster database distribution
kubectl exec <cluster-name>-server-0 -- cypher-shell -u neo4j -p <password> "SHOW DATABASES"
```

### Backup Debugging
```bash
# Check backup pod status
kubectl get pods -l neo4j.com/component=backup

# Check backup logs
kubectl logs <cluster-name>-backup-0

# Test backup functionality
kubectl exec <cluster-name>-backup-0 -- sh -c 'echo "{\"path\":\"/backups/test\",\"type\":\"FULL\"}" > /backup-requests/backup.request'
```

## Verification Results

- **Build Status**: ✅ All compilation successful
- **Architecture**: ✅ Server-based unified StatefulSet confirmed
- **Backup System**: ✅ Centralized backup StatefulSet implementation complete
- **Database Operations**: ✅ Neo4jDatabase controller works correctly
- **External Access**: ✅ Client service and external connectivity verified
- **Tests**: ✅ All backup tests updated and compiling

## Next Steps

- Monitor production deployments for resource efficiency gains
- Gather user feedback on simplified operational model
- Consider additional backup storage options and retention policies
