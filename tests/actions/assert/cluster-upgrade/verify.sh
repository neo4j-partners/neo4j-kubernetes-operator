#!/usr/bin/env bash
# assert/cluster-upgrade — ADR-017: changing spec.version on a running cluster is refused when Neo4j
# does not permit it, and otherwise rolled out in the order Neo4j requires, without losing data.
#
# The CR is deployed by the pipeline at NEO4J_VERSION_UPGRADE_FROM; this rolls it to NEO4J_UPGRADE_TO.
#
# Five properties, in the order they can first be observed:
#
#   1. refused    — a downgrade is rejected. Neo4j supports none, so the operator must not attempt
#                   one, and the running version must be untouched afterwards. Proved here rather
#                   than in operator-admission because it needs a *running* CR: the refusal compares
#                   spec.version against status.version, which only exists once something is up.
#   2. ordered    — the read pool reaches the new image and finishes before the primary pool's pod
#                   template changes. This is the property with no other coverage anywhere: Neo4j
#                   requires system-database secondaries to be upgraded before the system primary,
#                   and the read pool is rendered with system_database_mode=SECONDARY.
#   3. quorum     — while the primaries roll, at most one is un-Ready, and ClusterFormed never
#                   reports an error-severity reason. Same shape as assert/cluster-config-restart,
#                   because it is the same StatefulSet machinery underneath.
#   4. reported   — status.upgrade names the target while the roll is in flight, and on completion
#                   status.version reports the new version and lastUpgradeTime is stamped.
#   5. survived   — a row written before the upgrade is readable after it. The whole point of a
#                   rolling upgrade is that the data is still there.
#
# Ordering is sampled, like the quorum check in cluster-config-restart: a poll can miss a dip, so
# the gate only ever FAILS on an observed violation — the primary pool carrying the new image while
# the read pool is still behind. ponytail: sampling race ceiling, same as RSTR-02.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME (=<cr>-primary), NEO4J_AUTH_SECRET,
#         NEO4J_VERSION (from), NEO4J_UPGRADE_TO (to), CLUSTER_EXPECTED_MEMBERS,
#         CLUSTER_READ_MEMBERS, E2E_UPGRADE_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"
# shellcheck source=../../../lib/oracle.sh
source "${SCRIPT_DIR}/../../../lib/oracle.sh"

NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
PRIMARY_STS="statefulset/${NEO4J_STS_NAME}"
READ_STS="statefulset/${NEO4J_CR_NAME}-read"
MEMBERS="${CLUSTER_EXPECTED_MEMBERS:?CLUSTER_EXPECTED_MEMBERS not set}"
READ_MEMBERS="${CLUSTER_READ_MEMBERS:-1}"
QUORUM_FLOOR=$((MEMBERS - 1))
FROM="${NEO4J_VERSION:?NEO4J_VERSION not set}"
TO="${NEO4J_UPGRADE_TO:?NEO4J_UPGRADE_TO not set}"
TIMEOUT_SECS="${E2E_UPGRADE_TIMEOUT:-1800}"
READY_TIMEOUT_SECS="${E2E_CLUSTER_TIMEOUT:-600}"
MARKER="upgrade-${FROM}-to-${TO}"

# An upgrade to the version already running proves nothing and would pass vacuously. Fail on the
# fixture instead of on the property.
[[ "${FROM}" != "${TO}" ]] \
  || die "NEO4J_VERSION and NEO4J_UPGRADE_TO are both ${FROM} — there is no upgrade to observe; \
move NEO4J_VERSION_UPGRADE_FROM in tests/config/versions.sh"
[[ "${MEMBERS}" -ge 2 ]] \
  || die "CLUSTER_EXPECTED_MEMBERS=${MEMBERS} — the quorum property needs a multi-member pool"

