# Security

This guide explains how to secure your Neo4j Enterprise clusters.

## Authentication

The operator supports a variety of authentication providers, including:

*   Native Neo4j authentication
*   LDAP
*   Kerberos
*   JWT

To configure authentication, you can use the `spec.auth` field in the `Neo4jEnterpriseCluster` resource.

## TLS

The operator supports TLS encryption for all communication. You can enable TLS by setting the `spec.tls.enabled` field to `true`.

The operator integrates with `cert-manager` to automatically provision and manage TLS certificates.

## Network Policies

The operator can automatically create network policies to restrict traffic to your Neo4j cluster.
