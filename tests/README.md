# E2E tests (estate 2 — ADR-012)

End-to-end conformance tests on a real Kubernetes cluster: operator install, Neo4j Standalone deploy, operand assertions.

Unit tests remain under `src/` (`make test`). This directory is **Gate 2**.

## Layout

```
tests/
  config/        e2e configuration (cloud, operator/neo4j cases)
  pipelines/     reusable setup/case/teardown phases
  suites/        table-driven tests (cases + pipeline refs)
  azure/         AKS + ACR provisioning for e2e
  bin/           entry points (run-e2e, setup-local-kind)
  actions/       atomic run.sh + verify.sh steps
  runner/        suite executor
  fixtures/      parameterised manifests
  results/       run diagnostics (gitignored)
```

## Run locally — kind

```bash
# 1. Create kind cluster and load operator image
bash tests/bin/setup-local-kind.sh

# 2. Run full suite (scenario workload-standalone)
make test-e2e-local
# or
CLOUD=local-kind ./tests/bin/run-e2e.sh
```

## Run locally — Azure AKS

Prerequisites: `az login`, subscription access, `docker`.

```bash
export AZURE_SUBSCRIPTION_ID=$(az account show --query id -o tsv)
# optional overrides:
# export AZURE_RESOURCE_GROUP=neo4j-operator-ci-rg
# export AZURE_AKS_NAME=neo4j-operator-ci-aks
# export AZURE_ACR_NAME=neo4joperatorci  # globally unique

make test-e2e-azure
# matrix on AKS (6 runs — requires ensure-aks + image push first):
make test-e2e-azure-matrix
```

`tests/azure/ensure-aks.sh` creates the resource group, ACR, and AKS cluster **if they do not already exist**, then configures `kubectl`.

## Suites

Suites are named by family — `workload-*` (topology), `feature-*` (topology-agnostic domains,
run on the cheapest topology), and `operator-*` (operator behavior, not the workload).

| Suite | File | Description |
|-------|------|-------------|
| `workload-standalone` | [suites/workload-standalone.yaml](suites/workload-standalone.yaml) | Positive Standalone (happy path / matrix) |
| `workload-cluster` | [suites/workload-cluster.yaml](suites/workload-cluster.yaml) | Cluster mode — members created, cluster forms, routing works (1-primary lab + 3-primary HA) |
| `feature-connectivity` | [suites/feature-connectivity.yaml](suites/feature-connectivity.yaml) | Boots Neo4j (no TLS) and probes connectors from the pod and a client pod |
| `feature-config` | [suites/feature-config.yaml](suites/feature-config.yaml) | `spec.config` passthrough (AC-NEO-CONFIG-001) + invalid-setting startup error (AC-NEO-CONFIG-002) |
| `feature-credentials` | [suites/feature-credentials.yaml](suites/feature-credentials.yaml) | Generated password vs `passwordSecretRef`, each verified with a real bolt query |
| `feature-storage` | [suites/feature-storage.yaml](suites/feature-storage.yaml) | `spec.storage` data modes, Share logs/metrics, additionalMounts, and invalid-storage failures |
| `feature-uninstall` | [suites/feature-uninstall.yaml](suites/feature-uninstall.yaml) | Deleting the CR preserves the data PVC by default (NEO-2-018) |
| `feature-config-change` | [suites/feature-config-change.yaml](suites/feature-config-change.yaml) | `spec.config` change applied via a controlled restart (NEO-2-010) |
| `operator-admission` | [suites/operator-admission.yaml](suites/operator-admission.yaml) | Admission rejections + one happy case |
| `operator-scope` | [suites/operator-scope.yaml](suites/operator-scope.yaml) | Namespace-scoped operator ignores CRs outside WATCH_NAMESPACE + namespaced RBAC |

## Coverage by suite

What each suite asserts today, mapped to functional requirements (`NEO-*` / `OP-*`) and AC groups.
Legend: `[x]` implemented & asserted · `[ ]` not covered yet, or expected-fail pending a feature.

