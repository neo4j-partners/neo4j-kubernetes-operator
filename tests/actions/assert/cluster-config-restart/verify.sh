#!/usr/bin/env bash
# assert/cluster-config-restart — NEO-2-010 / NEO-3-010-RSTR-02 (AC-NEO-CONFIG-CHANGE +
# AC-NEO-CLUSTER): a config change on a Cluster restarts members ONE AT A TIME, and the
# cluster is formed again when the roll finishes.
#
# The standalone sibling (assert/config-restart, RSTR-01) already proves render -> rollout ->
# runtime. With a single member "rolling" is trivially safe, so it cannot show the property
# that matters here: a mass restart would drop quorum and take the DBMS down.
#
# Why sampling, not the spec. The operator sets PodManagementPolicy: Parallel on the pool
# StatefulSet (render/workload/statefulset.go) — pods are created and deleted in parallel
# when scaling — and leaves updateStrategy at its Kubernetes default, RollingUpdate, which
# still updates one pod at a time. Those two pull in opposite directions, and a change that
# made updates parallel too would drop quorum without any render test noticing.
#
# What is NOT asserted, deliberately: that ClusterFormed stays True throughout. It does not,
# and it should not. With minimumMembers=3 on a 3-primary pool, taking one member down puts
# enabledPrimaries below the minimum, so the operator correctly reports
# ClusterFormed=False/WaitingQuorum; it also reports False/BoltUnavailable while dialling the
# member that is restarting, and False/AligningTopology or EnablingServer as it converges.
# Those are honest intermediate states, not damage. Asserting "never False" fails a healthy
# roll — measured, not assumed (the first draft of this assert did exactly that).
#
# So the contract is: availability is preserved DURING the roll (readyReplicas never falls
# below N-1), the cluster is formed AFTER it, and no reason indicating a real refusal —
# UnsupportedSinglePrimary / UnsupportedSystemScaleUp / ShowServersFailed — appears at all.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME (pool STS), NEO4J_AUTH_SECRET,
#         CLUSTER_ROLL_TIMEOUT
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
CONFIG_VALUE="51s"
ROLL_TIMEOUT="${CLUSTER_ROLL_TIMEOUT:-900}"

# Reasons that mean the operator refused or failed, as opposed to converging.
# Source: src/internal/domain/formation/reconcile.go.
FATAL_REASONS="UnsupportedSinglePrimary UnsupportedSystemScaleUp ShowServersFailed"

sts_field() { kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" -o "jsonpath={$1}" 2>/dev/null || true; }
formed_field() {
  kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
    -o "jsonpath={.status.conditions[?(@.type=='ClusterFormed')].$1}" 2>/dev/null || true
}

log "Waiting for ${NEO4J_RESOURCE} Ready before changing config"
kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  --timeout=600s >/dev/null 2>&1 \
  || die "${NEO4J_RESOURCE} was not Ready before the config change"
[[ "$(formed_field status)" == "True" ]] \
  || die "ClusterFormed is not True before the config change — cannot assert it reformed after"

replicas="$(sts_field .spec.replicas)"
[[ "${replicas:-0}" -ge 2 ]] \
  || die "${STS} has ${replicas:-0} replica(s) — RSTR-02 needs a multi-member pool to mean anything"
rev_before="$(sts_field .status.updateRevision)"
log "Baseline: ${replicas} members, updateRevision=${rev_before}, ClusterFormed=True"

log "Patching ${NEO4J_RESOURCE}: spec.config.neo4j['${CONFIG_KEY}']=${CONFIG_VALUE}"
kubectl patch "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" --type merge \
  -p "{\"spec\":{\"config\":{\"neo4j\":{\"${CONFIG_KEY}\":\"${CONFIG_VALUE}\"}}}}" >/dev/null

log "Sampling the rollout (at most one member may be unavailable at a time)"
min_ready="${replicas}"
saw_dip=0
fatal_seen=""
reasons_seen=""
rev_after="${rev_before}"
deadline=$((SECONDS + ROLL_TIMEOUT))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  ready="$(sts_field .status.readyReplicas)"; ready="${ready:-0}"
  updated="$(sts_field .status.updatedReplicas)"; updated="${updated:-0}"
  cur_rev="$(sts_field .status.currentRevision)"
  rev_after="$(sts_field .status.updateRevision)"
  reason="$(formed_field reason)"

  [[ "${ready}" -lt "${min_ready}" ]] && min_ready="${ready}"
  [[ "${ready}" -lt "${replicas}" ]] && saw_dip=1
  # Keep a trail of the intermediate reasons — the useful thing in a failure dump.
  if [[ -n "${reason}" && " ${reasons_seen} " != *" ${reason} "* ]]; then
    reasons_seen="${reasons_seen}${reason} "
  fi
  for f in ${FATAL_REASONS}; do
    [[ "${reason}" == "${f}" ]] && fatal_seen="${reason}"
  done

  if [[ "${rev_after}" != "${rev_before}" && "${cur_rev}" == "${rev_after}" \
        && "${updated}" == "${replicas}" && "${ready}" == "${replicas}" ]]; then
    break
  fi
  sleep 2
done

[[ "${rev_after}" != "${rev_before}" ]] \
  || die "config changed but ${STS} pod template was never revised (updateRevision stayed ${rev_before}) — no restart"
[[ "$(sts_field .status.currentRevision)" == "${rev_after}" && "$(sts_field .status.readyReplicas)" == "${replicas}" ]] \
  || die "${STS} did not finish rolling to ${rev_after} within ${ROLL_TIMEOUT}s (ready=$(sts_field .status.readyReplicas)/${replicas}, current=$(sts_field .status.currentRevision)); ClusterFormed reasons seen: ${reasons_seen:-none}"

# The property under test: members went down one at a time, never two.
[[ "${min_ready}" -ge $((replicas - 1)) ]] \
  || die "at most one member may restart at a time, but readyReplicas fell to ${min_ready}/${replicas} — the roll was not serial (quorum at risk); ClusterFormed reasons seen: ${reasons_seen:-none}"

[[ -z "${fatal_seen}" ]] \
  || die "ClusterFormed reported ${fatal_seen} during the roll — that is a refusal, not a converging state"

[[ "${saw_dip}" -eq 1 ]] \
  || log "NOTE: never sampled a member as unready — the roll completed faster than expected"

# The cluster must be formed again once the roll settles. Converging reasons are fine while
# it happens; staying unformed afterwards is not.
log "Waiting for ClusterFormed to return to True (reasons seen during the roll: ${reasons_seen:-none})"
deadline=$((SECONDS + 300))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  [[ "$(formed_field status)" == "True" ]] && break
  sleep 5
done
[[ "$(formed_field status)" == "True" ]] \
  || die "ClusterFormed is $(formed_field status)/$(formed_field reason) after the roll settled — the cluster did not reform"

log "Rolled ${replicas} members to ${rev_after}: min readyReplicas=${min_ready}/${replicas}, reformed after"

conn_exec_serverpod() { kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"; }
CONN_EXEC_FN=conn_exec_serverpod
password="$(neo4j_password)"
conn_assert_setting localhost "${password}" "${CONFIG_KEY}" "${CONFIG_VALUE}" "cluster-config-restart"

log "Cluster config change applied by a one-by-one rolling restart, cluster reformed (NEO-3-010-RSTR-02)"
