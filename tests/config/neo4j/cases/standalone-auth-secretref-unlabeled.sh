#!/usr/bin/env bash
# Negative: BYO auth Secret without the namespace-owner opt-in label (NEO-005). The operator
# must refuse it before rendering any operand and report Error/SecretNotMountable.

export NEO4J_CASE_NAME=standalone-auth-secretref-unlabeled
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-dev-unlabeled}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false

# The CR is applied successfully; the refusal happens at reconcile, not at admission.
export E2E_ASSERT_NEO4J_READY=false

export AUTH_SECRET_CREATE=true
export AUTH_SECRET_NAME="${AUTH_SECRET_NAME:-neo4j-auth}"
export AUTH_KNOWN_PASSWORD="${AUTH_KNOWN_PASSWORD:-ClientPass123}"
export AUTH_SECRET_LABELS=none

# assert/reconcile-error expectation — reason from src/internal/status/oracle.go.
export EXPECT_REASON=SecretNotMountable
