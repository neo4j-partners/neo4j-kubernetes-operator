#!/usr/bin/env bash
# scope/apply-multi — the apply itself landed; whether the operator acted on it is
# assert/scope-multi-reconciled's job.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

WATCHED="${E2E_SCOPE_NAMESPACES:-e2e-scope-a,e2e-scope-b}"
CR="${E2E_SCOPE_MULTI_CR:-e2e-scope-multi}"

IFS=',' read -r -a watched_list <<<"${WATCHED}"
for ns in "${watched_list[@]}"; do
  ns="${ns// /}"
  [[ -n "${ns}" ]] || continue
  kubectl get neo4j "${CR}" -n "${ns}" >/dev/null 2>&1 \
    || die "CR ${CR} not present in ${ns} after apply"
done

log "CR ${CR} present in every watched namespace"
