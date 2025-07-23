# Neo4j Database Management

This guide covers database management features in the Neo4j Kubernetes Operator for Neo4j Enterprise 5.26+ and 2025.x.

## Overview

The operator provides the `Neo4jDatabase` CRD for managing databases within Neo4j Enterprise clusters. This includes:

- Creating databases with proper Neo4j 5.26+ syntax
- Database topology configuration for cluster distribution
- State management (start/stop databases)
- Initial data import
- Support for Neo4j 2025.x Cypher language versions

## Creating a Database

### Basic Database Creation

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: my-database
spec:
  clusterRef: my-neo4j-cluster
  name: mydb
  wait: true              # Wait for creation to complete
  ifNotExists: true       # Only create if doesn't exist
```

### Database with Topology

For clustered deployments, you can specify how the database should be distributed:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: orders-database
spec:
  clusterRef: my-neo4j-cluster
  name: orders
  topology:
    primaries: 2        # Number of primary servers
    secondaries: 1      # Number of secondary servers
  options:
    txLogEnrichment: "FULL"
```

### Neo4j 2025.x Cypher Language Version

For Neo4j 2025.x deployments, you can specify the default Cypher language version:

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jDatabase
metadata:
  name: modern-database
spec:
  clusterRef: my-neo4j-cluster
  name: modern
  defaultCypherLanguage: "25"  # Use Cypher 25 syntax
```

## Database Options

The `options` field supports all Neo4j database configuration options:

```yaml
spec:
  options:
    txLogEnrichment: "FULL"
    existingDataBehavior: "use"
    # Add any valid Neo4j database options
```

## Initial Data Import

You can import initial data when creating a database:

```yaml
spec:
  initialData:
    source: cypher
    cypherStatements:
      - "CREATE CONSTRAINT order_id_unique IF NOT EXISTS ON (o:Order) ASSERT o.orderId IS UNIQUE"
      - "CREATE INDEX order_date_index IF NOT EXISTS FOR (o:Order) ON (o.orderDate)"
      - "CREATE (:Order {orderId: 1, orderDate: date()})"
```

## Database Status

The operator tracks database state and provides status information:

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: DatabaseReady
  state: "online"          # Current database state
  servers:                 # Servers hosting this database
    - "server-0"
    - "server-1"
  dataImported: true
```

## Key Features

### WAIT/NOWAIT Support

- `wait: true` - Wait for database creation to complete (default)
- `wait: false` - Return immediately (async creation)

### IF NOT EXISTS

- `ifNotExists: true` - Only create if database doesn't exist (default)
- `ifNotExists: false` - Error if database already exists

### Topology Constraints

For clustered deployments, specify database distribution:
- `primaries`: Number of primary servers for this database
- `secondaries`: Number of secondary servers for this database

### Version-Specific Features

**Neo4j 5.26.x**:
- Standard database creation with topology
- All core database options supported

**Neo4j 2025.x**:
- Adds `defaultCypherLanguage` option
- Supports Cypher version selection (5 or 25)

## Best Practices

1. **Always use `ifNotExists: true`** in production to prevent errors on reconciliation
2. **Set appropriate topology** for production databases (e.g., 3 primaries for HA)
3. **Use `wait: true`** to ensure database is ready before dependent operations
4. **Validate initial data** statements before applying
5. **Monitor database state** in status for health checks

## Troubleshooting

### Database Creation Fails

Check the operator logs:
```bash
kubectl logs -n neo4j-operator deployment/neo4j-operator-controller-manager
```

Common issues:
- Insufficient cluster resources for topology
- Invalid database name (must follow Neo4j naming rules)
- Syntax errors in initial data statements

### Database Stuck in Creating State

This usually indicates the cluster is busy or unhealthy:
1. Check cluster status: `kubectl get neo4jenterprisecluster`
2. Verify cluster has enough primaries for requested topology
3. Check Neo4j logs for errors

### Initial Data Import Fails

- Ensure Cypher statements are valid
- Check for constraint violations
- Verify database exists before import
- Review operator logs for specific errors
