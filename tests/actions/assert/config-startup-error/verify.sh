#!/usr/bin/env bash
# assert/config-startup-error — AC-NEO-CONFIG-002: an unknown neo4j.conf setting is
# accepted at admission but rejected by Neo4j strict validation at startup, so the
# workload never becomes Ready and the error is observable in the pod logs.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

POD="${NEO4J_STS_NAME}-0"
MARKER="${EXPECT_CONTAINS:-Unrecognized setting}"
NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
TIMEOUT_SECS="${E2E_CONFIG_ERROR_TIMEOUT:-180}"

log "Waiting for ${NEO4J_RESOURCE} Installed condition (operator reconciled operands)"
kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout=120s >/dev/null 2>&1 \
  || die "${NEO4J_RESOURCE} was not reconciled — operator did not render operands"

log "Waiting up to ${TIMEOUT_SECS}s for Neo4j to reject '${MARKER}' at startup (pod ${POD})"
deadline=$((SECONDS + TIMEOUT_SECS))
found=0
reason=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  # Neo4j logs the error, then the container restarts (CrashLoopBackOff) — read both
  # the current and previous container logs.
  logs="$(
    kubectl logs "${POD}" -c neo4j -n "${NEO4J_NAMESPACE}" --tail=-1 2>/dev/null
    kubectl logs "${POD}" -c neo4j -n "${NEO4J_NAMESPACE}" --previous --tail=-1 2>/dev/null
  )"
  if grep -qiF -- "${MARKER}" <<<"${logs}"; then
    found=1
    break
  fi
  reason="$(kubectl get pod "${POD}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.status.containerStatuses[0].state.waiting.reason}' 2>/dev/null || true)"
  sleep 5
done

if [[ "${found}" -ne 1 ]]; then
  kubectl describe pod "${POD}" -n "${NEO4J_NAMESPACE}" >&2 || true
  kubectl logs "${POD}" -c neo4j -n "${NEO4J_NAMESPACE}" --tail=50 >&2 2>/dev/null || true
  die "expected Neo4j startup to fail with '${MARKER}' but it was not observed within ${TIMEOUT_SECS}s (last pod reason: ${reason:-none})"
fi

# The workload must not be Ready when config is invalid.
if kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout=1s >/dev/null 2>&1; then
  die "Neo4j reported Ready despite an invalid setting"
fi

log "Neo4j rejected the unknown setting at startup (marker: '${MARKER}') — workload not Ready (AC-NEO-CONFIG-002)"
