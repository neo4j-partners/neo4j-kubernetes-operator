#!/usr/bin/env bash
# Load and reconcile e2e configuration.
# Usage: source tests/config/load.sh && reconcile_config <profile> <cloud>

set -euo pipefail

CONFIG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=reconcile.sh
source "${CONFIG_DIR}/reconcile.sh"
