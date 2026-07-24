#!/usr/bin/env bash
# Auth via a pre-created Secret (auth.passwordSecretRef.name). The harness creates the
# Secret with a known password, then proves that exact password authenticates over bolt.

export NEO4J_CASE_NAME=standalone-auth-secretref
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-dev-secref}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false

export E2E_ASSERT_NEO4J_READY=true

# Pre-created auth Secret (matches passwordSecretRef.name in the fixture).
export AUTH_SECRET_CREATE=true
export AUTH_SECRET_NAME="${AUTH_SECRET_NAME:-neo4j-auth}"
export AUTH_KNOWN_PASSWORD="${AUTH_KNOWN_PASSWORD:-ClientPass123}"

# assert/credentials expectations.
export CRED_EXPECT_GENERATED=false
export CRED_EXPECT_SECRET="${AUTH_SECRET_NAME}"
