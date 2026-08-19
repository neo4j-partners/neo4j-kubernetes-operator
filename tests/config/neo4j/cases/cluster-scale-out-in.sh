#!/usr/bin/env bash
# Cluster mode, 3 primaries grown to 5 then shrunk back to 3 within one case — the resize path:
# ENABLE SERVER on the way out, drain before the StatefulSet shrinks on the way in.
#
# 3 -> 5 -> 3 and not 3 -> 1: Neo4j cannot narrow a multi-primary database to a single primary,
# so a shrink to one primary is refused (UnsupportedSinglePrimary) and belongs to its own case.

export NEO4J_CASE_NAME=cluster-scale-out-in
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-e2e-cluster-scale}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false
export NEO4J_TOPOLOGY_MODE=Cluster
export NEO4J_POOL=primary

# Starting shape, checked by the shared cluster asserts before anything is resized. The fixture is
# the 3-primary one, so defaultPrimariesCount is 3 and the neo4j database spans every primary.
export CLUSTER_EXPECTED_MEMBERS=3
export CLUSTER_EXPECTED_DB_PRIMARIES=3

# Resize targets. CLUSTER_SCALE_OUT_MEMBERS being set is what turns
# assert/cluster-scale-out-in on — it is a no-op for the other cases of the suite.
export CLUSTER_SCALE_OUT_MEMBERS=5
export CLUSTER_SCALE_IN_MEMBERS=3
# Created at the wide size with an explicit topology, so the scale-in has to deal with a database
# asking for more primaries than the target leaves.
export CLUSTER_SCALE_DB=scalewide
