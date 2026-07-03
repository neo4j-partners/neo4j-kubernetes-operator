# Operator installation — choose a platform

Install the Neo4j operator CRD and controller (`neo4j-operator-system` namespace).

Shared steps (all platforms):

1. [Prerequisites](../01-prerequisites.md)
2. Pick a platform guide below
3. [Verify](#verify-installation) (same on all platforms)
4. [Quickstart — Standalone Neo4j](../../neo4j/01-quickstart-standalone.md)

---

## Platform guides

| Platform | Guide | Use case |
|----------|-------|----------|
| **Local — kind** | [local/kind/install.md](local/kind/install.md) | Development, CI smoke, no cloud account |
| **Azure — AKS** | [azure/aks/install.md](azure/aks/install.md) | Cloud validation, Azure Disk storage |

More platforms (GKE, EKS, OpenShift) will be added when V1 cloud smoke tests are defined ([dependencies](../../../02-technical-design/dependencies.md)).

---

## What every install does

Whether kind or AKS, the operator install applies:

1. CRD — `config/crd/bases/neo4j.com_neo4js.yaml`
2. Namespace — `neo4j-operator-system`
3. RBAC — `config/rbac/`
4. Controller Deployment — `config/manager/`

Makefile shortcut (from repo root):

```bash
make deploy    # install + namespace + rbac + manager
```

Or step by step:

```bash
make install   # server-side apply (required for large CRD schema)
kubectl apply -f config/default/namespace.yaml
kubectl apply -k config/rbac
kubectl apply -k config/manager
```

> **Note:** The CRD OpenAPI schema is large (~1.5 MB). Use `make install` (server-side apply) — plain `kubectl apply -f` fails with annotation size limits.

---

## Verify installation

```bash
kubectl get crd neo4js.neo4j.com
kubectl get deployment -n neo4j-operator-system
kubectl wait --for=condition=Available deployment/neo4j-operator-controller-manager \
  -n neo4j-operator-system --timeout=120s
```

---

## RBAC note

Development manifests use a **ClusterRole** so the controller can reconcile `Neo4j` CRs in workload namespaces during local testing. V1 product scope targets namespace-scoped RBAC ([BDR-003](../../../02-technical-design/decision-records/business/operator/003-operator-install-scope.md)).

## Related design

- [BDR-003 — install scope](../../../02-technical-design/decision-records/business/operator/003-operator-install-scope.md)
- [ADR-010 — operator deployment](../../../02-technical-design/decision-records/architecture/010-operator-deployment.md)