cr_field() { kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" -o "jsonpath={$1}" 2>/dev/null || true; }
sts_field() { kubectl get "$1" -n "${NEO4J_NAMESPACE}" -o "jsonpath={$2}" 2>/dev/null || true; }
cond_field() {  # cond_field <condition> <field>
  cr_field ".status.conditions[?(@.type=='$1')].$2"
}
# The image a pool's pod template currently carries — the operator changing it is what starts a roll.
pool_image() {  # pool_image <statefulset/...>
  sts_field "$1" '.spec.template.spec.containers[?(@.name=="neo4j")].image'
}
pool_on_target() { [[ "$(pool_image "$1")" == *"${TO}"* ]]; }

# What counts as the operator giving up rather than converging, read from its own catalog so a
# reason added to formation later is watched without editing this file.
REFUSAL_REASONS=""
for reason in $(oracle_reasons_for ClusterFormed); do
  [[ "$(oracle_severity "${reason}")" == "error" ]] && REFUSAL_REASONS="${REFUSAL_REASONS}${reason} "
done
[[ -n "${REFUSAL_REASONS}" ]] \
  || die "no error-severity reason is catalogued for ClusterFormed — refusing to run a gate that cannot fail"

# ---------------------------------------------------------------- baseline
log "Waiting up to ${READY_TIMEOUT_SECS}s for ${NEO4J_RESOURCE} to be Ready at ${FROM}"
kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  --timeout="${READY_TIMEOUT_SECS}s" >/dev/null 2>&1 \
  || die "${NEO4J_RESOURCE} not Installed before the upgrade"
if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  --timeout="${READY_TIMEOUT_SECS}s" >/dev/null 2>&1; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} did not become Ready before the upgrade"
fi
[[ "$(cond_field ClusterFormed status)" == "True" ]] \
  || die "ClusterFormed=$(cond_field ClusterFormed status)/$(cond_field ClusterFormed reason) before the upgrade"

# status.version is the operator's record of what is running, and the anchor every refusal below
# compares against. If it does not report the deployed version, nothing after this means anything.
[[ "$(cr_field .status.version)" == "${FROM}" ]] \
  || die "status.version=$(cr_field .status.version) before the upgrade, expected the deployed ${FROM}"
pool_on_target "${PRIMARY_STS}" \
  && die "the primary pool already carries ${TO} before the upgrade was requested"
log "Baseline: ${MEMBERS} primaries + ${READ_MEMBERS} read on ${FROM}, ClusterFormed=True"

# A row to look for afterwards. Written through the client Service so it lands on the leader.
password="$(neo4j_password)"
LEAD_POD="${NEO4J_STS_NAME}-0"
conn_exec_member() { kubectl exec -n "${NEO4J_NAMESPACE}" "${LEAD_POD}" -c neo4j -- bash -c "$1"; }
# Writes must go through neo4j:// routing to reach the leader — bolt:// to a specific member
# fails with NotALeader, which is the pitfall tests/lib/connectivity.sh documents.
CLIENT_HOST="${NEO4J_CR_NAME}.${NEO4J_NAMESPACE}.svc.cluster.local"
cypher() {  # cypher <statement>
  conn_exec_member "cypher-shell -a 'neo4j://${CLIENT_HOST}:${CONN_BOLT_PORT}' \
    -u neo4j -p '${password}' --format plain '$1'"
}
log "[survived] writing a marker row before the upgrade"
cypher "CREATE (:E2EUpgrade {marker: '${MARKER}'});" >/dev/null \
  || die "[survived] could not write the marker row before the upgrade"

# ---------------------------------------------------------------- 1. refused
# Neo4j supports no downgrade, so the operator must refuse one outright rather than roll it.
# Any earlier release in the series: what is refused is the direction, not the distance.
DOWNGRADE="2025.01.0"
log "[refused] patching spec.version to the older ${DOWNGRADE} — expecting a refusal, not a roll"
kubectl patch "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" --type merge \
  -p "{\"spec\":{\"version\":\"${DOWNGRADE}\"}}" >/dev/null

# Pinned through the generated oracle, so renaming the reason in the catalog fails here
# immediately rather than after a two-minute wait for a string that no longer exists.
DOWNGRADE_REASON="VersionDowngradeRefused"
oracle_require Error "${DOWNGRADE_REASON}"

