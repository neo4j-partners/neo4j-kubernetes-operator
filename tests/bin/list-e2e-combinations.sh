#!/usr/bin/env bash
# List reconciled e2e matrix combinations without running tests.
# Usage: CLOUD=local-kind SCENARIO=p0-standalone ./tests/bin/list-e2e-combinations.sh

set -euo pipefail

BIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TESTS_DIR="$(cd "${BIN_DIR}/.." && pwd)"

CLOUD="${CLOUD:-local-kind}"
SCENARIO="${SCENARIO:-p0-standalone}"

# shellcheck source=../config/load.sh
source "${TESTS_DIR}/config/load.sh"

_combination_cr_name() {
  local neo4j_case=$1
  case "${neo4j_case}" in
    standalone-named-cr) echo "e2e-graph" ;;
    *) echo "dev" ;;
  esac
}

printf 'E2E matrix for cloud=%s scenario=%s\n\n' "${CLOUD}" "${SCENARIO}"
printf '%-4s %-16s %-28s %s\n' '#' 'operator' 'neo4j' 'cr'
printf '%-4s %-16s %-28s %s\n' '---' '--------' '-----' '--'

idx=0
while read -r op neo4j; do
  [[ -n "${op}" ]] || continue
  idx=$((idx + 1))
  cr="$(_combination_cr_name "${neo4j}")"
  printf '%-4s %-16s %-28s %s\n' "${idx}." "${op}" "${neo4j}" "${cr}"
done < <(reconcile_list_combinations "${CLOUD}" "${SCENARIO}")

echo ""
echo "Total: ${idx} combination(s)"
echo "Run all: E2E_PROFILE=matrix CLOUD=${CLOUD} ./tests/bin/run-e2e.sh ${SCENARIO}"
