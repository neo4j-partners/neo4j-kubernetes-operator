#!/usr/bin/env bash
# assert/tls-renewal — NEO-2-005 / BDR-006: a renewed leaf certificate is actually picked up
# by the running pod. This is the manual `openssl x509 -serial` check, automated.
#
# The leaf cert/key are subPath mounts, which Kubernetes NEVER updates in place. So the mounted
# serial can only change if the operator drives it: it stamps neo4j.com/tls-checksum on the pod
# template from the mounted TLS bytes (render/trust/checksum.go), watches the (mountable-labelled)
# Secret cert-manager writes (controller watches.go: mapSecretToNeo4j), and on a change re-stamps
# the checksum, which rolls the StatefulSet. The new pod then mounts the new subPath file.
#
# Renewal is forced exactly as tested by hand: delete the leaf Secret. cert-manager reissues a
# fresh certificate (new serial + key) against the same CA and re-labels the Secret via
# secretTemplate. The assertion then proves the full chain:
#   checksum annotation changes -> StatefulSet rolls -> mounted serial differs from the old one.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, E2E_CLUSTER_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NEO4J_NS="${NEO4J_NAMESPACE}"
STS="statefulset/${NEO4J_STS_NAME}"
POD="${NEO4J_STS_NAME}-0"
TIMEOUT_SECS="${E2E_CLUSTER_TIMEOUT:-600}"
CERT_PATH="/var/lib/neo4j/certificates/bolt/public.crt"
CHECKSUM_JSONPATH='{.spec.template.metadata.annotations.neo4j\.com/tls-checksum}'

# openssl runs inside the pod (the neo4j image has it, unlike the runner host in general).
mounted_serial() {
  kubectl exec -n "${NEO4J_NS}" "${POD}" -c neo4j -- \
    openssl x509 -in "${CERT_PATH}" -noout -serial 2>/dev/null | sed 's/^serial=//'
}
sts_checksum() {
  kubectl get "${STS}" -n "${NEO4J_NS}" -o jsonpath="${CHECKSUM_JSONPATH}" 2>/dev/null || true
}

# 1. Baseline: the serial the pod is serving now, and the checksum that produced it.
old_serial="$(mounted_serial)"
[[ -n "${old_serial}" ]] || die "could not read a serial from ${CERT_PATH} in ${POD} — is bolt TLS mounted?"
old_checksum="$(sts_checksum)"
[[ -n "${old_checksum}" ]] || die "pod template has no ${CHECKSUM_JSONPATH#*annotations.} annotation — operator did not stamp the TLS checksum"
log "baseline: bolt leaf serial=${old_serial}, tls-checksum=${old_checksum}"

# 2. Resolve the Secret cert-manager writes for the bolt policy from the operator-owned
#    Certificate (<cr>-bolt-tls), rather than hardcoding the fixture's secretName.
leaf_cert="${NEO4J_CR_NAME}-bolt-tls"
secret="$(kubectl get certificate "${leaf_cert}" -n "${NEO4J_NS}" -o jsonpath='{.spec.secretName}' 2>/dev/null || true)"
[[ -n "${secret}" ]] || die "could not resolve .spec.secretName from Certificate ${leaf_cert}"

# 3. Force renewal: delete the Secret. cert-manager reissues a fresh leaf (new serial).
log "deleting bolt leaf Secret ${secret} to force a cert-manager reissue"
kubectl delete secret "${secret}" -n "${NEO4J_NS}" --ignore-not-found

# 4. The operator observes the new material and re-stamps the checksum. While the Secret is
#    briefly absent the operator requeues (awaitIssuedSecrets) rather than rolling, so the
#    checksum only settles to a NEW value once cert-manager has repopulated the Secret.
log "waiting up to ${TIMEOUT_SECS}s for the operator to re-stamp neo4j.com/tls-checksum"
deadline=$((SECONDS + TIMEOUT_SECS))
new_checksum="${old_checksum}"
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  new_checksum="$(sts_checksum)"
  [[ -n "${new_checksum}" && "${new_checksum}" != "${old_checksum}" ]] && break
  sleep 5
done
[[ -n "${new_checksum}" && "${new_checksum}" != "${old_checksum}" ]] \
  || die "tls-checksum did not change within ${TIMEOUT_SECS}s (still ${old_checksum}) — operator did not react to the reissue"
log "tls-checksum changed: ${old_checksum} -> ${new_checksum}"

# 5. The checksum change is a rolling update; wait for it to land on every member.
log "waiting for the StatefulSet ${NEO4J_STS_NAME} to finish rolling"
kubectl rollout status "${STS}" -n "${NEO4J_NS}" --timeout="${TIMEOUT_SECS}s" \
  || die "StatefulSet ${NEO4J_STS_NAME} did not finish rolling within ${TIMEOUT_SECS}s after renewal"

# 6. The authoritative check: the pod now serves a different serial from the same CA. Retried,
#    since exec on a just-restarted pod-0 can race container readiness.
log "confirming ${POD} serves a new certificate serial"
new_serial=""
deadline=$((SECONDS + 120))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  new_serial="$(mounted_serial)"
  [[ -n "${new_serial}" ]] && break
  sleep 5
done
[[ -n "${new_serial}" ]] || die "could not read a serial from ${CERT_PATH} in ${POD} after the roll"
[[ "${new_serial}" != "${old_serial}" ]] \
  || die "mounted serial unchanged (${old_serial}) after renewal — the pod is still serving the old certificate"

log "certificate renewed and picked up: mounted serial ${old_serial} -> ${new_serial} (NEO-2-005 / BDR-006)"
