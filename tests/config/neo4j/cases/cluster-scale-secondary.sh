#!/usr/bin/env bash
# Cluster mode, one primary and a read pool grown 1 -> 2 then shrunk back to 1 — the secondary
# resize path, which the primary-pool case cannot reach.
#
# A departing secondary hands its user databases over and then keeps hosting `system`, and Neo4j
# can leave it in the Deallocating state for good: the state label alone never says the drain is
# over, so the operator reads what the member still hosts. That only happens on a secondary pool,
# and 1 primary is the shape it was reported on.
#
# The primary pool stays at 1 throughout on purpose: resizing a read pool must not roll the
# primary, so the assert holds it to the same pod UID, restart count and config checksum.

export NEO4J_CASE_NAME=cluster-scale-secondary
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-e2e-cluster-scale-read}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false
export NEO4J_TOPOLOGY_MODE=Cluster
# The shared cluster asserts inspect one pool: the primary one, which this case never resizes.
export NEO4J_POOL=primary

# Starting shape. CLUSTER_EXPECTED_MEMBERS is the primary pool, not the member total — the read
# member adds a server to SHOW SERVERS, and assert/cluster-formed takes it as a floor.
export CLUSTER_EXPECTED_MEMBERS=1
export CLUSTER_EXPECTED_DB_PRIMARIES=1

# Resize targets. SECONDARY_SCALE_POOL being set is what turns assert/cluster-scale-secondary on —
# it is a no-op for every other case of the suite.
export SECONDARY_SCALE_POOL=read
export SECONDARY_SCALE_OUT_MEMBERS=2
export SECONDARY_SCALE_IN_MEMBERS=1
