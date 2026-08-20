#!/usr/bin/env bash
# scope/cleanup-multi — remove the CRs applied in every watched namespace, bounded.
#
# Bounded on purpose: rendered pods carry terminationGracePeriodSeconds=3600 (Helm parity) and a
# PVC keeps its pvc-protection finalizer until the pod mounting it is gone, so a pod that ignores
# SIGTERM — typically one killed mid-bootstrap — would make an unbounded delete wait out the whole
# hour. Once the StatefulSets are gone nothing recreates the pods, and the grace period only
# delays a teardown with nothing left to flush.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

WATCHED="${E2E_SCOPE_NAMESPACES:-e2e-scope-a,e2e-scope-b}"
CR="${E2E_SCOPE_MULTI_CR:-e2e-scope-multi}"
SELECTOR="app.kubernetes.io/instance=${CR}"

IFS=',' read -r -a watched_list <<<"${WATCHED}"
for ns in "${watched_list[@]}"; do
  ns="${ns// /}"
  [[ -n "${ns}" ]] || continue

  log "Deleting neo4j/${CR} in ${ns}"
  kubectl delete neo4j "${CR}" -n "${ns}" --ignore-not-found --wait=false || true
  kubectl wait --for=delete statefulset -n "${ns}" -l "${SELECTOR}" --timeout=120s >/dev/null 2>&1 || true
  kubectl delete pod -n "${ns}" -l "${SELECTOR}" --ignore-not-found --force --grace-period=0 >/dev/null 2>&1 || true
  kubectl delete pvc -n "${ns}" -l "${SELECTOR}" --ignore-not-found --timeout=120s ||
    log "WARNING: PVCs of ${CR} still terminating in ${ns}"
done

log "Multi-namespace workload cleanup done"
