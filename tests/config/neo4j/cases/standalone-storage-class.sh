#!/usr/bin/env bash
# Classic: Standalone with explicit StorageClass (uses cloud STORAGE_CLASS_NAME).

export NEO4J_CASE_NAME=standalone-storage-class
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-dev}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-20Gi}"
export NEO4J_USE_STORAGE_CLASS=true
