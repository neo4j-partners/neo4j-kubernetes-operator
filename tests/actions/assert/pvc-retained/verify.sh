#!/usr/bin/env bash
# assert/pvc-retained — NEO-2-018 (AC-NEO-UNINSTALL / AC-NEO-UNINSTALL-PRESERVE):
# deleting the Neo4j CR removes its workload but PRESERVES the data PVC by default,
# so no data is lost on an accidental or intentional uninstall.
#
# Flow (all in verify.sh, after standalone-ready has confirmed operands exist):
#   1. record the data PVC + its bound PV,
#   2. delete the Neo4j CR and wait for the StatefulSet to disappear,
#   3. assert the PVC still exists, is still Bound, and still references the same PV.
#
# The lingering PVC is removed afterwards by the case_teardown (cleanup/standalone),
# which deletes PVCs by instance label.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, E2E_ASSERT_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

TIMEOUT="${E2E_ASSERT_TIMEOUT:-300s}"
INSTANCE_SELECTOR="app.kubernetes.io/instance=${NEO4J_CR_NAME}"

# 1. Snapshot the data PVC and the PV it is bound to.
pvc="$(kubectl get pvc -n "${NEO4J_NAMESPACE}" -l "${INSTANCE_SELECTOR}" \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
[[ -n "${pvc}" ]] || die "no PVC found for instance ${NEO4J_CR_NAME} before uninstall"

# The PVC must actually bind before we can meaningfully test its retention — and
# a real bound volume is what would hold data. On StorageClasses with binding mode
# WaitForFirstConsumer (kind's default "standard") this only happens once the pod
# consumes the claim, so wait rather than reading spec.volumeName immediately.
log "Waiting for PVC ${pvc} to reach phase Bound before uninstall"
if ! kubectl wait --for=jsonpath='{.status.phase}'=Bound \
  "pvc/${pvc}" -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT}" 2>/dev/null; then
  phase="$(kubectl get "pvc/${pvc}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  die "PVC ${pvc} not Bound within ${TIMEOUT} before uninstall (phase=${phase:-unknown})"
fi

pv_before="$(kubectl get "pvc/${pvc}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.volumeName}' 2>/dev/null || true)"
[[ -n "${pv_before}" ]] || die "PVC ${pvc} is Bound but has no volumeName before uninstall"
log "Before uninstall: PVC ${pvc} bound to PV ${pv_before}"

# 2. Uninstall the workload by deleting the CR, and wait for the StatefulSet to go.
#    --wait=false + an explicit poll keeps the timeout under our control.
log "Deleting ${NEO4J_CR_NAME} (uninstall) and waiting for StatefulSet ${NEO4J_STS_NAME} to be removed"
kubectl delete neo4j "${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" --wait=false --ignore-not-found

deadline=$((SECONDS + ${TIMEOUT%s}))
while kubectl get "statefulset/${NEO4J_STS_NAME}" -n "${NEO4J_NAMESPACE}" >/dev/null 2>&1; do
  [[ "${SECONDS}" -lt "${deadline}" ]] \
    || die "StatefulSet ${NEO4J_STS_NAME} still present ${TIMEOUT} after CR delete"
  sleep 3
done
log "StatefulSet ${NEO4J_STS_NAME} removed"

# 3. The PVC must survive the uninstall, still Bound to the same PV.
kubectl get "pvc/${pvc}" -n "${NEO4J_NAMESPACE}" >/dev/null 2>&1 \
  || die "PVC ${pvc} was DELETED on uninstall — data would be lost (NEO-2-018 violated)"

phase="$(kubectl get "pvc/${pvc}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
[[ "${phase}" == "Bound" ]] \
  || die "PVC ${pvc} survived but is '${phase:-unknown}', expected Bound"

pv_after="$(kubectl get "pvc/${pvc}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.spec.volumeName}' 2>/dev/null || true)"
[[ "${pv_after}" == "${pv_before}" ]] \
  || die "PVC ${pvc} rebound to a different PV (${pv_before} -> ${pv_after:-none})"

log "PVC ${pvc} retained and still Bound to ${pv_after} after uninstall (NEO-2-018)"