### `workload-standalone` — NEO-1-001
- [x] Deploy single-member Standalone and reach `Ready` — NEO-2-001-MODE-01 · AC-NEO-STANDALONE / AC-NEO-INSTALL
- [x] Enterprise edition + accepted license (fixture) — NEO-2-001-EDT-01 / NEO-2-001-LIC-01 · AC-NEO-LICENSE
- [x] Continuous reconcile to desired state — OP-1-002 · AC-OP-RECONCILE
- [x] Basic status condition `Ready` surfaced — OP-2-003-STATUS-01 · AC-OP-STATUS
- [x] Reconcile-combination matrix (`E2E_PROFILE=matrix`, operator installed once)

### `workload-cluster` — NEO-1-002
- [x] Deploy 1-primary lab topology: Cluster mode renders, boots, forms — AC-NEO-CLUSTER-001
- [x] Deploy 3-primary HA topology: genuine quorum formation — NEO-2-002-MODE-01 · AC-NEO-CLUSTER-002
- [x] Members created as pool StatefulSets (`<cr>-primary-N`) — NEO-2-002-CSZ-01
- [x] Cluster forms (`SHOW SERVERS`: Enabled + Available) — AC-NEO-CLUSTER-002
- [x] Routing works through the client Service (`neo4j://`) — AC-NEO-CLUSTER-003
- [ ] Cluster TLS material (`spec.trust`) — NEO-3-005-TLS-03 · AC-NEO-TLS (no TLS case yet)
- [ ] Rolling restart of members one-by-one on config change — NEO-3-010-RSTR-02

### `feature-connectivity` — NEO-2-007
- [x] Bolt (7687) reachable from the Neo4j pod — NEO-3-007-PRT-03 · AC-NEO-NETWORKING-PORTS-BOLT
- [x] HTTP (7474) reachable — NEO-3-007-PRT-01 · AC-NEO-NETWORKING-PORTS-HTTP
- [x] HTTP+Bolt exposed, HTTPS disabled — NEO-3-007-PCMB-03 · AC-NEO-NETWORKING-PORTS-HTTP-BOLT
- [x] Reachable via client ClusterIP Service from an external pod — NEO-3-007-SVC-01 · AC-NEO-NETWORKING-CLUSTERIP
- [x] Single-cluster only (multiCluster disabled) — NEO-3-007-MULTI-01

### `feature-config` — NEO-2-003
- [x] Valid `spec.config` rendered verbatim into ConfigMap — NEO-3-003-CFG-01 · AC-NEO-CONFIG-001
- [x] Unknown setting admitted but rejected by Neo4j at startup — AC-NEO-CONFIG-002
- [ ] Assert default JVM arguments applied — NEO-3-003-JVM-01
- [ ] APOC configuration — NEO-3-003-APOC-01/02

### `feature-credentials` — NEO-2-004
- [x] Operator-generated password authenticates over bolt — NEO-3-004-CRED-01 · AC-NEO-SECRETS
- [x] Password from referenced Secret authenticates over bolt — NEO-3-004-CRED-02 · AC-NEO-SECRETS
- [ ] Image pull secret honored — NEO-3-004-IMG-01

### `feature-storage` — NEO-2-006
- [x] Dynamic data via existing StorageClass — NEO-3-006-PVC-02 · AC-NEO-STORAGE-DYNAMIC
- [x] Data volume role provisioned — NEO-3-006-VOL-01 · AC-NEO-STORAGE
- [x] Existing PVC by `claimName`
- [x] Raw `volumeClaimTemplate` provisioning
- [x] Ephemeral `emptyDir` (no PVC)
- [x] logs+metrics Share the data volume
- [x] `additionalMounts` mounted at their paths
- [ ] Non-existent StorageClass → time out and mark CR `Failed` (message mentions PVC) — expected-fail, pending storage-timeout feature
- [ ] Missing `claimName` PVC → time out and mark CR `Failed` — expected-fail
- [ ] `volumeClaimTemplate` bad StorageClass → time out and mark CR `Failed` — expected-fail

### `feature-config-change` — NEO-2-010
- [x] `spec.config` change triggers controlled restart (STS template bump) — NEO-3-010-RSTR-01 · AC-NEO-CONFIG-CHANGE
- [ ] Rolling restart of cluster members one-by-one — NEO-3-010-RSTR-02 (see `workload-cluster`)

