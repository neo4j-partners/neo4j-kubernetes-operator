#!/usr/bin/env bash
# Cluster mode, 3 primaries, TLS material issued by cert-manager (NEO-2-005).
# The fixture carries a self-signed CA Issuer; the operator issues one leaf per policy
# (bolt, cluster) and the cluster must form over the issued material.

export NEO4J_CASE_NAME=cluster-tls-cert-manager
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-e2e-tls-cm}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false
export NEO4J_TOPOLOGY_MODE=Cluster
export NEO4J_POOL=primary
export CLUSTER_EXPECTED_MEMBERS=3
