# Running & contributing to e2e tests

How to run the suites locally, in CI, and how to add new tests. For the harness structure
see [design.md](design.md); for what is covered see [coverage.md](coverage.md).

## Run locally — kind

```bash
# 1. Create kind cluster and load operator image
bash tests/bin/setup-local-kind.sh

# 2. Run full suite (scenario workload-standalone)
make test-e2e-local
# or
CLOUD=local-kind ./tests/bin/run-e2e.sh

# Run a specific suite
CLOUD=local-kind ./tests/bin/run-e2e.sh feature-storage
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

`tests/azure/ensure-aks.sh` creates the resource group, ACR, and AKS cluster **if they do not
already exist**, then configures `kubectl`.

## Configuration profiles

| Profile | Command |
|---------|---------|
| Happy path (default, CI) | `make test-e2e-local` |
| All classic combinations | `E2E_PROFILE=matrix make test-e2e-local` or `make test-e2e-matrix` |
| Explicit single combo | `E2E_PROFILE=explicit OPERATOR_CASE=local-image NEO4J_CASE=standalone-named-cr make test-e2e` |

Full Neo4j pod readiness (requires Enterprise image pull):

```bash
E2E_ASSERT_NEO4J_READY=true CLOUD=local-kind ./tests/bin/run-e2e.sh
```

See [config/readme.md](config/readme.md) for classic cases per domain.

## Adding tests

1. Add `actions/<domain>/<name>/run.sh` and `verify.sh`
2. Add fixtures under `fixtures/` if needed
3. Add cases to a suite in `suites/<name>.yaml` (reuse a pipeline from `pipelines/`)
4. Run: `./tests/bin/run-e2e.sh <suite>`
5. Update [coverage.md](coverage.md) — tick the box or add the row for what the case asserts

Suite naming convention: `workload-*` (topology), `feature-*` (topology-agnostic domain),
`operator-*` (operator behavior). See [design.md](design.md).

## Azure CI setup (maintainers)

### Required secrets

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
