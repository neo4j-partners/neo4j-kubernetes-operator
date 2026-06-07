# TLS Configuration

This guide explains how to configure TLS for Neo4j deployments and work with the certificates the operator manages — both as a *cluster operator* (configuring the operator's TLS surface) and as a *client* (connecting to a TLS-enabled deployment).

## Overview

When you enable TLS using cert-manager, the Neo4j operator automatically:
- Creates certificate requests
- Stores certificates in Kubernetes secrets
- Mounts certificates in Neo4j pods
- Configures Neo4j SSL policies (Bolt, HTTPS, and intra-cluster)
- Renews certificates via cert-manager

## Prerequisites

- cert-manager installed in your cluster (v1.20+ recommended)
- A cert-manager-compatible issuer (`ClusterIssuer`, `Issuer`, or a third-party external issuer)
- Neo4j Enterprise 5.26 LTS or any CalVer release (2025.x, 2026.x, ...)

## Enabling TLS

Set `spec.tls.mode: cert-manager` and point at an issuer. That single block is enough for the operator to wire up the rest:

```yaml
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseCluster
metadata:
  name: secure-cluster
spec:
  topology:
    servers: 3
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer
    # Optional: customize cert lifetime
    duration: 8760h     # 1 year
    renewBefore: 720h   # 30 days
```

### What the operator configures

When TLS is enabled, the operator automatically:

1. Creates a `Certificate` resource via cert-manager.
2. Configures three Neo4j SSL policies:
   - `bolt` — encrypted database connections
   - `https` — encrypted web interface
   - `cluster` — encrypted intra-cluster communication
3. Mounts certificates at `/ssl/` in Neo4j containers.
4. Sets `server.bolt.tls_level=REQUIRED` (plain `bolt://` connections are rejected).

### DNS names baked into the cert

The operator includes every name the cluster needs in the certificate SANs:

- Service names (`{cluster}-client`, `{cluster}-internals`, `{cluster}-headless`)
- Pod FQDNs for every server in the cluster
- Short names and fully qualified names

You generally don't need to add anything yourself.

## TLS and Cluster Formation (Strict Peer Validation)

TLS-enabled clusters use mutual TLS (mTLS) on intra-cluster links **by default**. When `spec.tls.mode: cert-manager` and `spec.tls.strictPeerValidation: true` (the default), the operator emits Neo4j's canonical production cluster SSL policy:

```properties
# Automatically set by the operator
dbms.ssl.policy.cluster.enabled=true
dbms.ssl.policy.cluster.base_directory=/ssl
dbms.ssl.policy.cluster.private_key=tls.key
dbms.ssl.policy.cluster.public_certificate=tls.crt
dbms.ssl.policy.cluster.trust_all=false           # validate peers against ca.crt
dbms.ssl.policy.cluster.client_auth=REQUIRE       # mutual TLS
dbms.ssl.policy.cluster.verify_hostname=true      # peer cert must match FQDN
dbms.ssl.policy.cluster.tls_versions=TLSv1.3,TLSv1.2
```

The trust anchor is the cert-manager-issued Secret's `ca.crt`, projected to `/ssl/trusted/ca.crt` (Neo4j's expected `trusted_dir`). All cluster servers present a cert signed by the same CA, so peer validation works without any extra configuration.

### Opting out: `strictPeerValidation: false`

The opt-out exists for installations whose external issuer (e.g. some custom `AWSPCAClusterIssuer` setups) does not populate `ca.crt` in the Secret it issues. Without `ca.crt` the trust anchor is missing and strict validation rejects every peer. The operator detects this at reconcile time and refuses to apply the strict config — `status.phase` flips to `Failed` with a message naming the issuer. Two paths forward:

- **Recommended**: fix the issuer to include the CA in its Secret output.
- **Escape hatch**: set `spec.tls.strictPeerValidation: false`. The operator reverts to `trust_all=true` + `client_auth=NONE` — the legacy posture, which Neo4j's own docs flag as *"debugging only, since it does not offer security."*

