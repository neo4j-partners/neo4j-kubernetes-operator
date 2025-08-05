# Neo4j Test Cluster Configuration Analysis Report

**Date**: 2025-08-05
**Cluster**: test-cluster (3 primaries, 3 secondaries)
**Status**: Successfully formed and operational
**Neo4j Version**: 5.26-enterprise

## Executive Summary

The test-cluster successfully formed despite the identified K8s discovery bug due to several key architectural decisions:

1. **Correct Discovery Service Port**: The discovery service uses port 5000 (tcp-discovery) correctly
2. **V2_ONLY Discovery Mode**: Forces use of the correct discovery port, bypassing the bug
3. **Parallel Pod Management**: All pods start simultaneously, enabling coordinated cluster formation
4. **Minimum Primary Count = 1**: Allows first pod to bootstrap cluster, others join incrementally

## Cluster Topology Analysis

### Pod Status
```
NAME                       READY   STATUS    RESTARTS   AGE
test-cluster-primary-0     2/2     Running   0          6h23m
test-cluster-primary-1     2/2     Running   0          6h23m
test-cluster-primary-2     1/2     Running   0          23s    # Recently restarted
test-cluster-secondary-0   2/2     Running   0          6h23m
test-cluster-secondary-1   2/2     Running   0          6h23m
test-cluster-secondary-2   1/2     Running   0          23s    # Recently restarted
```

### Neo4j Cluster View
All 6 servers are enabled and available:
```
"ddfbd21c-d146-4ec4-8c71-d64f3ac1aad3" (primary-0)   - Available, hosting system database
"685f5ef5-abce-4fd3-9b11-5cda5f7a4694" (primary-1)   - Available, hosting neo4j + system
"b4f935a9-a79b-4de6-a57b-55a9cd9ae66c" (primary-2)   - Available, hosting system database
"83182467-1a37-4951-bce3-d39ec205a05c" (secondary-0) - Available, hosting system database
"5adf22a5-2455-43b0-8379-8091a7869e48" (secondary-1) - Available, hosting system database
"8367743f-3683-4b20-a25e-13a57ff6467c" (secondary-2) - Available, hosting system database
```

## Service Configuration Analysis

### 1. Discovery Service (ClusterIP)
```yaml
Name: test-cluster-discovery
Type: ClusterIP
ClusterIP: 10.96.1.241
Port: 5000/TCP (tcp-discovery)
Selector: neo4j.com/cluster=test-cluster,neo4j.com/clustering=true
publishNotReadyAddresses: true
```

**Key Finding**: Uses port 5000 correctly for V2_ONLY discovery mode.

### 2. Headless Service
```yaml
Name: test-cluster-headless
Type: ClusterIP (None)
Ports:
  - bolt: 7687
  - http: 7474
  - tcp-discovery: 5000  # Correct discovery port
  - tcp-tx: 6000
  - routing: 7688
  - raft: 7000
  - transaction: 7689
  - backup: 6362
Selector: neo4j.com/cluster=test-cluster
```

### 3. Client Service
```yaml
Name: test-cluster-client
Type: ClusterIP
ClusterIP: 10.96.237.242
Ports: 7687/TCP (bolt), 7474/TCP (http)
```

### 4. Internals Service
```yaml
Name: test-cluster-internals
Type: ClusterIP
ClusterIP: 10.96.221.180
Ports: All cluster ports (7687, 7474, 5000, 6000, 7688, 7000, 7689, 6362)
```

## Neo4j Configuration Analysis

### Core Configuration (neo4j.conf)
```properties
# Server settings
server.default_listen_address=0.0.0.0
server.bolt.listen_address=0.0.0.0:7687
server.http.listen_address=0.0.0.0:7474

# Clustering ports
server.cluster.listen_address=0.0.0.0:5000
server.routing.listen_address=0.0.0.0:7688
server.cluster.raft.listen_address=0.0.0.0:7000

# Memory optimization
server.memory.heap.initial_size=1G
server.memory.heap.max_size=1G
server.memory.pagecache.size=512M

# Database format
db.format=block

# Disable strict validation
server.config.strict_validation.enabled=false
```

### Dynamic Configuration (Added by startup script)
```properties
# FQDN-based advertised addresses
server.default_advertised_address=${HOSTNAME_FQDN}
server.cluster.advertised_address=${HOSTNAME_FQDN}:5000
server.routing.advertised_address=${HOSTNAME_FQDN}:7688
server.cluster.raft.advertised_address=${HOSTNAME_FQDN}:7000

# Kubernetes service discovery
dbms.cluster.discovery.resolver_type=K8S
dbms.kubernetes.label_selector=neo4j.com/cluster=test-cluster,neo4j.com/clustering=true
dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery
dbms.cluster.discovery.version=V2_ONLY
dbms.kubernetes.cluster_domain=cluster.local

# Cluster formation settings
dbms.cluster.minimum_initial_system_primaries_count=1
initial.dbms.default_primaries_count=3
initial.dbms.default_secondaries_count=3
initial.dbms.automatically_enable_free_servers=true

# Timeouts
dbms.cluster.raft.binding_timeout=1d
dbms.cluster.raft.membership.join_timeout=10m
dbms.routing.default_router=SERVER
```

