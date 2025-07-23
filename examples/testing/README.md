# Testing Examples

This directory contains example configurations used for testing and development of the Neo4j Kubernetes Operator.

## Test Configuration Files

### Version-Specific Tests
- **neo4j-2025-cluster.yaml**: Example cluster using Neo4j 2025.x (CalVer) for testing V2_ONLY discovery configuration
- **neo4j-526-cluster.yaml**: Example cluster using Neo4j 5.26 (SemVer) for testing V2_ONLY discovery configuration

### Cluster Formation Tests
- **test-cluster-formation.yaml**: Basic cluster formation test configuration
- **test-cluster.yaml**: Simple test cluster configuration
- **test-discovery-endpoints.yaml**: Tests for Neo4j Kubernetes discovery endpoints
- **test-full-parallel.yaml**: Full parallel pod startup configuration
- **test-parallel-formation.yaml**: Parallel cluster formation test
- **test-ordered-min1.yaml**: Ordered pod management with MIN_PRIMARIES=1
- **test-secondary-delay.yaml**: Tests secondary startup delay behavior

### Topology Tests
- **test-1primary-1secondary-cluster.yaml**: Minimal cluster topology for testing cluster formation
- **test-3primary-3secondary-cluster.yaml**: Larger cluster topology test (3+3 configuration)
- **test-primary-only.yaml**: Primary-only cluster configuration test

### Security Tests
- **test-tls-cluster.yaml**: TLS-enabled cluster configuration test

## Usage

These examples are primarily for development and testing purposes. They are used for:
1. Integration testing during operator development
2. Validating cluster formation strategies
3. Testing version-specific configurations
4. Debugging discovery and topology issues

For production deployments, use the examples in:
- `../clusters/` - Production-ready cluster configurations
- `../standalone/` - Single-node deployments
- `../end-to-end/` - Complete deployment scenarios

## Important Notes

### Discovery Configuration
- All cluster examples use the unified clustering approach with V2_ONLY discovery
- Examples include the critical fix for Neo4j 5.26+ that uses `tcp-discovery` port (5000) instead of `tcp-tx` port (6000)
- Discovery service uses `neo4j.com/clustering=true` label for proper endpoint resolution

### Cluster Formation
- Minimal clusters (1 primary + 1 secondary) require both pods to be ready for cluster formation
- Parallel pod management (`ParallelPodManagement`) is used for optimal formation speed
- `MIN_PRIMARIES=1` allows flexible cluster formation

### TLS Configuration
- TLS clusters require cert-manager to be installed
- Uses `trust_all=true` for cluster SSL policy to ensure proper handshake
- Parallel pod startup is essential for TLS cluster formation

## Running Tests

```bash
# Apply a test configuration
kubectl apply -f test-cluster.yaml

# Watch cluster formation
kubectl get pods -w

# Check cluster status
kubectl exec <pod-name> -- cypher-shell -u neo4j -p <password> "SHOW SERVERS"

# Clean up
kubectl delete -f test-cluster.yaml
```
