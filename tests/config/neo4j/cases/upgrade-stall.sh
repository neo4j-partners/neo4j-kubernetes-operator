#!/usr/bin/env bash
# Standalone whose upgrade cannot succeed: the target tag does not exist, so no member ever comes
# back and the operator's deadline is the only thing that can end it.

export NEO4J_CASE_NAME=upgrade-stall
export NEO4J_CR_NAME="${NEO4J_CR_NAME:-e2e-upgrade-stall}"
export NEO4J_DATA_SIZE="${NEO4J_DATA_SIZE:-5Gi}"
export NEO4J_USE_STORAGE_CLASS=false
export NEO4J_TOPOLOGY_MODE=Standalone
export NEO4J_POOL=server

# A tag no registry publishes. Preflight accepts it — the operator does not ask a registry whether a
# version exists, and could not without credentials — so the failure surfaces as a pod that never
# starts, which is exactly the path under test.
export NEO4J_UPGRADE_TO="2026.99.0"

# The fixture's probe gives each member 120s + 30s grace, so the deadline lands ~150s after the
# roll stalls. Twice that leaves room for image-pull backoff without making a stuck suite slow.
export E2E_UPGRADE_TIMEOUT="${E2E_UPGRADE_TIMEOUT:-420}"
