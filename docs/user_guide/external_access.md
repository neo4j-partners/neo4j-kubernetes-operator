# External Access Guide

This guide explains how to expose your Neo4j deployments for access from outside the Kubernetes cluster.

## Overview

The Neo4j Kubernetes Operator supports multiple methods for external access:

- **Port Forwarding** - Quick access for development/testing
- **LoadBalancer** - Cloud provider managed load balancers
- **NodePort** - Direct node access (on-premise/development)
- **Ingress** - HTTP/HTTPS access through ingress controllers

## Services created by the operator

The operator names its Services by deployment **kind**, and the names are
stable: they are baked into the issued TLS certificate's SANs and into each
pod's stable DNS identity, so they are **not** interchangeable between cluster
and standalone.

| Service | `Neo4jEnterpriseCluster` | `Neo4jEnterpriseStandalone` | Ports |
|---|---|---|---|
| Client-facing (your apps connect here) | `{name}-client` | `{name}-service` | `bolt 7687`, `http 7474`, `https 7473` (TLS); standalone also exposes `metrics 2004` here when monitoring is on |
| Headless (stable per-pod DNS) | `{name}-headless` | `{name}-headless` | `bolt 7687`, `http 7474`, `backup 6362`, `https 7473` (TLS) |
| Discovery / internals (cluster only) | `{name}-discovery`, `{name}-internals` | — | clustering ports (`6000/7000/7688/7689`) |
| Metrics (cluster only; standalone folds metrics into its client service) | `{name}-metrics` (`2004`) | — | `2004` |

Notes:

- **Client-service name differs by kind** (`-client` for clusters, `-service`
  for standalone). This is intentional and load-bearing — use the right name
  for the kind you deployed. A pod's stable address is always
  `{name}-0.{name}-headless.<namespace>.svc.cluster.local`.
- **The standalone headless service exposes `bolt`/`http`/`backup` (and `https`
  under TLS)** for direct per-pod addressing, mirroring the cluster headless
  service. It does **not** expose the clustering ports (`6000/7000/7688/7689`):
  a standalone is a single non-clustered node, so nothing listens on them.

## Quick Start

### Development Access

The fastest way to access Neo4j during development:

```bash
# For Neo4jEnterpriseCluster
kubectl port-forward svc/my-cluster-client 7474:7474 7687:7687

# For Neo4jEnterpriseStandalone
kubectl port-forward svc/my-standalone-service 7474:7474 7687:7687
```

Access Neo4j Browser at: http://localhost:7474

## Service Configuration

Ready-to-apply manifests for each exposure type (set `spec.service.type` /
Ingress / Route accordingly):

