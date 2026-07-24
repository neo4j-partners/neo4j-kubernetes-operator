#!/usr/bin/env bash
# Auth via operator-generated password (auth.generatePassword: true).
# Needs a running Neo4j to prove the generated password authenticates over bolt.

export NEO4J_CASE_NAME=standalone-auth-generated
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-dev-gen}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false

export E2E_ASSERT_NEO4J_READY=true

# assert/credentials expectations.
export AUTH_SECRET_CREATE=false
export CRED_EXPECT_GENERATED=true
