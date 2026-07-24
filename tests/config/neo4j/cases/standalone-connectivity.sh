#!/usr/bin/env bash
# Standalone without TLS, used to validate connector reachability.
# Connectivity requires a running Neo4j, so readiness is enforced here.
# No-TLS matrix: bolt/neo4j/http reachable, https refused (connector not exposed).

export NEO4J_CASE_NAME=standalone-connectivity
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-conn}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false

export E2E_ASSERT_NEO4J_READY=true

export EXPECT_CONN_BOLT="${EXPECT_CONN_BOLT:-success}"
export EXPECT_CONN_NEO4J="${EXPECT_CONN_NEO4J:-success}"
export EXPECT_CONN_HTTP="${EXPECT_CONN_HTTP:-success}"
export EXPECT_CONN_HTTPS="${EXPECT_CONN_HTTPS:-failure}"
