#!/usr/bin/env bash
# Cluster mode, 3 primaries — the smallest real HA topology (quorum of 3).
# This is the case that proves actual cluster formation, not just rendering.

export NEO4J_CASE_NAME=cluster-3primaries
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-e2e-cluster-ha}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false
export NEO4J_TOPOLOGY_MODE=Cluster
export NEO4J_POOL=primary
export CLUSTER_EXPECTED_MEMBERS=3
# The fixture sets topology.defaultPrimariesCount=3, so the neo4j database must end up
# on all three primaries — not on the single one the field defaults to.
export CLUSTER_EXPECTED_DB_PRIMARIES=3
