#!/usr/bin/env bash
# Standalone whose data volume is grown after it is serving (STO-004, BDR-005).
#
# STORAGE_GROW_TO is the trigger the storage-grow and storage-grow-refused asserts look for; the
# baseline is 5Gi, fixed in tests/fixtures/neo4j-storage-grow.yaml because the size is the subject
# of these cases and must not be inherited from a variable another case could move.

export NEO4J_CASE_NAME=standalone-storage-grow
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-st-grow}"
export NEO4J_USE_STORAGE_CLASS=false
export STORAGE_GROW_TO="${STORAGE_GROW_TO:-10Gi}"
