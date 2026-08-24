#!/usr/bin/env bash
# verify trust/provision-byo — the BYO bolt TLS Secrets exist, carry the NEO-005 mountable
# opt-in label, and hold the data keys the fixture mounts (private.key / public.crt). Without
# any of these the operator refuses the CR at reconcile, so catch it here with a clear message
# rather than as an opaque "not Ready" later.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

check() {  # check <secret> <data-key>
  local secret=$1 key=$2 label
  label="$(kubectl get secret "${secret}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.metadata.labels.neo4j\.com/mountable-by-operator}' 2>/dev/null || true)"
  [[ "${label}" == "true" ]] \
    || die "Secret ${secret} missing label neo4j.com/mountable-by-operator=true (got '${label:-<none>}')"
  # The data key itself contains a dot (private.key / public.crt); escape it so jsonpath does
  # not read it as a nested field.
  local escaped="${key//./\\.}" val
  val="$(kubectl get secret "${secret}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath="{.data.${escaped}}" 2>/dev/null || true)"
  [[ -n "${val}" ]] || die "Secret ${secret} missing non-empty data key '${key}'"
  log "Secret ${secret} ok (labelled, has ${key})"
}

check tls-byo-bolt-key private.key
check tls-byo-bolt-cert public.crt

log "BYO bolt TLS Secrets verified"
