#!/usr/bin/env bash
# assert/storage-grow — STO-004 (BDR-005), the accepted half of a storage change: raising
# spec.storage.volumes.data.dynamic.size grows the live claims in place.
#
# The defect this case exists to catch is a size change being accepted at admission, silently
# discarded, and the CR still reporting Ready=True/StorageReady=True with observedGeneration
# advanced — a grow that reads as applied and is not. So the claims are what gets asserted, on
# every ordinal of every pool, and the StatefulSet's volumeClaimTemplate is asserted NOT to move:
# Kubernetes makes it immutable once the StatefulSet exists, which is why the operator patches the
# claims instead and why a template still showing the old size is correct rather than a symptom.
#
# Runs for a Standalone and for a Cluster unchanged — the claims are discovered from labels, so a
# 3-primary cluster exercises three ordinals across a pool without the assert being told so.
#
# No member may be restarted by any of this: the grow reaches the claims, never the PodTemplate.
# That is asserted on pod UIDs and restart counts at the end.
#
# Two platform facts are handled rather than assumed. kind's local-path class forbids expansion, so
# the class is flipped for the duration and restored. And local-path has no resizer behind it: it
# takes the larger request and never updates the capacity, so the claim would sit half-grown
# forever. Where that is the provisioner, the capacity is written by hand to stand in for a
# resizer — the operator's own behaviour is what is under test, not the CSI driver's.
#
# No-op unless the case declares STORAGE_GROW_TO: the cluster pipeline runs every assert for every
# case, and only one case grows a volume.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, STORAGE_GROW_TO, STORAGE_GROW_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"

if [[ -z "${STORAGE_GROW_TO:-}" ]]; then
  log "skip storage-grow: case declares no STORAGE_GROW_TO"
  exit 0
fi

TARGET="${STORAGE_GROW_TO}"
# A grow is a control-plane operation on the CSI driver's side; managed providers take tens of
# seconds, and the claim count multiplies it on a cluster.
TIMEOUT_SECS="${STORAGE_GROW_TIMEOUT:-600}"

# Catalogued in src/internal/oracle/catalog.go — the reasons are the contract, the messages are
# not, and oracle_require turns a rename in the catalog into a failure here rather than a timeout.
RESIZING_REASON=StorageResizing
BOUND_REASON=PVCBound
COMPLETED_REASON=StorageResizeCompleted
FAILED_REASON=StorageResizeFailed
oracle_require StorageReady "${RESIZING_REASON}"
oracle_require StorageReady "${BOUND_REASON}"
oracle_require StorageReady "${FAILED_REASON}"
# Event-only: the completion is a fact about the cluster, not a condition reason.
oracle_require event "${COMPLETED_REASON}"

storage_wait_ready

CLASS="$(storage_data_class)"
[[ -n "${CLASS}" ]] || die "cannot determine the StorageClass the data claim is bound through"
PROVISIONER="$(storage_class_provisioner "${CLASS}")"
RESTORE_EXPANSION=""

cleanup() {
  [[ -n "${RESTORE_EXPANSION}" ]] && storage_set_class_expansion "${CLASS}" "${RESTORE_EXPANSION}"
  return 0
}
trap cleanup EXIT

if [[ "$(storage_class_expansion "${CLASS}")" != "true" ]]; then
  log "StorageClass ${CLASS} (${PROVISIONER}) does not allow expansion — enabling it for this case"
  RESTORE_EXPANSION=false
  storage_set_class_expansion "${CLASS}" true
fi

# Every pod of the CR, with the UID that changes when it is recreated and the restart count that
# rises when it is restarted in place. A grow patches the claims and touches nothing else, so the
# question "are the members restarted one at a time" has a stronger answer here than for a config
# change: they must not be restarted at all. Rolling them one by one is the discipline for a
# PodTemplate change (RSTR-02); a size change never reaches the PodTemplate, and a roll would be an
# availability cost paid for nothing. This is also the tripwire for the day someone adds an offline
# resize path — a CSI driver without ONLINE_EXPANSION needs the pod bounced for the filesystem to
# follow, and that roll would have to be ordered and quorum-aware rather than simultaneous.
workload_identity() {
  kubectl get pods -n "${NEO4J_NAMESPACE}" \
    -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" \
    -o jsonpath='{range .items[*]}{.metadata.name}{" uid="}{.metadata.uid}{" restarts="}{.status.containerStatuses[?(@.name=="neo4j")].restartCount}{"\n"}{end}' \
    2>/dev/null | sort
}

