# Build the operator image

This page is Choice B in [Operator installation](readme.md): you build the controller image from
this repository and make it reachable from your cluster. You need it when you changed the operator,
when your registry is air-gapped, or when you are developing against it — otherwise install the
published image, which needs none of this.

Everything below runs from the repository root.

## Build

```bash
make docker-build IMG=neo4j-operator:local
```

`IMG` is the tag every install path reads, so use the same value later in `make deploy` or
`make helm-install`. It defaults to `controller:latest`, which matches the reference in the
manifests but is a poor choice anywhere except a scratch cluster.

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

Load it straight into the node containers, no registry needed:

```bash
kind load docker-image neo4j-operator:local --name <cluster-name>
```

Combine that with `imagePullPolicy: IfNotPresent` or `Never`. With `Always`, the kubelet tries to
pull a tag that exists nowhere and fails despite the image being present locally.

### minikube

```bash
minikube image load neo4j-operator:local
```

### Any registry

Tag for the registry, push, and use that reference at install time:

```bash
docker tag neo4j-operator:local myregistry.example.com/neo4j-operator:0.1.0
docker push myregistry.example.com/neo4j-operator:0.1.0

make deploy IMG=myregistry.example.com/neo4j-operator:0.1.0
```

For a private registry, create a pull Secret in `neo4j-operator-system` and reference it from the
operator Deployment — with the Helm install that is `imagePullSecrets` in your values. Note that
this Secret is for the operator's own image; the Secret for the Neo4j image belongs on the `Neo4j`
resource, as described in
[Operations](../03-neo4j/09-operations.md#pulling-images-from-a-private-registry).

On Azure, `az acr login --name <registry>` before pushing, and attach the registry to the cluster
so nodes can pull without a Secret. The full sequence is in
[Azure AKS](../01-getting-started/azure-aks.md).

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
make install   # CRD only
make run
```

Do not leave the in-cluster Deployment running at the same time; two controllers reconciling the
same resources fight each other. Scale it to zero first, or never apply it. Details in
[Install the operator](03-install.md#run-the-controller-on-your-machine).

## Next

[Install the operator](03-install.md)
