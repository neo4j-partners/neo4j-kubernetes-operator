# Diagnostic

| File | Lines | Main problem |
|------|-------|--------------|
| `neo4jrestore_controller.go` | 3,772 | Job builder + stop/start cluster + Cypher restore + cloud + hooks in a single file |
| `resources/cluster.go` | 3,194 | Entire cluster builder (STS, services, TLS, discovery, conf) |
| `neo4jenterprisecluster_controller.go` | 2,997 | Orchestration + apply STS + env merge + diagnostics + fleet + MCP |
| `neo4j/client.go` | 2,843 | Connection + DB + users + cluster health + sharding + fleet |
| `neo4jenterprisestandalone_controller.go` | 2,662 | Inline StatefulSet (not factored into `resources/`) |
| `neo4jbackup_controller.go` | 2,228 | Job builder + cloud + sharded |
| `plugin_controller.go` | 1,920 | Still manageable but at the limit |

**Typical symptoms of AI-generated code:**

- Reconcilers as “god objects” (fetch → validate → build → apply → status → Bolt → events)
- Cluster/standalone duplication (fleet, MCP, route, storage expansion)
- Pure logic mixed with K8s I/O (hard to test without envtest)
- Partial extraction (`rolling_upgrade_statemachine.go`, `configmap_manager.go`) without finishing the job

---

# Guiding principle

Split into 4 layers with fixed responsibilities:

```
CRD (api/v1beta1)
    ↓
validation/          → reject invalid specs (already well placed)
    ↓
resources/           → PURE builders (desired K8s objects, zero client)
    ↓
reconcile/           → business logic + apply (client, retry, status)
    ↓
controller/          → thin orchestration: Reconcile() = step pipeline
    ↓
neo4j/               → Bolt client + Cypher (already separate, to subdivide)
```

**Golden rule**: `Reconcile()` must not exceed ~150 lines — only a chain of named steps.

---

# Proposed target structure

```
internal/
├── api/                          # unchanged (api/v1beta1/)
├── validation/                   # unchanged — already well split
│
├── resources/                    # pure builders only
│   ├── enterprise/               # extracted from cluster.go
│   │   ├── statefulset.go
│   │   ├── services.go           # headless, discovery, client, metrics
│   │   ├── discovery_rbac.go
│   │   ├── configmap.go
│   │   └── certificate.go
│   ├── standalone/               # migrate createStatefulSet() from controller
│   │   ├── statefulset.go
│   │   ├── services.go
│   │   └── configmap.go
│   ├── shared/                   # cluster + standalone factorization
│   │   ├── storage.go            # BuildDataVolumeClaimTemplate, StorageClassNamePtr
│   │   ├── security_context.go   # already there
│   │   ├── auth.go               # BuildAuthEnvVars, BuildAuthConfig
│   │   └── monitoring.go
│   ├── networking/
│   │   ├── networkpolicy.go
│   │   ├── ingress.go
│   │   └── route.go
│   ├── mcp.go                    # or mcp/ if it grows
│   └── plugin_init_container.go
│
├── reconcile/                    # NEW — shared, testable logic
│   ├── workload/
│   │   ├── statefulset_apply.go  # createOrUpdateResource, merge env
│   │   ├── env_merge.go          # mergeEnvVars, envVarsEqual, owned_keys
│   │   └── template_diff.go      # isTemplateChangeSignificant, etc.
│   ├── storage/
│   │   ├── preflight.go          # storageClassExists
│   │   └── expansion.go          # merge storage_expansion + standalone_*
│   ├── tls/
│   │   ├── strict_peer.go        # verifyTLSSecretHasCA
│   │   └── certificate.go
│   ├── fleet/
│   │   └── aura.go               # reconcileAuraFleetManagement (single impl)
│   ├── mcp/
│   │   └── reconcile.go          # MCP + route + warn APOC
│   ├── monitoring/
│   │   ├── diagnostics.go        # CollectDiagnostics (extracted from QueryMonitor)
│   │   └── query_monitor.go
│   ├── status/
│   │   └── writer.go             # generic RetryOnConflict status writes
│   └── target/
│       └── enterprise.go         # EnterpriseTarget interface (cluster | standalone)
│
├── controller/                   # THIN orchestrators
│   ├── cluster/
│   │   ├── reconciler.go         # struct + Reconcile + SetupWithManager + RBAC
│   │   ├── lifecycle.go          # deletion, finalizer
│   │   ├── formation.go          # verifyNeo4jClusterFormation
│   │   ├── upgrade.go            # delegates to rolling_upgrade_statemachine
│   │   ├── scale_down.go         # already extracted — move here
│   │   └── property_sharding.go
│   ├── standalone/
│   │   ├── reconciler.go
│   │   └── lifecycle.go
│   ├── backup/
│   │   ├── reconciler.go
│   │   ├── job_builder.go        # pure neo4j-admin commands
│   │   ├── cloud.go
│   │   └── sharded.go            # neo4jbackup_sharded.go
│   ├── restore/
│   │   ├── reconciler.go
│   │   ├── job_builder.go        # buildRestoreCommand, volumes (pure)
│   │   ├── cypher_path.go        # startClusterCypherRestore, poll online
│   │   ├── admin_path.go         # stop/start cluster, neo4j-admin restore
│   │   ├── coordination.go       # in-progress annotations
│   │   └── hooks.go
│   ├── database/
│   ├── sharded/
│   ├── plugin/
│   ├── auth/                     # user, role, rolebinding, authrule
│   │   ├── user/
│   │   ├── role/
│   │   └── ...
│   └── shared/                   # cross-cutting within controller package
│       ├── events.go
│       ├── conditions.go
│       ├── cluster_resolver.go
│       └── finalizer.go
│
├── neo4j/                        # subdivide client.go
│   ├── client.go                 # connection, circuit breaker, Close
│   ├── database.go               # CREATE/ALTER/DROP DATABASE
│   ├── cluster.go                # SHOW SERVERS, formation, health
│   ├── users.go                  # already there
│   ├── privileges.go
│   ├── sharding.go
│   ├── fleet.go                  # RegisterFleetManagementToken
│   └── version.go
│
└── monitoring/                   # unchanged
```

