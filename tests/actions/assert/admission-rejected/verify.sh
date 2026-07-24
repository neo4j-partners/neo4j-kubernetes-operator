#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

[[ "${LAST_APPLY_EXIT_CODE:-1}" -ne 0 ]] \
  || die "expected kubectl apply to fail but it succeeded (exit 0)"

stderr="${LAST_APPLY_STDERR:-}"
if [[ -n "${EXPECT_CONTAINS:-}" ]]; then
  [[ "${stderr}" == *"${EXPECT_CONTAINS}"* ]] \
    || die "apply stderr missing expected fragment '${EXPECT_CONTAINS}'"
fi

log "Admission rejected as expected (exit=${LAST_APPLY_EXIT_CODE})"