# ---------------------------------------------------------------------------
# 1. Baseline — every claim, and the templates that must not move
# ---------------------------------------------------------------------------
BASELINE_TEMPLATES="$(storage_template_sizes)"
# read -a rather than mapfile: the harness has to run under the bash 3.2 that ships with macOS.
CLAIMS=()
while IFS= read -r claim; do
  [[ -n "${claim}" ]] && CLAIMS+=("${claim}")
done < <(storage_claims)
[[ "${#CLAIMS[@]}" -gt 0 ]] || die "no operator-owned claims found for ${NEO4J_CR_NAME}"
log "Baseline: ${#CLAIMS[@]} claim(s), growing to ${TARGET}"
storage_claim_table
log "templates: ${BASELINE_TEMPLATES}"

BASELINE_COMPLETED="$(storage_event_count "${COMPLETED_REASON}")"
BASELINE_PODS="$(workload_identity)"

# ---------------------------------------------------------------------------
# 2. Ask for the larger size
# ---------------------------------------------------------------------------
log "Patching data size to ${TARGET}"
out="$(storage_patch_size "${TARGET}")" \
  || die "the API server refused the grow to ${TARGET}, which must be accepted: ${out}"

# ---------------------------------------------------------------------------
# 3. Every claim carries the new request — this is the operator's own work
# ---------------------------------------------------------------------------
log "Waiting up to ${TIMEOUT_SECS}s for all ${#CLAIMS[@]} claim(s) to request ${TARGET}"
deadline=$((SECONDS + TIMEOUT_SECS))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  pending=""
  for pvc in "${CLAIMS[@]}"; do
    [[ "$(storage_claim_field "${pvc}" requested)" == "${TARGET}" ]] || pending="${pending} ${pvc}"
  done
  [[ -z "${pending}" ]] && break
  sleep 5
done
if [[ -n "${pending}" ]]; then
  storage_dump "grow"
  die "claim(s)${pending} still do not request ${TARGET} after ${TIMEOUT_SECS}s — the size change was accepted and discarded, which is the defect this case guards"
fi
log "every claim requests ${TARGET}"

# A claim that is Bound but still serving the old size must not read as healthy — that is half of
# the reported defect. The condition is written by whichever reconcile pass runs next, not at the
# instant of the patch, so this waits instead of sampling once: sampled immediately it catches the
# pre-patch PVCBound, which is merely stale and not the defect.
#
# Either outcome is correct, and which one appears depends on the provisioner. A resize still in
# flight must say ${RESIZING_REASON}; one a fast CSI already finished may legitimately be back to
# ${BOUND_REASON}, but only once every claim actually serves the new size.
log "Waiting for StorageReady to account for the new size"
deadline=$((SECONDS + 120))
verdict=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  reason="$(storage_condition StorageReady reason)"
  behind=""
  for pvc in "${CLAIMS[@]}"; do
    [[ "$(storage_claim_field "${pvc}" actual)" == "${TARGET}" ]] || behind="${behind} ${pvc}"
  done
  [[ "${reason}" == "${RESIZING_REASON}" ]] && { verdict="reported ${RESIZING_REASON} while capacity lags"; break; }
  [[ -z "${behind}" && "${reason}" == "${BOUND_REASON}" ]] && { verdict="already settled at ${TARGET}"; break; }
  sleep 3
done
[[ -n "${verdict}" ]] \
  || { storage_dump "grow"; die "StorageReady stayed ${reason:-<absent>} while claim(s)${behind} still serve the old size — a stale-size claim must not read as healthy, which is the defect this case guards"; }
log "StorageReady ${verdict}"

# ---------------------------------------------------------------------------
# 4. Capacity follows — or is stood in for, where the provisioner has no resizer
# ---------------------------------------------------------------------------
# local-path is known to have no resize controller, so waiting on it is waiting on nothing: the
# full timeout would elapse every run on kind before the stand-in below took over. Any other
# provisioner is given the whole budget, and is expected to actually do the work.
CAPACITY_BUDGET="${TIMEOUT_SECS}"
if [[ "${PROVISIONER}" == "rancher.io/local-path" ]]; then
  CAPACITY_BUDGET=15
  log "${PROVISIONER} has no resize controller — allowing it ${CAPACITY_BUDGET}s before standing in"
