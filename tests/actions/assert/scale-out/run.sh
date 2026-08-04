#!/usr/bin/env bash
# assert/scale-out (run) — request one more member in the secondary pool under test.
# AC-NEO-SCALE-001: scale-out creates additional members.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/scale.sh
source "${SCRIPT_DIR}/../../../lib/scale.sh"

scale_patch_members "${SCALE_TARGET:?SCALE_TARGET not set}"
