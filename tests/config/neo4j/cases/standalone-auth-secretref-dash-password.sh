#!/usr/bin/env bash
# Negative: fully labelled BYO auth Secret whose password starts with "-". The Neo4j image
# entrypoint passes it as a positional argument to `neo4j-admin dbms set-initial-password`
# without a "--" separator, so the CLI reads it as an option and the container crash-loops with
# the cause buried in its log. The operator must refuse the Secret up front instead.

export NEO4J_CASE_NAME=standalone-auth-secretref-dash-password
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-dev-dashpw}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-10Gi}"
export NEO4J_USE_STORAGE_CLASS=false

# The CR is applied successfully; the refusal happens at reconcile, not at admission.
export E2E_ASSERT_NEO4J_READY=false

export AUTH_SECRET_CREATE=true
export AUTH_SECRET_NAME="${AUTH_SECRET_NAME:-neo4j-auth}"
# Set unconditionally: the leading "-" is the whole point of the case, an inherited or
# caller-provided password would make it pass for the wrong reason.
export AUTH_KNOWN_PASSWORD='-DashLeadingPass1'
# Labels are correct here — the value alone must be enough to refuse.
export AUTH_SECRET_LABELS=full

# assert/reconcile-error expectation — reason from src/internal/status/oracle.go.
export EXPECT_REASON=AuthSecretInvalid
