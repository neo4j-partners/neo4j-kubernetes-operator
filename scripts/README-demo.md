# Neo4j Kubernetes Operator Demo

Interactive end-to-end demo that deploys real Neo4j Enterprise instances and walks through the operator's core capabilities — TLS, HA clustering, database management, plugins, declarative RBAC, and live diagnostics — in 8 parts (~10–12 minutes total).

## Quick Start

### Option 1: Complete environment + demo (one command)

```bash
make demo            # interactive walkthrough — confirms before each part
make demo-fast       # automated, no prompts — good for video / CI
```

`make demo` chains `demo-setup` (creates a Kind cluster + cert-manager + deploys the operator) and then runs `scripts/demo.sh`. First run takes ~3 minutes for the bootstrap; the demo itself is ~10 minutes after that.

### Option 2: Reuse an existing dev cluster

Skip the bootstrap if you already have a Kind cluster with the operator deployed:

```bash
make demo-interactive    # interactive, prompts between parts
make demo-only           # fast mode, no prompts
```

### Option 3: Direct script invocation

```bash
./scripts/demo.sh                                  # interactive
./scripts/demo.sh --skip-confirmations             # automated
./scripts/demo.sh --skip-confirmations --speed fast # fastest
./scripts/demo.sh --cleanup                        # demo + auto-cleanup at end
./scripts/demo.sh --cleanup-only                   # just clean up a previous run
```

## What the demo does

The demo runs in 8 parts, each with a colourised section header, an explanation of what the operator is doing, the manifest being applied, and live verification:

| # | Part | What it shows |
|---|---|---|
| 1 | 🚀 TLS Standalone Deployment | `Neo4jEnterpriseStandalone` with cert-manager TLS, readiness/liveness probes, `bolt+s://` endpoint generation |
| 2 | 🏗️ TLS HA Cluster (3 servers) | `Neo4jEnterpriseCluster` with 3 servers, parallel pod startup, RAFT-based bootstrap, V2 LIST discovery |
| 3 | 🔌 Secure External Access | HTTPS browser endpoint and `bolt+s://` connection string sourced from `status.endpoints` — no port-forward gymnastics |
| 4 | 🗄️ Database Creation | `Neo4jDatabase` CRD: `appdb` with topology, schema, sample data via `initialData.cypherStatements` |
| 5 | 🔌 APOC Plugin | `Neo4jPlugin` CRD installs APOC declaratively, automatic rolling restart, verification via `RETURN apoc.version()` |
| 6 | 📊 Multi-Database Topologies | Multiple `Neo4jDatabase` CRs with different topologies (read-heavy, write-heavy) on the same cluster |
| 7 | 👥 Declarative User & Role Management | `Neo4jRole` with privileges + `Neo4jUser` (password from a Secret) bound to the role; verified via cypher-shell |
| 8 | 🔍 Live Cluster Diagnostics | `status.diagnostics.servers` / `databases` / `users` / `roles` populated by the operator — observability without `kubectl exec` |

Two pre-deployed clusters are reused across parts:

| Variable | Default | Used in |
|---|---|---|
| `$CLUSTER_NAME_SINGLE` | `neo4j-single` | Part 1 (standalone) |
| `$CLUSTER_NAME_MULTI` | `neo4j-cluster` | Parts 2, 3, 4, 5, 6, 7, 8 |

## Configuration

### Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `DEMO_NAMESPACE` | `default` | Kubernetes namespace the demo deploys into |
| `ADMIN_PASSWORD` | `demo123456` | Neo4j admin password (used for the cluster's admin Secret) |
| `CLUSTER_NAME_SINGLE` | `neo4j-single` | Standalone instance name |
| `CLUSTER_NAME_MULTI` | `neo4j-cluster` | HA cluster name |
| `DEMO_SPEED` | `normal` | One of `fast`, `normal`, `slow` — controls inter-step pauses |
| `SKIP_CONFIRMATIONS` | `false` | Set `true` to skip every interactive prompt |
| `CLEANUP_AFTER` | `false` | Set `true` to run `demo_cleanup` automatically at the end |

### Command-line flags

```bash
./scripts/demo.sh [options]
```

| Flag | Equivalent env var | Description |
|---|---|---|
| `--namespace NS` | `DEMO_NAMESPACE` | Target namespace |
| `--password PW` | `ADMIN_PASSWORD` | Admin password |
| `--speed {fast,normal,slow}` | `DEMO_SPEED` | Pacing |
| `--skip-confirmations` | `SKIP_CONFIRMATIONS=true` | No interactive prompts |
| `--cleanup` | `CLEANUP_AFTER=true` | Auto-clean at end of demo |
| `--cleanup-only` | — | Skip demo, just remove resources from a previous run |
| `--help`, `-h` | — | Show help |

## Make targets

| Target | What it does |
|---|---|
| `make demo` | Full bootstrap (`demo-setup`) + interactive demo |
| `make demo-fast` | Bootstrap + automated demo (`--skip-confirmations --speed fast`) |
| `make demo-interactive` | Interactive demo, **assumes** cluster + operator already running |
| `make demo-only` | Automated demo, **assumes** cluster + operator already running |
| `make demo-setup` | Bootstrap a fresh Kind cluster + cert-manager + operator |
| `make demo-cleanup` | Run `./scripts/demo.sh --cleanup-only` |

## Output

Rich, colourised terminal output:

- **Boxed section headers** for each part (`🎬`, `🚀`, `🏗️`, etc.)
- **Live status indicators**: phase transitions, ready timers, elapsed time per part
- **Inline manifest preview**: each CR is printed before being applied so you can see exactly what's going to the cluster
- **`kubectl` command echoes**: every command is shown before execution, so you can copy-paste them later
- **Resource dashboards** between parts: a snapshot of all Neo4j CRs, pods, services
- **Demo summary**: at the end, a checkmark grid for all 8 parts and the total elapsed time

Sample (Part 1 header):

```
═══════════════════════════════════════════════════════════════════════════
 PART 1  🚀  TLS Standalone Deployment
═══════════════════════════════════════════════════════════════════════════

[DEMO] Deploying a single-node Neo4j Enterprise instance with cert-manager TLS.
[DEMO] The operator will:
       • Issue a TLS certificate via cert-manager (CA: ca-cluster-issuer)
       • Create a StatefulSet, Service, ConfigMap, PVC
       • Configure server.bolt.tls_level=REQUIRED (Bolt over TLS only)
       • Surface a bolt+s:// endpoint in status.endpoints when Ready
```

## Prerequisites

The demo requires:

1. **Kind** (Kubernetes in Docker) — this project exclusively supports Kind
2. **cert-manager v1.18+** with a `ca-cluster-issuer` ClusterIssuer (auto-installed by `make demo-setup` / `make dev-cluster`)
3. **Neo4j Kubernetes Operator** running in the cluster (auto-deployed by `make demo-setup`)
4. **kubectl** configured for the dev cluster
5. **Neo4j Enterprise image** available — `make demo-setup` uses the default `neo4j:5.26-enterprise` image, override with `NEO4J_VERSION` if needed
6. **Python 3** — used to pretty-print JSON status snippets in the diagnostics part (the demo gracefully falls back to raw output if missing)

`make demo-setup` is the simplest route — it bootstraps everything in ~3 minutes.

### Cluster targeting

The setup detects which Kind clusters you have and prefers the dev cluster:

| Scenario | Behaviour |
|---|---|
| Only `neo4j-operator-dev` exists | Deploy operator there |
| Only `neo4j-operator-test` exists | Deploy operator there |
| Both exist | Prefer `neo4j-operator-dev` (demo-friendly) |
| Neither | Print an error with the right setup command |

For interactive choice when multiple clusters are available, use `make operator-setup-interactive`. To inspect or manage the operator across clusters: `make operator-status`, `make operator-logs`, `./scripts/setup-operator.sh cleanup`.

## Cleanup

The demo creates resources in `$DEMO_NAMESPACE` (default `default`). Three ways to clean up:

```bash
# Option 1: Clean up at the end of the demo automatically
./scripts/demo.sh --cleanup
# or:  CLEANUP_AFTER=true ./scripts/demo.sh

# Option 2: After the demo, run cleanup separately
make demo-cleanup
# or:  ./scripts/demo.sh --cleanup-only

# Option 3: Tear down the entire dev environment
make dev-destroy   # also removes the Kind cluster, cert-manager, the operator
```

`demo-cleanup` strips finalizers, deletes every demo CR (including `Neo4jUser`, `Neo4jRole`, `Neo4jRoleBinding`), force-deletes pods, and removes the demo Secrets, Services, ConfigMaps, and PVCs.

## Troubleshooting

**`ca-cluster-issuer not found`** — cert-manager isn't fully installed in the dev cluster:
```bash
make dev-cluster   # bootstraps cert-manager v1.20.0 + ca-cluster-issuer
```

**Pods stuck in `Pending`** — check resources/storage:
```bash
kubectl describe pod -l neo4j.com/cluster=$CLUSTER_NAME_MULTI
kubectl get events --sort-by=.lastTimestamp | tail -30
```

**TLS certificate issues** — inspect cert-manager state:
```bash
kubectl get certificates,certificaterequests -A
kubectl describe certificate neo4j-cluster-tls
```

**`cypher-shell` exec fails in Part 5/7** — TLS handshake can race with pod readiness on slow machines; re-run the part or increase `DEMO_SPEED=slow`.

**Demo hangs at "Waiting for cluster phase=Ready"** — `make operator-logs` will show what the operator is doing; common causes: image pull throttling, insufficient memory (Neo4j Enterprise needs ≥1.5Gi/server), or missing `NEO4J_ACCEPT_LICENSE_AGREEMENT`.

**Capture a full demo run with logs**:
```bash
./scripts/demo.sh --speed slow 2>&1 | tee demo.log
```

## Customising the demo

The demo script (`scripts/demo.sh`) is one file with focused functions per scenario:

| Function | Part | Modify to… |
|---|---|---|
| `deploy_single_node` | 1 | Change standalone image, resources, TLS config |
| `deploy_multi_node_cluster` | 2 | Adjust cluster size, topology constraints, resources |
| `demonstrate_external_access` | 3 | Show different service types (LoadBalancer, NodePort, Ingress) |
| `demonstrate_database_creation` | 4 | Add custom `initialData.cypherStatements`, change topology |
| `demonstrate_plugin_installation` | 5 | Switch to a different plugin (GDS, Bloom, GenAI) |
| `demonstrate_multi_database` | 6 | Add more databases or different topology mixes |
| `demonstrate_user_role_management` | 7 | Adjust the role's privileges, add a `Neo4jRoleBinding` |
| `demonstrate_diagnostics` | 8 | Add custom `kubectl get` queries against `status.diagnostics` |

Other knobs:

- **Pacing**: change `PAUSE_SHORT`, `PAUSE_MEDIUM`, `PAUSE_LONG` near the top of the script
- **Logging**: every `log_demo`, `log_info`, `log_command` call is replaceable
- **Adding a new part**: write a `demonstrate_<name>` function, append a `log_header N "🎯" "Title"` call, wire it into `main()`, and bump the welcome banner / summary part-counts

## Support

- [Main documentation](../docs/)
- [User & Role Management Guide](../docs/user_guide/user_role_management.md) (covers Part 7 in depth)
- [Troubleshooting guide](../docs/user_guide/guides/troubleshooting.md)
- [GitHub Issues](https://github.com/priyolahiri/neo4j-kubernetes-operator/issues) — report bugs or request demo additions
