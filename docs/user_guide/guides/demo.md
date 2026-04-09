# Neo4j Kubernetes Operator Demo Guide

The operator ships with an interactive demo that showcases its core capabilities using a local Kind cluster. The demo deploys real Neo4j Enterprise instances with TLS, creates databases, installs plugins, and demonstrates live diagnostics.

## What the Demo Shows

| Part | Feature | What it deploys |
|------|---------|----------------|
| 1 | **TLS Standalone** | Single-node Neo4jEnterpriseStandalone with cert-manager TLS |
| 2 | **TLS HA Cluster** | 3-node Neo4jEnterpriseCluster with cert-manager TLS |
| 3 | **External Access** | Port-forward for both standalone and cluster (HTTPS + Bolt+TLS) |
| 4 | **Database Management** | Neo4jDatabase on standalone (products) and cluster (orders) with sample data |
| 5 | **Plugin Management** | APOC plugin via Neo4jPlugin CRD with rolling restart |
| 6 | **Multi-Database Topologies** | Two databases on the same cluster with different primary/secondary distributions |
| 7 | **Live Diagnostics** | Server health and database status from CR status (no kubectl exec) |

## Prerequisites

- **Docker** running
- **Kind** v0.27+ installed
- **kubectl** configured
- **~8 GB RAM** available for Docker (4 Neo4j pods + system pods)

## Running the Demo

### Full demo with environment setup (recommended for first run)

```bash
make demo
```

This will:
1. Ask before destroying any existing Kind clusters
2. Create a fresh `neo4j-operator-dev` Kind cluster
3. Install cert-manager and self-signed ClusterIssuer
4. Build and deploy the operator
5. Run the interactive demo (pauses between sections for confirmation)
6. Ask whether to clean up at the end

### Fast automated demo (no prompts)

```bash
make demo-fast
```

Same as `make demo` but skips all confirmations and runs at fast speed. Good for CI or recordings.

### Demo on existing environment (skip setup)

```bash
# Fast mode (assumes cluster + operator already deployed)
make demo-only

# Interactive mode
make demo-interactive
```

### Setup only (no demo)

```bash
make demo-setup
```

Creates the Kind cluster, installs cert-manager, and deploys the operator — but does not run the demo. Useful if you want to deploy resources manually afterward.

### Clean up demo resources

```bash
make demo-cleanup
```

Deletes all resources created by the demo (databases, plugins, standalone, cluster, secrets) without running the demo. Uses forced cleanup (strips finalizers, force-deletes pods) for fast teardown (~5 seconds).

## Script Options

```
./scripts/demo.sh [options]

Options:
  --skip-confirmations      Skip all interactive prompts
  --cleanup                 Automatically clean up after demo completes
  --cleanup-only            Only clean up resources from a previous run
  --speed fast|normal|slow  Control pacing between sections
  --namespace NAMESPACE     Kubernetes namespace (default: default)
  --password PASSWORD       Neo4j admin password (default: demo123456)
```

### Examples

```bash
# Interactive demo with cleanup at end
./scripts/demo.sh --cleanup

# Fast automated demo
./scripts/demo.sh --skip-confirmations --speed fast

# Clean up a previous demo run
./scripts/demo.sh --cleanup-only

# Demo in a custom namespace
./scripts/demo.sh --namespace demo-ns --password mypassword
```

## Demo Flow Details

### Part 1: TLS Standalone

Creates a `Neo4jEnterpriseStandalone` with:
- `tls.mode: cert-manager` using `ca-cluster-issuer`
- 1.5Gi memory, 10Gi storage
- Readiness/liveness/startup health probes
- Verifies with `cypher-shell -a bolt+ssc://localhost:7687`

### Part 2: TLS HA Cluster

Creates a `Neo4jEnterpriseCluster` with:
- 3 servers, `tls.mode: cert-manager`
- `monitoring.enabled: true` for live diagnostics
- Verifies cluster formation with `SHOW SERVERS`

### Part 3: External Access

Demonstrates `kubectl port-forward` for both deployments:
- Standalone: `https://localhost:7473` (HTTPS) and `bolt+ssc://localhost:7687`
- Cluster: same ports via the client service

### Part 4: Database Management

Creates databases on both deployment types:
- **Standalone**: `products` database with schema constraints and sample data
- **Cluster**: `orders` database with 2 primaries + 1 secondary topology

### Part 5: Plugin Management

Installs APOC on the cluster via `Neo4jPlugin` CRD:
- Operator adds `NEO4J_PLUGINS=["apoc"]` to the StatefulSet
- Rolling restart preserves cluster availability
- Verified with `RETURN apoc.version()`

### Part 6: Multi-Database Topologies

Creates two additional databases on the same cluster:
- `analytics`: 1 primary, 2 secondaries (optimized for reads)
- `sessions`: 2 primaries, 0 secondaries (optimized for writes)
- Verified with `SHOW DATABASES` showing role distribution

### Part 7: Live Diagnostics

Reads `status.diagnostics` from the cluster CR:
- `status.diagnostics.servers` — name, address, state, health for each server
- `status.diagnostics.databases` — name, status, role for each database replica
- `status.conditions` — Ready, ServersHealthy, DatabasesHealthy

## Timing

| Part | Approximate duration |
|------|---------------------|
| Setup (make demo-setup) | ~2 min |
| Part 1: Standalone | ~1 min |
| Part 2: Cluster | ~2-3 min |
| Part 3: External access | ~30s |
| Part 4: Databases | ~1 min |
| Part 5: APOC plugin | ~3 min (rolling restart) |
| Part 6: Multi-database | ~30s |
| Part 7: Diagnostics | ~10s |
| **Total** | **~10-12 min** |

## Troubleshooting

### Standalone pod stays in CrashLoopBackOff
Check `kubectl logs <pod> -c neo4j` for config errors. The most common cause is duplicate config entries — ensure no manual `server.bolt.*` settings conflict with TLS config.

### Cluster stuck in "Forming"
Neo4j cluster formation requires all pods to discover each other. In Kind, this typically takes 1-3 minutes. If stuck beyond 5 minutes, check operator logs: `kubectl logs -n neo4j-operator-dev deployment/neo4j-operator-controller-manager`.

### Plugin installation takes too long
The APOC plugin requires a rolling restart of all cluster pods. Each pod restarts sequentially to maintain availability. Allow 2-3 minutes for a 3-node cluster.

### Diagnostics section shows empty
Diagnostics are collected by default whenever the cluster is Ready. They may take 30-60 seconds to appear after cluster formation. If still empty, check that `spec.monitoring.enabled` is not explicitly set to `false`.

### cypher-shell connection refused
Neo4j's bolt listener starts after the pod readiness probe passes. Wait 10-15 seconds after pod Ready before connecting. The demo includes appropriate delays.
