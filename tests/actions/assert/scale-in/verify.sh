#!/usr/bin/env bash
# assert/scale-in (verify) — AC-NEO-SCALE-003: the member is drained and removed and
# the cluster remains formed (no quorum loss, no orphaned server left Enabled).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"
# shellcheck source=../../../lib/scale.sh
source "${SCRIPT_DIR}/../../../lib/scale.sh"

scale_assert_members "${SCALE_BASELINE:?SCALE_BASELINE not set}" "AC-NEO-SCALE-003"
