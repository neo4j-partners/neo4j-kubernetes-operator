# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| v1.6.x-alpha | Yes |
| v1.5.x-alpha | Security fixes only |
| < v1.5.0-alpha | No |

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, report them via [GitHub Security Advisories](https://github.com/priyolahiri/neo4j-kubernetes-operator/security/advisories/new).

Include as much of the following as you can:

- Description of the vulnerability
- Steps to reproduce or proof of concept
- Affected versions
- Impact assessment (what an attacker could achieve)
- Any suggested fix (optional)

## What to Expect

- **Acknowledgement** within 3 business days
- **Initial assessment** within 7 business days
- **Fix timeline** communicated after assessment — critical vulnerabilities are prioritized for the next patch release
- **Credit** in the release notes (unless you prefer to remain anonymous)

## Scope

The following are in scope:

- Neo4j Kubernetes Operator code (`internal/`, `cmd/`, `api/`)
- Helm chart templates (`charts/neo4j-operator/`)
- OLM bundle manifests (`bundle/`)
- CI/CD workflows (`.github/workflows/`)
- Container images published to `ghcr.io/priyolahiri/neo4j-kubernetes-operator`

The following are out of scope:

- Neo4j database server itself (report to [Neo4j Security](https://neo4j.com/security/))
- Third-party dependencies (report upstream, but let us know if it affects this operator)
- Infrastructure hosting the repository (report to GitHub)

## Security Best Practices

See the [Security Guide](docs/user_guide/security.md) for recommendations on deploying Neo4j securely with this operator, including TLS, authentication, network policies, and encryption at rest.
