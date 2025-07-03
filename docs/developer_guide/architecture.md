# Architecture

This guide provides an overview of the Neo4j Enterprise Operator's architecture.

## Controllers

The operator is built around a set of controllers that manage the lifecycle of Neo4j resources. The main controller is the `Neo4jEnterpriseCluster` controller, which is responsible for deploying and managing Neo4j clusters. The reconciliation logic for this controller can be found in `internal/controller/neo4jenterprisecluster_controller.go`.

## Custom Resource Definitions (CRDs)

The operator defines a set of CRDs to represent Neo4j resources, such as clusters, backups, and databases. The Go type definitions for these CRDs are located in `api/v1alpha1/`.

## Input Validation

The operator uses client-side validation to ensure that Neo4j resources are correctly configured before they are created or updated. The validation logic can be found in `internal/validation/`.

## RBAC

The operator's permissions are defined in `config/rbac/`. It uses a `ClusterRole` to manage resources across different namespaces, which is necessary for its function. The RBAC configuration is designed to follow the principle of least privilege.
