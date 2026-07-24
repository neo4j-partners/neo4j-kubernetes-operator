#!/usr/bin/env bash
# scope/apply-unwatched (verify) — confirm the CR was actually accepted into the
# unwatched namespace. This only proves the apply succeeded; whether the operator
# *reconciles* it is the job of assert/scope-ignored.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NS="${E2E_SCOPE_NAMESPACE:-e2e-unwatched}"
CR="${E2E_SCOPE_CR:-e2e-scope}"

kubectl get neo4j "${CR}" -n "${NS}" >/dev/null 2>&1 \
  || die "CR ${CR} was not created in ${NS}"

log "CR ${CR} present in unwatched namespace ${NS}"
