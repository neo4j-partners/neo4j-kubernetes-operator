# Security

This guide explains how to secure your Neo4j Enterprise clusters using the features provided by the operator.

## Authentication

The operator provides first-class, typed configuration for external identity providers. You define providers via `spec.auth` and the operator generates the correct `neo4j.conf` entries automatically — no manual `dbms.security.*` keys required.

**Supported providers (first-class typed configuration):**

*   **Native Neo4j authentication**: The default, managed via Kubernetes secrets.
*   **LDAP / Active Directory**: Full typed support — host, DN templates, group-to-role mapping, system account credentials (injected securely via env vars, never in ConfigMap).
*   **OIDC / SSO**: Multiple named providers (e.g., Okta + Azure AD simultaneously) with discovery URI, claims mapping, and group-to-role mapping.

**JWT** is not exposed as a typed field. Configure it by adding `jwt` to `spec.auth.authenticationProviders` and setting `dbms.security.jwt.{secret,public_key}` (plus optional `dbms.security.jwt.audience`) in `spec.config`.

**Kerberos** is not supported through operator-typed configuration today. Neo4j Kerberos uses the separate [Kerberos Add-On](https://neo4j.com/docs/kerberos-add-on/current/) — a plugin JAR plus its own `kerberos.conf` and `krb5.conf` files — and the operator does not assemble that bundle for you. See the [Security Guide § Kerberos Authentication](../security.md#kerberos-authentication) for current state.

If you'd like typed support for either, [open an issue](https://github.com/priyolahiri/neo4j-kubernetes-operator/issues) describing the use case.

**Multi-provider support**: Neo4j evaluates providers in order, so you can configure `authenticationProviders: [ldap, native]` to try LDAP first with native as fallback.

**JVM TrustStore**: For LDAPS or OIDC with internal CAs, use the top-level `spec.trustedCASecrets` field to add extra trust anchors to the operator-managed JKS truststore (seeded from the JDK `cacerts`). The legacy `spec.auth.trustStore` field still works for back-compat but is folded into the same path — prefer `spec.trustedCASecrets` for new deployments. See the [Security Guide § JVM TrustStore](../security.md#jvm-truststore-for-internal-cas) for the full configuration.

For full configuration details and examples, see the [Security Best Practices](../security.md#authentication-configuration) guide and the [auth examples](https://github.com/priyolahiri/neo4j-kubernetes-operator/blob/main/examples/clusters/auth-example.yaml).

## TLS

The operator makes it easy to enable TLS encryption for all communication to and from your Neo4j cluster. Enable TLS by setting `spec.tls.mode: cert-manager` and pointing `spec.tls.issuerRef` at a cert-manager `Issuer` or `ClusterIssuer`:

```yaml
spec:
  tls:
    mode: cert-manager
    issuerRef:
      name: ca-cluster-issuer
      kind: ClusterIssuer
    # strictPeerValidation defaults to true (mutual TLS on cluster links).
```

The operator integrates with `cert-manager` to automatically provision and manage TLS certificates. This is the recommended approach for production environments. Intra-cluster mutual TLS is enabled by default via `spec.tls.strictPeerValidation: true`. See the dedicated [TLS Configuration](../tls_configuration.md) guide for the full configuration surface, including third-party issuers (AWS PCA, Vault) and the `strictPeerValidation: false` opt-out.

## Network Policies

The operator can automatically create Kubernetes `NetworkPolicy` resources to restrict traffic to your Neo4j cluster. This helps to enforce a zero-trust security model by ensuring that only authorized applications can connect to the database.

## RBAC

The operator itself runs with a specific `ServiceAccount` that is bound to a `ClusterRole` with the minimum necessary permissions to do its job. For end-users, the operator also provides a set of `ClusterRoles` (`neo4j-viewer`, `neo4j-editor`) that administrators can bind to users or groups to grant them appropriate levels of access to the Neo4j custom resources.

## Pod Security Defaults

The operator now hardens all Neo4j data-plane pods (servers, standalone, backup jobs, restore jobs, and hook jobs) with strict security contexts by default:

- `runAsNonRoot: true` with UID/GID `7474` (Neo4j user) and matching `fsGroup`.
- `allowPrivilegeEscalation: false`, `capabilities.drop: ["ALL"]`, `seccompProfile: RuntimeDefault`.
- Root filesystem remains writable to support Neo4j startup scripts and tmp usage; keep volumes for data/logs/plugins mounted as before.

These defaults are applied automatically. If you need to override them (e.g. for Pod Security Standards / OpenShift SCC compatibility), set:

- `spec.securityContext.podSecurityContext` — full `*corev1.PodSecurityContext` override (replaces the operator's default)
- `spec.securityContext.containerSecurityContext` — full `*corev1.SecurityContext` override on the Neo4j container

When either field is unset, the hardened default above is applied. If you add sidecars, align their security contexts with these defaults.
