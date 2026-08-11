#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

if [[ "${AUTH_SECRET_CREATE:-false}" != "true" ]]; then
  exit 0
fi

kubectl get secret "${AUTH_SECRET_NAME}" -n "${NEO4J_NAMESPACE}" >/dev/null \
  || die "auth Secret ${AUTH_SECRET_NAME} was not created"

# Check the labels match AUTH_SECRET_LABELS: on the happy path a missing label makes the
# operator fail its first pipeline step, so the CR never reaches Installed and assert/credentials
# only reports a timeout. Negative cases need the opposite guarantee — the label really is absent.
label() {
  kubectl get secret "${AUTH_SECRET_NAME}" -n "${NEO4J_NAMESPACE}" \
    -o "jsonpath={.metadata.labels.neo4j\\.com/$1}"
}

case "${AUTH_SECRET_LABELS:-full}" in
  full)      want_mountable="true" want_delegate="${NEO4J_CR_NAME}" ;;
  mountable) want_mountable="true" want_delegate="" ;;
  none)      want_mountable="" want_delegate="" ;;
  *) die "unknown AUTH_SECRET_LABELS='${AUTH_SECRET_LABELS}' (expected full|mountable|none)" ;;
esac

got_mountable="$(label mountable-by-operator)"
[[ "${got_mountable}" == "${want_mountable}" ]] \
  || die "auth Secret ${AUTH_SECRET_NAME} label mountable-by-operator='${got_mountable:-<absent>}', expected '${want_mountable:-<absent>}' (NEO-005)"

got_delegate="$(label allowed-for)"
[[ "${got_delegate}" == "${want_delegate}" ]] \
  || die "auth Secret ${AUTH_SECRET_NAME} label allowed-for='${got_delegate:-<absent>}', expected '${want_delegate:-<absent>}' (ADD-01)"
