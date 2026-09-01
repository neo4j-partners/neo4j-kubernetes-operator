#!/usr/bin/env bash
# assert/storage-grow-refused — a grow the StorageClass will not allow.
#
# The operator does not read the StorageClass to find out: that object is cluster-scoped and
# NEO-016 keeps V1 RBAC to a namespaced Role, so checking allowVolumeExpansion up front would need
# a ClusterRole for one boolean. It attempts the patch and reports what the API server says
# (ADR-001, amended). This case pins that contract: the refusal must reach the user as
# StorageReady=False/StorageResizeFailed plus a Warning Event carrying the API server's own words,
# and it must NOT be silently swallowed into a healthy-looking CR.
#
# What must also hold: Neo4j keeps serving. The claims are untouched, so the only thing wrong is
# that the CR wants a size it cannot have. A refused grow is not an outage and must not become one.
#
# allowVolumeExpansion is flipped rather than assumed, because the platforms disagree — kind's
# local-path class already forbids expansion while every managed provider's default class allows
# it. It is restored on exit.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, STORAGE_GROW_TO
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"

TARGET="${STORAGE_GROW_TO:-20Gi}"
BUDGET_SECS="${STORAGE_GROW_TIMEOUT:-300}"

FAILED_REASON=StorageResizeFailed
STORAGE_NOT_READY_REASON=StorageNotReady
oracle_require StorageReady "${FAILED_REASON}"
oracle_require Ready "${STORAGE_NOT_READY_REASON}"

storage_wait_ready

CLASS="$(storage_data_class)"
[[ -n "${CLASS}" ]] || die "cannot determine the StorageClass the data claim is bound through"
RESTORE_EXPANSION="$(storage_class_expansion "${CLASS}")"

cleanup() {
  if [[ "${RESTORE_EXPANSION}" == "true" ]]; then
    storage_set_class_expansion "${CLASS}" true
  fi
  return 0
}
trap cleanup EXIT

[[ "${RESTORE_EXPANSION}" == "true" ]] && storage_set_class_expansion "${CLASS}" false

# A while-read loop rather than mapfile: the harness has to run under macOS's bash 3.2.
CLAIMS=()
while IFS= read -r claim; do
  [[ -n "${claim}" ]] && CLAIMS+=("${claim}")
done < <(storage_claims)
[[ "${#CLAIMS[@]}" -gt 0 ]] || die "no operator-owned claims found for ${NEO4J_CR_NAME}"
BASELINE_SIZE="$(storage_claim_field "${CLAIMS[0]}" requested)"
log "Baseline: ${CLAIMS[0]} at ${BASELINE_SIZE}, class ${CLASS} now forbids expansion"

log "Patching data size to ${TARGET} — accepted at admission, impossible to apply"
out="$(storage_patch_size "${TARGET}")" \
  || die "admission refused the grow; a size increase is valid and the refusal must come from the provisioner, not the schema: ${out}"

log "Waiting up to ${BUDGET_SECS}s for the CR to report ${FAILED_REASON}"
deadline=$((SECONDS + BUDGET_SECS))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  [[ "$(storage_condition StorageReady reason)" == "${FAILED_REASON}" ]] && break
  sleep 5
done
[[ "$(storage_condition StorageReady reason)" == "${FAILED_REASON}" ]] \
  || { storage_dump "refused"; die "StorageReady=$(storage_condition StorageReady status)/$(storage_condition StorageReady reason) after ${BUDGET_SECS}s, expected False/${FAILED_REASON} — a discarded size change must never read as healthy"; }
[[ "$(storage_condition StorageReady status)" == "False" ]] \
  || { storage_dump "refused"; die "StorageReady reports ${FAILED_REASON} with status True"; }
log "StorageReady=False/${FAILED_REASON}"

# The reason on Ready must not blame the members: they are all up.
ready_reason="$(storage_condition Ready reason)"
[[ "${ready_reason}" == "${STORAGE_NOT_READY_REASON}" ]] \
  || { storage_dump "refused"; die "Ready reason is '${ready_reason}', expected ${STORAGE_NOT_READY_REASON} — the members are up and the reason must say what is actually holding Ready back"; }
log "Ready=False/${STORAGE_NOT_READY_REASON} — the reason names storage, not the members"

# The Warning Event must carry the provisioner's own explanation, not a paraphrase.
event_msg="$(kubectl get events -n "${NEO4J_NAMESPACE}" \
  --field-selector "involvedObject.name=${NEO4J_CR_NAME},reason=${FAILED_REASON}" \
  -o jsonpath='{.items[-1:].message}' 2>/dev/null || true)"
[[ -n "${event_msg}" ]] \
  || { storage_dump "refused"; die "no ${FAILED_REASON} Event recorded — the API server's explanation is the only place the real cause appears"; }
grep -qi 'resiz\|expan' <<<"${event_msg}" \
  || { storage_dump "refused"; die "the ${FAILED_REASON} Event does not carry the API server's refusal: '${event_msg}'"; }
log "Warning Event: ${event_msg}"

# Nothing was changed, and Neo4j never stopped.
for pvc in "${CLAIMS[@]}"; do
  got="$(storage_claim_field "${pvc}" requested)"
  [[ "${got}" == "${BASELINE_SIZE}" ]] \
    || { storage_dump "refused"; die "claim ${pvc} moved to ${got} on a class that forbids expansion — a refused grow must change nothing"; }
done
log "every claim still at ${BASELINE_SIZE}"

pod="$(storage_pod)"
phase="$(kubectl get "pod/${pod}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
[[ "${phase}" == "Running" ]] \
  || { storage_dump "refused"; die "pod ${pod} is ${phase:-absent} — a refused grow must not disturb the running database"; }
log "pod ${pod} still Running — the refusal cost no availability"
