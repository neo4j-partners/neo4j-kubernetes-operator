# Quick Start Guide

This comprehensive guide helps you get started with the Neo4j Kubernetes Operator, whether you're new to Kubernetes or an experienced user. We'll walk you through everything from basic concepts to advanced deployment scenarios.

## 📚 Before You Begin

### What You'll Learn

- How to deploy Neo4j clusters on Kubernetes using the operator
- Basic and advanced configuration options
- Monitoring and troubleshooting techniques
- Best practices for production deployments

### Prerequisites

**For Beginners:**

- Basic understanding of databases (Neo4j knowledge helpful but not required)
- Familiarity with command-line tools
- Kubernetes cluster access (we'll help you set one up)

**Technical Requirements:**

- Kubernetes cluster (1.19+ recommended)
- `kubectl` CLI tool installed and configured
- At least 4GB RAM and 2 CPU cores available in your cluster
- StorageClass configured for persistent volumes
- **Neo4j Enterprise License** - This operator **ONLY** supports Neo4j Enterprise Edition 5.26+

> **⚠️ Important**: This operator is designed exclusively for Neo4j Enterprise Edition. Neo4j Community Edition is explicitly not supported and will be rejected during validation.

### Enterprise Version Enforcement

The operator performs runtime validation to ensure only Enterprise editions are used:

1. **Edition Check**: Verifies Neo4j instance is Enterprise edition
2. **Version Check**: Ensures version is 5.26 or higher
3. **Connection Validation**: Tests connectivity before proceeding

If you attempt to use Community Edition, you'll see an error like:

```
Neo4j Community Edition is not supported. Only Neo4j Enterprise 5.26+ is supported
```

**Optional but Recommended:**

- `helm` CLI tool (v3.0+)
- Docker or compatible container runtime
- Git (for contributing to the project)

## 🚀 Installation Options

### Option 1: Quick Demo Setup (Recommended for Beginners)

Perfect for testing and learning. Uses Kind (Kubernetes in Docker) to create a local cluster.

```bash
# One-line setup for local development/testing
make setup-dev && make install-hooks && make dev-cluster && make install
```

This command will:

- Install all necessary development tools
- Create a local Kubernetes cluster using Kind
- Install the Neo4j Operator
- Set up monitoring and debugging tools

### Option 2: Helm Installation (Recommended for Production)

```bash
# Add the Neo4j Helm repository
helm repo add neo4j https://helm.neo4j.com/
helm repo update

# Install the operator
helm install neo4j-operator neo4j/neo4j-operator \
  --namespace neo4j-operator-system \
  --create-namespace \
  --set image.tag=latest

# Verify installation
kubectl get pods -n neo4j-operator-system
```

### Option 3: Manual Kubernetes Manifests

```bash
# Apply the CRDs and operator manifests
kubectl apply -f https://github.com/neo4j-labs/neo4j-kubernetes-operator/releases/latest/download/neo4j-operator.yaml

# Wait for operator to be ready
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=neo4j-operator -n neo4j-operator-system --timeout=300s
```

## 🎯 Your First Neo4j Cluster

### Step 1: Create Authentication Secret

Neo4j requires authentication. Let's create a secret with the default credentials:

```bash
kubectl create secret generic neo4j-auth \
  --from-literal=username=neo4j \
  --from-literal=password=your-secure-password-here
```

**Security Note:** In production, use a strong, randomly generated password.

### Step 2: Enterprise License Setup

Create a secret containing your Neo4j Enterprise license:

```bash
# Create the license secret
kubectl create secret generic neo4j-enterprise-license \
  --from-literal=license="$(cat /path/to/your/neo4j.license)" \
  -n default

# Verify the secret was created
kubectl get secret neo4j-enterprise-license -o yaml
```

**Important:** Replace `/path/to/your/neo4j.license` with the actual path to your Neo4j Enterprise license file.

### Step 3: Deploy a Basic Cluster

Create your first Neo4j cluster with this simple configuration:

```yaml
# basic-cluster.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: my-first-cluster
  namespace: default
spec:
  # Neo4j Docker image configuration
  image:
    repo: neo4j
    tag: "5.26-enterprise"
    pullPolicy: IfNotPresent

  # Cluster topology (high availability setup)
  topology:
    primaries: 3    # Number of primary (read-write) instances
    secondaries: 0  # Number of secondary (read-only) instances

  # Storage configuration
  storage:
    className: "standard"  # Use your cluster's default StorageClass
    size: "10Gi"           # Storage size per instance

  # Authentication configuration
  auth:
    provider: native       # Use Neo4j's built-in authentication
    secretRef: neo4j-auth  # Reference to the secret we created

  # Resource requests and limits
  resources:
    requests:
      cpu: "500m"
      memory: "2Gi"
    limits:
      cpu: "2"
      memory: "4Gi"
```

Apply the configuration:

```bash
kubectl apply -f basic-cluster.yaml
```

### Step 4: Monitor the Deployment

Watch your cluster come online:

```bash
# Check the cluster status
kubectl get neo4jenterprisecluster my-first-cluster -o wide

# Watch pods being created
kubectl get pods -l app.kubernetes.io/instance=my-first-cluster -w

# Check detailed status (wait for "Ready" condition)
kubectl describe neo4jenterprisecluster my-first-cluster
```

### Step 5: Access Your Neo4j Cluster

Once your cluster is ready, you can access it:

```bash
# Port forward to access Neo4j Browser
kubectl port-forward service/my-first-cluster-client 7474:7474 7687:7687 &

# Get the Neo4j password
kubectl get secret neo4j-auth -o jsonpath='{.data.password}' | base64 --decode

# Open Neo4j Browser
open http://localhost:7474
```

**Login Credentials:**

- URL: `bolt://localhost:7687`
- Username: `neo4j`
- Password: (the password you set in the secret)

## 🌐 Accessing Neo4j from Outside the Cluster

While port-forwarding is great for development and testing, production environments typically need persistent external access. Here are the recommended approaches:

### Method 1: LoadBalancer Service (Recommended for Cloud)

The easiest way for cloud deployments is to use a LoadBalancer service:

```yaml
# external-access-lb.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: external-cluster
spec:
  image:
    repo: neo4j
    tag: "5.26-enterprise"
  topology:
    primaries: 3
    secondaries: 0

  # External access configuration
  services:
    client:
      type: LoadBalancer
      annotations:
        # AWS
        service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
        service.beta.kubernetes.io/aws-load-balancer-scheme: "internet-facing"
        # GCP
        cloud.google.com/load-balancer-type: "External"
        # Azure
        service.beta.kubernetes.io/azure-load-balancer-internal: "false"
      ports:
        - name: bolt
          port: 7687
          targetPort: 7687
        - name: http
          port: 7474
          targetPort: 7474
        - name: https
          port: 7473
          targetPort: 7473

  # Security configuration for external access
  security:
    tls:
      enabled: true
      secretName: neo4j-tls-cert
    auth:
      provider: native
      secretRef: neo4j-auth

  storage:
    size: "10Gi"
```

After applying, get the external IP:

```bash
kubectl apply -f external-access-lb.yaml

# Wait for external IP assignment
kubectl get service external-cluster-client -w

# Once assigned, connect using:
# bolt://<EXTERNAL-IP>:7687
# http://<EXTERNAL-IP>:7474
```

### Method 2: NodePort Service (On-Premises/Bare Metal)

For on-premises or when LoadBalancer isn't available:

```yaml
# external-access-nodeport.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: nodeport-cluster
spec:
  image:
    repo: neo4j
    tag: "5.26-enterprise"
  topology:
    primaries: 3

  services:
    client:
      type: NodePort
      ports:
        - name: bolt
          port: 7687
          targetPort: 7687
          nodePort: 30687  # Accessible on all nodes at this port
        - name: http
          port: 7474
          targetPort: 7474
          nodePort: 30474  # Accessible on all nodes at this port

  # Enable TLS for secure external access
  security:
    tls:
      enabled: true

  storage:
    size: "10Gi"
```

Access via any node's IP:

```bash
# Get node IPs
kubectl get nodes -o wide

# Connect using any node IP:
# bolt://<NODE-IP>:30687
# http://<NODE-IP>:30474
```

### Method 3: Ingress Controller (HTTPS/HTTP Only)

For HTTP/HTTPS access through an ingress controller:

```yaml
# external-access-ingress.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: ingress-cluster
spec:
  image:
    repo: neo4j
    tag: "5.26-enterprise"
  topology:
    primaries: 3

  # Ingress configuration
  ingress:
    enabled: true
    className: "nginx"  # or your ingress class
    annotations:
      cert-manager.io/cluster-issuer: "letsencrypt-prod"
      nginx.ingress.kubernetes.io/ssl-redirect: "true"
      nginx.ingress.kubernetes.io/backend-protocol: "HTTP"
    hosts:
      - host: neo4j.yourdomain.com
        paths:
          - path: /
            pathType: Prefix
            service:
              name: ingress-cluster-client
              port: 7474
    tls:
      - secretName: neo4j-ingress-tls
        hosts:
          - neo4j.yourdomain.com

  # Browser-only access via ingress
  services:
    client:
      type: ClusterIP  # Internal only, accessed via ingress

  storage:
    size: "10Gi"
```

**Note:** Ingress controllers typically only support HTTP/HTTPS, not the Bolt protocol. For Bolt access, combine with NodePort or LoadBalancer for port 7687.

### Method 4: Bolt Proxy for Ingress

To expose Bolt through ingress, use a TCP proxy:

```yaml
# bolt-proxy-ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: neo4j-bolt-proxy
  annotations:
    nginx.ingress.kubernetes.io/tcp-services-configmap: "default/tcp-services"
spec:
  rules:
  - host: bolt.yourdomain.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: ingress-cluster-client
            port:
              number: 7687

---
apiVersion: v1
kind: ConfigMap
metadata:
  name: tcp-services
data:
  7687: "default/ingress-cluster-client:7687"
```

### Security Considerations for External Access

#### 1. Enable TLS Encryption

Always enable TLS for external access:

```yaml
spec:
  security:
    tls:
      enabled: true
      # Use cert-manager for automatic certificate management
      issuer: "letsencrypt-prod"
      # Or provide your own certificate
      secretName: "neo4j-custom-tls"
```

#### 2. Network Policies

Restrict network access to authorized sources:

```yaml
# neo4j-network-policy.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: neo4j-external-access
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: neo4j
  policyTypes:
  - Ingress
  ingress:
  - from:
    # Allow from specific IP ranges
    - ipBlock:
        cidr: 10.0.0.0/8    # Internal networks
    - ipBlock:
        cidr: 192.168.0.0/16 # Private networks
    # Allow from specific namespaces
    - namespaceSelector:
        matchLabels:
          name: trusted-apps
    ports:
    - protocol: TCP
      port: 7687  # Bolt
    - protocol: TCP
      port: 7474  # HTTP
    - protocol: TCP
      port: 7473  # HTTPS
```

#### 3. Authentication and Authorization

Configure strong authentication:

```yaml
spec:
  auth:
    provider: native
    # Use strong passwords
    secretRef: neo4j-strong-auth

  # Enable additional security features
  security:
    authorization:
      enabled: true
    audit:
      enabled: true
```

### Cloud Provider Examples

#### AWS (EKS)

```yaml
services:
  client:
    type: LoadBalancer
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
      service.beta.kubernetes.io/aws-load-balancer-scheme: "internet-facing"
      service.beta.kubernetes.io/aws-load-balancer-ssl-cert: "arn:aws:acm:..."
      service.beta.kubernetes.io/aws-load-balancer-ssl-ports: "7473,7687"
```

#### Google Cloud (GKE)

```yaml
services:
  client:
    type: LoadBalancer
    annotations:
      cloud.google.com/load-balancer-type: "External"
      cloud.google.com/backend-config: '{"default": "neo4j-backendconfig"}'
```

#### Azure (AKS)

```yaml
services:
  client:
    type: LoadBalancer
    annotations:
      service.beta.kubernetes.io/azure-load-balancer-internal: "false"
      service.beta.kubernetes.io/azure-dns-label-name: "my-neo4j-cluster"
```

### Testing External Connectivity

Once configured, test your external access:

```bash
# Test Bolt connectivity
cypher-shell -a bolt://<EXTERNAL-ENDPOINT>:7687 -u neo4j -p <PASSWORD> "RETURN 'Connected successfully' as status"

# Test HTTP Browser access
curl -u neo4j:<PASSWORD> http://<EXTERNAL-ENDPOINT>:7474/browser/

# Test HTTPS access
curl -u neo4j:<PASSWORD> https://<EXTERNAL-ENDPOINT>:7473/browser/

# Test from external application
docker run --rm neo4j/neo4j-admin:5.26-enterprise \
  cypher-shell -a bolt://<EXTERNAL-ENDPOINT>:7687 -u neo4j -p <PASSWORD> \
  "CALL dbms.cluster.overview()"
```

### External Access Checklist

Before enabling external access, ensure:

- [ ] **TLS enabled** for encrypted connections
- [ ] **Strong authentication** configured
- [ ] **Network policies** restrict access appropriately
- [ ] **Firewall rules** allow only necessary ports
- [ ] **Monitoring** configured for external connections
- [ ] **Backup strategy** in place
- [ ] **DNS records** configured (for ingress)
- [ ] **Certificate management** automated (cert-manager)

## 📊 Understanding Your Deployment

### Cluster Components

Your Neo4j cluster consists of several Kubernetes resources:

```bash
# View all resources created for your cluster
kubectl get all -l app.kubernetes.io/instance=my-first-cluster

# Detailed breakdown:
kubectl get statefulsets  # The main Neo4j instances
kubectl get services      # Load balancers and discovery services
kubectl get configmaps    # Configuration data
kubectl get secrets       # Sensitive configuration (passwords, certificates)
```

### Key Services Created

1. **Client Service** (`my-first-cluster-client`): Main entry point for applications
2. **Headless Service** (`my-first-cluster-headless`): For cluster member discovery
3. **Metrics Service** (`my-first-cluster-metrics`): For monitoring integration

## 🔧 Configuration Examples

### Basic Single Instance (Development)

Perfect for development and testing:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: dev-instance
spec:
  image:
    repo: neo4j
    tag: "5.26"  # Community edition
  storage:
    size: "1Gi"
  auth:
    secretRef: neo4j-auth
```

### High Availability Cluster (Production)

Recommended for production workloads:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: production-cluster
spec:
  image:
    repo: neo4j
    tag: "5.26-enterprise"

  topology:
    primaries: 3
    secondaries: 2

  storage:
    className: "premium-ssd"
    size: "100Gi"

  # Backup configuration
  backup:
    enabled: true
    schedule: "0 2 * * *"  # Daily at 2 AM
    storage:
      type: "s3"
      bucket: "my-neo4j-backups"

  # Monitoring integration
  monitoring:
    enabled: true
    prometheus:
      enabled: true

  # Security configuration
  security:
    tls:
      enabled: true
    encryption:
      enabled: true

  # Auto-scaling (Enterprise feature)
  autoScaling:
    enabled: true
    primaries:
      minReplicas: 3
      maxReplicas: 7
    secondaries:
      minReplicas: 0
      maxReplicas: 10
```

### Multi-Region Deployment (Advanced)

For global applications requiring low latency:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: global-cluster
spec:
  multiCluster:
    enabled: true
    topology:
      strategy: "active-active"
      clusters:
        - name: "us-east"
          region: "us-east-1"
          nodeAllocation:
            primaries: 2
            secondaries: 1
        - name: "europe"
          region: "eu-west-1"
          nodeAllocation:
            primaries: 1
            secondaries: 1
```

## 🧪 Testing Your Deployment

### Health Checks

```bash
# Check if Neo4j is responding
kubectl exec -it my-first-cluster-primary-0 -- cypher-shell -u neo4j -p <PASSWORD> "CALL dbms.cluster.overview()"

# Check system metrics
kubectl exec -it my-first-cluster-primary-0 -- cypher-shell -u neo4j -p <PASSWORD> "CALL dbms.queryJmx('*:*')"
```

### Performance Testing

```bash
# Simple performance test
kubectl exec -it my-first-cluster-primary-0 -- cypher-shell -u neo4j -p <PASSWORD> "
CREATE INDEX FOR (n:Person) ON (n.id);
UNWIND range(1, 10000) AS i
CREATE (p:Person {id: i, name: 'Person ' + i});
"

# Query performance test
kubectl exec -it my-first-cluster-primary-0 -- cypher-shell -u neo4j -p <PASSWORD> "
MATCH (p:Person) WHERE p.id = 5000 RETURN p;
"
```

## 📊 Monitoring and Observability

### Built-in Monitoring

The operator provides comprehensive monitoring out of the box:

```bash
# Check operator metrics
kubectl port-forward service/neo4j-operator-metrics 8080:8080 &
curl http://localhost:8080/metrics

# Check Neo4j metrics
kubectl port-forward service/my-first-cluster-metrics 2004:2004 &
curl http://localhost:2004/metrics
```

### Prometheus Integration

If you have Prometheus installed:

```yaml
# monitoring.yaml
apiVersion: v1
kind: ServiceMonitor
metadata:
  name: neo4j-cluster-metrics
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: neo4j
  endpoints:
  - port: metrics
    interval: 30s
```

### Grafana Dashboards

Import the pre-built Neo4j Grafana dashboard:

```bash
# Dashboard ID: 13516 (Neo4j Kubernetes Operator)
# Or use the dashboard JSON from docs/monitoring/grafana-dashboard.json
```

## 📏 Scaling Your Cluster

### Manual Scaling

To scale your cluster, simply update the `topology` specification:

```bash
# Scale up primaries (must be odd numbers for quorum)
kubectl patch neo4jenterprisecluster my-first-cluster --type='merge' -p='
spec:
  topology:
    primaries: 5  # Scale from 3 to 5
'

# Scale up secondaries (for read workloads)
kubectl patch neo4jenterprisecluster my-first-cluster --type='merge' -p='
spec:
  topology:
    secondaries: 4  # Scale from 0 to 4
'

# Watch the scaling progress
kubectl get pods -l app.kubernetes.io/instance=my-first-cluster -w
```

### Auto-scaling (Advanced)

For dynamic workloads, enable auto-scaling:

```yaml
# auto-scaling-cluster.yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: auto-scaling-cluster
spec:
  topology:
    primaries: 3
    secondaries: 2
  autoScaling:
    enabled: true
    primaries:
      enabled: true
      minReplicas: 3
      maxReplicas: 7
      metrics:
      - type: "cpu"
        target: "70"
        weight: "1.0"
      - type: "memory"
        target: "80"
        weight: "0.8"
    secondaries:
      enabled: true
      minReplicas: 1
      maxReplicas: 10
      metrics:
      - type: "cpu"
        target: "60"
        weight: "1.0"
      - type: "query_latency"
        target: "100ms"
        weight: "1.2"
  # ... other configuration
```

### Scaling Best Practices

1. **Primary scaling**: Always use odd numbers (1, 3, 5, 7) for proper quorum
2. **Secondary scaling**: Scale based on read workload requirements
3. **Resource planning**: Ensure your cluster has sufficient CPU/memory/storage
4. **Monitoring**: Watch cluster health during scaling operations

```bash
# Monitor cluster health during scaling
kubectl describe neo4jenterprisecluster my-first-cluster
kubectl get events --sort-by=.metadata.creationTimestamp
```

## 🔍 Troubleshooting Guide

### Common Issues and Solutions

#### Issue: Pods Stuck in Pending State

```bash
# Check events for scheduling issues
kubectl describe pod my-first-cluster-primary-0

# Common causes and solutions:
# 1. Insufficient resources
kubectl top nodes

# 2. Storage issues
kubectl get storageclass
kubectl get pv

# 3. Node selector constraints
kubectl get nodes --show-labels
```

#### Issue: Cluster Not Forming

```bash
# Check cluster formation status
kubectl logs my-first-cluster-primary-0 | grep -i cluster

# Check network policies
kubectl get networkpolicy

# Check service discovery
kubectl get endpoints my-first-cluster-headless
```

#### Issue: Connection Refused

```bash
# Check if services are properly configured
kubectl get service my-first-cluster-client

# Test internal connectivity
kubectl run test-pod --rm -it --image=busybox -- /bin/sh
# Inside the pod:
# nc -zv my-first-cluster-client 7687
```

### Getting Help

1. **Check the logs:**

   ```bash
   kubectl logs deployment/neo4j-operator-controller-manager -n neo4j-operator-system
   ```

2. **Community support:**
   - GitHub Issues: <https://github.com/neo4j-labs/neo4j-kubernetes-operator/issues>
   - Neo4j Community Forum: <https://community.neo4j.com/>
   - Discord: <https://discord.gg/neo4j>

3. **Enterprise support:**
   - Neo4j Support Portal (for Enterprise customers)

## 🚀 Next Steps

### For Beginners

1. **Learn Neo4j Cypher**: Complete the [Neo4j GraphAcademy](https://graphacademy.neo4j.com/) courses
2. **Explore the samples**: Check out `config/samples/` for more examples
3. **Try the enterprise features**: Auto-scaling, backup/restore, monitoring
4. **Join the community**: Participate in forums and contribute to the project

### For Advanced Users

1. **Custom Resource Definitions**: Explore the full API reference in `docs/api-reference.md`
2. **Operator development**: Check out the [Developer Guide](development/developer-guide.md) for contributing
3. **Multi-cluster setups**: Implement cross-region deployments
4. **CI/CD integration**: Automate deployments with GitOps tools

### Production Checklist

Before going to production, ensure you have:

- [ ] **Security**: TLS enabled, strong passwords, network policies
- [ ] **Backup**: Automated backup strategy configured
- [ ] **Monitoring**: Metrics collection and alerting set up
- [ ] **Resource limits**: CPU and memory limits configured
- [ ] **High availability**: Multi-instance cluster configured
- [ ] **Disaster recovery**: Cross-region or cross-cluster strategy
- [ ] **Maintenance**: Upgrade and patching procedures documented

## 📁 Additional Resources

- **[API Reference](api-reference.md)**: Complete API documentation
- **[Architecture Guide](development/architecture.md)**: Understanding the operator design
- **[Performance Guide](performance-guide.md)**: Optimize your deployments
- **[Multi-Cluster Deployment Guide](multi-cluster-deployment-guide.md)**: Cross-region deployments and networking
- **[Disaster Recovery Guide](disaster-recovery-guide.md)**: Comprehensive DR strategies
- **[Auto-scaling Guide](auto-scaling-guide.md)**: Intelligent automatic scaling
- **[Plugin Management Guide](plugin-management-guide.md)**: Dynamic plugin installation
- **[Backup & Restore](backup-restore-guide.md)**: Data protection strategies
- **[Rolling Upgrades](rolling-upgrade-guide.md)**: Safe upgrade procedures

---

**Need help?** Don't hesitate to reach out to the community or check our troubleshooting guides. We're here to help you succeed with Neo4j on Kubernetes!
