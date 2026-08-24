#!/usr/bin/env bash
# verify trust/provision-byo-cluster — both BYO cluster Secrets exist, carry the NEO-005
# mountable label, and hold the three data keys each policy mounts (private.key / public.crt /
# ca.crt). Caught here so a provisioning slip is a clear message, not a later opaque "not Ready".
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

check_key() {  # check_key <secret> <data-key>
  local secret=$1 key=$2 escaped val
  escaped="${key//./\\.}"
  val="$(kubectl get secret "${secret}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath="{.data.${escaped}}" 2>/dev/null || true)"
  [[ -n "${val}" ]] || die "Secret ${secret} missing non-empty data key '${key}'"
}

check_secret() {  # check_secret <secret>
  local secret=$1 label
  label="$(kubectl get secret "${secret}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.metadata.labels.neo4j\.com/mountable-by-operator}' 2>/dev/null || true)"
  [[ "${label}" == "true" ]] \
    || die "Secret ${secret} missing label neo4j.com/mountable-by-operator=true (got '${label:-<none>}')"
  check_key "${secret}" private.key
  check_key "${secret}" public.crt
  check_key "${secret}" ca.crt
  log "Secret ${secret} ok (labelled, has private.key/public.crt/ca.crt)"
}

check_secret tls-byo-cl-cluster
check_secret tls-byo-cl-bolt

log "BYO cluster TLS Secrets verified"
