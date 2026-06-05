# TLS Certificate Management Guide

This guide explains how to work with TLS certificates when deploying Neo4j clusters with SSL/TLS enabled.

## Overview

When you enable TLS using cert-manager, the Neo4j operator automatically:
- Creates certificate requests
- Stores certificates in Kubernetes secrets
- Mounts certificates in Neo4j pods
- Configures Neo4j SSL policies

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

### Quick Export Commands

Create a helper script `neo4j-cert-export.sh`:

```bash
#!/bin/bash
# neo4j-cert-export.sh - Export Neo4j TLS certificates for client use

DEPLOYMENT_NAME=$1
NAMESPACE=${2:-default}
OUTPUT_DIR=${3:-.}

if [ -z "$DEPLOYMENT_NAME" ]; then
    echo "Usage: $0 <deployment-name> [namespace] [output-dir]"
    echo "Example: $0 my-cluster default ./certs"
    exit 1
fi

SECRET_NAME="${DEPLOYMENT_NAME}-tls-secret"
mkdir -p "$OUTPUT_DIR"

echo "Exporting certificates from $SECRET_NAME in namespace $NAMESPACE..."

# Export CA certificate (for client trust stores)
kubectl get secret "$SECRET_NAME" -n "$NAMESPACE" -o jsonpath='{.data.ca\.crt}' | base64 -d > "$OUTPUT_DIR/neo4j-ca.crt"

# Export server certificate (optional, for verification)
kubectl get secret "$SECRET_NAME" -n "$NAMESPACE" -o jsonpath='{.data.tls\.crt}' | base64 -d > "$OUTPUT_DIR/neo4j-server.crt"

# Display certificate information
echo ""
echo "Certificate Subject:"
openssl x509 -in "$OUTPUT_DIR/neo4j-server.crt" -noout -subject

echo ""
echo "Certificate Validity:"
openssl x509 -in "$OUTPUT_DIR/neo4j-server.crt" -noout -dates

echo ""
echo "Certificate DNS Names:"
openssl x509 -in "$OUTPUT_DIR/neo4j-server.crt" -noout -text | grep -A1 "Subject Alternative Name"

echo ""
echo "Certificates exported to $OUTPUT_DIR/"
echo "  - neo4j-ca.crt: Use this for client trust stores"
echo "  - neo4j-server.crt: Server certificate (for reference)"
```

Make it executable:
```bash
chmod +x neo4j-cert-export.sh
./neo4j-cert-export.sh my-cluster default ./certs
```

### Manual Commands

```bash
# View certificate details
kubectl get secret <deployment-name>-tls-secret -o yaml

# Extract CA certificate
kubectl get secret <deployment-name>-tls-secret -o jsonpath='{.data.ca\.crt}' | base64 -d > ca.crt

# Extract server certificate
kubectl get secret <deployment-name>-tls-secret -o jsonpath='{.data.tls\.crt}' | base64 -d > server.crt

# View certificate details
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

### 4. Monitor Certificate Expiry

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
