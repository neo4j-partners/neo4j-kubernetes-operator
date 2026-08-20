#!/usr/bin/env bash
# scope/cleanup-watched — remove the positive-control CRs (and their PVCs) that
# scope/apply-watched created in each watched namespace. Best-effort: teardown runs even on
# failure, so never abort the suite here.
#
# Bounded on purpose: rendered pods carry terminationGracePeriodSeconds=3600 (Helm parity) and a
# PVC keeps its pvc-protection finalizer until the pod mounting it is gone, so an unbounded delete
# would wait out that whole hour on a pod that ignores SIGTERM. Once the StatefulSets are gone
# nothing recreates the pods.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

WATCHED="${E2E_SCOPE_WATCHED_NAMESPACES:-e2e-scope-a,e2e-scope-b}"
CR="${E2E_SCOPE_WATCHED_CR:-e2e-scope-watched}"
SELECTOR="app.kubernetes.io/instance=${CR}"

IFS=',' read -r -a watched_list <<<"${WATCHED}"
for ns in "${watched_list[@]}"; do
  ns="${ns// /}"
  [[ -n "${ns}" ]] || continue

  kubectl delete neo4j "${CR}" -n "${ns}" --ignore-not-found --wait=false || true
  kubectl wait --for=delete statefulset -n "${ns}" -l "${SELECTOR}" --timeout=120s >/dev/null 2>&1 || true
  kubectl delete pod -n "${ns}" -l "${SELECTOR}" --ignore-not-found --force --grace-period=0 >/dev/null 2>&1 || true
  kubectl delete pvc -n "${ns}" -l "${SELECTOR}" --ignore-not-found --timeout=120s ||
    log "WARNING: PVCs of ${CR} still terminating in ${ns}"
done

log "Watched-namespace CR ${CR} cleanup done"
