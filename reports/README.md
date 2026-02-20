# Neo4j Kubernetes Operator — Technical Reports

This directory contains technical reports that document significant architectural decisions, investigations, and implementation details for the Neo4j Kubernetes Operator. Reports are retained only when they provide lasting reference value.

## Naming Convention

All report files use the format: `YYYY-MM-DD-descriptive-name.md`

## Reports

### 🏗️ Architecture & Design

- **[2025-08-19-server-based-architecture-implementation.md](2025-08-19-server-based-architecture-implementation.md)** — Server-based architecture: single `{cluster}-server` StatefulSet replacing old primary/secondary split. **Referenced in CLAUDE.md.**
- **[2025-08-20-neo4j-plugin-architecture-compatibility-prd.md](2025-08-20-neo4j-plugin-architecture-compatibility-prd.md)** — Plugin architecture PRD: env-var vs neo4j.conf plugin categories, dependency resolution, compatibility matrix.
- **[2025-09-03-property-sharding-implementation-analysis.md](2025-09-03-property-sharding-implementation-analysis.md)** — Property sharding (`Neo4jShardedDatabase` CRD): implementation analysis, resource requirements (5+ servers, 4–8 Gi each).

### 🔧 Neo4j Version Analysis

- **[2025-08-05-neo4j-2025.01.0-enterprise-cluster-analysis.md](2025-08-05-neo4j-2025.01.0-enterprise-cluster-analysis.md)** — Neo4j 2025.x calver compatibility: discovery parameter differences, cluster formation requirements. **Referenced in CLAUDE.md.**
- **[2025-08-08-seed-uri-and-server-architecture-release-notes.md](2025-08-08-seed-uri-and-server-architecture-release-notes.md)** — Seed URI feature implementation and server architecture integration notes. **Referenced in CLAUDE.md.**
- **[2025-08-12-neo4j-syntax-modernization.md](2025-08-12-neo4j-syntax-modernization.md)** — Neo4j 5.x/2025.x Cypher syntax modernization: `TOPOLOGY` clause, deprecated 4.x syntax.
- **[2025-07-16-deprecated-neo4j-4x-settings-audit.md](2025-07-16-deprecated-neo4j-4x-settings-audit.md)** — Audit of deprecated Neo4j 4.x settings to avoid (`causal_clustering.*`, `dbms.mode=SINGLE`, etc.).

### 🚀 Cluster Formation & Reliability

- **[2025-08-05-resource-version-conflict-resolution-analysis.md](2025-08-05-resource-version-conflict-resolution-analysis.md)** — Critical fix: `retry.RetryOnConflict` for Neo4j 2025.x cluster formation. Root cause and solution.
- **[2025-07-18-neo4j-discovery-milestone-summary.md](2025-07-18-neo4j-discovery-milestone-summary.md)** — V2_ONLY discovery architecture: `tcp-discovery` port (5000), service configuration.
- **[2025-07-24-reconcile-loop-analysis.md](2025-07-24-reconcile-loop-analysis.md)** — Reconciliation loop performance: debounce, ConfigMap manager, frequency analysis.

### 💾 Backup & Restore

- **[2025-07-21-neo4j-5.26-2025-database-backup-restore-implementation.md](2025-07-21-neo4j-5.26-2025-database-backup-restore-implementation.md)** — Centralized backup StatefulSet implementation: `--to-path` syntax, automated path creation, Neo4j 5.26+ compatibility.

### 🔒 Security & TLS

- **[2025-07-16-tls-implementation-analysis.md](2025-07-16-tls-implementation-analysis.md)** — TLS/SSL implementation: cert-manager integration, `dbms.ssl.policy.*` configuration, cluster TLS.
- **[2025-11-20-security-review.md](2025-11-20-security-review.md)** — Security review: RBAC, secret handling, network policies, CRD validation.

### 🐛 Bug Analysis

- **[2025-08-12-database-validation-oom-fix.md](2025-08-12-database-validation-oom-fix.md)** — OOM fix: Neo4j Enterprise minimum 1.5 Gi memory requirement for database operations.

### 🧪 Testing

- **[2025-08-29-comprehensive-test-suite-documentation.md](2025-08-29-comprehensive-test-suite-documentation.md)** — Complete test suite documentation: unit, integration, e2e structure, AfterEach cleanup patterns.

### 📋 Audits

- **[2026-01-19-neo4j-operator-comprehensive-audit-report.md](2026-01-19-neo4j-operator-comprehensive-audit-report.md)** — Most recent comprehensive operator audit (January 2026).

## Guidelines

### When to Create a Report

Create a report when:
1. Implementing significant architectural changes
2. Resolving complex bugs that required investigation
3. Conducting security or compliance audits
4. Producing analysis that informs future decisions

### When NOT to Create a Report

Do NOT create a report for:
- Brief implementation summaries (use git commit messages instead)
- Release notes (not stored as files in this repo)
- Cleanup or refactoring summaries
- Routine test fixes

### Report Structure

1. **Date**: In the filename (`YYYY-MM-DD-`) and at the top of the document
2. **Executive Summary**: Brief overview
3. **Problem/Context**: What prompted the work
4. **Analysis**: Investigation steps
5. **Solution**: What was implemented
6. **Results**: Outcomes and impact

Last Updated: 2026-01-21
