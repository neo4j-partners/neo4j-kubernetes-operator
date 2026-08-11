#!/usr/bin/env bash
# Cluster mode, 1 primary — the "lab" topology (no quorum, not HA).
# Cheap smoke test that Cluster mode renders, boots and forms with a single member.

export NEO4J_CASE_NAME=cluster-single
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-e2e-cluster-lab}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false
export NEO4J_TOPOLOGY_MODE=Cluster
# Cluster StatefulSets are named <cr>-<pool>; primaries live in the "primary" pool.
export NEO4J_POOL=primary
export CLUSTER_EXPECTED_MEMBERS=1
# One server, so the default database can only be single-primary.
export CLUSTER_EXPECTED_DB_PRIMARIES=1
