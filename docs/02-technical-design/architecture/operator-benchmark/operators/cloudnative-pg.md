# Operator benchmark — CloudNativePG

| | |
|---|---|
| **Tier** | 1 |
| **Repo** | https://github.com/cloudnative-pg/cloudnative-pg |
| **Version studied** | v1.25.1 (tag) |
| **Date** | 2026-06-22 |
| **Analyst** | operator-benchmark-analyst |

---

## Executive summary

CloudNativePG is the closest **Go + kubebuilder + controller-runtime** reference for a stateful database operator. It combines a rich `pkg/` library (spec builders, sub-reconcilers, instance manager) with file-split controllers, strong admission webhooks, and documented RBAC separation between operator SA and per-cluster operand SA. The main caution for Neo4j: the `ClusterReconciler` package is large (~1.4k lines in `cluster_controller.go` alone) — acceptable only because logic is split across many files; Neo4j should still enforce thinner controller entrypoints per ADR-002.

---

## D1 — Repository layout

```
cloudnative-pg/
├── api/v1/                    # CRD types (+ kubebuilder markers)
├── cmd/manager/               # Cobra multi-command entry (controller, instance, wal*, backup…)
├── internal/
│   ├── controller/            # Reconcilers (cluster, backup, scheduledbackup, pooler)
│   ├── webhook/v1/            # Admission webhooks
│   ├── configuration/         # Operator ConfigMap / env config
│   ├── cnpi/                  # Plugin interface (extensions)
│   └── cmd/manager/           # Controller manager bootstrap
├── pkg/
│   ├── specs/                 # K8s object builders (Roles, SA, pods…)
│   ├── reconciler/            # Sub-reconcilers (instance, PVC, hibernation…)
│   ├── management/            # Operand-side Postgres logic (runs in pod)
│   ├── postgres/              # Postgres helpers
│   └── multicache/            # Multi-namespace cache builder
├── config/                    # Kustomize: crd, rbac, webhook, olm, helm samples
├── tests/e2e/                 # Ginkgo end-to-end suite
├── releases/                  # Single-file install YAML per version
└── docs/                      # MkDocs site (security, installation)
```

| Area | Path | Notes |
|------|------|-------|
| API types | `api/v1/*_types.go` | Cluster, Backup, ScheduledBackup, Pooler, Database, … |
| Controllers | `internal/controller/` | One file-set per CRD; cluster split across create/scale/status/upgrade |
| Builders / manifests | `pkg/specs/`, `pkg/servicespec/` | Role/SA/Pod builders — not in controller |
| Webhooks | `internal/webhook/v1/` | Per-CRD validators + mutators |
| Tests | `internal/controller/*_test.go`, `tests/e2e/` | envtest suite + large e2e |
| Deploy / config | `config/`, `releases/`, external Helm chart | OLM bundles in `config/olm-*` |

**Neo4j takeaway**: Adopt **`api/` + `internal/controller` + `pkg/specs` (render) + `pkg/reconciler` (domain)** split. The operand runs separate commands (`instance`, `walarchive`) — analogue to Neo4j sidecar/init if needed; keep Bolt admin client in `internal/neo4j/`, not in render.

---

## D2 — Internal layering

| Layer | Present? | Package(s) | Notes |
|-------|----------|------------|-------|
| Pure builders | Yes | `pkg/specs`, `pkg/servicespec`, `pkg/podspec` | `CreateRole`, pod templates — no client |
| Domain / reconcile logic | Yes | `pkg/reconciler/*`, methods on `ClusterReconciler` in dedicated files | `cluster_create.go`, `cluster_scale.go`, … |
| Thin controllers | Partial | `internal/controller/*_controller.go` | `Reconcile()` orchestrates; cluster file still heavy |
| Shared admin client | Yes | `pkg/management/postgres`, `pkg/postgres` | SQL/admin from operand pod and operator |

