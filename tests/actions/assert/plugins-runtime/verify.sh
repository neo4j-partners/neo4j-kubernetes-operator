#!/usr/bin/env bash
# assert/plugins-runtime — BDR-004 / NEO-3-003-APOC-01 (runtime): a plugin declared on spec.plugins
# is not just downloaded, its procedures are callable over bolt. Reproduces the manual check from
# docs/user-guide/03-neo4j/07-plugins.md: connect with cypher-shell and run the plugin's version
# function. This is the runtime half feature-config's apoc-config case cannot cover — SHOW SETTINGS
# never exposes plugin procedures, only the allowlist that opens them.
#
# Tied to tests/fixtures/neo4j-plugins-standalone.yaml (plugins: [apoc, gds]):
#   1. apoc.version() answers  — the APOC JAR loaded and apoc.* is allowlisted.
#   2. gds.version()  answers  — the GDS JAR loaded (Community form, no license needed) and gds.* is
#                                allowlisted.
#   3. dbms.security.procedures.allowlist contains apoc.* and gds.* — the operator opened exactly the
#      assigned plugins (SHOW SETTINGS, the neo4j.conf key the operator injects; it never sets
#      unrestricted, that stays opt-in).
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_AUTH_SECRET, E2E_TLS_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
POD="${NEO4J_STS_NAME}-0"
TIMEOUT="${E2E_ASSERT_TIMEOUT:-600s}"

log "Waiting for ${NEO4J_RESOURCE} Ready (Neo4j must accept connections to call plugin procedures)"
kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout=120s >/dev/null 2>&1 \
  || die "${NEO4J_RESOURCE} was not reconciled"
if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT}" >/dev/null 2>&1; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} did not become Ready"
fi

# Run cypher-shell inside the Neo4j container over its localhost bolt interface (plaintext — this
# fixture has no TLS), same shape as assert/config-runtime.
conn_exec_serverpod() {
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"
}
CONN_EXEC_FN=conn_exec_serverpod

password="$(neo4j_password)"

# assert_proc <label> <cypher> — run a plugin function over bolt; require success + a value row.
# An unloaded/unallowlisted procedure makes cypher-shell exit non-zero ("Unknown function ..."), so
# exit 0 already proves it resolved; the >=2 non-empty lines (header + value in --format plain) is a
# belt-and-suspenders guard. Retried: the image downloads the JAR at start, so the procedure can lag
# the pod becoming Ready.
assert_proc() {
  local label=$1 cypher=$2 out ok=1 max="${CONN_RETRIES:-30}" i
  for ((i = 1; i <= max; i++)); do
    if out="$(conn_exec_serverpod \
      "cypher-shell -a 'bolt://localhost:${CONN_BOLT_PORT}' -u neo4j -p '${password}' --format plain '${cypher}'" 2>&1)"; then
      [[ "$(grep -c . <<<"${out}")" -ge 2 ]] && { ok=0; break; }
    fi
    [[ "${i}" -lt "${max}" ]] && sleep "${CONN_RETRY_DELAY:-5}"
  done
  [[ "${ok}" -eq 0 ]] \
    || die "[${label}] '${cypher}' did not return a value after ${max} attempts; got: ${out:-<no output — plugin not loaded or procedure not allowlisted>}"
  log "[${label}] '${cypher}' answered — plugin loaded and callable"
}

# 1 + 2: the version functions resolve, proving the JARs loaded and the procedures are allowlisted.
assert_proc "apoc.version()" "RETURN apoc.version();"
assert_proc "gds.version()" "RETURN gds.version();"

# 3: the operator opened the allowlist to exactly the assigned plugins (the neo4j.conf key it injects).
conn_assert_setting localhost "${password}" dbms.security.procedures.allowlist "apoc" "plugins-runtime"
conn_assert_setting localhost "${password}" dbms.security.procedures.allowlist "gds" "plugins-runtime"

log "APOC + GDS procedures callable at runtime and allowlist opened for both (BDR-004 / NEO-3-003-APOC-01)"
