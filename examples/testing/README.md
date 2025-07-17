# Testing Examples

This directory contains example configurations used for testing and development of the Neo4j Kubernetes Operator.

## Files

- **neo4j-2025-cluster.yaml**: Example cluster using Neo4j 2025.x (CalVer) for testing V2_ONLY discovery configuration
- **neo4j-526-cluster.yaml**: Example cluster using Neo4j 5.26 (SemVer) for testing V2_ONLY discovery configuration
- **test-1primary-1secondary-cluster.yaml**: Minimal cluster topology for testing cluster formation

## Usage

These examples are primarily for development and testing purposes. For production deployments, use the examples in the `../clusters/` and `../standalone/` directories.

## Important Notes

- All cluster examples use the unified clustering approach with V2_ONLY discovery
- Examples include the critical fix for Neo4j 5.26+ that uses `tcp-discovery` port (5000) instead of `tcp-tx` port (6000)
- Minimal clusters (1 primary + 1 secondary) require both pods to be ready for cluster formation
