#!/usr/bin/env bash
# Classic: minimal Standalone — default namespace, dynamic PVC, no StorageClass override.

export NEO4J_CASE_NAME=standalone-minimal
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-dev}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false