deadline=$((SECONDS + 120))
seen=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  seen="$(cond_field Error reason)"
  [[ "${seen}" == "${DOWNGRADE_REASON}" ]] && break
  sleep 3
done
[[ "${seen}" == "${DOWNGRADE_REASON}" ]] \
  || die "[refused] Error reason is '${seen:-none}' after a downgrade to ${DOWNGRADE}, expected ${DOWNGRADE_REASON}"

# The refusal has to stop the roll, not merely complain about it.
pool_on_target "${PRIMARY_STS}" \
  && die "[refused] the primary pool's image changed despite the refusal"
[[ "$(pool_image "${PRIMARY_STS}")" == *"${FROM}"* ]] \
  || die "[refused] the primary pool no longer carries ${FROM}: $(pool_image "${PRIMARY_STS}")"
[[ "$(cr_field .status.version)" == "${FROM}" ]] \
  || die "[refused] status.version moved to $(cr_field .status.version) on a refused downgrade"
log "[refused] downgrade rejected with ${DOWNGRADE_REASON}; nothing rolled"

# ---------------------------------------------------------------- 2/3. the upgrade
log "[ordered] patching spec.version ${FROM} -> ${TO}"
kubectl patch "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" --type merge \
  -p "{\"spec\":{\"version\":\"${TO}\"}}" >/dev/null

min_primary_ready="${MEMBERS}"
reasons_seen=""
read_finished_at=""
primary_moved_at=""
saw_upgrade_status=""
deadline=$((SECONDS + TIMEOUT_SECS))

while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  # --- ordering: the read pool must be done before the primary pool is even given the new image.
  if [[ -z "${read_finished_at}" ]] && pool_on_target "${READ_STS}"; then
    r_ready="$(sts_field "${READ_STS}" .status.readyReplicas)"; r_ready="${r_ready:-0}"
    r_updated="$(sts_field "${READ_STS}" .status.updatedReplicas)"; r_updated="${r_updated:-0}"
    r_cur="$(sts_field "${READ_STS}" .status.currentRevision)"
    r_upd="$(sts_field "${READ_STS}" .status.updateRevision)"
    if [[ "${r_cur}" == "${r_upd}" && "${r_ready}" -ge "${READ_MEMBERS}" && "${r_updated}" -ge "${READ_MEMBERS}" ]]; then
      read_finished_at="${SECONDS}"
      log "[ordered] read pool finished on ${TO}"
    fi
  fi
  if [[ -z "${primary_moved_at}" ]] && pool_on_target "${PRIMARY_STS}"; then
    primary_moved_at="${SECONDS}"
    [[ -n "${read_finished_at}" ]] \
      || die "[ordered] the primary pool was given ${TO} while the read pool was still behind — \
Neo4j requires system-database secondaries to be upgraded first, and the read pool is one"
    log "[ordered] primary pool released onto ${TO} after the read pool finished"
  fi

  # --- quorum: at most one primary un-Ready, and no refusal from formation.
  p_ready="$(sts_field "${PRIMARY_STS}" .status.readyReplicas)"; p_ready="${p_ready:-0}"
  [[ "${p_ready}" -lt "${min_primary_ready}" ]] && min_primary_ready="${p_ready}"
  formed_reason="$(cond_field ClusterFormed reason)"
  if [[ -n "${formed_reason}" && " ${reasons_seen} " != *" ${formed_reason} "* ]]; then
    reasons_seen="${reasons_seen}${formed_reason} "
  fi
  for refusal in ${REFUSAL_REASONS}; do
    [[ "${formed_reason}" == "${refusal}" ]] \
      && die "[quorum] ClusterFormed reported ${formed_reason} during the upgrade — that is the \
operator giving up, not converging (reasons seen: ${reasons_seen})"
  done
  [[ "${p_ready}" -lt "${QUORUM_FLOOR}" ]] \
    && die "[quorum] readyReplicas=${p_ready} on ${PRIMARY_STS}, below the floor ${QUORUM_FLOOR} — \
more than one primary was down at once (ClusterFormed reasons seen: ${reasons_seen:-none})"

  # --- reported: status.upgrade must name the target while the roll is in flight.
  [[ -z "${saw_upgrade_status}" && "$(cr_field .status.upgrade.targetVersion)" == "${TO}" ]] \
    && saw_upgrade_status="yes" && log "[reported] status.upgrade targets ${TO}, phase $(cr_field .status.upgrade.phase)"

  # --- done: every pool on the new revision and the operator has accepted the new version.
  p_cur="$(sts_field "${PRIMARY_STS}" .status.currentRevision)"
  p_upd="$(sts_field "${PRIMARY_STS}" .status.updateRevision)"
  p_updated="$(sts_field "${PRIMARY_STS}" .status.updatedReplicas)"; p_updated="${p_updated:-0}"
  if [[ "${p_cur}" == "${p_upd}" && "${p_updated}" -ge "${MEMBERS}" && "${p_ready}" -ge "${MEMBERS}" \
    && "$(cr_field .status.version)" == "${TO}" ]]; then
    break
  fi
  sleep 3
