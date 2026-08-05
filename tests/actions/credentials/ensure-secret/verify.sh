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
