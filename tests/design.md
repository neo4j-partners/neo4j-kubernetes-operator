# E2E harness design

How the Gate 2 e2e harness is structured and how a suite runs. For what is tested see
[coverage.md](coverage.md); to run or add tests see [contribute.md](contribute.md).

End-to-end conformance tests run on a real Kubernetes cluster: operator install, Neo4j
deploy (Standalone or Cluster), operand assertions. Unit tests remain under `src/`
(`make test`) — this directory is **Gate 2** (ADR-012).

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

## Suite / pipeline / action model

A **suite** is a table of cases plus pipeline overrides; a **pipeline** is the reusable
setup → case → teardown phase definition; an **action** is one atomic `run.sh` + `verify.sh`.

- Setup runs **once**, cases loop (run → assert → case_teardown), teardown runs **once**.
- `neo4j-suite` is the generic single-CR lifecycle (topology-agnostic): install operator,
  apply the CR fixture the case picks, clean it up. Suites override `case_assert` with the
  topology/feature-specific checks.

See [suites/readme.md](suites/readme.md) for the pipeline / case field reference and
[ADR-012](../docs/02-technical-design/decision-records/architecture/012-testing-strategy.md)
for the full harness model.

## Assertions

Default (`E2E_ASSERT_NEO4J_READY=false`): verifies the operator is ready, the Neo4j CR is
applied, and StatefulSet, Services, Secret, and ConfigMap exist. Set
`E2E_ASSERT_NEO4J_READY=true` for full Neo4j pod readiness (requires Enterprise image pull).

## Storage suite mechanics (`feature-storage`)

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

The additionalMounts volume name/path are generated per run by `deploy/neo4j` (random) and
read back by `assert/storage-additional`. The `claimname-ok` fixture bundles the PVC as a second
document; `storage/cleanup-extra` removes it (label `app.kubernetes.io/managed-by=neo4j-e2e`).

## Connectivity suite mechanics (`feature-connectivity`)

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

## Configuration profiles

| Profile | Behaviour |
|---------|-----------|
| Happy path (default, CI) | Fixed picks per layer |
| Matrix | All valid operator × neo4j combinations for the cloud and scenario |
| Explicit | You set `OPERATOR_CASE` and `NEO4J_CASE` |

See [config/readme.md](config/readme.md) for classic cases per domain.

## GitHub Actions

Workflow: [`.github/workflows/ci.yml`](../.github/workflows/ci.yml)

| Job | When | Cluster |
|-----|------|---------|
| `unit` | Every PR / push | — |
| `e2e-local-kind` | After unit | kind on ubuntu-latest (one step per suite) |
| `e2e-azure-aks` | After unit | AKS (create if missing) |

Azure CI credentials and variables are documented in [contribute.md](contribute.md).
