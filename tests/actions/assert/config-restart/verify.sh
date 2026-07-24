#!/usr/bin/env bash
# assert/config-restart — NEO-2-010 / NEO-3-010-RSTR-01 (AC-NEO-CONFIG-CHANGE):
# changing spec.config on a running Neo4j is detected and applied through a
# controlled restart. Two observable effects:
#   1. the rendered neo4j.conf ConfigMap gains the new setting, and
#   2. the StatefulSet's pod template is revised (new updateRevision, i.e. a
#      rolling restart was triggered) rather than silently ignored.
#
# Runs after standalone-ready, so operands already exist. The setting used is a
# valid, overridable one (db.transaction.timeout) so Neo4j does not reject it.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_CONFIGMAP,
#         E2E_ASSERT_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
STS="statefulset/${NEO4J_STS_NAME}"
CONFIG_KEY="db.transaction.timeout"
CONFIG_VALUE="33s"
TIMEOUT_SECS="${E2E_ASSERT_TIMEOUT%s}"
TIMEOUT_SECS="${TIMEOUT_SECS:-300}"

# jsonpath treats dots as path separators — escape them to read the literal key.
key_esc="${CONFIG_KEY//./\\.}"

# Ensure operands are settled before we snapshot the baseline.
kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${E2E_ASSERT_TIMEOUT:-300s}" >/dev/null 2>&1 \
  || die "${NEO4J_RESOURCE} not Installed before config change"

# Baseline: the StatefulSet's current pod-template revision. A controlled restart
# rolls the template, so this hash must change once the config is applied.
rev_before="$(kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.updateRevision}' 2>/dev/null || true)"
[[ -n "${rev_before}" ]] || die "could not read updateRevision of ${STS}"
log "Baseline ${STS} updateRevision=${rev_before}"

# Apply the config change via a strategic merge patch on spec.config.neo4j.
log "Patching ${NEO4J_RESOURCE}: spec.config.neo4j['${CONFIG_KEY}']=${CONFIG_VALUE}"
kubectl patch "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" --type merge \
  -p "{\"spec\":{\"config\":{\"neo4j\":{\"${CONFIG_KEY}\":\"${CONFIG_VALUE}\"}}}}" \
  >/dev/null

# 1. Wait for the operator to render the new value into the ConfigMap.
log "Waiting up to ${TIMEOUT_SECS}s for ${NEO4J_CONFIGMAP} to carry the new setting"
deadline=$((SECONDS + TIMEOUT_SECS))
got=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  got="$(kubectl get configmap "${NEO4J_CONFIGMAP}" -n "${NEO4J_NAMESPACE}" \
    -o "jsonpath={.data.${key_esc}}" 2>/dev/null || true)"
  [[ "${got}" == "${CONFIG_VALUE}" ]] && break
  sleep 3
done
[[ "${got}" == "${CONFIG_VALUE}" ]] \
  || die "ConfigMap ${NEO4J_CONFIGMAP}['${CONFIG_KEY}']='${got:-none}' after ${TIMEOUT_SECS}s, expected '${CONFIG_VALUE}'"
log "ConfigMap updated: ${CONFIG_KEY}=${got}"

# 2. Wait for the StatefulSet to roll its pod template (the controlled restart).
log "Waiting up to ${TIMEOUT_SECS}s for ${STS} pod template to be revised"
deadline=$((SECONDS + TIMEOUT_SECS))
rev_after="${rev_before}"
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  rev_after="$(kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.status.updateRevision}' 2>/dev/null || true)"
  [[ -n "${rev_after}" && "${rev_after}" != "${rev_before}" ]] && break
  sleep 3
done
[[ "${rev_after}" != "${rev_before}" ]] \
  || die "config changed but ${STS} pod template was not revised (updateRevision stayed ${rev_before}) — no controlled restart"

log "Config change triggered a controlled restart: ${STS} updateRevision ${rev_before} -> ${rev_after} (NEO-2-010)"