done

[[ -n "${primary_moved_at}" ]] \
  || die "[ordered] the primary pool never received ${TO} within ${TIMEOUT_SECS}s (reasons seen: ${reasons_seen:-none})"
log "[quorum] quorum held throughout (min primary readyReplicas=${min_primary_ready}, floor=${QUORUM_FLOOR})"

# ---------------------------------------------------------------- 4. reported
[[ "$(cr_field .status.version)" == "${TO}" ]] \
  || die "[reported] status.version=$(cr_field .status.version) after the upgrade, expected ${TO}"
[[ -n "$(cr_field .status.lastUpgradeTime)" ]] \
  || die "[reported] status.lastUpgradeTime is empty after a completed upgrade"
[[ -z "$(cr_field .status.upgrade.phase)" ]] \
  || die "[reported] status.upgrade still reports phase $(cr_field .status.upgrade.phase) after completion"
log "[reported] status.version=${TO}, lastUpgradeTime=$(cr_field .status.lastUpgradeTime), upgrade block cleared"

# Every pod actually runs the new image — the operator's own status is not evidence of that.
for ((i = 0; i < MEMBERS; i++)); do
  img="$(kubectl get pod "${NEO4J_STS_NAME}-${i}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.spec.containers[?(@.name=="neo4j")].image}' 2>/dev/null || true)"
  [[ "${img}" == *"${TO}"* ]] \
    || die "[reported] pod ${NEO4J_STS_NAME}-${i} runs ${img:-none}, expected an image carrying ${TO}"
done
log "[reported] all ${MEMBERS} primary pods run ${TO}"

log "[reformed] waiting up to ${READY_TIMEOUT_SECS}s for the cluster to re-form"
deadline=$((SECONDS + READY_TIMEOUT_SECS))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  [[ "$(cond_field ClusterFormed status)" == "True" ]] && break
  sleep 5
done
[[ "$(cond_field ClusterFormed status)" == "True" ]] \
  || die "[reformed] ClusterFormed=$(cond_field ClusterFormed status)/$(cond_field ClusterFormed reason) \
after the upgrade settled (reasons seen during the roll: ${reasons_seen:-none})"

# ---------------------------------------------------------------- 5. survived
log "[survived] reading the marker row back"
found="$(cypher "MATCH (n:E2EUpgrade {marker: '${MARKER}'}) RETURN count(n) AS c;" 2>/dev/null | tr -dc '0-9' || true)"
[[ "${found}" == *1* ]] \
  || die "[survived] the marker row written before the upgrade is gone (count=${found:-none}) — \
the upgrade did not preserve data"

log "Upgraded ${FROM} -> ${TO}: downgrade refused, read pool before primaries, quorum held, \
status reported, data intact (ADR-017)"
