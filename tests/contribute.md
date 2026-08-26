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

## Run locally — GCP GKE

Prerequisites: `gcloud auth login`, a project you can create clusters in, the
`gke-gcloud-auth-plugin` component (`gcloud components install gke-gcloud-auth-plugin`), and
`docker`.

```bash
export GCP_PROJECT=kop12345
# optional overrides:
# export GCP_ZONE=europe-west1-b          # cluster is zonal
# export GCP_REGION=europe-west1           # Artifact Registry location
# export GKE_CLUSTER_NAME=neo4j-operator-ci-gke
# export GCP_AR_REPOSITORY=neo4j-operator-ci

make test-e2e-gke
# matrix on GKE (6 runs — requires ensure-gke + image push first):
make test-e2e-gke-matrix
```

`tests/gcp/ensure-gke.sh` creates the Artifact Registry repository and the GKE cluster **if they
do not already exist**, grants the node service account read access to the repository, then
configures `kubectl`. The repository is never deleted by teardown — only the cluster is, since
that is what bills by the hour.

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

Every suite runs on kind, on AKS and on GKE, so a fixture may not name a StorageClass that
exists on only one of them. Use a placeholder instead:

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
| `e2e-all-platforms.yml` | 05:00 UTC daily, plus manual dispatch | Unit and audit, then every suite on kind, on AKS, on GKE **and** on EKS, in parallel |
| `cloud-cleanup.yml` | 09:00 UTC daily, plus manual dispatch | Deletes whichever managed cluster outlived its run — one job per cloud |

The first two share `unit.yml` and the `.github/actions/e2e` composite action, which takes a
`cloud` input of `local-kind`, `azure-aks`, `gcp-gke` or `aws-eks` and an optional `suite`. CI
passes a suite per job so each one reports on its own; the scheduled workflow passes none and runs
them all in one job per platform. Neither hardcodes the list — it comes from `tests/suites/*.yaml`.

The scheduled hour is UTC — GitHub cron has no timezone — so it fires at 07:00 Paris in summer
and 06:00 in winter.

### Leftover clusters

`e2e-all-platforms.yml` tears each platform down with `if: always()`, which also covers a cancelled
run. It cannot cover a **force-cancel** (documented to bypass `always()`) or a lost runner, and
every managed control plane bills by the hour. `cloud-cleanup.yml` is the net: one job per cloud,
daily, each skipping itself while an e2e run is in flight. Dispatch it with `force: true` for a run
stuck holding a cluster, and with `cloud:` to target a single provider.

By hand, if you would rather not wait for the workflow:

```bash
# Azure — deleting the group takes AKS and ACR with it
az group delete --name "${AZURE_RESOURCE_GROUP:-neo4j-operator-ci-rg}" --yes --no-wait

# GCP
gcloud container clusters delete "${GKE_CLUSTER_NAME:-neo4j-operator-ci-gke}" \
  --zone "${GCP_ZONE:-europe-west1-b}" --quiet --async

# AWS — nodegroups first, which is why this one is a script
make teardown-e2e-eks
```

The image registries are left in place on purpose — ACR, Artifact Registry and ECR each hold a few
image layers and save a full push on the next run.

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

## GCP CI setup (maintainers)

### No secrets

GKE authenticates through workload identity federation: the job presents its GitHub OIDC token,
the provider trusts this repository, and GCP returns a short-lived credential impersonating the
service account. Nothing is stored in the repository, and nothing expires the way an Azure client
secret or an AWS access key does — which is why the three clouds are configured differently.

The provider and the service account are defaults in `e2e-all-platforms.yml` and
`cloud-cleanup.yml`. They are configuration, not secrets, and a Variable overrides either one:

| Variable | Default |
|----------|---------|
| `GCP_WORKLOAD_IDENTITY_PROVIDER` | `projects/1024447859763/locations/global/workloadIdentityPools/github-pool/providers/github-provider` |
| `GCP_SERVICE_ACCOUNT` | `gh-actions-k8s-operator-test@kop12345.iam.gserviceaccount.com` |

The job that uses them declares `id-token: write`; without that permission the token exchange has
nothing to present and the run fails before any cluster exists.

### Bootstrapping the pool and the provider

One-off, in the project that owns the service account. Note that both the provider path above and
the `principalSet` below identify the project by its **number**, not its ID: `1024447859763` is
`kop12345`. A pool that lives in another project is that project's to configure — its attribute
condition decides which repositories may exchange a token, and granting
`workloadIdentityUser` on our side cannot widen it.

