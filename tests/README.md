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

# 2. Run full suite (scenario p0-standalone)
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

| Suite | File | Description |
|-------|------|-------------|
| `p0-standalone` | [suites/p0-standalone.yaml](suites/p0-standalone.yaml) | Positive Standalone (happy path / matrix) |
| `neo4j-admission` | [suites/neo4j-admission.yaml](suites/neo4j-admission.yaml) | Admission rejections + one happy case |
| `p1-connectivity` | [suites/p1-connectivity.yaml](suites/p1-connectivity.yaml) | Boots Neo4j (no TLS) and probes connectors from the pod and a client pod |
| `p2-serverconfig` | [suites/p2-serverconfig.yaml](suites/p2-serverconfig.yaml) | `spec.config` passthrough (AC-NEO-CONFIG-001) + invalid-setting startup error (AC-NEO-CONFIG-002) |
| `p3-credentials` | [suites/p3-credentials.yaml](suites/p3-credentials.yaml) | Generated password vs `passwordSecretRef`, each verified with a real bolt query |
| `p4-storage` | [suites/p4-storage.yaml](suites/p4-storage.yaml) | `spec.storage` data modes, Share logs/metrics, additionalMounts, and invalid-storage failures |

### Storage (`p4-storage`)

Covers the `spec.storage` surface, one case per feature (all admitted). Mount points are
verified from inside the `neo4j` container via `/proc/mounts` (no write permission required).

> **Expected-fail:** the three PVC-impossible cases use `assert/storage-error`, which encodes
> the target contract — the operator should **time out and mark the CR `Failed`** with a
> message that mentions the PVC. That timeout/failure status is **not implemented yet**, so
> these cases (and therefore the `p4-storage` suite / CI step) currently **fail on purpose**.
> Do not patch operator code to make them pass — that work is tracked separately.

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

### Connectivity (`p1-connectivity`)

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
./tests/bin/run-e2e.sh neo4j-admission
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
