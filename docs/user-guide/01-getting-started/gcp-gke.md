# Quickstart — Google Kubernetes Engine

Minimal path from zero to a running Standalone Neo4j on [Google Kubernetes Engine (GKE)](https://cloud.google.com/kubernetes-engine/docs).

Everything here uses the published operator image and Helm chart, so you need no clone, no Docker
and no container registry of your own. If you do need to build the operator — you changed it, or
your policy requires images from your own Artifact Registry — the two extra steps live in
[Build the operator image](../02-operator-installation/02-build-image.md) and slot in before
step 2, without changing anything else on this page.

---

## Prerequisites

| Requirement | Notes |
|-------------|-------|
| Google Cloud project | Billing enabled, and permission to create a GKE cluster |
| [gcloud CLI](https://cloud.google.com/sdk/docs/install) | Installed — the commands below sign you in |
| `gke-gcloud-auth-plugin` | `gcloud components install gke-gcloud-auth-plugin` — kubectl cannot authenticate to GKE without it |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | Context set by `gcloud container clusters get-credentials` |
| [Helm](https://helm.sh/docs/intro/install/) 3.8+ | Installs the chart from its OCI registry |
| Neo4j Enterprise image | GKE nodes must pull `neo4j:<version>` — configure pull Secret if required |

Shared requirements: [operator prerequisites](../02-operator-installation/01-prerequisites.md).

Authenticate, select the project you want to bill, and enable the API that serves GKE. Enabling is
per project, once, and takes a few seconds:

```bash
gcloud auth login
gcloud config set project <PROJECT_ID>

gcloud services enable container.googleapis.com          # GKE
gcloud services enable artifactregistry.googleapis.com   # only if you push images yourself

# Confirm before creating the cluster
gcloud services list --enabled --filter="config.name:container.googleapis.com"
```

GKE provides StorageClass **`standard-rwo`** (pd-balanced, the default) and **`premium-rwo`**. Set
`storageClassName: standard-rwo` on the Neo4j CR.

---

## Install steps

Export the names you want (adjust zone and sizing):

```bash
export CLUSTER=neo4j-operator-gke
export ZONE=europe-west1-b
```

### 1. Create the GKE cluster

A zonal cluster: one control plane, nodes in one zone. Cheaper and quicker to create than a
regional one, and enough for everything on this page.

```bash
gcloud container clusters create "$CLUSTER" \
  --zone "$ZONE" \
  --num-nodes 2 \
  --machine-type e2-standard-4
gcloud container clusters get-credentials "$CLUSTER" --zone "$ZONE"
kubectl get storageclass
```

Autopilot clusters (`gcloud container clusters create-auto`) also work, with one caveat worth
knowing: Autopilot rewrites pod resource requests, so the Neo4j pods will not run with exactly
what the operator asked for. Set `spec.resources` explicitly if that matters to you.

### 2. Install the operator

The CRD ships as a release asset and the controller as a chart. Take the version from the
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

Two things this leaves at their defaults: the operator reconciles the `default` namespace only, and
the image comes from GHCR. To widen the scope, or to run your own image from an Artifact Registry
repository, see
[Watch scope and RBAC](../02-operator-installation/04-operator-scope.md) and
[Point the install at your image](../02-operator-installation/02-build-image.md#point-the-install-at-your-image).

### 3. Install Neo4j

Deploy a Standalone `Neo4j` resource backed by a Compute Engine persistent disk. The manifest is
explained field by field in [Your first Neo4j](first-neo4j.md).

**3a. Apply the CR** (no `metadata.namespace` — deploys to **`default`**)

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
          storageClassName: standard-rwo
  auth:
    generatePassword: true
EOF
```

Alternative — start from [`examples/standalone/01-minimal.yaml`](../../../examples/standalone/01-minimal.yaml)
and add `storageClassName: standard-rwo` under `spec.storage.volumes.data.dynamic`.

**3b. Watch progress**

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

`standard-rwo` binds on first use, so the PVC stays `Pending` until the pod is scheduled. That is
expected, not a failure.

**3c. Check status**

```bash
kubectl get neo4j dev -n default -o wide
kubectl get neo4j dev -n default -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){"\n"}{end}'
```

When ready:

- `status.phase`: `Running`
- `status.conditions[Ready]`: `True`
- `status.credentials.secretName`: `dev-auth`

If the Neo4j pod fails to pull the Enterprise image, create an image pull Secret and set
`spec.image.pullSecrets` on the resource — see
[Operations](../03-neo4j/09-operations.md#pulling-images-from-a-private-registry).

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
To expose Bolt through a Google Cloud load balancer instead of port-forwarding, see
[Connectivity](../03-neo4j/04-connectivity.md#loadbalancer).

---

## Tear down

Delete the resource before the cluster. Dynamically provisioned disks are released when their
PersistentVolumeClaim goes, and deleting the cluster first can leave them behind, still billing:

```bash
kubectl delete neo4j dev -n default --ignore-not-found
kubectl delete pvc -n default -l app.kubernetes.io/instance=dev

gcloud container clusters delete "$CLUSTER" --zone "$ZONE" --quiet
```

Then confirm nothing outlived it. An unattached disk costs money for as long as it exists:

```bash
gcloud compute disks list --filter="zone:${ZONE} AND -users:*"
```

To remove Neo4j but keep the cluster, note that PersistentVolumeClaims are preserved on purpose —
which is why the teardown above deletes them explicitly. See
[Uninstall](../02-operator-installation/05-uninstall.md#persistentvolumeclaim-retention).

---

## Go deeper

| Topic | Page |
|-------|------|
| The resource you just created | [Your first Neo4j](first-neo4j.md) |
| What is implemented today | [What works today](feature-status.md) |
| Running your own build, from an Artifact Registry | [Build the operator image](../02-operator-installation/02-build-image.md) |
| Reconciling namespaces other than `default` | [Watch scope and RBAC](../02-operator-installation/04-operator-scope.md) |
| Shaping the deployment — storage, connectivity, security, plugins | [Neo4j topics](../03-neo4j/readme.md) |
| Uninstalling the operator | [Uninstall](../02-operator-installation/05-uninstall.md) |
| Something is wrong | [Troubleshooting](../04-troubleshooting/01-common-issues.md) |
