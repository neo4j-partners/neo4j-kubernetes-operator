# TLS/SSL Configuration

This guide covers how to configure TLS/SSL encryption for Neo4j deployments managed by the Neo4j Kubernetes Operator.

## Overview

The operator supports automatic TLS certificate management through cert-manager, providing:
- Encrypted client connections (HTTPS and Bolt)
- Encrypted intra-cluster communication
- Automatic certificate renewal
- Support for both cluster and standalone deployments

## Prerequisites

- cert-manager installed in your cluster (v1.0+)
- A ClusterIssuer or Issuer configured
- Neo4j Enterprise 5.26+ or 2025.x+

## Basic TLS Configuration

### Enable TLS with cert-manager

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: secure-cluster
spec:
  topology:
    primaries: 3
    secondaries: 2

  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer
    # Optional: customize certificate settings
    duration: 8760h    # 1 year
    renewBefore: 720h  # 30 days
```

### What Gets Configured

When TLS is enabled, the operator automatically:

1. **Creates a Certificate resource** via cert-manager
2. **Configures Neo4j SSL policies**:
   - `bolt`: For encrypted database connections
   - `https`: For encrypted web interface
   - `cluster`: For encrypted intra-cluster communication
3. **Mounts certificates** at `/ssl/` in Neo4j containers
4. **Sets appropriate Neo4j configuration**

## Important: TLS and Cluster Formation

TLS-enabled clusters require special consideration for reliable cluster formation:

### Key Configuration

The operator automatically configures cluster SSL policy with `trust_all=true`:

```properties
# Automatically set by the operator
dbms.ssl.policy.cluster.enabled=true
dbms.ssl.policy.cluster.trust_all=true
```

This is **critical** for cluster formation as it allows nodes to trust each other's certificates during the initial handshake.

### Parallel Pod Startup

The operator uses parallel pod management for faster cluster formation:
- All pods start simultaneously
- First pod forms the initial cluster
- Other pods discover and join
- Works reliably with TLS when properly configured

### Recommended Configuration for TLS Clusters

For optimal TLS cluster formation, consider these settings:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: tls-optimized-cluster
spec:
  topology:
    primaries: 3
    secondaries: 2

  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer

  # Optional: Increase discovery timeouts for TLS handshake delays
  config:
    dbms.cluster.discovery.v2.initial_timeout: "10s"
    dbms.cluster.discovery.v2.retry_timeout: "20s"

    # DO NOT override these - operator sets optimal values:
    # dbms.cluster.raft.membership.join_timeout: "10m"
    # dbms.ssl.policy.cluster.trust_all: "true"
```

## Certificate Details

### DNS Names

The operator automatically includes all necessary DNS names in the certificate:

- Service names (client, internals, headless)
- Pod FQDNs for all primaries and secondaries
- Short names and fully qualified names

### Certificate Files

Certificates are mounted as:
- `/ssl/tls.crt` - The certificate
- `/ssl/tls.key` - The private key
- `/ssl/ca.crt` - The CA certificate

## Connecting to TLS-Enabled Neo4j

### Using Neo4j Browser

```bash
# Port-forward the HTTPS port
kubectl port-forward svc/<cluster-name>-client 7473:7473

# Access via browser
https://localhost:7473
```

### Using Bolt Drivers

```bash
# Port-forward the Bolt port
kubectl port-forward svc/<cluster-name>-client 7687:7687

# Connect with self-signed certificate acceptance
bolt+ssc://localhost:7687
```

### Using cypher-shell

```bash
# From within the cluster
kubectl exec -it <pod-name> -- cypher-shell -u neo4j -p <password>

# From outside (after port-forward)
cypher-shell -a bolt+ssc://localhost:7687 -u neo4j -p <password>
```

## Troubleshooting TLS Issues

### Split-Brain in TLS Clusters

TLS clusters may experience split-brain during initial formation if not properly configured. The operator includes fixes to minimize this:

1. **Automatic trust_all**: Cluster SSL policy includes `trust_all=true`
2. **Parallel startup**: All pods start together for faster formation
3. **Proper RBAC**: Endpoints permission for discovery

If split-brain occurs, see the [Split-Brain Recovery Guide](../troubleshooting/split-brain-recovery.md).

### Certificate Issues

Check certificate status:
```bash
kubectl get certificate <cluster-name>-tls
kubectl describe certificate <cluster-name>-tls
```

Check cert-manager logs:
```bash
kubectl logs -n cert-manager deployment/cert-manager
```

### Connection Refused

If TLS connections fail:
1. Verify certificate is ready
2. Check Neo4j logs for SSL errors
3. Ensure ports 7473 (HTTPS) and 7687 (Bolt) are accessible

## Advanced Configuration

### Using External CA

```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: external-ca-issuer
      kind: ClusterIssuer
      group: cert-manager.io  # Optional, defaults to cert-manager.io
```

### Custom Certificate Duration

```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer
    duration: 17520h    # 2 years
    renewBefore: 1440h  # 60 days
```

### Mutual TLS (mTLS)

For client certificate authentication, configure after deployment:

```yaml
spec:
  config:
    # Enable client auth for Bolt
    dbms.ssl.policy.bolt.client_auth: "REQUIRE"
    # Keep cluster communication simple
    dbms.ssl.policy.cluster.client_auth: "NONE"
```

## Best Practices

1. **Always use cert-manager** for automatic certificate lifecycle management
2. **Don't override cluster trust settings** - Let the operator manage `trust_all`
3. **Monitor certificate expiry** - Set up alerts for certificate renewal
4. **Test in staging** - Always test TLS configuration in non-production first
5. **Use parallel pod management** - Already configured by the operator
6. **Keep join timeout at 10m** - Don't reduce `dbms.cluster.raft.membership.join_timeout`

## Summary

TLS configuration with the Neo4j Kubernetes Operator is straightforward:
1. Install cert-manager and create an issuer
2. Enable TLS in your Neo4j resource
3. The operator handles all certificate and Neo4j configuration
4. Connect using TLS-enabled endpoints

The operator includes specific optimizations for TLS cluster formation, making it reliable even with encryption enabled.
