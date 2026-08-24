#!/usr/bin/env bash
# Standalone with Bring-Your-Own bolt TLS (NEO-2-005). The trust/provision-byo action creates the
# referenced Secrets; the operator mounts them, dials bolt+s verifying against them, and Neo4j
# serves Bolt over TLS. Standalone (default pool "server") — no cluster mTLS.

export NEO4J_CASE_NAME=standalone-tls-byo
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-e2e-tls-byo}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false
export NEO4J_TOPOLOGY_MODE=Standalone
