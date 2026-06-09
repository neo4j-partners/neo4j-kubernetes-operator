# Standalone Examples

Single-node `Neo4jEnterpriseStandalone` deployments — for development, testing,
and simple workloads that don't need clustering. Create the admin Secret first:

```bash
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password='<your-secure-password>'
```

| Example | Description |
|---|---|
| [`single-node-standalone.yaml`](single-node-standalone.yaml) | Basic single-node standalone (start here) |
| [`tls-standalone.yaml`](tls-standalone.yaml) | TLS enabled via cert-manager (`ca-cluster-issuer`) |
| [`standalone-with-trusted-ca.yaml`](standalone-with-trusted-ca.yaml) | Trust an internal/corporate CA via `spec.trustedCASecrets` |
| [`ldap-standalone.yaml`](ldap-standalone.yaml) | LDAP authentication provider |

```bash
kubectl apply -f examples/standalone/single-node-standalone.yaml
kubectl get neo4jenterprisestandalone
kubectl port-forward svc/standalone-neo4j-service 7474:7474 7687:7687
```

> For external access (LoadBalancer / NodePort / Ingress / OpenShift Route), see
> the cluster examples under [`../clusters/`](../clusters) — the `spec.service`
> shape is identical for standalone and cluster.
