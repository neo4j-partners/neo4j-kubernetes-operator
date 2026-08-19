#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

SELECTOR="app.kubernetes.io/instance=${NEO4J_CR_NAME}"

kubectl delete neo4j "${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" --ignore-not-found --wait=false || true

# Rendered pods carry terminationGracePeriodSeconds=3600 (Helm parity), and a PVC keeps its
# pvc-protection finalizer until the pod that mounts it is gone — so a pod that ignores SIGTERM,
# typically one killed mid-bootstrap, would make the delete below wait out that whole hour.
# Once the owning StatefulSets are gone nothing recreates the pods, so the grace period only
# delays a teardown that has nothing left to flush.
kubectl wait --for=delete statefulset -n "${NEO4J_NAMESPACE}" -l "${SELECTOR}" --timeout=120s >/dev/null 2>&1 || true
kubectl delete pod -n "${NEO4J_NAMESPACE}" -l "${SELECTOR}" --ignore-not-found --force --grace-period=0 >/dev/null 2>&1 || true

# Cases share a cluster (E2E_PROFILE=matrix, the single AKS behind the azure-aks target, a reused
# local kind), so a surviving PVC would hand the next case an initialised data directory.
kubectl delete pvc -n "${NEO4J_NAMESPACE}" -l "${SELECTOR}" --ignore-not-found --timeout=120s ||
  log "WARNING: PVCs of ${NEO4J_CR_NAME} still terminating; a later case reusing that name may fail"

log "Neo4j workload cleanup done"
