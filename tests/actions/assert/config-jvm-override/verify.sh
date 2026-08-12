#!/usr/bin/env bash
# assert/config-jvm-override — NEO-3-003-JVM-01 (conflict): when useDefaults is true and an
# additionalArguments entry targets the same JVM key as a Neo4j default, the user value wins
# and replaces the default *in place* — one entry per key, keeping the default's position so
# ordering stays stable across reconciles.
#
# Two collisions are covered, one per key shape handled by jvmArgKey
# (src/internal/render/serverconfig/configmap.go): a -D value and a -XX boolean flip.
#
# A resolved collision must also be reported, never silent: a Warning Event carrying the oracle
# reason and an operator log line, both naming the value kept and the value dropped.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

CONFIGMAP="${NEO4J_CONFIGMAP:-${NEO4J_CR_NAME}-config}"
SETTING_NAME="${EXPECT_SETTING_NAME:-server.jvm.additional}"
# Tied to tests/fixtures/neo4j-config-jvm-override.yaml: "<key prefix>|<winner>|<loser>".
OVERRIDES=(
  "-Djdk.nio.maxCachedBufferSize=|-Djdk.nio.maxCachedBufferSize=2048|-Djdk.nio.maxCachedBufferSize=1024"
  "OmitStackTraceInFastThrow|-XX:+OmitStackTraceInFastThrow|-XX:-OmitStackTraceInFastThrow"
)
# Default left alone by the fixture, and the last default of the list — an override that kept
# its slot sits before it, an override wrongly appended would sit after.
UNTOUCHED_DEFAULT="-XX:+UseG1GC"
LAST_DEFAULT="-Dlog4j2.disable.jmx=true"
# Operator-side reporting of the drop: oracle reason on the Event, msg on the log line. Both are
# field-agnostic (render.Duplicate) — the message names spec.config.jvm.additionalArguments.
EXPECT_EVENT_REASON="${EXPECT_EVENT_REASON:-DuplicateEntry}"
EXPECT_LOG_MSG="${EXPECT_LOG_MSG:-duplicate entry}"
EXPECT_EVENT_FIELD="${EXPECT_EVENT_FIELD:-spec.config.jvm.additionalArguments}"
TIMEOUT="${E2E_ASSERT_TIMEOUT:-300s}"
NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
POD="${NEO4J_STS_NAME}-0"

log "Waiting for ${NEO4J_RESOURCE} Installed condition (config ConfigMap rendered)"
if ! kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT}" 2>/dev/null; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} Installed condition not True within ${TIMEOUT}"
fi

rendered="$(kubectl get configmap "${CONFIGMAP}" -n "${NEO4J_NAMESPACE}" \
  -o 'jsonpath={.data.server\.jvm\.additional}' 2>/dev/null || true)"
[[ -n "${rendered}" ]] \
  || die "${SETTING_NAME} missing from ConfigMap ${CONFIGMAP}"

grep -qxF -- "${UNTOUCHED_DEFAULT}" <<<"${rendered}" \
  || die "overriding two defaults dropped the others ('${UNTOUCHED_DEFAULT}' missing); got: ${rendered}"

last_default_line="$(grep -nxF -- "${LAST_DEFAULT}" <<<"${rendered}" | head -1 | cut -d: -f1)"
[[ -n "${last_default_line}" ]] \
  || die "default '${LAST_DEFAULT}' missing from ${CONFIGMAP} ${SETTING_NAME}; got: ${rendered}"

for entry in "${OVERRIDES[@]}"; do
  IFS='|' read -r key winner loser <<<"${entry}"

  grep -qxF -- "${winner}" <<<"${rendered}" \
    || die "additionalArguments '${winner}' did not win over the default; got: ${rendered}"
  if grep -qxF -- "${loser}" <<<"${rendered}"; then
    die "default '${loser}' survived alongside '${winner}' in ${CONFIGMAP}; got: ${rendered}"
  fi

  # One line per key: an override replaces, it does not append a second entry.
  occurrences="$(grep -cF -- "${key}" <<<"${rendered}" || true)"
  [[ "${occurrences}" -eq 1 ]] \
    || die "expected 1 entry for key '${key}', found ${occurrences}; got: ${rendered}"

  winner_line="$(grep -nxF -- "${winner}" <<<"${rendered}" | head -1 | cut -d: -f1)"
  [[ "${winner_line}" -lt "${last_default_line}" ]] \
    || die "'${winner}' (line ${winner_line}) was appended instead of replacing the default in place (last default at line ${last_default_line}); got: ${rendered}"
