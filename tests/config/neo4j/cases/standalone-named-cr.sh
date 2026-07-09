#!/usr/bin/env bash
# Classic: non-default CR name (validates labels/selectors with custom instance name).

export NEO4J_CASE_NAME=standalone-named-cr
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-e2e-graph}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false
