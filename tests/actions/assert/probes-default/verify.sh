#!/usr/bin/env bash
# assert/probes-default — NEO-2-009 / NEO-3-009-PROBE-01 (AC-NEO-PROBES): the operator
# renders its default startup / readiness / liveness probes on the neo4j container.
#
# The requirement is that the defaults are "appropriate for Neo4j startup, recovery and
# cluster formation", so the thresholds are asserted, not just the presence of a probe:
# the startup budget (1000 x 5s ~= 83 min) is what lets a cluster finish forming before
# the kubelet gives up. A silent drop to a Kubernetes-default threshold would still leave
# three probes in place while breaking every slow boot — that is the regression this
# catches. Readiness is already exercised implicitly by every `Ready` wait; only the
# rendered shape is unverified, and one topology-agnostic check covers it.
#
# Probe port is compared against the container's own "bolt" port rather than a literal
# 7687, so a fixture that moves the Bolt listener stays consistent instead of failing.
#
# Render-only: needs the StatefulSet to exist (Installed), not a Ready Neo4j.
#
# Inputs: NEO4J_STS_NAME, NEO4J_NAMESPACE
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

STS="statefulset/${NEO4J_STS_NAME}"
CONTAINER='.spec.template.spec.containers[?(@.name=="neo4j")]'

kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" >/dev/null 2>&1 \
  || die "StatefulSet ${NEO4J_STS_NAME} not found — cannot check probes"

# read <jsonpath-suffix> — value from the neo4j container, empty when unset.
read_container() {
  kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath="{${CONTAINER}$1}" 2>/dev/null || true
}

bolt_port="$(read_container '.ports[?(@.name=="bolt")].containerPort')"
[[ -n "${bolt_port}" ]] \
  || die "neo4j container declares no \"bolt\" port — cannot validate probe targets"
log "neo4j container bolt port = ${bolt_port}"

# probe:expected failureThreshold — periodSeconds/timeoutSeconds are shared (5 / 10).
EXPECTED_PERIOD=5
EXPECTED_TIMEOUT=10
failures=0

check_probe() {
  local probe=$1 want_threshold=$2
  local tcp_port threshold period timeout

  tcp_port="$(read_container ".${probe}.tcpSocket.port")"
  if [[ -z "${tcp_port}" ]]; then
    log "FAIL ${probe}: missing, or not a tcpSocket probe (expected tcpSocket on Bolt)"
    failures=$((failures + 1))
    return
  fi
  [[ "${tcp_port}" == "${bolt_port}" ]] \
    || { log "FAIL ${probe}: tcpSocket.port=${tcp_port}, expected the Bolt port ${bolt_port}"; failures=$((failures + 1)); }

  threshold="$(read_container ".${probe}.failureThreshold")"
  [[ "${threshold}" == "${want_threshold}" ]] \
    || { log "FAIL ${probe}: failureThreshold=${threshold:-unset}, expected ${want_threshold}"; failures=$((failures + 1)); }

  period="$(read_container ".${probe}.periodSeconds")"
  [[ "${period}" == "${EXPECTED_PERIOD}" ]] \
    || { log "FAIL ${probe}: periodSeconds=${period:-unset}, expected ${EXPECTED_PERIOD}"; failures=$((failures + 1)); }

  timeout="$(read_container ".${probe}.timeoutSeconds")"
  [[ "${timeout}" == "${EXPECTED_TIMEOUT}" ]] \
    || { log "FAIL ${probe}: timeoutSeconds=${timeout:-unset}, expected ${EXPECTED_TIMEOUT}"; failures=$((failures + 1)); }

  log "${probe}: tcpSocket:${tcp_port} failureThreshold=${threshold} period=${period}s timeout=${timeout}s"
}

# Defaults per src/internal/render/workload/probes.go (applyProbes).
check_probe startupProbe 1000
check_probe readinessProbe 20
check_probe livenessProbe 40

if [[ "${failures}" -ne 0 ]]; then
  kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath="{${CONTAINER}.startupProbe}{'\n'}{${CONTAINER}.readinessProbe}{'\n'}{${CONTAINER}.livenessProbe}{'\n'}" >&2 || true
  die "default probe assertions failed (${failures} mismatch(es)) — NEO-3-009-PROBE-01"
fi

log "Default startup/readiness/liveness probes rendered as expected (NEO-3-009-PROBE-01)"