### Hands off the SSL policy keys

> **Do not set `dbms.ssl.policy.*` keys in `spec.config`.** The operator owns the SSL policy surface end-to-end. The cluster validator rejects any `dbms.ssl.policy.*` / `server.bolt.tls_level` / `server.directories.certificates` key in `spec.config` with a `Forbidden` error at apply time, because user values would silently override operator-managed configuration (`server.config.strict_validation.enabled=false` makes Neo4j accept duplicates without warning).

### Bolt client-certificate auth is not exposed today

Bolt and HTTPS SSL policies are managed with `client_auth=NONE` so standard Neo4j drivers — none of which ship with operator-issued client certs — can connect. There is no typed field for Bolt mTLS today, and no design committed for one. The blocker isn't the operator (which can emit any `dbms.ssl.policy.bolt.client_auth` value) — it's that none of the Neo4j Bolt drivers expose a clean way to load a client cert from a K8s Secret in the application namespace, so the app-side story is the harder half. If you need this for a specific compliance regime, [open an issue](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues) describing the driver/language combination.

## Certificate Storage

### Secret Names
- **Cluster**: `<cluster-name>-tls-secret`
- **Standalone**: `<standalone-name>-tls-secret`

### Certificate Files
Inside the secret, you'll find:
- `tls.crt` - The server certificate
- `tls.key` - The private key (DO NOT SHARE)
- `ca.crt` - The Certificate Authority certificate

## Retrieving Certificates for Clients

```bash
# Extract the CA certificate for client trust stores
kubectl get secret <deployment-name>-tls-secret \
    -o jsonpath='{.data.ca\.crt}' | base64 -d > ca.crt

# Extract the server certificate
kubectl get secret <deployment-name>-tls-secret \
    -o jsonpath='{.data.tls\.crt}' | base64 -d > server.crt

# Inspect (SANs, validity)
openssl x509 -in server.crt -text -noout
```

## Client Configuration Examples

### Neo4j Browser

1. **With Self-Signed Certificates**:
   ```bash
   # Port forward to HTTPS port
   kubectl port-forward svc/<deployment-name>-client 7473:7473

   # Access: https://localhost:7473
   # Accept the certificate warning in your browser
   ```

