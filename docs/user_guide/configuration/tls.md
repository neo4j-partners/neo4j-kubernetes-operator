# TLS/SSL Configuration

This guide covers how to configure TLS/SSL encryption for Neo4j deployments managed by the Neo4j Kubernetes Operator.

## Overview

The operator supports automatic TLS certificate management through cert-manager, providing:
- Encrypted client connections (HTTPS and Bolt)
- Encrypted intra-cluster communication
- Automatic certificate renewal
- Support for both cluster and standalone deployments

## Prerequisites

- cert-manager installed in your cluster (v1.18+ recommended; the project's dev/test clusters install v1.20.0)
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

## Troubleshooting

See [tls_certificates.md § Troubleshooting](../tls_certificates.md#troubleshooting) for cert-not-found, connection-refused, and validation errors. For split-brain in TLS clusters (typically a peer-trust mismatch on first formation), see [Split-Brain Recovery](../troubleshooting/split-brain-recovery.md).

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

**HashiCorp Vault** (native cert-manager — no separate CRD):

Vault is supported by cert-manager itself, not via a third-party external-issuer CRD. Configure it on a standard `Issuer`/`ClusterIssuer` via `spec.vault` (see [cert-manager Vault docs](https://cert-manager.io/docs/configuration/vault/)) and reference it the same way as any other built-in issuer:

```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: vault-issuer          # name of your Issuer/ClusterIssuer with spec.vault
      kind: ClusterIssuer
      # group defaults to cert-manager.io — Vault is built-in, no external group
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

**Intra-cluster mTLS is enabled by default.** When `spec.tls.mode: cert-manager` and `spec.tls.strictPeerValidation: true` (the default), the operator emits:

```properties
dbms.ssl.policy.cluster.trust_all=false
dbms.ssl.policy.cluster.client_auth=REQUIRE       # mutual TLS between server pods
dbms.ssl.policy.cluster.verify_hostname=true
```

No user configuration required. See the *Important: TLS and Cluster Formation* section above for the full rationale and the `strictPeerValidation: false` opt-out.

> **Do not set `dbms.ssl.policy.*` keys in `spec.config`.** The operator owns the SSL policy surface end-to-end. The cluster validator rejects any `dbms.ssl.policy.*` / `server.bolt.tls_level` / `server.directories.certificates` key in `spec.config` with a `Forbidden` error at apply time, because user values would silently override the operator-managed configuration (`server.config.strict_validation.enabled=false` makes Neo4j accept duplicates without a warning).

**Bolt mTLS (client-certificate authentication for external drivers) is not configurable via the operator today.** Bolt and HTTPS SSL policies are managed by the operator with `client_auth=NONE` so standard Neo4j drivers — none of which ship with operator-issued client certs — can connect. A future enhancement could expose a typed field for this; track via [issue #128](https://github.com/neo4j-partners/neo4j-kubernetes-operator/issues/128) (security checklist gaps).
