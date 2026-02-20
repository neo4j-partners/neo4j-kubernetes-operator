# Neo4j Enterprise Operator Documentation

Welcome to the documentation for the Neo4j Enterprise Operator for Kubernetes.

## 📖 User Guide

The [User Guide](user_guide) is for users of the Neo4j Enterprise Operator. It contains all the information you need to deploy, manage, and operate Neo4j clusters on Kubernetes.

*   **[Getting Started](user_guide/getting_started.md)**: A quickstart guide to get you up and running in minutes.
*   **[Deployment Guide](deployment-guide.md)**: Complete deployment reference with local and registry-based options.
*   **[Operator Modes](user_guide/operator-modes.md)**: Complete guide to production and development modes, caching strategies, and performance tuning.
*   **[Installation](user_guide/installation.md)**: Detailed installation instructions via git clone and make commands.
*   **[Configuration](user_guide/configuration.md)**: Comprehensive configuration options.
*   **[External Access](user_guide/external_access.md)**: Expose Neo4j outside Kubernetes using LoadBalancer, NodePort, or Ingress.
*   **[Topology Placement](user_guide/topology_placement.md)**: Configure zone distribution, anti-affinity, and advanced placement strategies.
*   **[Property Sharding](user_guide/property_sharding.md)**: Horizontal scaling for large datasets with property sharding
*   **[Aura Fleet Management](user_guide/aura_fleet_management.md)**: Monitor self-managed deployments from the Neo4j Aura console
*   **[Guides](user_guide/guides)**: In-depth guides on specific topics, such as:
    *   [Configuration Best Practices](user_guide/guides/configuration_best_practices.md) - Neo4j 5.26+ configuration guidelines and **seed URI best practices**
    *   [Backup and Restore](user_guide/guides/backup_restore.md) - Comprehensive backup and restore operations including PITR
    *   [Backup & Restore Troubleshooting](user_guide/guides/troubleshooting_backup_restore.md) - Troubleshooting guide for backup/restore issues
    *   [MCP Client Setup](user_guide/guides/mcp_client_setup.md) - Connect VSCode/Claude to MCP over HTTP
    *   [Security](user_guide/guides/security.md)
    *   [Performance](user_guide/guides/performance.md)
    *   [Monitoring](user_guide/guides/monitoring.md)
    *   [Upgrades](user_guide/guides/upgrades.md)
    *   [Troubleshooting](user_guide/guides/troubleshooting.md) - Including **seed URI troubleshooting**

## 👨‍💻 Developer Guide

The [Developer Guide](developer_guide) is for contributors and developers who want to work on the Neo4j Enterprise Operator itself.

*   **[Development](developer_guide/development.md)**: How to set up your development environment and get started with contributing.
*   **[Architecture](developer_guide/architecture.md)**: An overview of the operator's architecture.
*   **[Testing](developer_guide/testing.md)**: How to run the test suite.

## 📋 Quick Reference

Need something fast? Check out our quick reference materials:

*   **[Operator Modes Cheat Sheet](quick-reference/operator-modes-cheat-sheet.md)**: Essential commands, flags, and troubleshooting for production and development modes

## 📄 API Reference

The [API Reference](api_reference) contains detailed information about the operator's Custom Resource Definitions (CRDs).

*   **[Neo4jEnterpriseCluster](api_reference/neo4jenterprisecluster.md)**
*   **[Neo4jEnterpriseStandalone](api_reference/neo4jenterprisestandalone.md)**
*   **[Neo4jBackup](api_reference/neo4jbackup.md)**
*   **[Neo4jRestore](api_reference/neo4jrestore.md)**
*   **[Neo4jDatabase](api_reference/neo4jdatabase.md)** - Enhanced with IF NOT EXISTS, WAIT/NOWAIT, topology support, and **seed URI functionality**
*   **[Neo4jPlugin](api_reference/neo4jplugin.md)** - Smart plugin management with Neo4j 5.26+ compatibility
*   **[Neo4jShardedDatabase](api_reference/neo4jshardeddatabase.md)** - Property sharding for horizontal scaling

