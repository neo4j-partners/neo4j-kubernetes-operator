#!/usr/bin/env bash
# assert/cluster-members — AC-NEO-CLUSTER-001: the expected number of Neo4j members
# is created for the pool. Checks the pool StatefulSet's desired replicas and that
# that many pods actually reach Running.
#
# Cluster names operands per pool: the StatefulSet is <cr>-<pool> (e.g. prod-primary),
# not <cr>-server as in Standalone — NEO4J_STS_NAME is derived from NEO4J_POOL.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_POOL,
#         CLUSTER_EXPECTED_MEMBERS, E2E_ASSERT_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

WANT="${CLUSTER_EXPECTED_MEMBERS:?CLUSTER_EXPECTED_MEMBERS not set — cluster cases must declare it}"
STS="statefulset/${NEO4J_STS_NAME}"
NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
TIMEOUT="${E2E_ASSERT_TIMEOUT:-300s}"

log "Waiting for ${NEO4J_RESOURCE} Installed condition (operator reconciled operands)"
if ! kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT}" 2>/dev/null; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} Installed condition not True within ${TIMEOUT}"
fi

# 1. The pool StatefulSet must exist and declare the requested member count.
kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" >/dev/null 2>&1 \
  || die "pool StatefulSet ${NEO4J_STS_NAME} not found (pool=${NEO4J_POOL:-?})"

replicas="$(kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.replicas}' 2>/dev/null || true)"
[[ "${replicas}" == "${WANT}" ]] \
  || die "${STS} replicas=${replicas:-none}, expected ${WANT}"
log "${STS} declares ${replicas} member(s)"

# 2. That many pods must actually be Running (scheduling + boot, not just the spec).
log "Waiting for ${WANT} Running pod(s) in pool ${NEO4J_POOL:-?}"
deadline=$((SECONDS + ${TIMEOUT%s}))
running=0
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  running="$(kubectl get pods -n "${NEO4J_NAMESPACE}" \
    -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" \
    --field-selector=status.phase=Running --no-headers 2>/dev/null | wc -l | tr -d ' ')"
  [[ "${running}" -ge "${WANT}" ]] && break
  sleep 5
done

if [[ "${running}" -lt "${WANT}" ]]; then
  kubectl get pods -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
  kubectl describe "${STS}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "only ${running}/${WANT} member pod(s) Running within ${TIMEOUT}"
fi

log "${running}/${WANT} member pod(s) Running (AC-NEO-CLUSTER-001)"
