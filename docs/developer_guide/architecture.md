# Architecture

This guide provides an overview of the Neo4j Enterprise Operator's architecture.

## Controllers

The operator is built around a set of controllers that manage the lifecycle of Neo4j resources. The main controller is the `Neo4jEnterpriseCluster` controller, which is responsible for deploying and managing Neo4j clusters.

## Custom Resource Definitions (CRDs)

The operator defines a set of CRDs to represent Neo4j resources, such as clusters, backups, and databases.

## Webhooks

The operator uses admission webhooks to validate and mutate Neo4j resources.
