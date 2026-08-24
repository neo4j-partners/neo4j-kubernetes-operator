#!/usr/bin/env bash
# assert/cluster-config-restart — NEO-3-010-RSTR-02: changing spec.config on a running CLUSTER is
# applied to every member through a rolling restart that preserves quorum. This is the cluster
# counterpart of assert/config-restart (RSTR-01, Standalone). There is no cluster-specific restart
# machinery in the operator — the pool StatefulSet has the default RollingUpdate strategy and the
# neo4j.com/config-checksum pod-template annotation is what rolls it, exactly as for Standalone. So
# the cluster-specific content to prove is the emergent behaviour on a 3-replica pool:
#
#   1. render        — the rendered neo4j.conf ConfigMap gains the new setting.
#   2. rolling roll   — the <cr>-primary pool StatefulSet revises its pod template (updateRevision
#                       changes) and, WHILE it rolls, never drops below quorum: at most one member is
#                       un-Ready at a time (readyReplicas >= members-1). This is the "one-by-one"
#                       guarantee — if the pool restarted every member at once, a 3-primary Raft
#                       cluster would lose quorum and the roll would be unsafe.
#   3. converged      — after the roll the CR is Ready again AND every member reports the new value
#                       via SHOW SETTINGS, proving the change reached all three, not just pod-0.
#
# The quorum sampler is best-effort by nature (it polls; a roll faster than the poll could miss the
# dip) — ponytail: sampling race ceiling. It is reliable here because Neo4j pods take tens of seconds
# to restart and become Ready, a window a ~2s poll observes many times; the sampler only ever FAILS
# on a genuine quorum violation (readyReplicas < members-1), never on a missed dip.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME (=<cr>-primary), NEO4J_CONFIGMAP,
#         NEO4J_AUTH_SECRET, CLUSTER_EXPECTED_MEMBERS, E2E_CLUSTER_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
STS="statefulset/${NEO4J_STS_NAME}"
MEMBERS="${CLUSTER_EXPECTED_MEMBERS:?CLUSTER_EXPECTED_MEMBERS not set}"
QUORUM_FLOOR=$((MEMBERS - 1))   # at most one member un-Ready at a time => readyReplicas >= members-1
CONFIG_KEY="db.transaction.timeout"
CONFIG_VALUE="37s"
TIMEOUT_SECS="${E2E_CLUSTER_TIMEOUT:-600}"

# jsonpath treats dots as path separators — escape them to read the literal key.
key_esc="${CONFIG_KEY//./\\.}"

# Baseline must be a formed, Ready cluster so the change hits live members and the runtime check is
# meaningful.
log "Waiting up to ${TIMEOUT_SECS}s for ${NEO4J_RESOURCE} Ready before changing config"
kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT_SECS}s" >/dev/null 2>&1 \
  || die "${NEO4J_RESOURCE} not Installed before config change"
if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT_SECS}s" >/dev/null 2>&1; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} did not become Ready before config change"
fi

rev_before="$(kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.updateRevision}' 2>/dev/null || true)"
[[ -n "${rev_before}" ]] || die "could not read updateRevision of ${STS}"
log "Baseline ${STS} updateRevision=${rev_before}, members=${MEMBERS}, quorum floor=${QUORUM_FLOOR} ready"

# Apply the config change via a strategic merge patch on spec.config.neo4j.
log "Patching ${NEO4J_RESOURCE}: spec.config.neo4j['${CONFIG_KEY}']=${CONFIG_VALUE}"
kubectl patch "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" --type merge \
  -p "{\"spec\":{\"config\":{\"neo4j\":{\"${CONFIG_KEY}\":\"${CONFIG_VALUE}\"}}}}" \
  >/dev/null

# 1. render — the operator must render the new value into the pool ConfigMap.
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

# 2a. rolling roll — wait for the pod template to be revised (the restart was triggered).
log "[roll] Waiting up to ${TIMEOUT_SECS}s for ${STS} pod template to be revised"
deadline=$((SECONDS + TIMEOUT_SECS))
rev_after="${rev_before}"
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  rev_after="$(kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.status.updateRevision}' 2>/dev/null || true)"
  [[ -n "${rev_after}" && "${rev_after}" != "${rev_before}" ]] && break
  sleep 2
done
[[ "${rev_after}" != "${rev_before}" ]] \
  || die "[roll] config changed but ${STS} pod template was not revised (updateRevision stayed ${rev_before}) — no rolling restart"
log "[roll] pod template revised: updateRevision ${rev_before} -> ${rev_after}; sampling readyReplicas through the roll"

# 2b. Sample readyReplicas while the roll completes; quorum must hold the whole time.
#     Complete = the new revision is current AND all members are updated and Ready again.
sts_field() { kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" -o jsonpath="{$1}" 2>/dev/null || true; }
min_ready="${MEMBERS}"
deadline=$((SECONDS + TIMEOUT_SECS))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  ready="$(sts_field .status.readyReplicas)"; ready="${ready:-0}"
  updated="$(sts_field .status.updatedReplicas)"; updated="${updated:-0}"
  cur_rev="$(sts_field .status.currentRevision)"
  [[ "${ready}" -lt "${min_ready}" ]] && min_ready="${ready}"
  [[ "${ready}" -lt "${QUORUM_FLOOR}" ]] \
    && die "[roll] quorum violated during the roll: readyReplicas=${ready} < floor ${QUORUM_FLOOR} — more than one member restarted at once"
  # Rolled fully: new revision current, every replica updated and Ready.
  [[ "${cur_rev}" == "${rev_after}" && "${updated}" -ge "${MEMBERS}" && "${ready}" -ge "${MEMBERS}" ]] && break
  sleep 2
done
[[ "${cur_rev:-}" == "${rev_after}" ]] \
  || die "[roll] ${STS} did not finish rolling to ${rev_after} within ${TIMEOUT_SECS}s (currentRevision=${cur_rev:-none})"
log "[roll] roll complete, quorum held throughout (min readyReplicas observed=${min_ready}, floor=${QUORUM_FLOOR})"

# 3. converged — the CR is Ready again and EVERY member reports the new value.
log "[converged] Waiting for ${NEO4J_RESOURCE} Ready again after the roll"
if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT_SECS}s" >/dev/null 2>&1; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} did not return to Ready after the config-change roll"
fi

password="$(neo4j_password)"
MEMBER_POD=""
conn_exec_member() { kubectl exec -n "${NEO4J_NAMESPACE}" "${MEMBER_POD}" -c neo4j -- bash -c "$1"; }
CONN_EXEC_FN=conn_exec_member
for ((i = 0; i < MEMBERS; i++)); do
  MEMBER_POD="${NEO4J_STS_NAME}-${i}"
  log "[converged] checking ${MEMBER_POD} reports ${CONFIG_KEY}=${CONFIG_VALUE}"
  conn_assert_setting localhost "${password}" "${CONFIG_KEY}" "${CONFIG_VALUE}" "member-${i}"
done

log "Cluster config change rolled one-by-one with quorum preserved and reached all ${MEMBERS} members: ${CONFIG_KEY}=${CONFIG_VALUE} (NEO-3-010-RSTR-02)"