### `feature-uninstall` — NEO-2-018
- [x] CR delete preserves data PVC by default — OP-2-005-UNINST-01 · AC-NEO-UNINSTALL-PRESERVE
- [ ] Optional cleanup of services/jobs/PVCs on request — NEO-2-018 (optional path)
- [ ] Operator control-plane uninstall — OP-1-005 / OP-2-005-UNINST-02 (different scope)

### `operator-admission` — validation (ADR-001)
- [x] Reject CR without accepted license — NEO-2-001-LIC-01 · AC-NEO-LICENSE
- [x] Reject Cluster on Community edition — NEO-2-001-EDT-01 · AC-NEO-LICENSE (edition guard)
- [x] Accept a valid minimal Standalone CR (sanity)

### `operator-scope` — OP-1-001
- [x] RBAC is namespaced, least-privilege, no cluster-wide grant — OP-1-006 · AC-OP-RBAC / AC-OP-SCOPE-SINGLE-004
- [x] CR reconciled inside WATCH_NAMESPACE — OP-2-001-SCOPE-01 · AC-OP-SCOPE-SINGLE-002
- [x] CR outside WATCH_NAMESPACE ignored — AC-OP-SCOPE-SINGLE-003

### Not yet covered by any suite
- [ ] Operator install/uninstall via YAML manifests asserted as a requirement — OP-1-001 / OP-2-001-PKG-01 · AC-OP-INSTALL (only exercised in `setup`/`teardown` today)
- [ ] Scale out/in cluster members after deploy — NEO-2-011 / NEO-3-011-CSZ-01 · AC-NEO-SCALE
- [ ] Automatic ENABLE SERVER for added servers — NEO-3-011-SRV-01 · AC-NEO-SCALE
- [ ] Default startup/readiness/liveness probes asserted — NEO-2-009 / NEO-3-009-PROBE-01 · AC-NEO-PROBES (only implicit in readiness)

### Storage (`feature-storage`)

Covers the `spec.storage` surface, one case per feature (all admitted). Mount points are
verified from inside the `neo4j` container via `/proc/mounts` (no write permission required).

> **Expected-fail:** the three PVC-impossible cases use `assert/storage-error`, which encodes
> the target contract — the operator should **time out and mark the CR `Failed`** with a
> message that mentions the PVC. That timeout/failure status is **not implemented yet**, so
> these cases (and therefore the `feature-storage` suite / CI step) currently **fail on purpose**.
> Do not patch operator code to make them pass — that work is tracked separately.
> Each fail-case waits `STORAGE_ERROR_TIMEOUT` (default 45s) before giving up; raise it to
> match the operator's storage timeout once that is implemented.

| Case | Fixture | Assertion |
|------|---------|-----------|
| `dynamic-sc-ok` | `neo4j-storage-dynamic-sc.yaml` | data PVC Bound with `storageClassName=standard` |
| `dynamic-sc-fail` | `neo4j-storage-dynamic-sc-bad.yaml` | **want:** non-existent StorageClass → operator times out, `phase=Failed`, message contains `pvc` *(expected-fail)* |
| `claimname-ok` | `neo4j-storage-claimname.yaml` | pod mounts a pre-created PVC via `existing.claimName` |
| `claimname-fail` | `neo4j-storage-claimname-missing.yaml` | **want:** missing PVC → operator times out, `phase=Failed`, message contains `pvc` *(expected-fail)* |
| `vct-ok` | `neo4j-storage-vct.yaml` | `existing.volumeClaimTemplate` provisions `data-<cr>-server-0` |
| `vct-fail` | `neo4j-storage-vct-bad.yaml` | **want:** template with bad StorageClass → operator times out, `phase=Failed`, message contains `pvc` *(expected-fail)* |
| `emptydir` | `neo4j-storage-emptydir.yaml` | inline `emptyDir` data volume mounted at `/data`, no PVC |
| `share-logs-metrics` | `neo4j-storage-share.yaml` | logs/metrics Share the data volume; `/logs` + `/metrics` mounted |
| `additional-mounts` | `neo4j-storage-additional.yaml` | `additionalMounts` (random name) mounted at its `mountPath` |

