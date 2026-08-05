#!/usr/bin/env bash
# assert/config-restart — NEO-2-010 / NEO-3-010-RSTR-01 (AC-NEO-CONFIG-CHANGE):
# changing spec.config on a running Neo4j is detected and applied end-to-end.
# Three observable levels, checked in order:
#   1. render  — the rendered neo4j.conf ConfigMap gains the new setting,
#   2. rollout — the StatefulSet pod template is revised (a controlled restart was
#                triggered) rather than silently ignored, and
#   3. runtime — after the restart, SHOW SETTINGS over bolt reports the new effective
#                value (the change actually reached the running server).
#
# Deploys via the pipeline (deploy/neo4j), so operands already exist. The setting used is
# a valid, overridable one (db.transaction.timeout) so Neo4j does not reject it.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_CONFIGMAP,
#         NEO4J_AUTH_SECRET, E2E_ASSERT_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
STS="statefulset/${NEO4J_STS_NAME}"
POD="${NEO4J_STS_NAME}-0"
CONFIG_KEY="db.transaction.timeout"
CONFIG_VALUE="33s"
TIMEOUT_SECS="${E2E_ASSERT_TIMEOUT%s}"
TIMEOUT_SECS="${TIMEOUT_SECS:-300}"

# jsonpath treats dots as path separators — escape them to read the literal key.
key_esc="${CONFIG_KEY//./\\.}"

# Baseline must be a *running* Neo4j so the change is applied to a live server and the
# runtime check below is meaningful.
log "Waiting for ${NEO4J_RESOURCE} Ready before changing config (change must hit a live server)"
kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${E2E_ASSERT_TIMEOUT:-300s}" >/dev/null 2>&1 \
  || die "${NEO4J_RESOURCE} not Installed before config change"
if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout=600s >/dev/null 2>&1; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} did not become Ready before config change"
fi

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

# 1. render — wait for the operator to render the new value into the ConfigMap.
log "[render] Waiting up to ${TIMEOUT_SECS}s for ${NEO4J_CONFIGMAP} to carry the new setting"
deadline=$((SECONDS + TIMEOUT_SECS))
got=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  got="$(kubectl get configmap "${NEO4J_CONFIGMAP}" -n "${NEO4J_NAMESPACE}" \
    -o "jsonpath={.data.${key_esc}}" 2>/dev/null || true)"
  [[ "${got}" == "${CONFIG_VALUE}" ]] && break
  sleep 3
done
[[ "${got}" == "${CONFIG_VALUE}" ]] \
  || die "[render] ConfigMap ${NEO4J_CONFIGMAP}['${CONFIG_KEY}']='${got:-none}' after ${TIMEOUT_SECS}s, expected '${CONFIG_VALUE}'"
log "[render] ConfigMap updated: ${CONFIG_KEY}=${got}"

# 2. rollout — wait for the StatefulSet to roll its pod template (the controlled restart).
log "[rollout] Waiting up to ${TIMEOUT_SECS}s for ${STS} pod template to be revised"
deadline=$((SECONDS + TIMEOUT_SECS))
rev_after="${rev_before}"
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  rev_after="$(kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.status.updateRevision}' 2>/dev/null || true)"
  [[ -n "${rev_after}" && "${rev_after}" != "${rev_before}" ]] && break
  sleep 3
done
[[ "${rev_after}" != "${rev_before}" ]] \
  || die "[rollout] config changed but ${STS} pod template was not revised (updateRevision stayed ${rev_before}) — no controlled restart"
log "[rollout] Controlled restart: ${STS} updateRevision ${rev_before} -> ${rev_after}"

# 3. runtime — after the restart, the running server must report the new value.
log "[runtime] Waiting for ${NEO4J_RESOURCE} Ready again after the restart"
if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout=600s >/dev/null 2>&1; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} did not return to Ready after the config-change restart"
fi

conn_exec_serverpod() {
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"
}
CONN_EXEC_FN=conn_exec_serverpod
password="$(neo4j_password)"
conn_assert_setting localhost "${password}" "${CONFIG_KEY}" "${CONFIG_VALUE}" "config-restart"

log "Config change applied end-to-end (render + rollout + runtime): ${CONFIG_KEY}=${CONFIG_VALUE} (NEO-2-010)"