**Neo4j takeaway**: **Adapt** CNPG's `pkg/reconciler` sub-packages per concern (PVC, instance, rollout) → Neo4j `internal/domain/{persistence,workload,formation}`. **Avoid** a single 1.4k-line reconciler file — keep `Reconcile()` as dispatcher only (`cluster_controller.go` is the counter-example).

---

## D3 — CRD & controller count

| CRD | Controller | Notes |
|-----|------------|-------|
| `Cluster` | `ClusterReconciler` | Core workload |
| `Backup` | `BackupReconciler` | One-shot backup Job |
| `ScheduledBackup` | `ScheduledBackupReconciler` | Cron semantics |
| `Pooler` | `PoolerReconciler` | PgBouncer |
| `Database`, `Publication`, `Subscription` | (controllers / webhooks) | Declarative SQL — post-core |
| `ImageCatalog`, `ClusterImageCatalog` | — | Image catalog cluster-scoped |

**Neo4j takeaway**: **Adapt** separate controllers for **satellite lifecycle** (Backup, Restore, Database) per BDR-001; core `Neo4j` reconciler stays one. Defer declarative SQL-style CRDs to V2+.

---

## D4 — Go & Kubernetes dependencies

| Dep | Version | Pin policy |
|-----|---------|------------|
| go | 1.23.5 | `go.mod` |
| controller-runtime | v0.20.2 | Direct require |
| k8s.io/* | aligned with CR v0.20 | Via controller-runtime |
| Shared libs | `github.com/cloudnative-pg/machinery` | Extracted common code |

**Neo4j takeaway**: Pin **controller-runtime + k8s.io** to one kubebuilder scaffold generation (ADR-012). CNPG extracts `machinery` — only consider shared module if multiple Neo4j repos; otherwise keep monorepo `internal/`.

---

## D5 — Third-party libraries

| Library | Purpose | In operator pod? |
|---------|---------|------------------|
| `jackc/pgx`, `lib/pq` | Postgres SQL | Operator + operand |
| `cloudnative-pg/barman-cloud` | Backup to S3/Azure/GCS | Webhook validation + backup jobs |
| `prometheus/client_golang` | Metrics | Yes |
| `kubernetes-csi/external-snapshotter` | Volume snapshots | API types / client |

**Neo4j takeaway**: Keep **cloud SDKs out of operator hot path** — mount credentials in backup Jobs only (ADR-016). `neo4j-go-driver` in operator for formation only (`internal/neo4j/`).

---

## D6 — Operator RBAC

Source: `config/rbac/role.yaml` → ClusterRole `manager`.

| Resource group | Key resources | Verbs | Documented? |
|----------------|---------------|-------|-------------|
| core | pods, pvc, secrets, configmaps, services, sa | full mutate + watch | `docs/src/security.md` |
| core | pods/exec | create | For admin/debug |
| core | nodes | get, list, watch | ClusterRole-only need |
| apps | statefulsets, deployments | full mutate | |
| batch | jobs | full mutate | Backup jobs |
| postgresql.cnpg.io | clusters, backups, … | full mutate | |
| admissionregistration | mutating/validatingwebhookconfigurations | mutate | Self-manage webhooks |
| rbac.authorization.k8s.io | roles, rolebindings | mutate | **Creates operand RBAC** |

**Neo4j takeaway**: **Adopt** documented RBAC rationale (`security.md`). Operator needs **roles/rolebindings** verbs to provision per-cluster workload SA (D7). **Minimize** ClusterRole: CNPG needs `nodes` + `ClusterImageCatalog` cluster-scoped — Neo4j may need similar for zone/topology or avoid cluster-scoped reads in V1.

---

## D7 — Workload RBAC

- Operator creates a **ServiceAccount** named like the `Cluster` in the **same namespace**.
- **Role** + **RoleBinding** per cluster — rules in `pkg/specs/roles.go` (`CreateRole`).
- Operand **instance manager** uses that SA to read named Secrets/ConfigMaps and patch `clusters/status`.
- Rules use **`resourceNames`** scoping — not namespace-wide secret list.

Evidence: `docs/src/security.md` § "Calls to the API server made by the instance manager"; `pkg/specs/roles.go`.

**Neo4j takeaway**: **Adopt** per-`Neo4j` CR **Role** (not ClusterRole) for database pods — discovery/bolt TLS secrets with `resourceNames`. Operator SA remains separate. Document in ADR-013.

---

## D8 — Watch scope

| Mode | Supported | Default | Config |
|------|-----------|---------|--------|
| All namespaces | Yes | **Yes** — empty `WATCH_NAMESPACE` | `internal/cmd/manager/controller/controller.go` L132–140 |
| Single / multi namespace | Yes | — | `WATCH_NAMESPACE` comma-separated; `multicache.DelegatingMultiNamespacedCacheBuilder` |
| OLM granular | Yes | — | OLM can deploy operator in own NS, watch target NSes |

Evidence: `internal/configuration/configuration.go` (`WatchNamespace`); `docs/src/security.md` (OLM vs ClusterRoleBinding).

**Neo4j takeaway** (BDR-003): CNPG defaults **cluster-wide** (platform operator). Neo4j V1 **single-namespace** is the opposite default — but **implement the same mechanism** (`WATCH_NAMESPACE` + multi-cache) for V1.1+. **Avoid** multi-namespace as recommended production mode without Strimzi-style overhead docs.

---

## D9 — Webhooks

| Webhook | Deploy | TLS | failurePolicy |
|---------|--------|-----|---------------|
| Mutating + Validating per CRD | With operator Deployment | `/run/secrets/cnpg.io/webhook` or OLM-generated | **Fail** (`config/webhook/manifests.yaml`) |
| Cluster, Backup, Pooler, ScheduledBackup | Yes | cert-manager or OLM | Fail |

**Neo4j takeaway**: **Adopt** `failurePolicy: Fail` for validating. Webhook certs via Secret volume or cert-manager (G-05). Large validators live in dedicated files (`cluster_webhook.go` ~2.3k lines) — keep Neo4j validators split by concern.

---

## D10 — Admission vs reconcile split

| Admission (webhook) | Reconcile (async) |
|---------------------|-------------------|
| Storage size, image ref, bootstrap spec, backup config, immutability guards | Pod/STS creation, failover, scale, status, certificate renewal |
| Barman-cloud backup stanza validation | Backup Job execution |
| Resource defaults / normalization (mutating) | Instance health, replication lag |

Evidence: `internal/webhook/v1/cluster_webhook.go`; controller files for runtime operations.

**Neo4j takeaway**: Aligns with **ADR-001** — CEL/OpenAPI for simple cross-field; webhook for semver, Secret refs, storage class; reconciler for formation/Bolt quorum and drift. **Do not** call Bolt from webhooks.

---

## D11 — Status & conditions

| Condition type | When set | Pool-level? |
|----------------|----------|-------------|
| `Ready` | Cluster instances healthy | `status.readyInstances` / `instances` counts |
| `ContinuousArchiving` | WAL archiving OK | — |
| `LastBackupSucceeded` | Last backup outcome | — |
| Pod-level | Per-instance in `status.instances` | Per Postgres instance |

Evidence: `api/v1/cluster_types.go` (`ConditionClusterReady`, print columns).

**Neo4j takeaway**: **Adopt** top-level `Ready` + **per-pool / per-member** sub-status for Cluster mode (BDR-009). Separate backup conditions to backup CRD status (V2+).

---

## D12 — Formation / day-2

- **Scale up**: operator creates new Pods/STS replicas; **instance manager** bootstraps Postgres (initdb or join via streaming replication / `pg_basebackup`).
- **Scale down**: `scaleDownCluster` picks sacrificial instance, deletes Pod then PVC (`cluster_scale.go`).
- **Failover**: reconciler detects primary failure, promotes replica (postgres admin via instance manager).
- **No separate "ENABLE SERVER" Job** — logic in operand + reconciler.

Evidence: `internal/controller/cluster_scale.go`, `pkg/management/postgres/`.

**Neo4j takeaway**: Neo4j **Bolt `ENABLE SERVER` / decommission** maps to CNPG join + sacrifice instance flow. Prefer **in-reconciler + init/sidecar** over Helm-style ops Job (aligns with `20-operator-proposal.md`). ADR-007 should cite this pattern.

---

## D15 — Testing pyramid

| Tier | Tool | Scope |
|------|------|-------|
| Unit | Ginkgo/Gomega (`*_test.go`) | Controllers, specs, webhooks |
| Integration | envtest (`internal/controller/suite_test.go`) | Reconciler with fake API server |
| E2E | `tests/e2e/*_test.go` on kind | Backup (MinIO/Azure), HA, certs, scale — **40+ scenarios** |
| Smoke | CI matrix `k8s-versions-check.yml` | Multiple K8s versions |

**Neo4j takeaway**: **Adopt** render/spec **table tests** + envtest per domain + **kind e2e** for P0 (`13-v1-scope-lock`). E2E with real Neo4j container (not just Postgres image) — budget explicitly in ADR-020.

---

## D16 — Quality gates

| Gate | Tool | Blocks merge? |
|------|------|---------------|
| Lint | golangci-lint (incl. gosec) | Yes — `docs/src/security.md` |
| Vuln | govulncheck, Snyk nightly, CodeQL | Yes on PR |
| Spell / inclusive | woke, spellcheck | CI workflows |
| E2E | `continuous-integration.yml` | Release pipeline |

Evidence: `.github/workflows/continuous-integration.yml`, `Makefile` (`lint`, `test`), `docs/src/security.md`.

**Neo4j takeaway**: **Adopt** golangci-lint + govulncheck as ADR-018 minimum. Match CNPG policy: **lint failure blocks merge**.

---

## D17 — Packaging

| Channel | Artifact | Notes |
|---------|----------|-------|
| Plain YAML | `releases/cnpg-1.25.1.yaml` | Single bundle |
| Helm | [cloudnative-pg/charts](https://github.com/cloudnative-pg/charts) | `config.clusterWide`, `watchNamespace` in chart values |
| OLM | `config/olm-*`, OperatorHub | `installModes`: AllNamespaces / OwnNamespace |
| CLI plugin | `cmd/kubectl-cnpg` | kubectl plugin |

**Neo4j takeaway**: V1 **Helm + YAML**; defer OLM. External Helm chart repo (like CNPG) keeps install decoupled from operator code. Document **restricted** values overlay (single-ns Role) per BDR-003.

---

## Recommendations for Neo4j operator

| Topic | Adopt | Adapt | Avoid | Evidence |
|-------|-------|-------|-------|----------|
| Package split | `pkg/specs` render, `pkg/reconciler` domain | Rename to `internal/render`, `internal/domain` | Monolithic controller file | `pkg/specs`, `cluster_*.go` |
| Workload RBAC | Per-CR Role + resourceNames | Neo4j discovery secrets | ClusterRole for pods | `pkg/specs/roles.go` |
| Watch scope | `WATCH_NAMESPACE` + multi-cache | V1 default single NS | Undocumented multi-NS | `controller.go` L132–140 |
| Webhooks | failurePolicy Fail | CEL + thin webhook | Bolt in webhook | `config/webhook/manifests.yaml` |
| Formation | In-reconciler + operand | Bolt client in operator | Helm ops Job | `cluster_scale.go` |
| Testing | envtest + kind e2e | Neo4j testcontainers | Cloud-only e2e for P0 | `tests/e2e/` |

---

## References

- https://cloudnative-pg.io/documentation/1.25/
- https://github.com/cloudnative-pg/cloudnative-pg/blob/main/docs/src/security.md
- https://github.com/cloudnative-pg/charts
- [BDR-003](../../../decision-records/business/operator/003-operator-install-scope.md) · [ADR-001](../../../decision-records/architecture/001-crd-validation-process.md)
