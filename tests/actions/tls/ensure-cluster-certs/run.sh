#!/usr/bin/env bash
# tls/ensure-cluster-certs — generate BYO cluster TLS material and publish it as the three
# Secrets the CR references, before the CR is applied.
#
# Generated per run rather than committed: these are private keys, and the repo has a
# standing rule against committing key material (NEO-019 / NEO-021). Regenerating also keeps
# the SANs correct if the CR name or namespace ever change.
#
# Mirrors hack/gen-cluster-tls.sh, which is the hand-run version of the same thing.
#
# SANs must match what members advertise, or the mTLS handshake fails and the cluster never
# forms — a failure that looks like a product bug but is a test bug. The operator advertises
# server.cluster.advertised_address as <pod>-internals.<ns>.svc.<cluster-domain>
# (render/workload/statefulset.go), so every ordinal's -internals FQDN is listed explicitly,
# plus the per-pod and client Services and a wildcard for anything derived.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, CLUSTER_EXPECTED_MEMBERS
set -euo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

require_cmd openssl kubectl

NS="${NEO4J_NAMESPACE}"
NAME="${NEO4J_CR_NAME}"
PRIMARIES="${CLUSTER_EXPECTED_MEMBERS:-3}"
DOMAIN="${NEO4J_CLUSTER_DOMAIN:-cluster.local}"

# NEO-019: unpredictable private dir, wiped on exit — key material must not linger.
OUT="$(mktemp -d "${TMPDIR:-/tmp}/e2e-tls.XXXXXX")"
trap 'rm -rf "${OUT}"' EXIT

log "Generating cluster TLS material for ${NAME} (${PRIMARIES} primaries, ns=${NS})"

openssl genrsa -out "${OUT}/ca.key" 2048 2>/dev/null
openssl req -x509 -new -nodes -key "${OUT}/ca.key" -sha256 -days 3650 \
  -subj "/CN=neo4j-e2e-ca" -out "${OUT}/ca.crt" 2>/dev/null

openssl genrsa -out "${OUT}/cluster.key" 2048 2>/dev/null
openssl req -new -key "${OUT}/cluster.key" -subj "/CN=${NAME}-cluster" -out "${OUT}/cluster.csr" 2>/dev/null

{
  echo "subjectAltName = @alt_names"
  echo "extendedKeyUsage = serverAuth, clientAuth"
  echo "keyUsage = digitalSignature, keyEncipherment"
  echo "[alt_names]"
  i=1
  for ((ord = 0; ord < PRIMARIES; ord++)); do
    # What members actually advertise to each other.
    echo "DNS.${i} = ${NAME}-primary-${ord}-internals.${NS}.svc.${DOMAIN}"; i=$((i + 1))
    echo "DNS.${i} = ${NAME}-primary-${ord}.${NS}.svc.${DOMAIN}"; i=$((i + 1))
  done
  echo "DNS.${i} = ${NAME}.${NS}.svc.${DOMAIN}"; i=$((i + 1))
  echo "DNS.${i} = *.${NS}.svc.${DOMAIN}"; i=$((i + 1))
  echo "DNS.${i} = localhost"
} >"${OUT}/cluster.ext"

openssl x509 -req -in "${OUT}/cluster.csr" -CA "${OUT}/ca.crt" -CAkey "${OUT}/ca.key" \
  -CAcreateserial -out "${OUT}/cluster.crt" -days 825 -sha256 -extfile "${OUT}/cluster.ext" 2>/dev/null

# Secret names are literal in tests/fixtures/neo4j-cluster-tls.yaml — the deploy sed only
# substitutes the CR's own metadata.name, so these must agree with the fixture by hand.
KEY_SECRET="${NAME}-cluster-key"
CRT_SECRET="${NAME}-cluster-cert"
CA_SECRET="${NAME}-cluster-ca"

kubectl -n "${NS}" create secret generic "${KEY_SECRET}" \
  --from-file=private.key="${OUT}/cluster.key" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n "${NS}" create secret generic "${CRT_SECRET}" \
  --from-file=public.crt="${OUT}/cluster.crt" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n "${NS}" create secret generic "${CA_SECRET}" \
  --from-file=ca.crt="${OUT}/ca.crt" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

# NEO-005: the operator refuses to mount a Secret the namespace owner has not opted in.
# managed-by marks them for tls/cleanup-certs.
for s in "${KEY_SECRET}" "${CRT_SECRET}" "${CA_SECRET}"; do
  kubectl -n "${NS}" label secret "${s}" \
    neo4j.com/mountable-by-operator=true app.kubernetes.io/managed-by=neo4j-e2e --overwrite >/dev/null
done

log "Published ${KEY_SECRET}, ${CRT_SECRET}, ${CA_SECRET} (mountable-by-operator=true)"
