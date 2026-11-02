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

This concerns your own builds only. The published image is a manifest index covering `linux/amd64`
and `linux/arm64`, so installing it needs nothing here.

`make docker-build` produces a single-architecture image, for the architecture of your machine. On
Apple silicon that is `arm64`, which will crash-loop on the usual `amd64` nodes with an exec format
error. Ask for the target platform explicitly:

```bash
make docker-build IMG=neo4j-operator:local DOCKER_PLATFORM=linux/amd64
```

The manager is pure Go, so this cross-compiles on a native builder instead of emulating the target
— a plain `go build` for another architecture, not a slower image build. Single-architecture is
deliberate for local work: `kind load docker-image` does not take a multi-platform archive.

### OpenShift on IBM Z (linux/s390x)

OpenShift itself does not require a custom image. `platform: auto` leaves the Neo4j UID unset on
any OpenShift cluster, including Azure Red Hat OpenShift, and the published `linux/amd64` or
`linux/arm64` image runs there. This section is only for IBM Z, where that index has no
`linux/s390x` entry.

Published images, and the usual ACR build, are a manifest index of `linux/amd64` and `linux/arm64`.
An s390x kubelet refuses that index:

```text
no image found in image index for architecture "s390x"
```

`image.digest` makes it stick. The digest on `neo4joperatoracr.azurecr.io/neo4j-operator` is that
index, so the pod stays in `ImagePullBackOff` no matter which tag you set beside it. Drop the
digest and build an s390x image on the cluster. A laptop cannot produce one without emulation, and
the OpenShift nodes already are s390x.

The kit in [`hack/openshift-s390x`](../../../hack/openshift-s390x/README.md) is the path that was
run on this cluster. A Docker-strategy `BuildConfig` uploads the repo from your laptop, builds on
an s390x node, and buildah pushes the result into the internal registry. You do not `docker push`.
From the repository root, logged in with `oc`:

```bash
oc new-project neo4j-operator-system 2>/dev/null || oc project neo4j-operator-system
oc apply -f hack/openshift-s390x/builds.yaml

# Context is the repo root; the repo Dockerfile cross-compiles with TARGETARCH=s390x.
oc start-build neo4j-operator-s390x -n neo4j-operator-system --from-dir=. --follow

helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system \
  -f hack/openshift-s390x/values-s390x.yaml
```

That values file points `image.repository` at
`image-registry.openshift-image-registry.svc:5000/neo4j-operator-system/neo4j-operator` and sets
`image.tag` to `1.0.0`. It does not set `image.digest`, and it does not use `imagePullSecrets` —
the internal registry is pulled by the project's builder identity. `platform` stays at `auto`, so
the operator detects OpenShift and does not pin UID 7474.

Neo4j itself is also unpublished for s390x. The same kit builds that image (`neo4j-s390x`
BuildConfig, `Dockerfile.neo4j`) and the sample `Neo4j` resources that reference it. Follow
[`hack/openshift-s390x/README.md`](../../../hack/openshift-s390x/README.md) for that build, the
allowlist entry, and the small memory limits this cluster needed.

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
docker tag neo4j-operator:local myregistry.example.com/neo4j-operator:1.0.0
docker push myregistry.example.com/neo4j-operator:1.0.0
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

export IMG="${ACR_NAME}.azurecr.io/neo4j-operator:1.0.0"
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

export IMG="${REGION}-docker.pkg.dev/${PROJECT_ID}/${AR_REPO}/neo4j-operator:1.0.0"
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

### Elastic Container Registry (AWS)

Create the repository, hand Docker a registry token, and push. This assumes the cluster from the
[EKS quickstart](../01-getting-started/aws-eks.md):

