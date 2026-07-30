#!/usr/bin/env bash
# assert/primary-scale-blocked (run) — ask a single-primary cluster to grow its
# primary pool. The operator must refuse: growing the system database from one
# primary to many is not supported (bootstrap at the final size instead).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

TARGET="${PRIMARY_SCALE_TARGET:-3}"

log "Patching topology.primaries.members 1 -> ${TARGET} (expected to be refused)"
kubectl patch "neo4j/${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" --type json \
  -p "[{\"op\":\"replace\",\"path\":\"/spec/topology/primaries/members\",\"value\":${TARGET}}]" >/dev/null \
  || die "failed to patch topology.primaries.members"