```bash
PROJECT=kop12345
PROJECT_NUMBER=1024447859763
REPO=neo4j-partners/neo4j-kubernetes-operator
SA=gh-actions-k8s-operator-test@kop12345.iam.gserviceaccount.com

gcloud services enable \
  iam.googleapis.com sts.googleapis.com iamcredentials.googleapis.com \
  compute.googleapis.com container.googleapis.com artifactregistry.googleapis.com \
  --project="$PROJECT"

gcloud iam workload-identity-pools create github-pool \
  --project="$PROJECT" --location=global --display-name="GitHub Actions"

gcloud iam workload-identity-pools providers create-oidc github-provider \
  --project="$PROJECT" --location=global --workload-identity-pool=github-pool \
  --display-name="GitHub OIDC" \
  --issuer-uri="https://token.actions.githubusercontent.com" \
  --attribute-mapping="google.subject=assertion.sub,attribute.repository=assertion.repository,attribute.repository_owner=assertion.repository_owner,attribute.ref=assertion.ref" \
  --attribute-condition="assertion.repository == '${REPO}'"

gcloud iam service-accounts add-iam-policy-binding "$SA" --project="$PROJECT" \
  --role=roles/iam.workloadIdentityUser \
  --member="principalSet://iam.googleapis.com/projects/${PROJECT_NUMBER}/locations/global/workloadIdentityPools/github-pool/attribute.repository/${REPO}"
```

The condition matches the repository and deliberately says nothing about the branch, so a
maintainer can run `e2e-all-platforms` by `workflow_dispatch` from a feature branch. Pinning
`assertion.ref` to `refs/heads/main` would restrict the scheduled run only, and reject every
dispatch with `unauthorized_client: The given credential is rejected by the attribute condition`.

### Optional repository variables

| Variable | Default |
|----------|---------|
| `GCP_PROJECT` | `kop12345` |
| `GCP_ZONE` | `europe-west1-b` (the cluster is zonal) |
| `GCP_REGION` | `europe-west1` (Artifact Registry location) |
| `GKE_CLUSTER_NAME` | `neo4j-operator-ci-gke` |
| `GCP_AR_REPOSITORY` | `neo4j-operator-ci` |
| `GKE_NODE_COUNT` | `2` |
| `GKE_MACHINE_TYPE` | `e2-standard-4` |

### Roles the service account needs

| Role | Why |
|------|-----|
| `roles/container.admin` | Create, describe and delete the GKE cluster |
| `roles/artifactregistry.admin` | Create the repository, push images, and grant the node account read access |
| `roles/iam.serviceAccountUser` | Attach the node service account when creating the cluster |

The node service account — the project's default compute account unless you changed it — needs to
read the repository. `ensure-gke.sh` grants `roles/artifactregistry.reader` on the repository
itself, and treats a failure as non-fatal: the binding is usually already implied by that
account's project role. If the operator Deployment ends up in `ImagePullBackOff`, that grant is
the first thing to check.

## AWS CI setup (maintainers)

### Required secrets

| Secret | Description |
|--------|-------------|
| `AWS_ACCESS_KEY_ID` | Access key of the CI IAM user |
| `AWS_SECRET_ACCESS_KEY` | Its secret access key |

A long-lived key rather than federation, unlike GCP, because an organisation policy on this account
reserves role assumption for SSO principals: an automated workload gets an IAM user. Access keys do
not expire on their own, so the credential carries an agreed rotation date rather than an
expiry — the run starts failing at `sts get-caller-identity` when it is revoked, not at a cluster
operation.

Both secrets are passed in the `with:` block of the credentials step, never a job-level `env:`. The
action exports only the resolved credentials to the remaining steps, which keeps the raw key out of
every other step's environment.

### Optional repository variables

| Variable | Default |
|----------|---------|
| `AWS_REGION` | `eu-west-1` |
| `AWS_EKS_NAME` | `neo4j-operator-ci-eks` |
| `AWS_ECR_REPOSITORY` | `neo4j-operator-ci` |
| `AWS_EKS_NODE_COUNT` | `2` |
| `AWS_EKS_NODE_INSTANCE_TYPE` | `m5.xlarge` (4 vCPU / 16 GiB, as on AKS and GKE) |
| `AWS_EKS_CLUSTER_ROLE` | `neo4j-operator-ci-eks-cluster-role` |
| `AWS_EKS_NODE_ROLE` | `neo4j-operator-ci-eks-node-role` |
| `AWS_EKS_SUBNET_IDS` | empty — discover the default VPC |

