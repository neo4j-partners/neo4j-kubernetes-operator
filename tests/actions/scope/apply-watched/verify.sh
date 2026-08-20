#!/usr/bin/env bash
# scope/apply-watched (verify) — confirm the CR was accepted into every watched namespace.
# Whether the operator then acted on it is assert/reconciled-in-namespace's job.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

WATCHED="${E2E_SCOPE_WATCHED_NAMESPACES:-e2e-scope-a,e2e-scope-b}"
CR="${E2E_SCOPE_WATCHED_CR:-e2e-scope-watched}"

IFS=',' read -r -a watched_list <<<"${WATCHED}"
for ns in "${watched_list[@]}"; do
  ns="${ns// /}"
  [[ -n "${ns}" ]] || continue
  kubectl get neo4j "${CR}" -n "${ns}" >/dev/null 2>&1 \
    || die "CR ${CR} was not created in watched namespace ${ns}"
done

log "CR ${CR} present in every watched namespace (${WATCHED})"
