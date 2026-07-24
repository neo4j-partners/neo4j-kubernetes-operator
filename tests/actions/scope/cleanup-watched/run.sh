#!/usr/bin/env bash
# scope/cleanup-watched — remove the positive-control CR (and its PVC) that
# scope/apply-watched created in the watched namespace. Best-effort.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NS="${NEO4J_NAMESPACE:-default}"
CR="${E2E_SCOPE_WATCHED_CR:-e2e-scope-watched}"

kubectl delete neo4j "${CR}" -n "${NS}" --ignore-not-found --wait=false || true
kubectl delete pvc -n "${NS}" -l "app.kubernetes.io/instance=${CR}" --ignore-not-found || true

log "Watched-namespace CR ${CR} cleanup done"
