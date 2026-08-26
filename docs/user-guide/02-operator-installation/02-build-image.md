# Build the operator image

This page is Choice B in [Operator installation](readme.md): you build the controller image from
this repository and make it reachable from your cluster. You need it when you changed the operator,
when your registry is air-gapped, or when you are developing against it — otherwise install the
published image, which needs none of this.

Commands using `make` run from the repository root. Two sections need no clone at all:
[mirroring the published image](#mirroring-the-published-image-instead-of-building) and the Helm
half of [Point the install at your image](#point-the-install-at-your-image).

## Build

```bash
make docker-build IMG=neo4j-operator:local
```

`IMG` is the tag being built. It defaults to `controller:latest`, which matches the reference
hard-coded in the manifests but is a poor choice anywhere except a scratch cluster. Keep the value
you choose at hand: installing does not infer it, you pass it again in
[Point the install at your image](#point-the-install-at-your-image).

The build is a multi-stage Dockerfile producing a distroless image that contains the manager
binary and nothing else — no shell, no package manager. That matters when you debug later:
`kubectl exec ... -- sh` will fail, and reading a file inside the container needs an ephemeral
debug container.

### Cross-building for your cluster's architecture

Docker builds for the architecture of your machine. On Apple silicon, that produces an `arm64`
image that will crash-loop on the usual `amd64` nodes with an exec format error. Ask for the
target platform explicitly:

```bash
make docker-build IMG=neo4j-operator:local DOCKER_PLATFORM=linux/amd64
```

## Make the image reachable

How the image gets to the nodes depends on your cluster.

### kind

kind nodes read your local Docker daemon, so the image goes straight into them and no registry is
involved. With the CRD already applied, that is three commands from build to running controller:

```bash
make docker-build IMG=neo4j-operator:local
kind load docker-image neo4j-operator:local --name <cluster-name>

helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system --create-namespace \
  --set image.repository=neo4j-operator \
  --set image.tag=local \
  --set image.pullPolicy=Never
```

`Never` matters here: with the default `IfNotPresent` the loaded image is still found, but any
event that makes the kubelet re-pull — and `Always` on every start — looks for a tag that exists in
no registry and fails while the image sits on the node.

### minikube

```bash
minikube image load neo4j-operator:local
```

### Any registry

Tag for the registry and push. Nothing here is specific to this operator:

```bash
docker tag neo4j-operator:local myregistry.example.com/neo4j-operator:0.1.0
docker push myregistry.example.com/neo4j-operator:0.1.0
```

For a private registry, create a pull Secret in `neo4j-operator-system` and reference it from the
operator Deployment — with the Helm install that is `imagePullSecrets` in your values. Note that
this Secret is for the operator's own image; the Secret for the Neo4j image belongs on the `Neo4j`
resource, as described in
[Operations](../03-neo4j/09-operations.md#pulling-images-from-a-private-registry).

### Azure Container Registry

Create the registry, log in, push, and let the cluster pull from it without a Secret. This assumes
the resource group and cluster from the [AKS quickstart](../01-getting-started/azure-aks.md):

```bash
export RESOURCE_GROUP=neo4j-operator-rg
export AKS_NAME=neo4j-operator-aks
export ACR_NAME=neo4joperatoracr   # globally unique, alphanumeric only

az acr create --resource-group "$RESOURCE_GROUP" --name "$ACR_NAME" --sku Basic
az acr login --name "$ACR_NAME"

export IMG="${ACR_NAME}.azurecr.io/neo4j-operator:0.1.0"
make docker-build IMG="$IMG" DOCKER_PLATFORM=linux/amd64
docker push "$IMG"

# Grant the cluster's identity AcrPull. At creation time: az aks create --attach-acr "$ACR_NAME"
az aks update --resource-group "$RESOURCE_GROUP" --name "$AKS_NAME" --attach-acr "$ACR_NAME"
```

`--attach-acr` is what replaces a pull Secret on AKS: it assigns the `AcrPull` role to the
cluster's kubelet identity, so `imagePullSecrets` stays empty. Creating an ACR requires the
`Microsoft.ContainerRegistry` provider to be registered on the subscription, as listed in the
[AKS quickstart prerequisites](../01-getting-started/azure-aks.md#prerequisites). AKS nodes are
`amd64`, hence the explicit platform on a Mac.

### Artifact Registry (GCP)

Create the repository, register gcloud as a Docker credential helper, and push. This assumes the
project and cluster from the [GKE quickstart](../01-getting-started/gcp-gke.md):

```bash
export PROJECT_ID=my-project
export REGION=europe-west1
export AR_REPO=neo4j-operator

gcloud artifacts repositories create "$AR_REPO" \
  --repository-format=docker --location "$REGION"
gcloud auth configure-docker "${REGION}-docker.pkg.dev" --quiet

export IMG="${REGION}-docker.pkg.dev/${PROJECT_ID}/${AR_REPO}/neo4j-operator:0.1.0"
make docker-build IMG="$IMG" DOCKER_PLATFORM=linux/amd64
docker push "$IMG"
```

No pull Secret is involved: GKE nodes authenticate as their own service account, which needs
`roles/artifactregistry.reader` on the repository. In a project where that account is still a
project Editor — the default — it already has it. Otherwise grant it once:

```bash
NODE_SA="$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')-compute@developer.gserviceaccount.com"
gcloud artifacts repositories add-iam-policy-binding "$AR_REPO" \
  --location "$REGION" \
  --member="serviceAccount:${NODE_SA}" \
  --role=roles/artifactregistry.reader
```

GKE nodes are `amd64`, hence the explicit platform on a Mac. A missing reader role shows up as
`ImagePullBackOff` on the operator Deployment, with the denied reference in
`kubectl describe pod`.

### Mirroring the published image instead of building

If you only need the image to come from your own registry, copy the released one — no clone, no
Docker build, and you keep the exact bits that were tested:

```bash
VERSION=1.0.0-rc1
SRC=ghcr.io/neo4j-partners/neo4j-kubernetes-operator:${VERSION}
DST=myregistry.example.com/neo4j-operator:${VERSION}

docker pull "$SRC" && docker tag "$SRC" "$DST" && docker push "$DST"
```

With [crane](https://github.com/google/go-containerregistry/tree/main/cmd/crane) or
[skopeo](https://github.com/containers/skopeo), `crane copy "$SRC" "$DST"` does the same without a
local Docker daemon and preserves the multi-architecture manifest.

## Point the install at your image

Building and pushing changes nothing on its own — the install has to reference your tag. There are
two mechanisms, and only the first one is a supported input.

### With Helm — pass the reference

```bash
helm upgrade --install neo4j-operator \
  oci://ghcr.io/neo4j-partners/charts/neo4j-operator --version 1.0.0-rc1 \
  --namespace neo4j-operator-system --create-namespace \
  --set image.repository=myregistry.example.com/neo4j-operator \
  --set image.tag=0.1.0
```

`--version` selects the chart, `image.*` selects what it runs; the two are independent, so a chart
from GHCR can perfectly well deploy an image from your registry. `image.digest` takes precedence
over `image.tag` when you want to pin bits rather than a name.

The same two values work on the chart in your working tree, `./charts/neo4j-operator`, which is
what you want when you changed the chart as well as the image. On kind, add
`--set image.pullPolicy=Never`: the image was loaded into the nodes, not pushed anywhere, so any
attempt to pull it fails.

### With the raw manifests — edit the Deployment

The manifests carry a literal image reference and no substitution: `config/manager/kustomization.yaml`
has no `images:` transformer, so `kubectl apply -k config/manager` deploys the directory as it
stands, whatever you passed to the build. Change the one line that decides it, in
`config/manager/manager.yaml`:

```yaml
      containers:
      - name: manager
        image: myregistry.example.com/neo4j-operator:0.1.0   # was controller:latest
        imagePullPolicy: IfNotPresent                        # Never on kind
```

If you would rather not touch the Deployment, add the transformer kustomize would use, then apply:

```bash
cat >> config/manager/kustomization.yaml <<'EOF'
images:
- name: controller
  newName: myregistry.example.com/neo4j-operator
  newTag: 0.1.0
EOF

kubectl apply -k config/manager
```

Either way, confirm what the cluster ended up with before blaming the build — that is the next
section. When the reference is wrong the pod sits in `ImagePullBackOff`, which
[Troubleshooting](../04-troubleshooting/01-common-issues.md) reads back to this page.

## Verify what you built

```bash
docker image inspect neo4j-operator:local --format '{{.Os}}/{{.Architecture}} {{.Created}}'
```

After installing, confirm the cluster is running that image rather than a stale one:

```bash
kubectl get deployment neo4j-operator-controller-manager \
  -n neo4j-operator-system -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
```

Re-building a tag your cluster already pulled does not restart anything. Either use a new tag, or
force a rollout:

```bash
kubectl rollout restart deployment/neo4j-operator-controller-manager -n neo4j-operator-system
```

## Skipping the image entirely

For development you can run the controller as a local process against your kubeconfig, which
needs no image at all:

```bash
kubectl apply --server-side --force-conflicts -f config/crd/bases/neo4j.com_neo4js.yaml
make run
```

Do not leave the in-cluster Deployment running at the same time; two controllers reconciling the
same resources fight each other. Scale it to zero first, or never apply it. Details in
[Install the operator](03-install.md#run-the-controller-on-your-machine).

## Next

[Install the operator](03-install.md)
