#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

apply_exit="$(read_apply_exit)"
[[ -n "${apply_exit}" && "${apply_exit}" -ne 0 ]] \
  || die "expected kubectl apply to fail but it succeeded (exit ${apply_exit:-unknown})"

stderr="$(read_apply_stderr)"
if [[ -n "${EXPECT_CONTAINS:-}" ]]; then
  [[ "${stderr}" == *"${EXPECT_CONTAINS}"* ]] \
    || die "apply stderr missing expected fragment '${EXPECT_CONTAINS}' (got: ${stderr})"
fi

log "Admission rejected as expected (exit=${apply_exit})"