**Important Go note**: sub-packages `controller/cluster/`, `controller/restore/`, etc. can remain `package cluster`, `package restore` (import `github.com/.../internal/controller/cluster`). `+kubebuilder:rbac` markers stay on each controller’s `reconciler.go`.

---

# Critical blocks to isolate first

These areas concentrate regressions documented in `docs/knowledge/`:

| Block | Current file(s) | Target package | Why critical |
|-------|-------------------|----------------|--------------|
| Env merge / owned keys | cluster controller L1168–1333 | `reconcile/workload/env_merge.go` | Subset-merge rule; plugin/fleet must not overwrite each other |
| Rolling upgrade SM | `rolling_upgrade_statemachine.go` | `controller/cluster/upgrade.go` | Already extracted; keep isolated |
| Cluster formation | cluster controller L1872+ | `controller/cluster/formation.go` + `neo4j/cluster.go` | SHOW SERVERS, 300s timeouts |
| Split-brain | `splitbrain_detector.go` | unchanged | Already well isolated |
| Restore command builder | restore controller L1756+ | `controller/restore/job_builder.go` | Pure functions → unit tests without cluster |
| Backup command builder | backup controller | `controller/backup/job_builder.go` | Same |
| Storage expansion | 2 duplicated files | `reconcile/storage/expansion.go` | Single code path, parameterized by STS name |
| Fleet Management | 2× copied | `reconcile/fleet/aura.go` | Near-exact duplicate |
| Diagnostics | QueryMonitor in cluster ctrl | `reconcile/monitoring/diagnostics.go` | Non-fatal by design |
| STS template apply | cluster controller L775+ | `reconcile/workload/statefulset_apply.go` | `sts.UID != ""`, not ResourceVersion |

Each block = 1 prod file + 1 focused test file, without envtest when possible.

---

# Target Reconcile() pattern (cluster example)

```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    cluster, err := r.fetch(ctx, req)
    if err != nil || cluster == nil { return ctrl.Result{}, err }
    if cluster.DeletionTimestamp != nil { return r.lifecycle.Delete(ctx, cluster) }
    if err := r.pipeline.Run(ctx, cluster,
        r.pipeline.Validate,
        r.pipeline.PreflightStorage,
        r.pipeline.EnsureFinalizer,
        r.pipeline.UpgradeOrContinue,
        r.pipeline.ReconcileTLS,
        r.pipeline.ReconcileConfigMap,
        r.pipeline.ReconcileRBAC,
        r.pipeline.ReconcileServices,
        r.pipeline.ReconcileStatefulSet,
        r.pipeline.ReconcileNetworking,
        r.pipeline.ReconcileMCP,
        r.pipeline.ReconcileFleet,
        r.pipeline.VerifyFormation,
        r.pipeline.CollectDiagnostics,
        r.pipeline.UpdateStatus,
    ); err != nil {
        return r.pipeline.HandleError(ctx, cluster, err)
    }
    return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}
```

Each step = one testable function with mocks or pure builders.

---

# EnterpriseTarget interface (factorize cluster + standalone)

