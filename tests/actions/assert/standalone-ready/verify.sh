#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

TIMEOUT="${E2E_ASSERT_TIMEOUT}"
NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"

log "Waiting for ${NEO4J_RESOURCE} Installed condition (operator reconciled operands)"

if ! kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT}" 2>/dev/null; then
  log "Installed condition not reached — diagnostics:"
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  kubectl get sts,svc,secret,configmap,pvc,pods -n "${NEO4J_NAMESPACE}" \
    -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
  kubectl get events -n "${NEO4J_NAMESPACE}" --sort-by='.lastTimestamp' 2>/dev/null | tail -20 >&2 || true
  die "${NEO4J_RESOURCE} Installed condition not True within ${TIMEOUT}"
fi

log "Checking rendered operands (Standalone / pool server)"
kubectl get "statefulset/${NEO4J_STS_NAME}" -n "${NEO4J_NAMESPACE}" >/dev/null \
  || die "StatefulSet ${NEO4J_STS_NAME} not found"
kubectl get svc "${NEO4J_CLIENT_SVC}" -n "${NEO4J_NAMESPACE}" >/dev/null \
  || die "client Service ${NEO4J_CLIENT_SVC} not found"
kubectl get svc "${NEO4J_STS_NAME}" -n "${NEO4J_NAMESPACE}" >/dev/null \
  || die "headless Service ${NEO4J_STS_NAME} not found"
kubectl get secret "${NEO4J_AUTH_SECRET}" -n "${NEO4J_NAMESPACE}" >/dev/null \
  || die "auth Secret ${NEO4J_AUTH_SECRET} not found"
kubectl get configmap "${NEO4J_CONFIGMAP}" -n "${NEO4J_NAMESPACE}" >/dev/null \
  || die "ConfigMap ${NEO4J_CONFIGMAP} not found"

phase=$(kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.phase}' 2>/dev/null || true)
log "Neo4j status.phase=${phase:-unknown}"

if [[ "${E2E_ASSERT_NEO4J_READY}" == "true" ]]; then
  log "E2E_ASSERT_NEO4J_READY=true — waiting for Ready condition and Running pod"
  if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
    -n "${NEO4J_NAMESPACE}" --timeout=600s 2>/dev/null; then
    kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
    kubectl get pods -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
    die "Neo4j Ready condition not True within 600s (Neo4j Enterprise image pull may be required)"
  fi
  running=$(kubectl get pods -n "${NEO4J_NAMESPACE}" \
    -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" \
    --field-selector=status.phase=Running --no-headers 2>/dev/null | wc -l | tr -d ' ')
  [[ "${running:-0}" -ge 1 ]] || die "no Running Neo4j pod in ${NEO4J_NAMESPACE}"
  log "Neo4j pod Running"
fi

log "Standalone reconcile assertions passed (${NEO4J_RESOURCE})"
