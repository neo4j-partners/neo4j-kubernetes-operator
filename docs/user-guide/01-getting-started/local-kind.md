# Quickstart — kind (local)

Minimal path from zero to a running Standalone Neo4j on a local [kind](https://kind.sigs.k8s.io/) cluster.

---

## Prerequisites

| Requirement | Notes |
|-------------|-------|
| [Docker](https://docs.docker.com/get-docker/) | Running — kind runs its nodes as containers |
| [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) | Cluster runtime |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | Configured after cluster creation |
| [Helm](https://helm.sh/docs/intro/install/) 3.8+ | Installs the chart from its OCI registry |
| Neo4j Enterprise image | Sample uses `neo4j:2026.05.0` — pull access or pre-load into kind |

Shared requirements (StorageClass, license, RBAC): [operator prerequisites](../02-operator-installation/01-prerequisites.md).

kind ships with StorageClass **`standard`** (default). No `storageClassName` override needed in the sample.

---

## Install steps

### 1. Create the cluster

```bash
kind create cluster --name neo4j-operator
kubectl cluster-info --context kind-neo4j-operator
kubectl get storageclass
```

### 2. Install the operator

The CRD ships as a release asset and the controller as a Helm chart, both public, so nothing is
built here. Take the version from the
[latest release](https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases) without its
leading `v`:

```bash
VERSION=1.0.0-rc1

kubectl apply --server-side --force-conflicts \
  -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/v${VERSION}/neo4j-crd-${VERSION}.yaml

helm upgrade --install neo4j-operator \
  oci://ghcr.io/neo4j-partners/charts/neo4j-operator --version ${VERSION} \
  --namespace neo4j-operator-system --create-namespace \
  --wait --timeout 300s
```

Verify the CRD and controller:

```bash
kubectl get crd neo4js.neo4j.com
kubectl get pods -n neo4j-operator-system
```

Running your own build instead is the other reason to use kind. Build the image, load it into the
node — kind reads your local Docker daemon, so no registry is involved — and install the chart
against that reference with `image.pullPolicy=Never`. The three commands are in
[Build the operator image](../02-operator-installation/02-build-image.md#kind).

### 3. Install Neo4j

Deploy a Standalone `Neo4j` resource. The manifest is explained field by field in
[Your first Neo4j](first-neo4j.md).

**3a. Apply the resource** (no `metadata.namespace` — deploys to **`default`**)

```bash
kubectl apply -f - <<'EOF'
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: dev
spec:
  edition: enterprise
  version: "2026.05.0"
  license:
    accept: "yes"
  topology:
    mode: Standalone
  storage:
    volumes:
      data:
        mode: Dynamic
        dynamic:
          size: 10Gi
  auth:
    generatePassword: true
EOF
```

From a clone, [`examples/standalone/01-minimal.yaml`](../../../examples/standalone/01-minimal.yaml)
is the same resource, and kind's default `standard` StorageClass means no override is needed.

**3b. Pre-load the Neo4j image (if pull fails on kind)**

```bash
kind load docker-image neo4j:2026.05.0 --name neo4j-operator
```

If pulls still fail, configure `spec.image.pullSecrets` on the resource — see
[Operations](../03-neo4j/09-operations.md#pulling-images-from-a-private-registry).

**3c. Watch progress**

```bash
kubectl get neo4j dev -n default -w
kubectl get pods -n default -l app.kubernetes.io/instance=dev
```

Expected resources:

| Resource | Name |
|----------|------|
| StatefulSet | `dev-server` |
| Headless Service | `dev-server` |
| Client Service | `dev` |
| Auth Secret | `dev-auth` (operator-generated) |
| ConfigMap | `dev-config` |
| PVC | `data-dev-server-0` |

**3d. Check status**

```bash
kubectl get neo4j dev -n default -o wide
kubectl get neo4j dev -n default -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){"\n"}{end}'
```

When ready:

- `status.phase`: `Running`
- `status.conditions[Ready]`: `True`
- `status.credentials.secretName`: `dev-auth`

How to read those conditions, and what to do when one is `False`:
[Your first Neo4j](first-neo4j.md#4-read-the-status).

### 4. Connect

Retrieve credentials:

```bash
kubectl get secret dev-auth -n default -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d && echo
```

Port-forward Bolt:

```bash
kubectl port-forward -n default svc/dev 7687:7687
```

Use `neo4j://localhost:7687` with user `neo4j` and the password from the Secret.

Browser HTTP (optional):

```bash
kubectl port-forward -n default svc/dev 7474:7474
# Open http://localhost:7474
```

Connection details, including in-cluster URIs: [Your first Neo4j](first-neo4j.md#5-connect).

---

## Tear down

```bash
kubectl delete neo4j dev -n default --ignore-not-found
kind delete cluster --name neo4j-operator
```

Deleting the kind cluster discards the PersistentVolumes with it. On a cluster you keep, PVCs are
preserved until you delete them — see
[Uninstall](../02-operator-installation/05-uninstall.md#persistentvolumeclaim-retention).

---

## Go deeper

| Topic | Page |
|-------|------|
| The resource you just created | [Your first Neo4j](first-neo4j.md) |
| What is implemented today | [What works today](feature-status.md) |
| Shaping the deployment — storage, connectivity, security, plugins | [Neo4j topics](../03-neo4j/readme.md) |
| Installing on a non-kind cluster | [Operator installation](../02-operator-installation/readme.md) |
| Something is wrong | [Troubleshooting](../04-troubleshooting/01-common-issues.md) |