2. **With Trusted Certificates**:
   Configure cert-manager with a proper issuer (Let's Encrypt, internal CA)

### Neo4j Drivers

#### Python Driver

```python
from neo4j import GraphDatabase
import ssl

# For self-signed certificates (development)
driver = GraphDatabase.driver(
    "bolt+ssc://localhost:7687",
    auth=("neo4j", "password")
)

# For trusted certificates with CA verification
ssl_context = ssl.create_default_context()
ssl_context.load_verify_locations("neo4j-ca.crt")

driver = GraphDatabase.driver(
    "bolt+s://localhost:7687",
    auth=("neo4j", "password"),
    ssl_context=ssl_context
)
```

#### Java Driver

```java
import org.neo4j.driver.*;
import java.io.File;

// For self-signed certificates (development)
Driver driver = GraphDatabase.driver(
    "bolt+ssc://localhost:7687",
    AuthTokens.basic("neo4j", "password")
);

// For trusted certificates with custom trust store
Config config = Config.builder()
    .withTrustStrategy(Config.TrustStrategy.trustCustomCertificateSignedBy(
        new File("neo4j-ca.crt")
    ))
    .build();

Driver driver = GraphDatabase.driver(
    "bolt+s://localhost:7687",
    AuthTokens.basic("neo4j", "password"),
    config
);
```

#### JavaScript Driver

```javascript
const neo4j = require('neo4j-driver');
const fs = require('fs');

// For self-signed certificates (development)
const driver = neo4j.driver(
    'bolt+ssc://localhost:7687',
    neo4j.auth.basic('neo4j', 'password')
);

// For trusted certificates with CA
const driver = neo4j.driver(
    'bolt+s://localhost:7687',
    neo4j.auth.basic('neo4j', 'password'),
    {
        encrypted: 'ENCRYPTION_ON',
        trust: 'TRUST_CUSTOM_CA_SIGNED_CERTIFICATES',
        trustedCertificates: [fs.readFileSync('neo4j-ca.crt', 'utf8')]
    }
);
```

#### .NET Driver

```csharp
using Neo4j.Driver;
using System.Security.Cryptography.X509Certificates;

// For self-signed certificates (development)
var driver = GraphDatabase.Driver(
    "bolt+ssc://localhost:7687",
    AuthTokens.Basic("neo4j", "password")
);

// For trusted certificates with CA
var certificate = new X509Certificate2("neo4j-ca.crt");
var driver = GraphDatabase.Driver(
    "bolt+s://localhost:7687",
    AuthTokens.Basic("neo4j", "password"),
    o => o.WithTrustManager(TrustManager.CreateCustomTrustManager(certificate))
);
```

### Creating Java Keystore

For Java applications, you may need to import the CA certificate into a keystore:

```bash
# Create a keystore with the CA certificate
keytool -import -file neo4j-ca.crt \
        -alias neo4j-ca \
        -keystore neo4j-truststore.jks \
        -storepass changeit \
        -noprompt

# Use in Java application
System.setProperty("javax.net.ssl.trustStore", "neo4j-truststore.jks");
System.setProperty("javax.net.ssl.trustStorePassword", "changeit");
```

### Cypher Shell

```bash
# With self-signed certificates
cypher-shell -a bolt+ssc://localhost:7687 -u neo4j -p password

# With CA certificate
cypher-shell -a bolt+s://localhost:7687 -u neo4j -p password \
    --encryption=true \
    --trusted-ca=neo4j-ca.crt
```

## Production Best Practices

### 1. Use Proper Certificate Issuers

Instead of self-signed certificates, configure cert-manager with:

```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: letsencrypt-prod  # or your internal CA
      kind: ClusterIssuer
```

### 2. Certificate Rotation

The operator handles certificate renewal automatically through cert-manager. To manually trigger renewal:

```bash
# Delete the certificate to force regeneration
kubectl delete certificate <deployment-name>-tls

# The operator will recreate it automatically
```

### 3. External Access with Valid Certificates

For external access with valid certificates:

```yaml
spec:
  service:
    ingress:
      enabled: true
      className: nginx
      host: neo4j.example.com
      tlsSecretName: neo4j-tls  # Managed by cert-manager
      annotations:
        cert-manager.io/cluster-issuer: letsencrypt-prod
```

### 4. Use a Third-Party / External Issuer

The operator supports any cert-manager-compatible issuer. Set `kind` to the issuer's resource kind and `group` to its API group:

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

**HashiCorp Vault** (cert-manager built-in — no separate CRD; configure via `spec.vault` on a standard `Issuer`/`ClusterIssuer`):
```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: vault-issuer
      kind: ClusterIssuer
      # group defaults to cert-manager.io
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

The `kind` field is intentionally unrestricted — the operator passes it straight through to cert-manager's `Certificate` resource, which supports any registered external issuer CRD.

### 5. Override the CA Bundle (`trustedCASecret`)

By default, the operator automatically loads `ca.crt` from the cert-manager-generated TLS Secret (`{cluster-name}-tls-secret`) to verify Neo4j connections from inside the operator (admin Cypher calls, diagnostics, etc.). This works out of the box for issuers that populate `ca.crt`.

If your issuer doesn't include `ca.crt` (or you need to trust a different CA bundle), point at a separate Secret:

```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: my-issuer
      kind: ClusterIssuer
    trustedCASecret: my-ca-bundle   # Secret must contain key "ca.crt"
```

```bash
kubectl create secret generic my-ca-bundle --from-file=ca.crt=/path/to/ca-certificate.pem
```

When `trustedCASecret` is set it takes priority over the auto-discovered cert-manager CA.

### 6. Customize Certificate Duration

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

### 7. Monitor Certificate Expiry

```bash
# Check certificate expiration
kubectl get certificate <deployment-name>-tls -o jsonpath='{.status.notAfter}'

# Set up alerts for certificates expiring within 30 days
```

## Troubleshooting

### Certificate Not Found

```bash
# Check if certificate was created
kubectl get certificate -n <namespace>

# Check cert-manager logs
kubectl logs -n cert-manager deployment/cert-manager
```

### Connection Refused with TLS

1. Verify certificate is mounted:
   ```bash
   kubectl exec <pod-name> -- ls -la /ssl/
   ```

2. Check Neo4j SSL configuration:
   ```bash
   kubectl exec <pod-name> -- cat /conf/neo4j.conf | grep ssl
   ```

3. Verify ports are exposed:
   ```bash
   kubectl get svc <deployment-name>-client -o yaml | grep -A5 ports
   ```

### Certificate Validation Errors

1. **Hostname Mismatch**: Ensure you're connecting to a hostname that matches the certificate
2. **Expired Certificate**: Check certificate validity dates
3. **Unknown CA**: Import the CA certificate into your client's trust store

## Quick Reference

### Connection Schemes

| Scheme | Description | Use Case |
|--------|-------------|----------|
| `bolt://` | Unencrypted, direct | Dev only, TLS-disabled deployments. **Rejected by the server when TLS is enabled** (`server.bolt.tls_level=REQUIRED`). |
| `bolt+s://` | Encrypted with CA validation | Production with trusted certs |
| `bolt+ssc://` | Encrypted, self-signed cert | Development with TLS |
| `neo4j://` | Auto-negotiation, unencrypted | Cluster routing, TLS-disabled deployments only |
| `neo4j+s://` | Auto-negotiation with TLS | Production cluster routing |
| `neo4j+ssc://` | Auto-negotiation, self-signed | Development cluster routing with TLS |

### Common Commands

```bash
# Export certificates
kubectl get secret <name>-tls-secret -o jsonpath='{.data.ca\.crt}' | base64 -d > ca.crt

# Test connection
openssl s_client -connect localhost:7687 -CAfile ca.crt -servername <cluster-name>-client

# View certificate chain
openssl s_client -showcerts -connect localhost:7687 < /dev/null

# Verify certificate
openssl verify -CAfile ca.crt server.crt
```

## Outbound TLS — trusting external CAs from inside Neo4j

The sections above cover **inbound** TLS — clients connecting *to* Neo4j over
Bolt/HTTPS. Neo4j also makes **outbound** TLS calls (OIDC providers, LDAPS,
plugin downloads, cross-cluster replication peers, Aura Fleet Management).
For those, Neo4j needs to *trust* the remote endpoint's CA.

The operator exposes two fields for this:

| Field | When to use |
|---|---|
| `spec.trustedCASecrets` (list of `{name, key}`) | Most cases. Adds each Secret's CA to Neo4j's JVM-default truststore. Works for OIDC, LDAPS, generic outbound HTTPS. Cert-manager-issued Secrets reference directly — default key `ca.crt` matches. |
| `spec.extraVolumes` + `spec.extraVolumeMounts` | When a Neo4j SSL policy (e.g. cross-cluster replication) needs a CA at a specific filesystem path via `dbms.ssl.policy.<name>.truststore_path`. |

The legacy singular `spec.auth.trustStore` continues to work and is folded
into the same JKS at reconcile time.

```yaml
# cert-manager Certificate produces a Secret with ca.crt; reference it directly
trustedCASecrets:
  - name: corp-oidc-ca
```

Full prose in
[Security Best Practices Guide § JVM TrustStore for Internal CAs](security.md#jvm-truststore-for-internal-cas).

## Related Documentation

- [External Access Guide](./external_access.md)
- [Security Guide](./guides/security.md)
- [cert-manager Documentation](https://cert-manager.io/docs/)
