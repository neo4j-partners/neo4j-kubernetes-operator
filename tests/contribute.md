# Running & contributing to e2e tests

How to run the suites locally, in CI, and how to add new tests. For the harness structure
see [design.md](design.md); for what is covered see [coverage.md](coverage.md). For the branch,
pull request and merge rules see
[docs/developer-guide/01-contributing.md](../docs/developer-guide/01-contributing.md).

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
3. Add cases to a suite in `suites/<name>.yaml` (reuse a pipeline from `pipelines/`), each with
   a `comment:` — see [Case comments](#case-comments) below
4. Run: `./tests/bin/run-e2e.sh <suite>`
5. Update [coverage.md](coverage.md) — tick the box or add the row for what the case asserts

Suite naming convention: `workload-*` (topology), `feature-*` (topology-agnostic domain),
`operator-*` (operator behavior). See [design.md](design.md).

### Fixtures must not hard-code a platform

Every suite runs on kind and on AKS, so a fixture may not name a StorageClass that exists on
only one of them. Use a placeholder instead:

| Placeholder | Rendered as |
|---|---|
| `storageClassName: __STORAGE_CLASS__` | the cloud profile's class when the case sets `NEO4J_USE_STORAGE_CLASS=true`; the line is **dropped** otherwise, leaving the cluster default |
| `storageClassName: __CLOUD_STORAGE_CLASS__` | always the cloud profile's class — for cases whose subject *is* naming an existing class |

An invalid class the operator is expected to reject (`no-such-storage-class`) is portable and
stays literal. Add a `clouds:` key only when the case cannot mean anything on another platform.

### Case comments

Every case carries a `comment:` stating what it proves. The runner echoes it under the case
banner, so a CI log reads on its own instead of forcing the reader to map a `cr_name` back to
the suite file:

```
[14:22:07] ######################## CASE [2/3] ha-3-primaries ########################
[14:22:07]   Smallest real HA topology (3 primaries, quorum of 3) with defaultPrimariesCount=3, so the neo4j database spans every primary.
[14:22:07]   suite=workload-cluster case=ha-3-primaries cloud=local-kind assert= cr=e2e-cluster-ha expect=success
```

Rules the parser imposes (`suite_parse_cases` in [lib/suite.sh](lib/suite.sh) is line-based awk,
not a YAML library):

- One physical line — no folded (`>`) or multi-line scalars.
- Plain scalar or double-quoted; the quotes are stripped before logging.
- Avoid `: ` and ` #` inside the text so the file stays valid YAML.

Rationale that only matters when reading the suite file (undecided behaviour, pointers to a
decision record) stays a `#` comment above the case.

## Which workflow runs what

| Workflow | Trigger | Targets |
|----------|---------|---------|
| `ci.yml` | Every pull request and push to `main`, plus manual dispatch | Unit and audit, then every suite on kind |
| `e2e-all-platforms.yml` | 05:00 UTC daily, plus manual dispatch | Unit and audit, then every suite on kind **and** on AKS, in parallel |
| `azure-cleanup.yml` | 09:00 UTC daily, plus manual dispatch | Deletes the Azure CI resource group if it outlived its run |

The first two delegate to the same reusable workflows, so the suite list exists once:
`unit.yml` and `e2e.yml` (which takes a `cloud` input of `local-kind` or `azure-aks`).

The scheduled hour is UTC — GitHub cron has no timezone — so it fires at 07:00 Paris in summer
and 06:00 in winter.

### Leftover Azure resources

`e2e.yml` tears the resource group down with `if: always()`, which also covers a cancelled run.
It cannot cover a **force-cancel** (documented to bypass `always()`) or a lost runner, and an AKS
cluster bills by the hour. `azure-cleanup.yml` is the net: it deletes the group daily, skipping
itself while an e2e run is in flight. If a run is stuck holding the cluster, dispatch it with
`force: true`, or delete the group by hand:

```bash
az group delete --name "${AZURE_RESOURCE_GROUP:-neo4j-operator-ci-rg}" --yes --no-wait
```

## Azure CI setup (maintainers)

### Required secrets

| Secret | Description |
|--------|-------------|
| `AZURE_CLIENT_ID` | Service principal application (client) ID |
| `AZURE_SERVICE_ACCOUNT_SECRET` | Service principal client secret |
| `AZURE_TENANT_ID` | Directory (tenant) ID |
| `AZURE_SUBSCRIPTION_ID` | Target subscription |

`azure/login` receives these as a single `creds` JSON object, because passing `client-id` as an
input switches the action to OIDC and makes it ignore the secret.

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
  --scopes /subscriptions/<SUBSCRIPTION_ID>
```

Map the output to the secrets above: `appId` → `AZURE_CLIENT_ID`, `password` →
`AZURE_SERVICE_ACCOUNT_SECRET`, `tenant` → `AZURE_TENANT_ID`. The client secret expires (one year
by default), so the scheduled Azure job will start failing at login when it does.
