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
#   4. reformed       — ClusterFormed is True again once the roll settles. Ready only speaks for
#                       member health, so a roll that leaves the Neo4j cluster unformed while every
#                       pod is Ready satisfies 1-3 and is still a broken cluster.
#
# ClusterFormed is deliberately NOT required to stay True during the roll: with
# topology.minimumMembers=3 on a 3-primary pool, taking one member down drops enabledPrimaries
# below the minimum, so the operator honestly reports False/WaitingQuorum, and False/BoltUnavailable
# while it dials the member that is restarting. Those are converging states. What may never appear
# is a refusal — the error-severity reasons of ClusterFormed (src/internal/oracle/catalog.go) —
# because that is the operator giving up rather than waiting.
#
# The quorum sampler is best-effort by nature (it polls; a roll faster than the poll could miss the
# dip) — ponytail: sampling race ceiling. It is reliable here because Neo4j pods take tens of seconds
# to restart and become Ready, a window a ~2s poll observes many times; the sampler only ever FAILS
# on a genuine quorum violation (readyReplicas < members-1), never on a missed dip. The same poll
# keeps the trail of ClusterFormed reasons it saw, which is the useful part of a failure dump.
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
FORMED_TIMEOUT_SECS="${CLUSTER_FORMED_TIMEOUT:-300}"

# jsonpath treats dots as path separators — escape them to read the literal key.
key_esc="${CONFIG_KEY//./\\.}"

formed_field() {  # formed_field <field>
  kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
    -o "jsonpath={.status.conditions[?(@.type=='ClusterFormed')].$1}" 2>/dev/null || true
}

# What counts as a refusal is read from the operator's own catalog instead of being copied here:
# every error-severity reason of ClusterFormed means it gave up rather than waited, so a reason added
# to formation later is watched without editing this assert (tests/lib/oracle.sh, generated).
REFUSAL_REASONS=""
for reason in $(oracle_reasons_for ClusterFormed); do
  [[ "$(oracle_severity "${reason}")" == "error" ]] \
    && REFUSAL_REASONS="${REFUSAL_REASONS}${reason} "
done
# An empty set would leave a gate that cannot fail, which is worse than no gate at all: it reads
# green. Only a broken projection can produce it, so say so instead of passing.
[[ -n "${REFUSAL_REASONS}" ]] \
  || die "no error-severity reason is catalogued for ClusterFormed — refusing to run a gate that cannot fail; check src/internal/oracle/catalog.go and run 'make errors'"
log "Refusals watched during the roll: ${REFUSAL_REASONS}"

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

# A single member makes "one at a time" vacuously true, so the case would report a pass while
# proving nothing — fail on the fixture instead of on the property.
[[ "${MEMBERS}" -ge 2 ]] \
  || die "CLUSTER_EXPECTED_MEMBERS=${MEMBERS} — RSTR-02 needs a multi-member pool to mean anything"
sts_replicas="$(kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.replicas}' 2>/dev/null || true)"
[[ "${sts_replicas:-0}" == "${MEMBERS}" ]] \
  || die "${STS} declares ${sts_replicas:-0} replica(s) but the case expects ${MEMBERS} — the quorum floor would be wrong"

# ClusterFormed must hold before the change, otherwise "it reformed after" is unprovable.
[[ "$(formed_field status)" == "True" ]] \
  || die "ClusterFormed=$(formed_field status)/$(formed_field reason) before the config change — cannot assert the cluster reforms after the roll"

rev_before="$(kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.updateRevision}' 2>/dev/null || true)"
[[ -n "${rev_before}" ]] || die "could not read updateRevision of ${STS}"
log "Baseline ${STS} updateRevision=${rev_before}, members=${MEMBERS}, quorum floor=${QUORUM_FLOOR} ready, ClusterFormed=True"

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
reasons_seen=""
deadline=$((SECONDS + TIMEOUT_SECS))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  ready="$(sts_field .status.readyReplicas)"; ready="${ready:-0}"
  updated="$(sts_field .status.updatedReplicas)"; updated="${updated:-0}"
  cur_rev="$(sts_field .status.currentRevision)"
  formed_reason="$(formed_field reason)"
  [[ "${ready}" -lt "${min_ready}" ]] && min_ready="${ready}"
  # Keep each distinct reason once, in the order observed: it is what explains a failure below.
  if [[ -n "${formed_reason}" && " ${reasons_seen} " != *" ${formed_reason} "* ]]; then
    reasons_seen="${reasons_seen}${formed_reason} "
  fi
  for refusal in ${REFUSAL_REASONS}; do
    [[ "${formed_reason}" == "${refusal}" ]] \
      && die "[roll] ClusterFormed reported ${formed_reason} during the roll — that is the operator refusing, not converging (reasons seen: ${reasons_seen})"
  done
  [[ "${ready}" -lt "${QUORUM_FLOOR}" ]] \
    && die "[roll] quorum violated during the roll: readyReplicas=${ready} < floor ${QUORUM_FLOOR} — more than one member restarted at once (ClusterFormed reasons seen: ${reasons_seen:-none})"
  # Rolled fully: new revision current, every replica updated and Ready.
  [[ "${cur_rev}" == "${rev_after}" && "${updated}" -ge "${MEMBERS}" && "${ready}" -ge "${MEMBERS}" ]] && break
  sleep 2
done
[[ "${cur_rev:-}" == "${rev_after}" ]] \
  || die "[roll] ${STS} did not finish rolling to ${rev_after} within ${TIMEOUT_SECS}s (currentRevision=${cur_rev:-none}, ClusterFormed reasons seen: ${reasons_seen:-none})"
log "[roll] roll complete, quorum held throughout (min readyReplicas observed=${min_ready}, floor=${QUORUM_FLOOR}; ClusterFormed reasons seen: ${reasons_seen:-none})"

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

# 4. reformed — the members are back, so the operator must consider the cluster formed again. The
#    converging reasons above are allowed while the roll happens; still holding one afterwards is a
#    cluster that came back as separate servers rather than as a cluster.
log "[reformed] Waiting up to ${FORMED_TIMEOUT_SECS}s for ClusterFormed to return to True"
deadline=$((SECONDS + FORMED_TIMEOUT_SECS))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  [[ "$(formed_field status)" == "True" ]] && break
  sleep 5
done
[[ "$(formed_field status)" == "True" ]] \
  || die "[reformed] ClusterFormed=$(formed_field status)/$(formed_field reason) after the roll settled — the cluster did not reform (reasons seen during the roll: ${reasons_seen:-none})"
log "[reformed] ClusterFormed=True/$(formed_field reason)"

log "Cluster config change rolled one-by-one with quorum preserved, reached all ${MEMBERS} members and reformed: ${CONFIG_KEY}=${CONFIG_VALUE} (NEO-3-010-RSTR-02)"
