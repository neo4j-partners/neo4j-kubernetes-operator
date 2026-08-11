#!/usr/bin/env bash
# Negative: BYO auth Secret that is mountable but not delegated to this CR (ADD-01). Proves the
# delegation label is load-bearing on its own — the mountable opt-in alone must not authorize an
# auth Secret the operator reads to dial admin Bolt.

export NEO4J_CASE_NAME=standalone-auth-secretref-undelegated
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-dev-undelegated}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false

# The CR is applied successfully; the refusal happens at reconcile, not at admission.
export E2E_ASSERT_NEO4J_READY=false

export AUTH_SECRET_CREATE=true
export AUTH_SECRET_NAME="${AUTH_SECRET_NAME:-neo4j-auth}"
export AUTH_KNOWN_PASSWORD="${AUTH_KNOWN_PASSWORD:-ClientPass123}"
export AUTH_SECRET_LABELS=mountable

# assert/reconcile-error expectation — reason from src/internal/status/oracle.go.
export EXPECT_REASON=SecretNotDelegated
