# Install the operator

Install the Neo4j operator CRD and controller into namespace `neo4j-operator-system`.

The operator install is **the same on every Kubernetes cluster**. Platform-specific steps (kind cluster, AKS + ACR, image load/push) live in the [quickstart guides](../quickstart/readme.md).

| Platform | End-to-end guide (cluster + operator + Neo4j) |
|----------|-----------------------------------------------|
| kind (local) | [quickstart/local-kind/install.md](../quickstart/local-kind/install.md) |
| Azure AKS | [quickstart/azure-aks/install.md](../quickstart/azure-aks/install.md) |

---

## Prerequisites

[01-prerequisites.md](01-prerequisites.md) — Kubernetes 1.28+, `kubectl`, StorageClass, tools.

Before deploying the operator you also need:

1. A working cluster with `kubectl` configured.
2. The controller container image available to the cluster (see [Platform notes](#platform-notes) below).

---

## Install

From the **repository root**:

```bash
make deploy
```

This applies, in order:

1. CRD — `config/crd/bases/neo4j.com_neo4js.yaml` via `make install` (server-side apply)
2. Namespace — `neo4j-operator-system`
3. RBAC — `config/rbac/`
4. Controller Deployment — `config/manager/`

Or step by step:

```bash
make install   # server-side apply (required — CRD schema ~1.5 MB)
kubectl apply -f config/default/namespace.yaml
kubectl apply -k config/rbac
kubectl apply -k config/manager
```

> Plain `kubectl apply -f` on the CRD fails with annotation size limits. Always use `make install`.

### Operator scheduling (tainted AKS pools)

If Neo4j nodes are tainted, the controller must tolerate the same taints or it never becomes Ready and cannot reconcile CRs. Defaults in `config/manager/manager.yaml` include:

```yaml
tolerations:
- key: dedicated
  operator: Equal
  value: neo4j
  effect: NoSchedule
```

Change those fields (and optional `nodeSelector`) to match your pool, then re-apply `kubectl apply -k config/manager`. Keep them aligned with `spec.scheduling` on Neo4j CRs.
---

## Verify

```bash
kubectl get crd neo4js.neo4j.com
kubectl get deployment -n neo4j-operator-system
kubectl get pods -n neo4j-operator-system
kubectl wait --for=condition=Available deployment/neo4j-operator-controller-manager \
  -n neo4j-operator-system --timeout=120s
```

---

## Run the controller locally (optional)

For debugging without rebuilding the container image:

```bash
make install    # CRD only
make run        # --leader-elect=false, uses kubeconfig
```

Do **not** apply `config/manager`, or scale the Deployment to zero — avoid two controllers running.

---

## Platform notes

Only the **controller image delivery** differs by platform. Cluster creation and Neo4j workload steps are documented in the quickstart guides.

| Platform | Before `make deploy` | Details |
|----------|---------------------|---------|
| **kind** | Build and load `controller:latest` into kind nodes | [quickstart/local-kind/install.md §2](../quickstart/local-kind/install.md#2-build-and-load-the-operator-image) |
| **AKS** | Push image to ACR; patch `config/manager/manager.yaml` or kustomize `images:` | [quickstart/azure-aks/install.md §2–3](../quickstart/azure-aks/install.md#2-push-the-operator-image) |
| **Other** | Push to any registry the cluster can pull from; set Deployment image accordingly | — |

Default Deployment image in manifests: `controller:latest`.

---

## Next — install Neo4j

[neo4j/readme.md](../neo4j/readme.md) · [neo4j/01-quickstart-standalone.md](../neo4j/01-quickstart-standalone.md)

---

## RBAC note

Development manifests use a **ClusterRole** for local testing. V1 product scope targets namespace-scoped RBAC ([BDR-003](../../02-technical-design/decision-records/business/operator/003-operator-install-scope.md)).

## Related design

- [BDR-003 — install scope](../../02-technical-design/decision-records/business/operator/003-operator-install-scope.md)
- [ADR-010 — operator deployment](../../02-technical-design/decision-records/architecture/010-operator-deployment.md)
