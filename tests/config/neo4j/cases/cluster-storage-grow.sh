#!/usr/bin/env bash
# Cluster whose data volumes are grown after the cluster has formed (STO-004, BDR-005).
#
# The cluster half of the grow matters on its own: the StatefulSet template keeps the size it was
# created with, so a grow that only reached ordinal 0 would leave the other two primaries serving
# the old size — and the status writer used to look at ordinal 0 alone, which is precisely how that
# would have gone unnoticed. Three primaries mean three claims for one patch of the CR.

export NEO4J_CASE_NAME=cluster-storage-grow
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-e2e-cluster-grow}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-5Gi}"
export NEO4J_USE_STORAGE_CLASS=false
export NEO4J_TOPOLOGY_MODE=Cluster
export NEO4J_POOL=primary
export CLUSTER_EXPECTED_MEMBERS=3
# The suite runs its whole assert list for every case, so this case has to describe the cluster to
# the shared asserts as fully as any other. The fixture sets defaultPrimariesCount=3, so the neo4j
# database spans all three primaries.
export CLUSTER_EXPECTED_DB_PRIMARIES=3
export STORAGE_GROW_TO="${STORAGE_GROW_TO:-10Gi}"
export STORAGE_GROW_TIMEOUT="${STORAGE_GROW_TIMEOUT:-900}"
