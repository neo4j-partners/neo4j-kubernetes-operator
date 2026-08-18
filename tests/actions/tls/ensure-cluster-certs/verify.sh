#!/usr/bin/env bash
# tls/ensure-cluster-certs (verify) — the three Secrets exist, carry the key the CR
# references, and are opted in for operator mounting.
#
# Checked before the CR is applied on purpose. Without the NEO-005 label the operator
# refuses the CR at reconcile and never renders operands, so assert/cluster-formed would
# report a formation timeout and the real cause — a missing label — would be invisible.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

# secret:key pairs, matching tests/fixtures/neo4j-cluster-tls.yaml.
for pair in \
  "${NEO4J_CR_NAME}-cluster-key:private.key" \
  "${NEO4J_CR_NAME}-cluster-cert:public.crt" \
  "${NEO4J_CR_NAME}-cluster-ca:ca.crt"
do
  secret="${pair%%:*}"
  key="${pair##*:}"

  kubectl get secret "${secret}" -n "${NEO4J_NAMESPACE}" >/dev/null 2>&1 \
    || die "TLS Secret ${secret} was not created"

  # jsonpath needs the dot in the key escaped, or it reads a nested field.
  key_esc="${key//./\\.}"
  value="$(kubectl get secret "${secret}" -n "${NEO4J_NAMESPACE}" \
    -o "jsonpath={.data.${key_esc}}" 2>/dev/null || true)"
  [[ -n "${value}" ]] \
    || die "TLS Secret ${secret} has no key '${key}' — the CR references it by subPath and the mount would be empty"

  mountable="$(kubectl get secret "${secret}" -n "${NEO4J_NAMESPACE}" \
    -o 'jsonpath={.metadata.labels.neo4j\.com/mountable-by-operator}' 2>/dev/null || true)"
  [[ "${mountable}" == "true" ]] \
    || die "TLS Secret ${secret} is missing neo4j.com/mountable-by-operator=true — the operator would refuse the CR before rendering operands (NEO-005)"
done

log "Cluster TLS Secrets present, keyed, and opted in for operator mounting"
