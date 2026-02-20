# External Access Guide

This guide explains how to expose your Neo4j deployments for access from outside the Kubernetes cluster.

## Overview

The Neo4j Kubernetes Operator supports multiple methods for external access:
- **Port Forwarding** - Quick access for development/testing
- **LoadBalancer** - Cloud provider managed load balancers
- **NodePort** - Direct node access (on-premise/development)
- **Ingress** - HTTP/HTTPS access through ingress controllers

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

### LoadBalancer (Recommended for Cloud)

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
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

> **Note**: Currently, Ingress needs to be manually created for Neo4jEnterpriseCluster and is not yet supported for Neo4jEnterpriseStandalone.

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

## Connection URLs

After configuring external access:

### Neo4j Browser
- **Port Forward**: http://localhost:7474
- **LoadBalancer**: http://<external-ip>:7474
- **NodePort**: http://<node-ip>:<node-port>
- **Ingress**: https://neo4j.example.com

### Bolt (Applications)
- **Port Forward**: bolt://localhost:7687
- **LoadBalancer**: bolt://<external-ip>:7687
- **NodePort**: bolt://<node-ip>:<node-port>
- **With TLS**: bolt+ssc://<host>:7687

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

For LoadBalancer services, use source ranges:
```yaml
spec:
  service:
    type: LoadBalancer
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-source-ranges: "10.0.0.0/8,172.16.0.0/12"
```

### 3. Use Strong Authentication

```yaml
spec:
  auth:
    adminSecretName: neo4j-admin-secret
    passwordPolicy:
      minLength: 12
      requireUppercase: true
      requireNumbers: true
      requireSpecialChars: true
```

## Cloud-Specific Examples

### AWS with NLB

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
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

## Manual Ingress Creation

Until automatic Ingress creation is implemented, create it manually:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: neo4j-ingress
  annotations:
    nginx.ingress.kubernetes.io/backend-protocol: "HTTP"
    nginx.ingress.kubernetes.io/proxy-body-size: "0"
    nginx.ingress.kubernetes.io/proxy-read-timeout: "600"
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - neo4j.example.com
    secretName: neo4j-tls
  rules:
  - host: neo4j.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: my-cluster-client  # or my-standalone-service
            port:
              number: 7474
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

## Future Improvements

The operator team is working on:
- Automatic Ingress creation for clusters
- Ingress support for standalone deployments
- Better service configuration for standalone
- Built-in DNS management
- OAuth/OIDC integration for Browser access