```bash
export REGION=eu-west-1
export ECR_REPO=neo4j-operator
export ACCOUNT_ID="$(aws sts get-caller-identity --query Account --output text)"
export ECR_HOST="${ACCOUNT_ID}.dkr.ecr.${REGION}.amazonaws.com"

aws ecr create-repository --repository-name "$ECR_REPO" --region "$REGION"
aws ecr get-login-password --region "$REGION" \
  | docker login --username AWS --password-stdin "$ECR_HOST"

export IMG="${ECR_HOST}/${ECR_REPO}:1.0.0"
make docker-build IMG="$IMG" DOCKER_PLATFORM=linux/amd64
docker push "$IMG"
```

One ECR repository holds one image name, so the repository *is* the image path — unlike an ACR or an
Artifact Registry repository, which hold several. No pull Secret is involved: nodes authenticate
with their own instance role, which needs `AmazonEC2ContainerRegistryReadOnly`. A cluster created by
`eksctl` already has it. Otherwise attach it once:

```bash
NODE_ROLE="$(aws eks describe-nodegroup --cluster-name "$CLUSTER" --region "$REGION" \
  --nodegroup-name "$(aws eks list-nodegroups --cluster-name "$CLUSTER" --region "$REGION" \
    --query 'nodegroups[0]' --output text)" \
  --query 'nodegroup.nodeRole' --output text)"

aws iam attach-role-policy --role-name "${NODE_ROLE##*/}" \
  --policy-arn arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly
```

EKS nodes are `amd64`, hence the explicit platform on a Mac. The login token lasts 12 hours, so a
push that worked yesterday fails with a `401` that mentions no credentials at all — run
`get-login-password` again.

### Mirroring the published image instead of building

If you only need the image to come from your own registry, copy the released one — no clone, no
Docker build, and you keep the exact bits that were tested.

Copy it with [crane](https://github.com/google/go-containerregistry/tree/main/cmd/crane) or
[skopeo](https://github.com/containers/skopeo) rather than Docker. The released tag is a manifest
index covering `linux/amd64` and `linux/arm64`, and these copy it whole:

```bash
VERSION=1.0.0
SRC=ghcr.io/neo4j-partners/neo4j-kubernetes-operator:${VERSION}
DST=myregistry.example.com/neo4j-operator:${VERSION}

crane copy "$SRC" "$DST"
```

`docker pull` then `docker push` also works but **flattens the index to one architecture** — the
one the machine running the commands happens to be. Mirroring from an Apple silicon laptop that way
publishes an `arm64`-only tag, and the operator then crash-loops on `amd64` nodes with an exec
format error. If Docker is the only tool available, name the platform and accept that the mirror
covers that platform alone:

```bash
docker pull --platform linux/amd64 "$SRC" && docker tag "$SRC" "$DST" && docker push "$DST"
```

## Point the install at your image

Building and pushing changes nothing on its own — the install has to reference your tag. There are
two mechanisms, and only the first one is a supported input.

### With Helm — pass the reference

```bash
helm upgrade --install neo4j-operator \
  oci://ghcr.io/neo4j-partners/charts/neo4j-operator --version 1.0.0 \
  --namespace neo4j-operator-system --create-namespace \
  --set image.repository=myregistry.example.com/neo4j-operator \
  --set image.tag=1.0.0
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
        image: myregistry.example.com/neo4j-operator:1.0.0   # was controller:latest
        imagePullPolicy: IfNotPresent                        # Never on kind
```

If you would rather not touch the Deployment, add the transformer kustomize would use, then apply:

```bash
cat >> config/manager/kustomization.yaml <<'EOF'
images:
- name: controller
  newName: myregistry.example.com/neo4j-operator
  newTag: 1.0.0
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
kubectl apply --server-side --force-conflicts -k config/crd/bases
make run
```

Do not leave the in-cluster Deployment running at the same time; two controllers reconciling the
same resources fight each other. Scale it to zero first, or never apply it. Details in
[Install the operator](03-install.md#run-the-controller-on-your-machine).

## Next

[Install the operator](03-install.md)
