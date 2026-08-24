#!/usr/bin/env bash
# Cluster (3 primaries) with Bring-Your-Own TLS for bolt + cluster (NEO-2-005). The
# trust/provision-byo-cluster action creates the referenced Secrets (a private CA signs shared
# leaves with per-member SANs); the operator mounts them, members do mTLS, and the cluster forms
# and serves over TLS. TLS_EXPECT_CERTIFICATES=false so assert/tls-ready skips the cert-manager
# Certificate check (there are none — the user supplies the material).

export NEO4J_CASE_NAME=cluster-tls-byo
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-e2e-tls-byo-cl}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false
export NEO4J_TOPOLOGY_MODE=Cluster
export NEO4J_POOL=primary
export CLUSTER_EXPECTED_MEMBERS=3
export TLS_EXPECT_CERTIFICATES=false
