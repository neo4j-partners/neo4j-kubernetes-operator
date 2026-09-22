#!/usr/bin/env bash
# assert/config-apoc-rendered — NEO-3-003-APOC-01: apoc.* keys from spec.config.apoc are
# rendered into the dedicated <cr>-apoc-config ConfigMap (key apoc.conf) when APOC is
# assigned via spec.plugins. This checks the rendered config file, not runtime procedures
# (that belongs to the feature-plugins suite).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

# Standalone pool ConfigMap name; override via NEO4J_APOC_CONFIGMAP for other pools.
APOC_CONFIGMAP="${NEO4J_APOC_CONFIGMAP:-${NEO4J_CR_NAME}-apoc-config}"
# Tied to tests/fixtures/neo4j-config-apoc.yaml.
EXPECT_APOC_ENTRY="${EXPECT_APOC_ENTRY:-apoc.export.file.enabled=true}"
TIMEOUT="${E2E_ASSERT_TIMEOUT:-300s}"
NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"

log "Waiting for ${NEO4J_RESOURCE} Installed condition (operator reconciled operands)"
if ! kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT}" 2>/dev/null; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} Installed condition not True within ${TIMEOUT}"
fi

kubectl get configmap "${APOC_CONFIGMAP}" -n "${NEO4J_NAMESPACE}" >/dev/null \
  || die "APOC ConfigMap ${APOC_CONFIGMAP} not found (is apoc assigned via spec.plugins?)"

# apoc.conf is a single blob under the data key 'apoc.conf' (escape the dot for jsonpath).
got="$(kubectl get configmap "${APOC_CONFIGMAP}" -n "${NEO4J_NAMESPACE}" \
  -o 'jsonpath={.data.apoc\.conf}' 2>/dev/null || true)"

[[ -n "${got}" ]] \
  || die "apoc.conf missing from ConfigMap ${APOC_CONFIGMAP}"
grep -qF -- "${EXPECT_APOC_ENTRY}" <<<"${got}" \
  || die "apoc.conf in ${APOC_CONFIGMAP} does not contain '${EXPECT_APOC_ENTRY}'; got: ${got}"

log "spec.config.apoc rendered '${EXPECT_APOC_ENTRY}' into ${APOC_CONFIGMAP} apoc.conf (NEO-3-003-APOC-01)"
