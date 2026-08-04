#!/usr/bin/env bash
# assert/config-jvm-rendered — NEO-3-003-JVM-02: spec.config.jvm.additionalArguments are
# rendered into the neo4j.conf ConfigMap under the single key server.jvm.additional.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

# Tied to tests/fixtures/neo4j-config-jvm.yaml.
JVM_KEY="server.jvm.additional"
EXPECT_JVM_ARG="${EXPECT_JVM_ARG:--XX:+ExitOnOutOfMemoryError}"
TIMEOUT="${E2E_ASSERT_TIMEOUT:-300s}"
NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"

log "Waiting for ${NEO4J_RESOURCE} Installed condition (operator reconciled operands)"
if ! kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT}" 2>/dev/null; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} Installed condition not True within ${TIMEOUT}"
fi

kubectl get configmap "${NEO4J_CONFIGMAP}" -n "${NEO4J_NAMESPACE}" >/dev/null \
  || die "ConfigMap ${NEO4J_CONFIGMAP} not found"

# jsonpath treats dots as path separators — escape them to read the literal key.
key_esc="${JVM_KEY//./\\.}"
got="$(kubectl get configmap "${NEO4J_CONFIGMAP}" -n "${NEO4J_NAMESPACE}" \
  -o "jsonpath={.data.${key_esc}}" 2>/dev/null || true)"

[[ -n "${got}" ]] \
  || die "JVM key '${JVM_KEY}' missing from ConfigMap ${NEO4J_CONFIGMAP}"
grep -qF -- "${EXPECT_JVM_ARG}" <<<"${got}" \
  || die "JVM key '${JVM_KEY}' = '${got}', expected to contain '${EXPECT_JVM_ARG}'"

log "spec.config.jvm.additionalArguments rendered into ${JVM_KEY} ('${got}') in ${NEO4J_CONFIGMAP} (NEO-3-003-JVM-02)"
