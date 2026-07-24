#!/usr/bin/env bash
# scope/cleanup-unwatched — case_teardown for the scope suite. Deleting the
# namespace cascades to the CR (and anything else in it). Best-effort: teardown
# runs even on failure, so never abort the suite here.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NS="${E2E_SCOPE_NAMESPACE:-e2e-unwatched}"

log "Deleting unwatched namespace ${NS}"
kubectl delete namespace "${NS}" --ignore-not-found --wait=false || true

log "Scope test cleanup done"
