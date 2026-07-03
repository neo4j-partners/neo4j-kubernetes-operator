# Install the operator

Install the Neo4j operator CustomResourceDefinition (CRD) and controller into cluster `neo4j-operator-system`.

## Option A — Makefile (recommended for development)

From the repository root:

```bash
# 1. Build the manager binary (optional if using pre-built image)
make build

# 2. Install CRD + namespace + RBAC + operator Deployment
make deploy
```

What `make deploy` does:

1. `make install` — applies `config/crd/bases/neo4j.com_neo4js.yaml`
2. Creates namespace `neo4j-operator-system`
3. Applies RBAC from `config/rbac/`
4. Applies the controller Deployment from `config/manager/`

### Run the controller locally (alternative)

Useful when debugging without rebuilding the container image:

```bash
make install          # CRD only
make run              # starts controller on your machine (--leader-elect=false)
```

Point `kubectl` at the same cluster; the local process uses your kubeconfig.

### Build and use a custom image

```bash
make docker-build IMG=neo4j-operator:dev
# Update config/manager/manager.yaml image, or use kustomize image override, then:
make deploy
```

## Option B — kubectl only

```bash
kubectl apply -f config/crd/bases/neo4j.com_neo4js.yaml
kubectl apply -f config/default/namespace.yaml
kubectl apply -k config/rbac
kubectl apply -k config/manager
```

## Verify installation

```bash
kubectl get crd neo4js.neo4j.com
kubectl get deployment -n neo4j-operator-system
kubectl get pods -n neo4j-operator-system
kubectl wait --for=condition=Available deployment/neo4j-operator-controller-manager \
  -n neo4j-operator-system --timeout=120s
```

Expected:

- CRD `neo4js.neo4j.com` exists
- Pod `neo4j-operator-controller-manager-*` is `Running`

## Install a Standalone Neo4j (quick test)

```bash
make sample-standalone
```

This creates namespace `graph-dev` and applies [`config/samples/neo4j_v1beta1_neo4j.yaml`](../../../config/samples/neo4j_v1beta1_neo4j.yaml).

Follow [Quickstart — Standalone](../neo4j/01-quickstart-standalone.md) for status checks and credentials.

## RBAC note

Current development manifests use a **ClusterRole** so the controller can reconcile `Neo4j` resources in workload namespaces during local testing. V1 product scope targets **namespace-scoped** RBAC ([BDR-003](../../02-technical-design/decision-records/business/operator/003-operator-install-scope.md)); production packaging may differ.

## Related design docs

- Install scope decision: [BDR-003](../../02-technical-design/decision-records/business/operator/003-operator-install-scope.md)
- Operator Deployment HA: [ADR-010](../../02-technical-design/decision-records/architecture/010-operator-deployment.md)

## Next step

[Quickstart — Standalone Neo4j](../neo4j/01-quickstart-standalone.md)