The additionalMounts volume name/path are generated per run by `deploy/standalone` (random) and
read back by `assert/storage-additional`. The `claimname-ok` fixture bundles the PVC as a second
document; `storage/cleanup-extra` removes it (label `app.kubernetes.io/managed-by=neo4j-e2e`).

### Connectivity (`feature-connectivity`)

Boots a real Neo4j (`E2E_ASSERT_NEO4J_READY=true`) and probes each connector both from the
Neo4j pod itself (`localhost`) and from a separate client pod (client Service DNS):

| Protocol | Port | Probe | No-TLS expectation |
|----------|------|-------|--------------------|
| `bolt`   | 7687 | `cypher-shell bolt://`  | success |
| `neo4j`  | 7687 | `cypher-shell neo4j://` | success |
| `http`   | 7474 | raw HTTP over `/dev/tcp` | success |
| `https`  | 7473 | TCP connect | failure (connector not exposed without TLS) |

Expectations are data-driven via `EXPECT_CONN_{BOLT,NEO4J,HTTP,HTTPS}` (see
`config/neo4j/cases/standalone-connectivity.sh`); a TLS case flips `https` to `success`.

See [suites/readme.md](suites/readme.md) for the pipeline / case model.

```bash
./tests/bin/run-e2e.sh operator-admission
```

## Assertions

Default (`E2E_ASSERT_NEO4J_READY=false`): verifies operator is ready, Neo4j CR applied, StatefulSet, Services, Secret, and ConfigMap exist.

### Configuration profiles

| Profile | Command |
|---------|---------|
| Happy path (default, CI) | `make test-e2e-local` |
| All classic combinations | `E2E_PROFILE=matrix make test-e2e-local` or `make test-e2e-matrix` |
| Explicit single combo | `E2E_PROFILE=explicit OPERATOR_CASE=local-image NEO4J_CASE=standalone-named-cr make test-e2e` |

See [config/readme.md](config/readme.md) for classic cases per domain.

Full Neo4j pod readiness (requires Enterprise image pull):

```bash
E2E_ASSERT_NEO4J_READY=true CLOUD=local-kind ./tests/bin/run-e2e.sh
```

## GitHub Actions

Workflow: [`.github/workflows/ci.yml`](../.github/workflows/ci.yml)

| Job | When | Cluster |
|-----|------|---------|
| `unit` | Every PR / push | — |
| `e2e-local-kind` | After unit | kind on ubuntu-latest |
| `e2e-azure-aks` | After unit | AKS (create if missing) |

### Required secrets (Azure job)

| Secret | Description |
|--------|-------------|
| `AZURE_CREDENTIALS` | JSON from `az ad sp create-for-rbac --sdk-auth` |
| `AZURE_SUBSCRIPTION_ID` | Target subscription (optional if embedded in credentials) |

### Optional repository variables

| Variable | Default |
|----------|---------|
| `AZURE_RESOURCE_GROUP` | `neo4j-operator-ci-rg` |
| `AZURE_AKS_NAME` | `neo4j-operator-ci-aks` |
| `AZURE_ACR_NAME` | `neo4joperatorci` |
| `AZURE_LOCATION` | `westeurope` |

Set variables under **Settings → Secrets and variables → Actions → Variables**.

### Create service principal (one-time)

```bash
az ad sp create-for-rbac \
  --name neo4j-operator-github-ci \
  --role contributor \
  --scopes /subscriptions/<SUBSCRIPTION_ID> \
  --sdk-auth
```

Store the JSON output as `AZURE_CREDENTIALS`.

## Adding tests

1. Add `actions/<domain>/<name>/run.sh` and `verify.sh`
2. Add fixtures under `fixtures/` if needed
3. Add cases to a suite in `suites/<name>.yaml` (reuse a pipeline from `pipelines/`)
4. Run: `./tests/bin/run-e2e.sh <suite>`

See [ADR-012](../docs/02-technical-design/decision-records/architecture/012-testing-strategy.md) for the full harness model.
