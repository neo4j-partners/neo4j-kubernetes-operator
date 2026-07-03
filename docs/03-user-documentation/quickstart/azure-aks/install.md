# Quickstart — Azure AKS

Minimal path from zero to a running Standalone Neo4j on [Azure Kubernetes Service (AKS)](https://learn.microsoft.com/en-us/azure/aks/).

---

## Prerequisites

| Requirement | Notes |
|-------------|-------|
| Azure subscription | Permissions to create resource group, AKS, ACR |
| [Azure CLI](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli) (`az`) | Logged in (`az login`) |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | After `az aks get-credentials` |
| `make`, Docker | Build and push operator image to ACR |
| Neo4j Enterprise image | AKS nodes must pull `neo4j:<version>` — configure pull Secret if required |

Shared requirements: [operator prerequisites](../../operator/01-prerequisites.md).

AKS provides StorageClass **`managed-csi`** (and `managed-csi-premium`). Set `storageClassName: managed-csi` on the Neo4j CR.

---

## Install steps

Run from the **repository root** after exporting names (adjust region and sizing):

```bash
export RESOURCE_GROUP=neo4j-operator-rg
export LOCATION=westeurope
export AKS_NAME=neo4j-operator-aks
export ACR_NAME=neo4joperatoracr   # globally unique, alphanumeric only
```

### 1. Create AKS and ACR

```bash
az group create --name "$RESOURCE_GROUP" --location "$LOCATION"
az acr create --resource-group "$RESOURCE_GROUP" --name "$ACR_NAME" --sku Basic
az aks create \
  --resource-group "$RESOURCE_GROUP" \
  --name "$AKS_NAME" \
  --node-count 2 \
  --node-vm-size Standard_D4s_v3 \
  --attach-acr "$ACR_NAME" \
  --generate-ssh-keys
az aks get-credentials --resource-group "$RESOURCE_GROUP" --name "$AKS_NAME"
kubectl get storageclass
```

### 2. Push the operator image

```bash
export ACR_LOGIN_SERVER="${ACR_NAME}.azurecr.io"
export IMG="${ACR_LOGIN_SERVER}/neo4j-operator:latest"

az acr login --name "$ACR_NAME"
make docker-build IMG="$IMG"
docker push "$IMG"
```

Patch `config/manager/manager.yaml` (or kustomize `images:`) so the Deployment uses `"${ACR_LOGIN_SERVER}/neo4j-operator:latest"`.

### 3. Deploy the operator

```bash
make install
kubectl apply -f config/default/namespace.yaml
kubectl apply -k config/rbac
kubectl apply -k config/manager
kubectl wait --for=condition=Available deployment/neo4j-operator-controller-manager \
  -n neo4j-operator-system --timeout=300s
```

Verify the CRD and controller:

```bash
kubectl get crd neo4js.neo4j.com
kubectl get pods -n neo4j-operator-system
```

### 4. Install Neo4j

Deploy a Standalone `Neo4j` CR with Azure Disk storage. Full workload guide: [neo4j/01-quickstart-standalone.md](../../neo4j/01-quickstart-standalone.md) · [neo4j documentation index](../../neo4j/readme.md).

**4a. Apply the CR** (no `metadata.namespace` — deploys to **`default`**)

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
          storageClassName: managed-csi
  auth:
    generatePassword: true
EOF
```

Alternative — patch [`config/samples/neo4j_v1beta1_neo4j.yaml`](../../../../config/samples/neo4j_v1beta1_neo4j.yaml) with `storageClassName: managed-csi`, then:

```bash
kubectl apply -f config/samples/neo4j_v1beta1_neo4j.yaml
```

**4b. Watch progress**

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

**4c. Check status**

```bash
kubectl get neo4j dev -n default -o wide
kubectl get neo4j dev -n default -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){"\n"}{end}'
```

When ready:

- `status.phase`: `Running`
- `status.conditions[Ready]`: `True`
- `status.credentials.secretName`: `dev-auth`

If the Neo4j pod fails to pull the Enterprise image, create an image pull Secret and set `spec.image.pullSecrets` on the CR — see [neo4j/01-quickstart-standalone.md](../../neo4j/01-quickstart-standalone.md).

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
# Operator: see operator/03-uninstall.md
az group delete --name "$RESOURCE_GROUP" --yes --no-wait
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
| Uninstall | [operator/03-uninstall.md](../../operator/03-uninstall.md) |
| Troubleshooting | [operator/04-troubleshooting.md](../../operator/04-troubleshooting.md) |
