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
- A cert-manager-compatible issuer configured (`ClusterIssuer`, `Issuer`, or a third-party external issuer)
- Neo4j Enterprise 5.26 LTS or any CalVer release (2025.x, 2026.x, and onward)

## Basic TLS Configuration

### Enable TLS with cert-manager

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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

The operator emits Neo4j's canonical production cluster SSL policy by default:

```properties
# Automatically set by the operator when spec.tls.mode=cert-manager
# and spec.tls.strictPeerValidation (default true) is enabled:
dbms.ssl.policy.cluster.enabled=true
dbms.ssl.policy.cluster.base_directory=/ssl
dbms.ssl.policy.cluster.private_key=tls.key
dbms.ssl.policy.cluster.public_certificate=tls.crt
dbms.ssl.policy.cluster.trust_all=false           # validate peers against ca.crt
dbms.ssl.policy.cluster.client_auth=REQUIRE       # mutual TLS
dbms.ssl.policy.cluster.verify_hostname=true      # peer's cert must match the FQDN
dbms.ssl.policy.cluster.tls_versions=TLSv1.3,TLSv1.2
```

The trust anchor is the cert-manager-issued Secret's `ca.crt`, projected to `/ssl/trusted/ca.crt` (Neo4j's expected `trusted_dir` location). All cluster servers present a cert signed by the same CA, so peer validation just works.

#### Opting out: `strictPeerValidation: false`

The opt-out exists for installations whose external issuer (e.g. some custom `AWSPCAClusterIssuer` setups) does not populate `ca.crt` in the Secret it issues. Without `ca.crt` the trust anchor is missing and strict validation rejects every peer. The operator detects this at reconcile time and refuses to apply the strict config — `status.phase` flips to `Failed` with a message naming the issuer. Two paths forward:

- **Recommended**: fix the issuer to include the CA in its Secret output.
- **Escape hatch**: set `spec.tls.strictPeerValidation: false`. The operator reverts to `trust_all=true` + `client_auth=NONE` — the legacy posture, which Neo4j's own docs flag as *"debugging only, since it does not offer security."*

### Parallel Pod Startup

The operator uses parallel pod management for faster cluster formation:
- All pods start simultaneously
- First pod forms the initial cluster
- Other pods discover and join
- Works reliably with TLS when properly configured

### Recommended Configuration for TLS Clusters

For optimal TLS cluster formation, consider these settings:

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
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
    # dbms.ssl.policy.cluster.trust_all: "false"     (managed by strictPeerValidation)
    # dbms.ssl.policy.cluster.client_auth: "REQUIRE" (managed by strictPeerValidation)
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

When TLS is enabled, the operator sets `server.bolt.tls_level=REQUIRED` on both cluster and standalone deployments. This means **plain `bolt://` connections are rejected** — clients must use `bolt+s://` (with CA verification) or `bolt+ssc://` (self-signed cert, skips hostname verification).

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
# From within the cluster (via kubectl exec)
kubectl exec -it <pod-name> -c neo4j -- cypher-shell \
  -a bolt+ssc://localhost:7687 -u neo4j -p <password>

# From outside (after port-forward)
cypher-shell -a bolt+ssc://localhost:7687 -u neo4j -p <password>
```

## Troubleshooting TLS Issues

### Split-Brain in TLS Clusters

TLS clusters may experience split-brain during initial formation if not properly configured. The operator includes fixes to minimize this:

1. **Strict peer validation** (default): the operator validates peers against the issuer's CA at `/ssl/trusted/ca.crt` with mutual TLS. Mismatched certs fail closed instead of silently joining the wrong cluster.
2. **Parallel startup**: All pods start together for faster formation.
3. **Proper RBAC**: Endpoints permission for discovery.

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

### Using an External CA or Third-Party Issuer

The operator supports any cert-manager-compatible issuer, including third-party external issuers.
Set `kind` to the issuer's resource kind and `group` to its API group.

**Standard cert-manager CA** (most common):
```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: my-internal-ca
      kind: ClusterIssuer
      # group defaults to cert-manager.io — can be omitted
```

**AWS Private Certificate Authority**:
```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: aws-pca-issuer
      kind: AWSPCAClusterIssuer
      group: awspca.cert-manager.io
```

**HashiCorp Vault**:
```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: vault-issuer
      kind: VaultIssuer
      group: cert.cert-manager.io
```

**Google Certificate Authority Service**:
```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: google-cas-issuer
      kind: GoogleCASClusterIssuer
      group: cas-issuer.jetstack.io
```

The `kind` field is intentionally unrestricted — the operator passes it through directly to cert-manager's `Certificate` resource, which supports any registered external issuer CRD.

### CA Certificate Verification

By default, the operator automatically loads the CA certificate from the cert-manager-generated TLS Secret (`{cluster-name}-tls-secret`) to verify Neo4j connections. This means TLS verification works out of the box when cert-manager includes `ca.crt` in the Secret.

If the CA certificate cannot be loaded (e.g., during initial startup before cert-manager issues the cert, or when using an issuer that doesn't provide `ca.crt`), the operator falls back to skipping TLS verification with a warning.

To use a custom CA certificate (e.g., from an external PKI), specify `trustedCASecret`:

```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: my-issuer
      kind: ClusterIssuer
    trustedCASecret: my-ca-bundle  # Secret must contain key "ca.crt"
```

Create the CA Secret:
```bash
kubectl create secret generic my-ca-bundle --from-file=ca.crt=/path/to/ca-certificate.pem
```

When `trustedCASecret` is set, it takes priority over the auto-discovered cert-manager CA.

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
2. **Don't override cluster trust settings** in `spec.config` — let the operator manage `trust_all`, `client_auth`, and `verify_hostname` via the `spec.tls.strictPeerValidation` toggle
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
