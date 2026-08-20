#!/usr/bin/env bash
# assert/scope-multi-reconciled — AC-OP-SCOPE-MULTI-001: every namespace listed in
# WATCH_NAMESPACE is reconciled, not just the first. Each CR must reach Installed and own real
# operands, so the assertion fails both when a namespace is outside the cache and when it is
# inside the cache but without the permissions to build anything.
#
# Inputs:
#   E2E_SCOPE_NAMESPACES — comma-separated watched namespaces
#   E2E_SCOPE_MULTI_CR   — CR name, identical in each namespace
#   E2E_ASSERT_TIMEOUT   — wait budget per namespace
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

WATCHED="${E2E_SCOPE_NAMESPACES:-e2e-scope-a,e2e-scope-b}"
CR="${E2E_SCOPE_MULTI_CR:-e2e-scope-multi}"
TIMEOUT="${E2E_ASSERT_TIMEOUT:-300s}"

IFS=',' read -r -a watched_list <<<"${WATCHED}"
[[ "${#watched_list[@]}" -ge 2 ]] \
  || die "E2E_SCOPE_NAMESPACES must list at least two namespaces to prove multi-namespace scope, got ${WATCHED}"

for ns in "${watched_list[@]}"; do
  ns="${ns// /}"
  [[ -n "${ns}" ]] || continue

  log "Waiting for neo4j/${CR} Installed in ${ns}"
  if ! kubectl wait --for=condition=Installed "neo4j/${CR}" \
    -n "${ns}" --timeout="${TIMEOUT}" 2>/dev/null; then
    kubectl describe "neo4j/${CR}" -n "${ns}" >&2 || true
    die "CR ${CR} in watched ns ${ns} was NOT reconciled (Installed not True) — scope or RBAC missing for that namespace"
  fi

  sts="$(kubectl get statefulset -n "${ns}" \
    -l "app.kubernetes.io/instance=${CR}" -o name 2>/dev/null || true)"
  [[ -n "${sts}" ]] || die "no StatefulSet created for reconciled CR ${CR} in ${ns}"
  log "CR ${CR} reconciled in ${ns} (${sts})"
done

log "Every watched namespace reconciled: ${WATCHED} (AC-OP-SCOPE-MULTI-001)"
