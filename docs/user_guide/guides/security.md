# Security

This guide explains how to secure your Neo4j Enterprise clusters using the features provided by the operator.

## Authentication

The operator supports a variety of authentication providers, allowing you to integrate with your existing identity management systems.

*   **Native Neo4j authentication**: The default, managed via Kubernetes secrets.
*   **LDAP**: Integrate with your corporate LDAP or Active Directory.
*   **Kerberos**: For enterprise environments with Kerberos infrastructure.
*   **JWT**: Use JSON Web Tokens for authentication.

To configure authentication, you can use the `spec.auth` field in the `Neo4jEnterpriseCluster` resource. See the [API Reference](../../api_reference/neo4jenterprisecluster.md) for more details.

## TLS

The operator makes it easy to enable TLS encryption for all communication to and from your Neo4j cluster. You can enable TLS by setting the `spec.tls.enabled` field to `true`.

The operator integrates with `cert-manager` to automatically provision and manage TLS certificates. This is the recommended approach for production environments. You can specify a `cert-manager` issuer to use for signing certificates.

## Network Policies

The operator can automatically create Kubernetes `NetworkPolicy` resources to restrict traffic to your Neo4j cluster. This helps to enforce a zero-trust security model by ensuring that only authorized applications can connect to the database.

## RBAC

The operator itself runs with a specific `ServiceAccount` that is bound to a `ClusterRole` with the minimum necessary permissions to do its job. For end-users, the operator also provides a set of `ClusterRoles` (`neo4j-viewer`, `neo4j-editor`) that administrators can bind to users or groups to grant them appropriate levels of access to the Neo4j custom resources.

## Pod Security Defaults

The operator now hardens all Neo4j data-plane pods (servers, centralized backup, standalone, restore jobs, and hook jobs) with strict security contexts by default:

- `runAsNonRoot: true` with UID/GID `7474` (Neo4j user) and matching `fsGroup`.
- `allowPrivilegeEscalation: false`, `capabilities.drop: ["ALL"]`, `seccompProfile: RuntimeDefault`.
- Root filesystem remains writable to support Neo4j startup scripts and tmp usage; keep volumes for data/logs/plugins mounted as before.

These defaults are applied automatically; customization is not yet exposed via CRD fields. If you add sidecars, align their security contexts with these defaults.
