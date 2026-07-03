# Install the operator on Azure (AKS)

Run the Neo4j operator on [Azure Kubernetes Service (AKS)](https://learn.microsoft.com/en-us/azure/aks/).

## Prerequisites

- [Shared prerequisites](../../01-prerequisites.md)
- Azure subscription with permissions to create AKS and (recommended) ACR
- [Azure CLI](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli) (`az`) logged in
- [kubectl](https://kubernetes.io/docs/tasks/tools/) configured for the AKS cluster

Optional: [Go 1.22+](https://go.dev/dl/) and `make` to build the operator image locally.

---

## 1. Create AKS (example)

Adjust names, region, and node size for your environment.

```bash
export RESOURCE_GROUP=neo4j-operator-rg
export LOCATION=westeurope
export AKS_NAME=neo4j-operator-aks
export ACR_NAME=neo4joperatoracr   # must be globally unique, alphanumeric only

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
```

Confirm storage — AKS provides **Azure Disk CSI** (`managed-csi`, `managed-csi-premium`):

```bash
kubectl get storageclass
```

Use `managed-csi` or `managed-csi-premium` in `spec.storage.volumes.data.dynamic.storageClassName` on Neo4j CRs.

---

## 2. Push the operator image to ACR

AKS cannot use a local `controller:latest` tag. Build, tag, and push to your registry:

```bash
export ACR_LOGIN_SERVER="${ACR_NAME}.azurecr.io"
export IMG="${ACR_LOGIN_SERVER}/neo4j-operator:latest"

az acr login --name "$ACR_NAME"

make docker-build IMG="$IMG"
docker push "$IMG"
```

---

## 3. Configure the Deployment image

Patch the manager manifest to use your ACR image before deploy.

**Option A — edit `config/manager/manager.yaml`**

Set `spec.template.spec.containers[0].image` to `"${ACR_LOGIN_SERVER}/neo4j-operator:latest"`.

**Option B — kustomize image override**

Create or extend a kustomization (example overlay):

```yaml
# config/manager/kustomization.yaml
images:
- name: controller
  newName: <your-acr>.azurecr.io/neo4j-operator
  newTag: latest
```

---

## 4. Deploy the operator

From the repository root:

```bash
make install
kubectl apply -f config/default/namespace.yaml
kubectl apply -k config/rbac
kubectl apply -k config/manager
```

Or, if the default kustomize image is already patched:

```bash
make deploy
```

---

## 5. Verify

```bash
kubectl get crd neo4js.neo4j.com
kubectl get pods -n neo4j-operator-system
kubectl wait --for=condition=Available deployment/neo4j-operator-controller-manager \
  -n neo4j-operator-system --timeout=300s
```

---

## 6. Deploy a Standalone Neo4j on AKS

Create a namespace and manifest. Set `storageClassName` for Azure Disk:

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: dev
  namespace: graph-dev
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
```

```bash
kubectl create namespace graph-dev
kubectl apply -f neo4j-standalone.yaml
```

Full walkthrough: [Quickstart — Standalone](../../../neo4j/01-quickstart-standalone.md).

### Neo4j Enterprise image pull

Ensure AKS nodes can pull `neo4j:<version>` from Neo4j's registry. If required, create an image pull Secret and reference it via `spec.image.pullSecrets` on the `Neo4j` CR.

---

## Azure-specific notes

| Topic | V1 guidance |
|-------|-------------|
| **Storage** | `managed-csi` / `managed-csi-premium` ([dependencies](../../../../02-technical-design/dependencies.md)) |
| **LoadBalancer** | V1 Neo4j uses ClusterIP only; Azure LB deferred |
| **Workload Identity** | For future backup/cloud storage — not required for Slice 1 operator install |
| **Pod Security** | AKS may enforce PSS baseline; operator Deployment uses restricted-friendly settings |

---

## Tear down

Remove the operator: [Uninstall](../../03-uninstall.md).

Delete Azure resources:

```bash
az group delete --name "$RESOURCE_GROUP" --yes --no-wait
```

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| Operator `ImagePullBackOff` | Verify ACR attach, image push, and `manager.yaml` image path |
| PVC `Pending` | Check `storageClassName` matches an AKS StorageClass |
| Neo4j `Pending` | Node pool size / Neo4j memory; check events on pod and PVC |

General issues: [operator troubleshooting](../../04-troubleshooting.md).

---

## Next step

[Quickstart — Standalone Neo4j](../../../neo4j/01-quickstart-standalone.md)