else
  log "Waiting up to ${CAPACITY_BUDGET}s for capacity to reach ${TARGET}"
fi
deadline=$((SECONDS + CAPACITY_BUDGET))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  behind=""
  for pvc in "${CLAIMS[@]}"; do
    [[ "$(storage_claim_field "${pvc}" actual)" == "${TARGET}" ]] || behind="${behind} ${pvc}"
  done
  [[ -z "${behind}" ]] && break
  if [[ "$(storage_condition StorageReady reason)" == "${FAILED_REASON}" ]]; then
    storage_dump "grow"
    die "the grow was refused (${FAILED_REASON}) on a class that allows expansion"
  fi
  sleep 5
done

if [[ -n "${behind}" ]]; then
  [[ "${PROVISIONER}" == "rancher.io/local-path" ]] || {
    storage_dump "grow"
    die "capacity did not reach ${TARGET} within ${CAPACITY_BUDGET}s on ${PROVISIONER}, and only rancher.io/local-path is allowed to need a stand-in resizer"
  }
  log "${PROVISIONER} has no resizer — writing capacity by hand for${behind} to stand in for one"
  for pvc in ${behind}; do storage_simulate_resizer "${pvc}" "${TARGET}"; done
fi

# ---------------------------------------------------------------------------
# 5. The CR reports the grow finished, exactly once
# ---------------------------------------------------------------------------
log "Waiting for StorageReady to return to ${BOUND_REASON}"
deadline=$((SECONDS + 180))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  [[ "$(storage_condition StorageReady status)" == "True" \
    && "$(storage_condition StorageReady reason)" == "${BOUND_REASON}" ]] && break
  sleep 5
done
[[ "$(storage_condition StorageReady reason)" == "${BOUND_REASON}" ]] \
  || { storage_dump "grow"; die "StorageReady=$(storage_condition StorageReady status)/$(storage_condition StorageReady reason) after the grow completed, expected True/${BOUND_REASON}"; }

kubectl wait --for=condition=Ready "neo4j/${NEO4J_CR_NAME}" \
  -n "${NEO4J_NAMESPACE}" --timeout=300s >/dev/null 2>&1 \
  || { storage_dump "grow"; die "the CR did not return to Ready after the grow"; }

completed="$(storage_event_count "${COMPLETED_REASON}")"
[[ "${completed}" -eq $((BASELINE_COMPLETED + 1)) ]] \
  || { storage_dump "grow"; die "${COMPLETED_REASON} recorded ${completed} time(s), expected exactly $((BASELINE_COMPLETED + 1)) — the completion Event is edge-triggered and a level-triggered one would spend the object's Event budget"; }
log "${COMPLETED_REASON} recorded once"

# ---------------------------------------------------------------------------
# 6. The templates did not move, and must not have
# ---------------------------------------------------------------------------
now_templates="$(storage_template_sizes)"
[[ "${now_templates}" == "${BASELINE_TEMPLATES}" ]] \
  || die "volumeClaimTemplates changed from '${BASELINE_TEMPLATES}' to '${now_templates}' — they are immutable after create, so a change here means the StatefulSet was recreated"
log "templates unchanged (${now_templates}) — the grow went to the claims, as it must"

# ---------------------------------------------------------------------------
# 7. Not one member was restarted
# ---------------------------------------------------------------------------
NOW_PODS="$(workload_identity)"
if [[ "${NOW_PODS}" != "${BASELINE_PODS}" ]]; then
  printf 'before:\n%s\nafter:\n%s\n' "${BASELINE_PODS}" "${NOW_PODS}" >&2
  storage_dump "grow"
  die "the grow disturbed the members — same pod UIDs and restart counts expected, because growing a claim never reaches the PodTemplate. A restart here means the volume was resized offline, which on a cluster must be ordered one member at a time with quorum held, not left to happen at once"
fi
log "no member restarted: $(wc -l <<<"${BASELINE_PODS}" | tr -d ' ') pod(s) kept their UID and restart count"

storage_claim_table
log "data volume grown to ${TARGET} on ${#CLAIMS[@]} claim(s), with every member left running"
