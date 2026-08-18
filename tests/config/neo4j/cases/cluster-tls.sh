#!/usr/bin/env bash
# Cluster mode, 3 primaries, with BYO cluster TLS material (NEO-3-005-TLS-03).
# Shares the shape of cluster-3primaries; the difference is spec.trust in the fixture and
# the certificate Secrets that tls/ensure-cluster-certs publishes before the CR is applied.

export NEO4J_CASE_NAME=cluster-tls
# Must match the literal Secret names in tests/fixtures/neo4j-cluster-tls.yaml, which
# tls/ensure-cluster-certs derives from this CR name.
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-e2e-tls}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false
export NEO4J_TOPOLOGY_MODE=Cluster
export NEO4J_POOL=primary
export CLUSTER_EXPECTED_MEMBERS=3
export CLUSTER_EXPECTED_DB_PRIMARIES=3
