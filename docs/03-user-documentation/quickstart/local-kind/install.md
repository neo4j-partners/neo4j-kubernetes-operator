# Quickstart — kind (local)

Minimal path from zero to a running Standalone Neo4j on a local [kind](https://kind.sigs.k8s.io/) cluster.

---

## Prerequisites

| Requirement | Notes |
|-------------|-------|
| [Docker](https://docs.docker.com/get-docker/) | Running — kind and image builds |
| [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) | Cluster runtime |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | Configured after cluster creation |
| `make` | From repository root — deploy targets |
| Go 1.22+ | Optional — only for `make run` (controller on laptop) |
| Neo4j Enterprise image | Sample uses `neo4j:2026.05.0` — pull access or pre-load into kind |

Shared requirements (StorageClass, license, RBAC): [operator prerequisites](../../operator/01-prerequisites.md).

kind ships with StorageClass **`standard`** (default). No `storageClassName` override needed in the sample.

---

## Install steps

Run from the **repository root**.

### 1. Create the cluster

```bash
kind create cluster --name neo4j-operator
kubectl cluster-info --context kind-neo4j-operator
kubectl get storageclass
```

### 2. Build and load the operator image

kind nodes use the local Docker daemon — build and load before deploy:

```bash
make docker-build IMG=controller:latest
kind load docker-image controller:latest --name neo4j-operator
```

### 3. Deploy the operator

```bash
make deploy
kubectl wait --for=condition=Available deployment/neo4j-operator-controller-manager \
  -n neo4j-operator-system --timeout=120s
```

Verify the CRD and controller:

```bash
kubectl get crd neo4js.neo4j.com
kubectl get pods -n neo4j-operator-system
```

### 4. Install Neo4j

Deploy a Standalone `Neo4j` CR. Full workload guide: [neo4j/01-quickstart-standalone.md](../../neo4j/01-quickstart-standalone.md) · [neo4j documentation index](../../neo4j/readme.md).

**4a. Apply the sample**

```bash
make sample-standalone
```

Or manually:

```bash
kubectl apply -f config/samples/neo4j_v1beta1_neo4j.yaml
```

The sample omits `metadata.namespace` — Neo4j is created in **`default`**.

**4b. Pre-load the Neo4j image (if pull fails on kind)**

```bash
kind load docker-image neo4j:2026.05.0 --name neo4j-operator
```

If pulls still fail, configure `spec.image.pullSecrets` on the CR — see [neo4j/01-quickstart-standalone.md](../../neo4j/01-quickstart-standalone.md).

**4c. Watch progress**

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

**4d. Check status**

```bash
kubectl get neo4j dev -n default -o wide
kubectl get neo4j dev -n default -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){"\n"}{end}'
```

When ready:

- `status.phase`: `Running`
- `status.conditions[Ready]`: `True`
- `status.credentials.secretName`: `dev-auth`

More detail (customization, troubleshooting): [neo4j/01-quickstart-standalone.md](../../neo4j/01-quickstart-standalone.md).

### 5. Connect

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

Connection details: [neo4j/01-quickstart-standalone.md#connect](../../neo4j/01-quickstart-standalone.md#connect).

---

## Tear down

```bash
kubectl delete neo4j dev -n default --ignore-not-found
kind delete cluster --name neo4j-operator
```

PVCs may remain until explicitly deleted — see [operator/03-uninstall.md](../../operator/03-uninstall.md).

---

## Go deeper

| Topic | Doc |
|-------|-----|
| Neo4j workload (Standalone, Cluster) | [neo4j/readme.md](../../neo4j/readme.md) |
| Standalone CR, status, customize | [neo4j/01-quickstart-standalone.md](../../neo4j/01-quickstart-standalone.md) |
| Install operator (generic) | [operator/02-installation.md](../../operator/02-installation.md) |
| Shared prerequisites | [operator/01-prerequisites.md](../../operator/01-prerequisites.md) |
| Uninstall operator only | [operator/03-uninstall.md](../../operator/03-uninstall.md) |
| Troubleshooting | [operator/04-troubleshooting.md](../../operator/04-troubleshooting.md) |
