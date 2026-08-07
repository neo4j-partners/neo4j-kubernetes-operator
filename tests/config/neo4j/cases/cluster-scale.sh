#!/usr/bin/env bash
# Cluster with 3 primaries + a read-secondary pool, used to exercise scale-out/in.
#
# NEO4J_POOL stays "primary" so the shared cluster asserts keep querying a primary
# pod (SHOW SERVERS must be run against a cluster member that hosts system).
# SCALE_POOL names the pool actually being resized — secondaries are the only
# supported scale unit (primary 1<->N is gated by the operator).

export NEO4J_CASE_NAME=cluster-scale
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-e2e-scale}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false
export NEO4J_TOPOLOGY_MODE=Cluster
export NEO4J_POOL=primary
export CLUSTER_EXPECTED_MEMBERS=3

# Pool under test, its spec path, and the scale steps.
export SCALE_POOL=read
export SCALE_SPEC_PATH="/spec/topology/secondaries/read/members"
export SCALE_BASELINE=1
export SCALE_TARGET=2
