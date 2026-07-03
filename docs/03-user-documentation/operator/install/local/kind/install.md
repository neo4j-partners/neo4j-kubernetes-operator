# Install the operator on kind (local)

Run the Neo4j operator on a local [kind](https://kind.sigs.k8s.io/) cluster for development and smoke tests.

## Prerequisites

- [Shared prerequisites](../../01-prerequisites.md)
- [Docker](https://docs.docker.com/get-docker/) running
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) installed
- [kubectl](https://kubernetes.io/docs/tasks/tools/) configured

Optional: [Go 1.22+](https://go.dev/dl/) and `make` to build from source.

---

## 1. Create a kind cluster

```bash
kind create cluster --name neo4j-operator
kubectl cluster-info --context kind-neo4j-operator
```

Confirm a default StorageClass exists (kind ships with `standard`):

```bash
kubectl get storageclass
```

---

## 2. Build the operator image

The Deployment manifest uses `controller:latest`. kind nodes pull images from the **local Docker daemon**, not your host registry — you must build and load the image.

```bash
# From repository root
make docker-build IMG=controller:latest
kind load docker-image controller:latest --name neo4j-operator
```

Verify the image is present on nodes (optional):

```bash
docker exec neo4j-operator-control-plane crictl images | grep controller
```

### Alternative — run controller outside the cluster

Skip the Deployment image and run the manager on your laptop (good for fast iteration):

```bash
make install    # CRD only
make run        # --leader-elect=false, uses kubeconfig
```

Do **not** apply `config/manager` if you use this mode, or scale the Deployment to zero to avoid two controllers.

---

## 3. Deploy the operator

```bash
make deploy
```

This runs `make install`, creates `neo4j-operator-system`, and applies RBAC + manager Deployment.

---

## 4. Verify

```bash
kubectl get pods -n neo4j-operator-system
kubectl wait --for=condition=Available deployment/neo4j-operator-controller-manager \
  -n neo4j-operator-system --timeout=120s
```

If the pod is `ImagePullBackOff`, the image was not loaded — repeat [step 2](#2-build-the-operator-image).

---

## 5. Deploy a test Neo4j (optional)

```bash
make sample-standalone
```

Creates namespace `graph-dev` and a Standalone `Neo4j` named `dev`. See [Quickstart — Standalone](../../../neo4j/01-quickstart-standalone.md).

### Neo4j Enterprise image on kind

The sample uses `neo4j:2026.05.0` (Enterprise). You need:

- Access to pull from Neo4j's container registry (license / credentials per Neo4j policy), **or**
- Pre-load a local image: `kind load docker-image neo4j:2026.05.0 --name neo4j-operator`

If pulls fail, check `kubectl describe pod -n graph-dev` and configure `spec.image.pullSecrets` on the `Neo4j` CR when needed.

---

## Tear down

```bash
kind delete cluster --name neo4j-operator
```

To remove workloads only: [Uninstall operator](../../03-uninstall.md).

---

## Troubleshooting

See [operator troubleshooting](../../04-troubleshooting.md) and kind-specific tips:

| Symptom | Fix |
|---------|-----|
| `ImagePullBackOff` on operator | `kind load docker-image controller:latest` |
| PVC `Pending` | `kubectl get sc` — kind should have `standard` (default) |
| Neo4j pod `ErrImagePull` | Load or pull Neo4j Enterprise image; add pull Secret |

---

## Next step

[Quickstart — Standalone Neo4j](../../../neo4j/01-quickstart-standalone.md)
