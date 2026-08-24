#!/usr/bin/env bash
# case_run/trust/provision-byo — create the Bring-Your-Own bolt TLS Secrets the CR references,
# BEFORE the CR is applied (the operator's EnsureMountable check fails a CR whose mount Secrets
# are absent or unlabelled). Generates a self-signed leaf whose SAN matches the client Service
# the operator dials with verified TLS — formation.ClientBoltURI is the SHORT form <cr>.<ns>.svc,
# so that name MUST be a SAN or the operator's bolt+s handshake fails and the CR never goes Ready.
#
# Secret shape mirrors the fixture: key under data key private.key, cert under public.crt, each
# carrying neo4j.com/mountable-by-operator=true (NEO-005). Named with the CR instance label so
# trust/cleanup-byo can find them.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE (exported per-case before case_run).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

require_cmd openssl kubectl
: "${NEO4J_CR_NAME:?NEO4J_CR_NAME not set}"
: "${NEO4J_NAMESPACE:?NEO4J_NAMESPACE not set}"

KEY_SECRET="tls-byo-bolt-key"
CERT_SECRET="tls-byo-bolt-cert"
domain="${NEO4J_CLUSTER_DOMAIN:-cluster.local}"
svc="${NEO4J_CR_NAME}.${NEO4J_NAMESPACE}.svc"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

# A config file (not -addext) so this works on both OpenSSL and macOS LibreSSL. The first SAN is
# the operator's dial target; the rest cover the FQDN and the per-member client name Neo4j
# advertises, so a client using the Service FQDN also verifies.
cat >"${tmp}/req.cnf" <<EOF
[req]
distinguished_name = dn
x509_extensions = v3
prompt = no
[dn]
CN = ${svc}
[v3]
subjectAltName = @san
[san]
DNS.1 = ${svc}
DNS.2 = ${svc}.${domain}
DNS.3 = ${NEO4J_CR_NAME}-server-0.${NEO4J_NAMESPACE}.svc.${domain}
EOF

log "Generating self-signed bolt leaf (CN/SAN=${svc}) for BYO trust Secrets"
openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
  -keyout "${tmp}/private.key" -out "${tmp}/public.crt" \
  -config "${tmp}/req.cnf" 2>/dev/null \
  || die "openssl failed to generate the self-signed bolt certificate"

for s in "${KEY_SECRET}" "${CERT_SECRET}"; do
  kubectl delete secret "${s}" -n "${NEO4J_NAMESPACE}" --ignore-not-found >/dev/null 2>&1 || true
done
kubectl create secret generic "${KEY_SECRET}" -n "${NEO4J_NAMESPACE}" \
  --from-file=private.key="${tmp}/private.key"
kubectl create secret generic "${CERT_SECRET}" -n "${NEO4J_NAMESPACE}" \
  --from-file=public.crt="${tmp}/public.crt"

for s in "${KEY_SECRET}" "${CERT_SECRET}"; do
  kubectl label secret "${s}" -n "${NEO4J_NAMESPACE}" --overwrite \
    "neo4j.com/mountable-by-operator=true" \
    "app.kubernetes.io/instance=${NEO4J_CR_NAME}" >/dev/null
done

log "BYO bolt TLS Secrets ready: ${KEY_SECRET} (private.key), ${CERT_SECRET} (public.crt)"