| Exposure | Example |
|---|---|
| LoadBalancer | [`examples/clusters/loadbalancer-cluster.yaml`](https://github.com/neo4j-partners/neo4j-kubernetes-operator/blob/main/examples/clusters/loadbalancer-cluster.yaml) |
| NodePort | [`examples/clusters/nodeport-cluster.yaml`](https://github.com/neo4j-partners/neo4j-kubernetes-operator/blob/main/examples/clusters/nodeport-cluster.yaml) |
| Ingress | [`examples/clusters/ingress-cluster.yaml`](https://github.com/neo4j-partners/neo4j-kubernetes-operator/blob/main/examples/clusters/ingress-cluster.yaml) |
| OpenShift Route | [`examples/clusters/route-cluster.yaml`](https://github.com/neo4j-partners/neo4j-kubernetes-operator/blob/main/examples/clusters/route-cluster.yaml) |

> These exposure paths aren't covered by the operator's integration tests (they
> need a real cloud LB / Ingress controller / OpenShift), so validate them in
> your own environment.

### LoadBalancer (Recommended for Cloud)

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: production-cluster
spec:
  service:
    type: LoadBalancer
    annotations:
      # AWS Network Load Balancer
      service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
      service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled: "true"

      # GCP Load Balancer
      # cloud.google.com/load-balancer-type: "Internal"

      # Azure Load Balancer
      # service.beta.kubernetes.io/azure-load-balancer-internal: "true"
```

After deployment:
```bash
# Get the external IP/hostname
kubectl get svc production-cluster-client
```

### NodePort (On-Premise/Development)

```yaml
spec:
  service:
    type: NodePort
```

Access via any node IP and the assigned port:
```bash
# Get the node port
kubectl get svc my-cluster-client -o jsonpath='{.spec.ports[?(@.name=="bolt")].nodePort}'
```

### Ingress (HTTP/HTTPS Access)

Both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone` reconcile an Ingress object automatically when `spec.service.ingress.enabled: true` is set.

```yaml
spec:
  service:
    ingress:
      enabled: true
      className: nginx
      host: neo4j.example.com
      tlsSecretName: neo4j-tls
      annotations:
        cert-manager.io/cluster-issuer: letsencrypt-prod
        nginx.ingress.kubernetes.io/backend-protocol: "HTTP"
```

### OpenShift Route

OpenShift clusters can use Route in place of Ingress. The operator reconciles a `route.openshift.io/v1` Route for both `Neo4jEnterpriseCluster` and `Neo4jEnterpriseStandalone` when `spec.service.route.enabled: true`.

```yaml
spec:
  service:
    route:
      enabled: true
      host: neo4j.apps.example.com   # optional; OpenShift generates one if empty
      targetPort: 7474               # defaults to 7474 (HTTP)
      tls:
        termination: edge            # edge | reencrypt | passthrough
        insecureEdgeTerminationPolicy: Redirect
```

### Public DNS via external-dns

When you expose Neo4j via `LoadBalancer` or `Ingress`, the cloud provider assigns a hostname like `a1b2c3.elb.us-east-1.amazonaws.com`. To map your real domain (`neo4j.example.com`) to that endpoint you typically install [external-dns](https://github.com/kubernetes-sigs/external-dns) — a Kubernetes controller that watches Services/Ingresses for an annotation and creates the matching record in your cloud DNS provider (Route 53, Cloud DNS, Azure DNS, Cloudflare, etc.).

Set `spec.service.dnsName` and the operator will:

1. Add `external-dns.alpha.kubernetes.io/hostname: <dnsName>` to the front-facing Service and (when enabled) the Ingress. external-dns picks this up and creates/updates the DNS record automatically.
2. Add `<dnsName>` to the cert-manager `Certificate`'s SAN list when `spec.tls` is enabled, so that `bolt+s://<dnsName>:7687` and `https://<dnsName>:7473` pass hostname verification.

```yaml
spec:
  service:
    type: LoadBalancer
    dnsName: neo4j.example.com
    # Optional: skip the typed field and supply the annotation directly via
    # spec.service.annotations — user-supplied annotations always win.
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer
```

Prerequisites: install [external-dns](https://github.com/kubernetes-sigs/external-dns/blob/master/docs/registry/registry.md) in your cluster and grant it write access to the DNS zone (IAM role on AWS, workload identity on GCP, service principal on Azure, API token elsewhere).

**Scope and limitations**

- `dnsName` covers the **single-endpoint Bolt** case (`bolt+s://<dnsName>:7687`) and the **Browser** case (`https://<dnsName>:7473`). External clients connecting via the routing scheme (`neo4j+s://`) need per-pod public endpoints; that's out of scope here because the driver fetches a routing table containing internal pod FQDNs that aren't externally resolvable.
- If you set the `external-dns.alpha.kubernetes.io/hostname` annotation directly in `spec.service.annotations`, that wins — the operator will not overwrite it.
- When `spec.service.ingress.enabled: true`, set `spec.service.ingress.host` to the same value as `spec.service.dnsName` so the Ingress controller routes the right host to your backend.

## Connection URLs

After configuring external access:

### Neo4j Browser
- **Port Forward**: `http://localhost:7474`
- **LoadBalancer**: `http://<external-ip>:7474`
- **NodePort**: `http://<node-ip>:<node-port>`
- **Ingress**: `https://neo4j.example.com`

For **SSO into Neo4j Browser** (login button → IdP redirect), configure one or more OIDC providers via `spec.auth.oidc` on the `Neo4jEnterpriseCluster` / `Neo4jEnterpriseStandalone` resource. Neo4j Browser supports OIDC natively — no oauth2-proxy or external gateway needed. See the [Authentication & Authorization guide](guides/security.md) for the field reference and provider examples (Okta, Azure AD, Google, etc.).

### Bolt (Applications)
- **Port Forward**: `bolt://localhost:7687`
- **LoadBalancer**: `bolt://<external-ip>:7687`
- **NodePort**: `bolt://<node-ip>:<node-port>`

**With TLS** — pick the scheme that matches your trust chain:

| Scheme | When to use |
|---|---|
| `bolt+s://<host>:7687` | Production. CA-signed certificate; the client validates the server cert against the system trust store. |
| `bolt+ssc://<host>:7687` | Development. Self-signed certificate; the client trusts any presented cert. **Never use in production** — vulnerable to MITM. |
| `neo4j+s://<host>:7687` | Routing scheme for clusters with CA-signed certificates. The driver discovers cluster members via `dbms.routing.getRoutingTable` and routes writes to the leader. The operator itself uses this scheme internally for admin calls (CLAUDE.md rule 11). |
| `neo4j+ssc://<host>:7687` | Routing scheme with self-signed certs. Dev only. |

Plain `bolt://` (no TLS) is **rejected** by Neo4j when the operator has TLS enabled — the connector is configured with `server.bolt.tls_level=REQUIRED`.

## Security Considerations

### 1. Always Use TLS in Production

```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer
```

### 2. Restrict Access

For LoadBalancer services, use the typed `loadBalancerSourceRanges` field (cloud-agnostic, K8s-native):

```yaml
spec:
  service:
    type: LoadBalancer
    loadBalancerSourceRanges:
      - 10.0.0.0/8
      - 172.16.0.0/12
```

Some clouds (e.g. AWS NLB) require a provider-specific annotation instead — check your cloud provider's docs.

### 3. Use Strong Authentication

```yaml
spec:
  auth:
    adminSecret: neo4j-admin-secret
  config:
    dbms.security.auth_minimum_password_length: "12"
```

## Cloud-Specific Examples

### AWS with NLB

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: aws-cluster
spec:
  service:
    type: LoadBalancer
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
      service.beta.kubernetes.io/aws-load-balancer-backend-protocol: "tcp"
      service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled: "true"
      service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout: "3600"
```

### GCP with Internal Load Balancer

```yaml
spec:
  service:
    type: LoadBalancer
    annotations:
      cloud.google.com/load-balancer-type: "Internal"
      networking.gke.io/internal-load-balancer-allow-global-access: "true"
```

### Azure with Public IP

```yaml
spec:
  service:
    type: LoadBalancer
    annotations:
      service.beta.kubernetes.io/azure-load-balancer-internal: "false"
      service.beta.kubernetes.io/azure-dns-label-name: "my-neo4j-cluster"
```

## Troubleshooting

### Service Not Accessible

1. Check service status:
   ```bash
   kubectl get svc
   kubectl describe svc <service-name>
   ```

2. Verify endpoints:
   ```bash
   kubectl get endpoints <service-name>
   ```

3. Test connectivity from within cluster:
   ```bash
   kubectl run test-pod --image=busybox -it --rm -- sh
   wget -O- http://<service-name>:7474
   ```

### LoadBalancer Stuck in Pending

- Check cloud provider quota
- Verify IAM permissions
- Check service annotations

### Connection Timeouts

- Increase timeout values in load balancer annotations
- Check security groups/firewall rules
- Verify Neo4j is ready: `kubectl logs <pod-name>`

## Best Practices

1. **Use LoadBalancer for Production**: Most reliable for cloud deployments
2. **Port Forwarding for Development**: Simple and secure for local access
3. **Always Enable TLS**: Essential for production security
4. **Monitor External IPs**: Set up alerts for IP changes
5. **Use DNS**: Point domain names to load balancer IPs
6. **Implement Rate Limiting**: Protect against abuse
