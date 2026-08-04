#!/usr/bin/env bash
# assert/scale-in (run) — return the secondary pool to its baseline size.
# AC-NEO-SCALE-003: scale-in removes members safely (operator drains first).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/scale.sh
source "${SCRIPT_DIR}/../../../lib/scale.sh"

scale_patch_members "${SCALE_BASELINE:?SCALE_BASELINE not set}"