## 🚀 End-to-End Examples

Complete deployment examples demonstrating real-world scenarios:

*   **[Complete Production Deployment](../examples/end-to-end/complete-deployment.yaml)** - Full production setup with TLS, monitoring, and automated backups
*   **[Disaster Recovery](../examples/end-to-end/disaster-recovery.yaml)** - Backup strategies, PITR, and cross-region recovery
*   **[Development Workflow](../examples/end-to-end/development-workflow.yaml)** - Local development, migrations, and CI/CD integration
*   **[Multi-Tenancy](../examples/end-to-end/multi-tenancy.yaml)** - Shared clusters with tenant isolation
*   **[Property Sharding](../examples/property_sharding/)** - Horizontal scaling with property sharding for large datasets

## 🆕 What's New

### Latest Features (Neo4j 5.26+ and 2025.x)
*   **Database Management**: Create databases with `IF NOT EXISTS`, `WAIT`/`NOWAIT` options
*   **🆕 Aura Fleet Management**: Monitor self-managed Neo4j deployments from the Aura console — operator installs the pre-bundled plugin and handles token registration automatically; works alongside any `Neo4jPlugin` CRDs
*   **🆕 Seed URI Functionality**: Create databases directly from existing backups stored in cloud storage
*   **🆕 Property Sharding**: Horizontal scaling for large datasets with separate graph and property shards
*   **🆕 MCP Server Support**: Optional MCP server deployment for Neo4j clusters and standalone workloads (HTTPS preferred, STDIO for in-cluster use)
*   **Topology Constraints**: Specify primary/secondary distribution for databases
*   **Version Detection**: Automatic adaptation for Neo4j 5.26.x (SemVer) and 2025.x (CalVer)
*   **CalVer Acceptance Note**: 2025.x+ (including 2026.x) is accepted by default, but new CalVer features may need operator updates for full compatibility
*   **Cypher Language**: Support for Cypher 25 in Neo4j 2025.x
*   **Backup Improvements**: FULL/DIFF/AUTO backup types, backup from secondaries
*   **Point-in-Time Recovery**: Restore to specific timestamps with `--restore-until`
*   **Centralized Backup**: Single backup StatefulSet per cluster for resource efficiency

## 🚀 Seed URI Database Creation

Create Neo4j databases directly from existing backups stored in cloud storage - perfect for migrations, disaster recovery, and environment setup.

### Quick Example

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: migrated-production-db
spec:
  clusterRef: production-cluster
  name: production

  # Create from S3 backup with point-in-time recovery
  seedURI: "s3://my-neo4j-backups/production-2025-01-15.backup"
  seedConfig:
    restoreUntil: "2025-01-15T10:30:00Z"  # Neo4j 2025.x only
    config:
      compression: "gzip"
      validation: "strict"

  # Database topology
  topology:
    primaries: 2
    secondaries: 2

  wait: true
  ifNotExists: true
```

### Supported Features

*   **☁️ Multi-Cloud Support**: S3, Google Cloud Storage, Azure Blob Storage
*   **🌐 HTTP/FTP**: Support for HTTP/HTTPS and FTP sources
*   **🔒 Flexible Authentication**: System-wide (IAM roles, workload identity) or explicit credentials
*   **⏰ Point-in-Time Recovery**: Restore to specific timestamps (Neo4j 2025.x)
*   **📊 Topology Control**: Specify database distribution across cluster servers
*   **⚡ Performance Optimized**: Support for .backup format with compression options

### Examples and Documentation

*   **[Database Seed URI Examples](../examples/databases/)** - Complete examples for all cloud providers
*   **[Seed URI Feature Guide](seed-uri-feature-guide.md)** - Comprehensive guide with authentication, troubleshooting, and best practices
*   **[Neo4jDatabase API Reference](api_reference/neo4jdatabase.md)** - Full API documentation with seed URI fields
