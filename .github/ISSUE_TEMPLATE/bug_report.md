---
name: Bug report
about: Report a problem with the operator
title: "[bug] "
labels: ["bug"]
---

## What happened

<!-- A clear description of the bug. -->

## Expected behavior

## Steps to reproduce

1.
2.

## Environment

- Operator version (image tag / Helm chart version):
- Install method (Helm repo / OCI / `kubectl apply` bundle / kustomize):
- Kubernetes version and platform (Kind / AKS / EKS / GKE / OpenShift):
- Neo4j version (e.g. `5.26-enterprise`, `2025.12-enterprise`):
- cert-manager installed? (yes/no):

## Logs / status

<!-- Helpful commands:
  kubectl logs -n neo4j-operator-system deploy/neo4j-operator-controller-manager
  kubectl describe neo4jenterprisecluster <name>
  kubectl get events -A --sort-by=.lastTimestamp | tail -30
-->

```
<paste relevant logs / status / events here>
```
