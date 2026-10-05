#!/usr/bin/env bash
# assert/upgrade-stall — ADR-017: an upgrade that cannot finish ends in Failed, and says why.
#
# Every other upgrade assertion follows the happy path. This is the one users actually hit when
# something goes wrong, and until now nothing exercised it: the deadline, the Failed phase, and
# lastError had no coverage at all.
#
# The stall is a version whose image does not exist. The operator does not ask a registry whether a
# tag is real — it has no credentials to do so and Preflight deliberately does not try — so the pod
# simply never starts. No member is ever upgraded, no progress restarts the clock, and the only
# thing that can end the upgrade is the deadline the operator derives from the CR's own probes.
#
# Four properties:
#
#   1. accepted   — the change is NOT refused. A nonexistent tag is well-formed and moves forward,
#                   so Preflight has no grounds to reject it. If this started failing at admission
#                   the rest of the test would be unreachable and the deadline unproven.
#   2. stalls     — the upgrade is reported in flight, and stays there. Neither readyReplicas nor
#                   progress.upgraded can say "no member came back on the new version": the old pod
#                   is ready for a while yet, and a pod stuck pulling an image still counts as
#                   created at the new revision. Failed and status.version are the real evidence.
#   3. failed     — status.upgrade.phase reaches Failed with a non-empty lastError.
#   4. intact     — status.version still reports the version that IS running. A failed upgrade must
#                   not leave the resource claiming a version that never started.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME (=<cr>-server), NEO4J_VERSION (from),
#         NEO4J_UPGRADE_TO (a tag that does not exist), E2E_UPGRADE_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
STS="statefulset/${NEO4J_STS_NAME}"
FROM="${NEO4J_VERSION:?NEO4J_VERSION not set}"
TO="${NEO4J_UPGRADE_TO:?NEO4J_UPGRADE_TO not set}"
TIMEOUT_SECS="${E2E_UPGRADE_TIMEOUT:-420}"
READY_TIMEOUT_SECS="${E2E_CLUSTER_TIMEOUT:-600}"

cr_field() { kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" -o "jsonpath={$1}" 2>/dev/null || true; }

[[ "${FROM}" != "${TO}" ]] || die "NEO4J_VERSION and NEO4J_UPGRADE_TO are both ${FROM}"

# ---------------------------------------------------------------- baseline
log "Waiting up to ${READY_TIMEOUT_SECS}s for ${NEO4J_RESOURCE} to be Ready at ${FROM}"
kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  --timeout="${READY_TIMEOUT_SECS}s" >/dev/null 2>&1 \
  || { kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
       die "${NEO4J_RESOURCE} did not become Ready before the upgrade"; }
[[ "$(cr_field .status.version)" == "${FROM}" ]] \
  || die "status.version=$(cr_field .status.version) before the upgrade, expected ${FROM}"
log "Baseline: Standalone Ready on ${FROM}"

# ---------------------------------------------------------------- 1. accepted
log "[accepted] patching spec.version to ${TO}, a tag no registry publishes"
kubectl patch "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" --type merge \
  -p "{\"spec\":{\"version\":\"${TO}\"}}" >/dev/null

# ---------------------------------------------------------------- 2/3. stalls, then fails
log "[failed] waiting up to ${TIMEOUT_SECS}s for the upgrade to reach its deadline"
deadline=$((SECONDS + TIMEOUT_SECS))
saw_in_flight=""
phase=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  phase="$(cr_field .status.upgrade.phase)"
  [[ -n "${phase}" && -z "${saw_in_flight}" ]] \
    && saw_in_flight="${phase}" && log "[stalls] upgrade reported in flight, phase ${phase}"
  # No readiness check here. Right after the patch the OLD pod is still up and ready, and
  # readyReplicas cannot tell it from a new one; progress.upgraded cannot either, since it counts
  # pods created at the new revision and a pod stuck pulling an image is one. That the upgrade never
  # succeeds is what the Failed phase below asserts, and status.version proves it after the fact.
  [[ "${phase}" == "Failed" ]] && break
  sleep 5
done

[[ -n "${saw_in_flight}" ]] \
  || die "[stalls] status.upgrade was never populated — the operator did not notice the version change"
[[ "${phase}" == "Failed" ]] \
  || die "[failed] status.upgrade.phase is '${phase:-none}' after ${TIMEOUT_SECS}s, expected Failed. \
An upgrade that cannot finish must end, not hang: check the deadline derived from spec.probes"

last_error="$(cr_field .status.upgrade.lastError)"
[[ -n "${last_error}" ]] \
  || die "[failed] status.upgrade.phase is Failed with an empty lastError — a failed upgrade must say why"
log "[failed] phase=Failed, lastError=${last_error}"

# ---------------------------------------------------------------- 4. intact
# The pod never started on ${TO}, so the resource must still report ${FROM} as what is running.
[[ "$(cr_field .status.version)" == "${FROM}" ]] \
  || die "[intact] status.version=$(cr_field .status.version) after a failed upgrade, expected the \
still-running ${FROM} — a version that never started must not be reported as running"

# And the workload must be untouched apart from the pod that cannot pull: the old replica set is
# what is still serving, so the StatefulSet must not have been scaled away.
replicas="$(kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.spec.replicas}' 2>/dev/null || true)"
[[ "${replicas}" == "1" ]] \
  || die "[intact] ${STS} declares ${replicas:-none} replica(s) after a failed upgrade, expected 1"

log "Stalled upgrade ${FROM} -> ${TO} ended in Failed with a reason, and the resource still reports \
${FROM} as running (ADR-017)"
