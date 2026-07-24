#!/usr/bin/env bash
# scope/apply-watched (verify) — confirm the CR was accepted into the watched namespace.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NS="${NEO4J_NAMESPACE:-default}"
CR="${E2E_SCOPE_WATCHED_CR:-e2e-scope-watched}"

kubectl get neo4j "${CR}" -n "${NS}" >/dev/null 2>&1 \
  || die "CR ${CR} was not created in watched namespace ${NS}"

log "CR ${CR} present in watched namespace ${NS}"
