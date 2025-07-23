# Neo4jDatabase

This document provides a reference for the `Neo4jDatabase` Custom Resource Definition (CRD). This resource is used to create and manage databases within a Neo4j cluster.

## Overview

The Neo4jDatabase CRD allows you to declaratively manage databases in Neo4j Enterprise clusters. It supports Neo4j 5.26+ features including:
- IF NOT EXISTS clause to prevent reconciliation errors
- WAIT/NOWAIT options for synchronous or asynchronous creation
- Database topology constraints for cluster distribution
- Cypher language version selection (Neo4j 2025.x)
- Initial data import and schema creation

## Spec

| Field | Type | Description |
|---|---|---|
| `clusterRef` | `string` | **Required**. The name of the Neo4j cluster to create the database in. |
| `name` | `string` | **Required**. The name of the database to create. |
| `wait` | `boolean` | Whether to wait for database creation to complete. Default: `true` |
| `ifNotExists` | `boolean` | Create database only if it doesn't exist. Prevents errors on reconciliation. Default: `true` |
| `topology` | `DatabaseTopology` | Database distribution topology in the cluster. |
| `defaultCypherLanguage` | `string` | Default Cypher language version (Neo4j 2025.x only). Values: `"5"`, `"25"` |
| `options` | `map[string]string` | Additional database options (e.g., `txLogEnrichment`). |
| `initialData` | `InitialDataSpec` | Initial data to import when creating the database. |
| `state` | `string` | Desired database state. Values: `"online"`, `"offline"` |

### DatabaseTopology

| Field | Type | Description |
|---|---|---|
| `primaries` | `integer` | Number of primary servers to host the database. |
| `secondaries` | `integer` | Number of secondary servers to host the database. |

### InitialDataSpec

| Field | Type | Description |
|---|---|---|
| `source` | `string` | Source type for initial data. Currently supports: `"cypher"` |
| `cypherStatements` | `[]string` | List of Cypher statements to execute on database creation. |

## Status

| Field | Type | Description |
|---|---|---|
| `phase` | `string` | Current phase of the database. Values: `"Pending"`, `"Creating"`, `"Ready"`, `"Failed"` |
| `state` | `string` | Current database state. Values: `"online"`, `"offline"`, `"starting"`, `"stopping"` |
| `servers` | `[]string` | List of servers hosting the database. |
| `dataImported` | `boolean` | Whether initial data has been imported. |
| `message` | `string` | Human-readable status message. |
| `lastUpdated` | `Time` | Timestamp of last status update. |

## Examples

### Basic Database Creation

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: my-database
spec:
  clusterRef: my-cluster
  name: mydb
  wait: true
  ifNotExists: true
```

### Database with Topology Constraints

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: distributed-database
spec:
  clusterRef: production-cluster
  name: distributed
  wait: true
  ifNotExists: true
  topology:
    primaries: 3
    secondaries: 2
  options:
    txLogEnrichment: "FULL"
```

### Database with Initial Schema (Neo4j 5.26.x)

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: app-database
spec:
  clusterRef: my-cluster
  name: appdb
  wait: true
  ifNotExists: true
  initialData:
    source: cypher
    cypherStatements:
      - "CREATE CONSTRAINT user_email IF NOT EXISTS ON (u:User) ASSERT u.email IS UNIQUE"
      - "CREATE INDEX user_name IF NOT EXISTS FOR (u:User) ON (u.name)"
      - "CREATE INDEX product_category IF NOT EXISTS FOR (p:Product) ON (p.category)"
```

### Neo4j 2025.x Database with Cypher 25

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: modern-database
spec:
  clusterRef: neo4j-2025-cluster
  name: moderndb
  wait: true
  ifNotExists: true
  defaultCypherLanguage: "25"  # Use Cypher 25 features
  topology:
    primaries: 2
    secondaries: 1
```

### Asynchronous Database Creation

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: async-database
spec:
  clusterRef: my-cluster
  name: asyncdb
  wait: false  # NOWAIT - returns immediately
  ifNotExists: true
```

## Behavior

### Creation Process

1. The operator checks if the database already exists (if `ifNotExists: true`)
2. Constructs the CREATE DATABASE command with appropriate options
3. Executes the command on the cluster
4. If `wait: true`, waits for database to be fully online
5. If initial data is specified, imports it after database is online
6. Updates the status with current state and hosting servers

### Version-Specific Behavior

**Neo4j 5.26.x**:
- Standard CREATE DATABASE syntax
- No Cypher language version support
- Supports all topology and option features

**Neo4j 2025.x**:
- Supports `defaultCypherLanguage` field
- Enhanced topology management
- Additional database options available

### Reconciliation

The operator continuously reconciles the database state:
- If database doesn't exist and `ifNotExists: true`, creates it
- If database exists and state differs, updates it (start/stop)
- If topology changes, redistributes database (Neo4j 5.20+)
- Updates status with current database information

## Best Practices

1. **Always use `ifNotExists: true`** in production to prevent reconciliation errors
2. **Set appropriate topology** based on your availability requirements
3. **Use `wait: true`** for critical databases to ensure they're ready
4. **Include IF NOT EXISTS** in schema creation statements
5. **Test database creation** in staging before production deployment

## Troubleshooting

### Database Creation Fails

Check the operator logs:
```bash
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager
```

Common issues:
- Insufficient cluster resources (primaries/secondaries)
- Name conflicts with existing databases
- Invalid Cypher statements in initial data
- Network connectivity to cluster

### Database Stuck in Pending

Verify cluster is ready:
```bash
kubectl get neo4jenterprisecluster <cluster-name>
```

Check database status:
```bash
kubectl describe neo4jdatabase <database-name>
```

### Initial Data Not Imported

- Ensure Cypher statements are valid
- Check for constraint/index conflicts
- Verify database is online before import
- Review operator logs for import errors
