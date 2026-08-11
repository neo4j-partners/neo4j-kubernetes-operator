#!/usr/bin/env bash
# Cluster mode, 3 primaries, defaultPrimariesCount omitted — proves what the default
# actually does at runtime: the neo4j database is allocated on 1 server, not 3.

export NEO4J_CASE_NAME=cluster-3primaries-default-db
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-e2e-cluster-defaultdb}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false
export NEO4J_TOPOLOGY_MODE=Cluster
export NEO4J_POOL=primary
export CLUSTER_EXPECTED_MEMBERS=3
# The field is unset in the fixture, so the operator must fall back to 1 — a 1-primary
# database on a 3-primary cluster.
export CLUSTER_EXPECTED_DB_PRIMARIES=1
