#!/usr/bin/env bash
# assert/scale-out (verify) — AC-NEO-SCALE-001 / AC-NEO-SCALE-002 (NEO-3-011-SRV-01):
# the added member is created AND enabled into the cluster by the operator
# (ENABLE SERVER), not merely added to the StatefulSet.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"
# shellcheck source=../../../lib/scale.sh
source "${SCRIPT_DIR}/../../../lib/scale.sh"

scale_assert_members "${SCALE_TARGET:?SCALE_TARGET not set}" "AC-NEO-SCALE-001/002"