The account id is deliberately absent: `ensure-eks.sh` reads it from the caller's identity, so it
never has to live in the repository. Either role variable also accepts a full `arn:...` value, for a
role shared from another account.

### The two IAM roles, created out of band

The CI user holds `PowerUserAccess`, which covers every service **except IAM**. It can pass a role
to EKS — once granted `iam:PassRole` and `iam:GetRole` on both, as below — but never create one,
and EKS requires two. Neither script attempts to: a missing role surfaces as a `create-cluster` or
`create-nodegroup` failure naming the ARN and pointing here.

| Role | Trusted by | Attached policies |
|------|-----------|-------------------|
| `neo4j-operator-ci-eks-cluster-role` | `eks.amazonaws.com` | `AmazonEKSClusterPolicy` |
| `neo4j-operator-ci-eks-node-role` | `ec2.amazonaws.com` | `AmazonEKSWorkerNodePolicy`, `AmazonEKS_CNI_Policy`, `AmazonEC2ContainerRegistryReadOnly`, `service-role/AmazonEBSCSIDriverPolicy` |

`AmazonEBSCSIDriverPolicy` on the **node** role is what lets the EBS CSI driver provision volumes
without IRSA: `ensure-eks.sh` installs the addon with no service account role, so its controller
falls back to the node's credentials. IRSA would need an OIDC provider, which is IAM again.
`AmazonEC2ContainerRegistryReadOnly` is what lets nodes pull the operator image from ECR, the
equivalent of `az aks --attach-acr`.

Commands for whoever holds IAM in the account:

```bash
CI_USER=github-actions-fieldeng-ps-eks-test

aws iam create-role --role-name neo4j-operator-ci-eks-cluster-role \
  --assume-role-policy-document '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"eks.amazonaws.com"},"Action":"sts:AssumeRole"}]}'
aws iam attach-role-policy --role-name neo4j-operator-ci-eks-cluster-role \
  --policy-arn arn:aws:iam::aws:policy/AmazonEKSClusterPolicy

aws iam create-role --role-name neo4j-operator-ci-eks-node-role \
  --assume-role-policy-document '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}'
# AmazonEBSCSIDriverPolicy sits under the service-role/ path, unlike the other three, so these are
# full ARNs rather than a list of names — attaching the wrong path fails with NoSuchEntity.
for policy_arn in \
  arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy \
  arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy \
  arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly \
  arn:aws:iam::aws:policy/service-role/AmazonEBSCSIDriverPolicy; do
  aws iam attach-role-policy --role-name neo4j-operator-ci-eks-node-role \
    --policy-arn "${policy_arn}"
done

# Two actions, not one. PassRole is what lets the user hand a role to EKS at all; CreateNodegroup
# additionally calls GetRole on the node role on the caller's behalf, and refuses without it — a
# second failure, several minutes into a run that already created the control plane.
aws iam put-user-policy --user-name "${CI_USER}" \
  --policy-name neo4j-operator-ci-eks-passrole \
  --policy-document '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["iam:PassRole","iam:GetRole"],"Resource":["arn:aws:iam::<ACCOUNT_ID>:role/neo4j-operator-ci-eks-cluster-role","arn:aws:iam::<ACCOUNT_ID>:role/neo4j-operator-ci-eks-node-role"]}]}'
```

### Reaching the cluster as a human

EKS grants cluster-admin to whichever principal created the cluster, and to nobody else. The CI
user creates it, so `aws eks update-kubeconfig` gives you a kubeconfig whose every request is
rejected until you add an access entry for yourself:

```bash
aws eks create-access-entry --cluster-name neo4j-operator-ci-eks \
  --principal-arn "$(aws sts get-caller-identity --query Arn --output text)"
aws eks associate-access-policy --cluster-name neo4j-operator-ci-eks \
  --principal-arn "$(aws sts get-caller-identity --query Arn --output text)" \
  --policy-arn arn:aws:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy \
  --access-scope type=cluster
```

Worth doing while the cluster is still up: after the nightly teardown there is nothing to attach an
entry to.

### What a run creates

Cluster, one managed nodegroup, the `aws-ebs-csi-driver` addon and a `gp3` StorageClass — EKS ships
no gp3 class, and its default `gp2` only works through in-tree CSI migration, so `ensure-eks.sh`
declares `ebs.csi.aws.com` explicitly with `WaitForFirstConsumer`, matching how the storage suites
already bind on kind, AKS and GKE. Expect 10 to 15 minutes before the first suite starts.

Teardown deletes the nodegroups then the cluster, in that order and with a bounded wait, since EKS
refuses to delete a cluster that still owns one. The ECR repository stays.