done

log "ConfigMap ${CONFIGMAP}: additionalArguments replaced the colliding defaults in place"

# A dropped argument must never be silent: the operator names the winner on a Warning Event and
# in its own log. Reason comes from the oracle (src/internal/status/oracle.go).
events=""
ev_deadline=$((SECONDS + ${E2E_EVENT_WAIT_SECONDS:-60}))
while [[ "${SECONDS}" -lt "${ev_deadline}" ]]; do
  events="$(kubectl get events -n "${NEO4J_NAMESPACE}" \
    --field-selector "involvedObject.name=${NEO4J_CR_NAME},reason=${EXPECT_EVENT_REASON}" \
    -o 'jsonpath={range .items[*]}{.type}{"\t"}{.message}{"\n"}{end}' 2>/dev/null || true)"
  [[ -n "${events}" ]] && break
  sleep 3
done
[[ -n "${events}" ]] \
  || die "no Event with reason ${EXPECT_EVENT_REASON} on neo4j/${NEO4J_CR_NAME} — the override went unreported"

operator_logs="$(kubectl logs -n "${OPERATOR_NAMESPACE:-neo4j-operator-system}" \
  -l "${OPERATOR_LABEL_SELECTOR:-app.kubernetes.io/name=neo4j-operator}" --tail=-1 2>/dev/null || true)"
log_lines="$(grep -F -- "${EXPECT_LOG_MSG}" <<<"${operator_logs}" || true)"
[[ -n "${log_lines}" ]] \
  || die "operator log has no '${EXPECT_LOG_MSG}' entry"

for entry in "${OVERRIDES[@]}"; do
  IFS='|' read -r _ winner loser <<<"${entry}"

  event="$(grep -F -- "${winner}" <<<"${events}" | head -1 || true)"
  [[ -n "${event}" ]] \
    || die "no ${EXPECT_EVENT_REASON} Event names the winning argument '${winner}'; events: ${events}"
  [[ "${event}" == Warning* ]] \
    || die "Event for '${winner}' should be a Warning; got: ${event}"
  grep -qF -- "${loser}" <<<"${event}" \
    || die "Event for '${winner}' does not name the dropped '${loser}'; got: ${event}"
  # The reason is shared by every field, so the message must say which one collided.
  grep -qF -- "${EXPECT_EVENT_FIELD}" <<<"${event}" \
    || die "Event for '${winner}' does not name the field '${EXPECT_EVENT_FIELD}'; got: ${event}"

  grep -F -- "${winner}" <<<"${log_lines}" | grep -qF -- "${loser}" \
    || die "operator log has no '${EXPECT_LOG_MSG}' line naming both '${winner}' and '${loser}'; got: ${log_lines}"
done

log "Override reported on Warning Events (${EXPECT_EVENT_REASON}) and in the operator log, naming the value kept"

log "Waiting for ${NEO4J_RESOURCE} Ready (Neo4j must accept connections for SHOW SETTINGS)"
if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout=600s >/dev/null 2>&1; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} did not become Ready"
fi

# Run cypher-shell inside the Neo4j container over its localhost bolt interface.
conn_exec_serverpod() {
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"
}
CONN_EXEC_FN=conn_exec_serverpod

password="$(neo4j_password)"
for entry in "${OVERRIDES[@]}"; do
  IFS='|' read -r _ winner _ <<<"${entry}"
  conn_assert_setting localhost "${password}" "${SETTING_NAME}" "${winner}" "config-jvm-override"
done

log "jvm.useDefaults true — colliding additionalArguments win at runtime in ${SETTING_NAME} (NEO-3-003-JVM-01)"
