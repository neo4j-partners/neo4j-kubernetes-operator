#!/usr/bin/env bash
# Negative: a plugin licence Secret without the namespace-owner opt-in label (NEO-005). The
# operator must refuse the CR before rendering any operand and report Error/SecretNotMountable.
#
# The Secret itself ships inside tests/fixtures/neo4j-plugins-unlabeled.yaml, so unlike the
# auth-secret cases there is no AUTH_SECRET_* block here — this fragment exists only to export
# EXPECT_REASON, which a bare-fixture case has no way to set.

export NEO4J_CASE_NAME=standalone-plugin-license-unlabeled
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-plg-nolbl}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false

# The CR is applied successfully; the refusal happens at reconcile, not at admission.
export E2E_ASSERT_NEO4J_READY=false

# assert/reconcile-error expectation — reason from src/internal/status/oracle.go.
export EXPECT_REASON=SecretNotMountable