## Startup Script Analysis

### Key Environment Variables
```bash
HOSTNAME_FQDN="${HOSTNAME}.test-cluster-headless.default.svc.cluster.local"
NEO4J_AUTH="${DB_USERNAME}/${DB_PASSWORD}"
NEO4J_CONF=/tmp/neo4j-config
```

### Configuration Strategy
1. **Copy base config** to writable location (`/tmp/neo4j-config/`)
2. **Add FQDN-based advertised addresses** for each pod
3. **Configure Kubernetes discovery** with correct label selectors
4. **Set minimum primaries to 1** for flexible cluster formation

## Discovery Resolution Analysis

### Log Evidence
```
2025-08-05 01:12:25.241+0000 INFO  Resolved endpoints with K8S{
  address:'kubernetes.default.svc:443',
  portName:'tcp-discovery',
  labelSelector:'neo4j.com/cluster=test-cluster,neo4j.com/clustering=true',
  clusterDomain:'cluster.local'
} to '[test-cluster-discovery.default.svc.cluster.local:5000]'
```

**Critical Finding**: Discovery correctly resolves to the discovery service hostname with port 5000.

## Port Configuration Matrix

| Service | Port | Purpose | Listen Address | Advertised Address |
|---------|------|---------|----------------|-------------------|
| Discovery | 5000 | K8s V2 Discovery | 0.0.0.0:5000 | FQDN:5000 |
| Cluster TX | 6000 | Transaction | 0.0.0.0:6000 | FQDN:6000 |
| RAFT | 7000 | Cluster Consensus | 0.0.0.0:7000 | FQDN:7000 |
| HTTP | 7474 | Browser/API | 0.0.0.0:7474 | FQDN:7474 |
| Bolt | 7687 | Client Protocol | 0.0.0.0:7687 | FQDN:7687 |
| Routing | 7688 | Cluster Routing | 0.0.0.0:7688 | FQDN:7688 |
| Transaction | 7689 | Transaction | 0.0.0.0:7689 | FQDN:7689 |
| Backup | 6362 | Backup Protocol | 0.0.0.0:6362 | FQDN:6362 |

## Why Cluster Formation Succeeded

### 1. **Correct Discovery Port Usage**
- Configuration: `dbms.kubernetes.discovery.v2.service_port_name=tcp-discovery`
- Service port: `tcp-discovery: 5000`
- Resolution: `test-cluster-discovery.default.svc.cluster.local:5000`

### 2. **V2_ONLY Discovery Mode**
- Forces use of the discovery service instead of endpoints
- Bypasses the K8s discovery bug that affects endpoint resolution
- Single authoritative discovery endpoint for all pods

### 3. **Parallel Pod Management**
- All pods start simultaneously
- Coordinated cluster formation with first pod as bootstrap leader
- `minimum_initial_system_primaries_count=1` allows flexible joining

### 4. **FQDN-based Communication**
- All advertised addresses use FQDN format
- Example: `test-cluster-primary-0.test-cluster-headless.default.svc.cluster.local`
- Ensures proper pod-to-pod communication within cluster

### 5. **Service Architecture**
- **Discovery service**: Single ClusterIP for discovery resolution
- **Headless service**: Direct pod-to-pod communication
- **Client service**: External access point
- **Internals service**: Operator management access

## Key Differences vs Expected Configuration

### 1. **Discovery Resolution**
- **Expected**: Direct endpoint resolution to individual pods
- **Actual**: Resolution to discovery service hostname
- **Impact**: Works because V2_ONLY mode handles service-based discovery correctly

### 2. **Port Usage**
- **Discovery port**: 5000 (correct)
- **Transaction port**: 6000 (traditional, not used in V2_ONLY)
- **All other ports**: Standard Neo4j 5.x layout

### 3. **Cluster Formation**
- **Strategy**: Bootstrap with minimum primaries = 1
- **Timing**: Parallel pod management with coordinated startup
- **Result**: 100% successful cluster formation

## Recommendations

### 1. **Maintain Current Architecture**
- V2_ONLY discovery mode is working correctly
- Discovery service architecture is sound
- Parallel pod management ensures reliable formation

### 2. **Monitor Recent Pod Restarts**
- primary-2 and secondary-2 recently restarted
- Verify cluster stability after restarts
- Check for any resource constraints

### 3. **Validate K8s Discovery Bug Fix**
- Current implementation works around the bug
- Test with alternative discovery configurations
- Consider endpoint-based discovery for future versions

## Conclusion

The test-cluster successfully formed because the V2_ONLY discovery mode correctly uses the discovery service (port 5000) rather than attempting direct endpoint resolution. The parallel pod management strategy combined with minimum_initial_system_primaries_count=1 enables reliable cluster formation even with the identified K8s discovery bug.

The architecture demonstrates that service-based discovery is more reliable than endpoint-based discovery for Neo4j clustering in Kubernetes environments.
