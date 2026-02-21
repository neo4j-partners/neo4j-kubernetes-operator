# GitOps Integration Guide

This directory contains configuration for integrating the Neo4j Kubernetes Operator
with GitOps tools (ArgoCD, Flux) and Prometheus monitoring.

## ArgoCD Health Checks

ArgoCD does not natively understand `status.phase` on custom resources and shows
everything as "Progressing". Apply the health check ConfigMap to teach ArgoCD how
to interpret Neo4j operator resource states.

```bash
kubectl patch configmap argocd-cm -n argocd \
  --type merge --patch-file docs/gitops/argocd-health-checks.yaml
```

Health state mapping:

| ArgoCD Status  | Neo4j Phase(s)                        |
|----------------|---------------------------------------|
| Healthy        | Ready, Completed, Succeeded, Installed |
| Degraded       | Failed, Degraded                       |
| Progressing    | Forming, Pending, Creating, or empty   |

Health checks are configured for all 7 CRDs in the `neo4j.neo4j.com` group:
`Neo4jEnterpriseCluster`, `Neo4jEnterpriseStandalone`, `Neo4jDatabase`,
`Neo4jBackup`, `Neo4jRestore`, `Neo4jPlugin`, `Neo4jShardedDatabase`.

## Flux Health Checks

Flux automatically detects readiness via `status.conditions` when CRDs expose a
standard `Ready` condition (type `Ready`, using `metav1.Condition`). No extra
Flux configuration is needed once the operator surfaces that condition.

## Prometheus ServiceMonitor

The Helm chart includes a `ServiceMonitor` for the Prometheus Operator. Enable it
at install or upgrade time:

```bash
helm upgrade --install neo4j-operator charts/neo4j-operator \
  --set metrics.enabled=true \
  --set metrics.serviceMonitor.enabled=true
```

Or set in `values.yaml`:

```yaml
metrics:
  enabled: true
  serviceMonitor:
    enabled: true
    interval: "30s"
    scrapeTimeout: "10s"
    labels: {}        # add Prometheus instance selector labels here if needed
```

The operator exposes metrics on port `8080` at `/metrics` (Prometheus text format).