```go
// internal/reconcile/target/enterprise.go
type EnterpriseTarget interface {
    client.Object
    Namespace() string
    Name() string
    Phase() string
    ImageTag() string
    StorageSpec() neo4jv1beta1.StorageSpec
    StatefulSetName() string   // "{name}-server" vs "{name}"
    TLSSpec() *neo4jv1beta1.TLSSpec
    FleetSpec() *neo4jv1beta1.AuraFleetManagementSpec
    MCPSpec() *neo4jv1beta1.MCPSpec
}
```

`clusterTarget` / `standaloneTarget` wrappers — replace limited use of `standaloneAsCluster()` for fleet, MCP, storage, TLS.

---

# Incremental migration plan (avoid big-bang)

| Phase | Scope | Risk | Gain |
|-------|-------|------|------|
| 0 | Team rule: max ~500 lines/file for new PRs | Low | Stops the bleeding |
| 1 | Extract `reconcile/fleet/`, `reconcile/mcp/`, `reconcile/storage/preflight.go` | Low | −~400 duplicated lines |
| 2 | Extract `reconcile/workload/env_merge.go` + tests | Medium | Env merge testability |
| 3 | `controller/restore/job_builder.go` + `cypher_path.go` + `admin_path.go` | Medium | restore 3772 → ~4×400 |
| 4 | `controller/backup/job_builder.go` + `cloud.go` | Medium | backup 2228 → ~3×500 |
| 5 | Migrate standalone STS → `resources/standalone/` | Medium | Cluster/standalone alignment |
| 6 | Split `resources/cluster.go` → `resources/enterprise/*` | Medium | Testable builders |
| 7 | Split `neo4j/client.go` by domain | Low | Bolt client navigation |
| 8 | Sub-packages `controller/cluster/`, `controller/restore/` | High | Final structure |

At each phase: `make test-unit` + relevant core integration tests + no behavior change (pure refactor).

---

# General recommendations

## 1. Naming conventions

- `*_reconciler.go` — struct + Reconcile + SetupWithManager + RBAC only
- `*_builder.go` — pure functions (no `client.Client`)
- `*_apply.go` — create/update K8s with retry
- `*_test.go` — colocated, table-driven

## 2. Testability

- **Pure first**: neo4j-admin commands, generated Cypher, env merge, template diff → `go test` without Kind
- **envtest** for K8s apply (already `suite_test.go`)
- **Ginkgo integration** for end-to-end contracts (do not duplicate business logic here)
- Each `docs/knowledge/` rule → at least one unit test that pins it

## 3. Living documentation

- `internal/README.md` — package map (1 paragraph per folder)
- Update `internal/controller/CLAUDE.md` → central `internal/README.md`
- Cluster pipeline diagram in `docs/developer_guide/architecture.md`

## 4. What not to do

- Merge cluster + standalone into one reconciler (HA vs single-node complexity)
- Move validation out of `internal/validation/` (invariant 1: no webhook)
- Big-bang refactor of `neo4jrestore_controller.go` without characterization tests first
- Create interfaces everywhere “just in case” — only at identified boundaries (`EnterpriseTarget`, `JobBuilder`)

## 5. Optional CI guardrails

- `check-file-size.sh` script: fail if a non-generated `.go` exceeds 800 lines (soft threshold, ratchet toward 500)
- `go test -cover` per package `internal/reconcile/...` with minimum threshold

## 6. Align standalone with cluster

**High priority**: standalone still builds its StatefulSet in the controller while cluster uses `resources/`. Migrating to `resources/standalone/` removes ~800 lines from the controller and unifies PVC templates.

---

# Overview (target)

```
api/v1beta1          → CRD types
validation           → Inline validators
resources            → Pure K8s builders
reconcile            → Shared logic (workload/env merge, storage/fleet/mcp/tls, monitoring/diagnostics)
controller           → Thin (cluster pipeline, standalone pipeline, backup, restore, database/auth/plugin)
neo4j                → Bolt client + Cypher
```

---

# Summary

The code is not functionally “bad” — it is poorly sliced: the logic exists, extraction has started (`rolling_upgrade_statemachine`, `splitbrain_detector`, `configmap_manager`), but the large files were never finished.

**ROI priority:**

1. Factorize duplicated code (fleet, MCP, storage)
2. Extract backup/restore command builders (immediate testability)
3. Isolate env merge (high regression risk zone)
4. Migrate standalone to `resources/`
5. Split controller packages by domain

If you want to act, I recommend starting with **phase 1** (fleet + MCP + storage preflight): small diff, visible gain, low risk.

---

# Strengths of this proposal

- Incremental migration — no API break
- Pure `resources/` — kubebuilder convention, tests without client
- Critical blocks identified — env merge, upgrade SM, restore job builder
- Respects project invariants

# Weaknesses of this proposal

- `reconcile/` is a technical name, less readable than `domain/`
- No long-term API vision (embedded `common_types`)
- No test/docs structure aligned with domains
