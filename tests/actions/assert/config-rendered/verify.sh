#!/usr/bin/env bash
# assert/config-rendered — AC-NEO-CONFIG-001: a spec.config.neo4j setting is passed
# through verbatim into the rendered neo4j.conf ConfigMap (one key per setting).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

# Key/value expected in the ConfigMap — tied to tests/fixtures/neo4j-config-valid.yaml.
CONFIG_KEY="${EXPECT_CONFIG_KEY:-db.transaction.timeout}"
CONFIG_VALUE="${EXPECT_CONFIG_VALUE:-42s}"
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
key_esc="${CONFIG_KEY//./\\.}"
got="$(kubectl get configmap "${NEO4J_CONFIGMAP}" -n "${NEO4J_NAMESPACE}" \
  -o "jsonpath={.data.${key_esc}}" 2>/dev/null || true)"

[[ -n "${got}" ]] \
  || die "config key '${CONFIG_KEY}' missing from ConfigMap ${NEO4J_CONFIGMAP}"
[[ "${got}" == "${CONFIG_VALUE}" ]] \
  || die "config key '${CONFIG_KEY}' = '${got}', expected '${CONFIG_VALUE}'"

log "spec.config.neo4j['${CONFIG_KEY}'] rendered as '${got}' in ${NEO4J_CONFIGMAP} (AC-NEO-CONFIG-001)"
