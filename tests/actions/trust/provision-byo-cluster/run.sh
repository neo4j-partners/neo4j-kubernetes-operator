#!/usr/bin/env bash
# case_run/trust/provision-byo-cluster — create the Bring-Your-Own TLS Secrets for a Cluster,
# BEFORE the CR is applied. Cluster BYO is the heavy case: a private CA signs two leaves shared
# by every member (the operator mounts the same Secret on all pods), each carrying the exact
# service FQDNs the operator and members dial with verified TLS:
#
#   cluster leaf — per-member internals Services (raft/discovery mTLS). clientAuth REQUIRE means
#                  the cert is presented from BOTH ends, so it needs serverAuth AND clientAuth.
#     SAN: <cr>-<pool>-<i>-internals.<ns>.svc.<domain>   (each member)
#   bolt leaf   — the client Service the operator dials (formation.ClientBoltURI, short form) and
#                 the per-member client FQDNs it dials for formation.
#     SAN: <cr>.<ns>.svc[.<domain>], <cr>-<pool>.<ns>.svc.<domain> (headless), and
#          <cr>-<pool>-<i>.<ns>.svc.<domain> (each member)
#
# Each Secret holds private.key + public.crt + ca.crt (the CA, for trustedCerts so members and
# the operator validate the peer/leaf) and carries the NEO-005 mountable label.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_POOL, CLUSTER_EXPECTED_MEMBERS.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

require_cmd openssl kubectl
: "${NEO4J_CR_NAME:?NEO4J_CR_NAME not set}"
: "${NEO4J_NAMESPACE:?NEO4J_NAMESPACE not set}"

pool="${NEO4J_POOL:-primary}"
members="${CLUSTER_EXPECTED_MEMBERS:-3}"
domain="${NEO4J_CLUSTER_DOMAIN:-cluster.local}"
ns="${NEO4J_NAMESPACE}"
sts="${NEO4J_CR_NAME}-${pool}"

CLUSTER_SECRET="tls-byo-cl-cluster"
BOLT_SECRET="tls-byo-cl-bolt"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

# --- Private CA (self-signed root) ------------------------------------------------------------
cat >"${tmp}/ca.cnf" <<EOF
[req]
distinguished_name = dn
x509_extensions = v3_ca
prompt = no
[dn]
CN = ${NEO4J_CR_NAME}-byo-ca
[v3_ca]
basicConstraints = critical,CA:TRUE
keyUsage = critical,keyCertSign,cRLSign
EOF
log "Generating private CA"
openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
  -keyout "${tmp}/ca.key" -out "${tmp}/ca.crt" -config "${tmp}/ca.cnf" 2>/dev/null \
  || die "openssl failed to generate the CA"

# emit_san_ext <file> <eku> <dns...> — write an x509 extension file for `openssl x509 -req`.
emit_san_ext() {
  local file=$1 eku=$2; shift 2
  {
    printf 'subjectAltName = @san\n'
    printf 'extendedKeyUsage = %s\n' "${eku}"
    printf 'basicConstraints = critical,CA:FALSE\n'
    printf '[san]\n'
    local i=1 dns
    for dns in "$@"; do
      printf 'DNS.%d = %s\n' "${i}" "${dns}"
      i=$((i + 1))
    done
  } >"${file}"
}

# sign_leaf <name> <cn> <eku> <dns...> — key + CSR + CA-signed cert into ${tmp}/<name>.{key,crt}.
sign_leaf() {
  local name=$1 cn=$2 eku=$3; shift 3
  openssl genrsa -out "${tmp}/${name}.key" 2048 2>/dev/null \
    || die "openssl genrsa failed for ${name}"
  openssl req -new -key "${tmp}/${name}.key" -out "${tmp}/${name}.csr" -subj "/CN=${cn}" 2>/dev/null \
    || die "openssl req (CSR) failed for ${name}"
  emit_san_ext "${tmp}/${name}.ext" "${eku}" "$@"
  openssl x509 -req -in "${tmp}/${name}.csr" -CA "${tmp}/ca.crt" -CAkey "${tmp}/ca.key" \
    -CAcreateserial -days 3650 -out "${tmp}/${name}.crt" -extfile "${tmp}/${name}.ext" 2>/dev/null \
    || die "openssl x509 (sign) failed for ${name}"
}

# --- SAN lists --------------------------------------------------------------------------------
cluster_sans=()
for ((i = 0; i < members; i++)); do
  cluster_sans+=("${sts}-${i}-internals.${ns}.svc.${domain}")
done

bolt_sans=(
  "${NEO4J_CR_NAME}.${ns}.svc"
  "${NEO4J_CR_NAME}.${ns}.svc.${domain}"
  "${sts}.${ns}.svc.${domain}"
)
for ((i = 0; i < members; i++)); do
  bolt_sans+=("${sts}-${i}.${ns}.svc.${domain}")
done

# cluster mTLS: presented as server AND client between members.
sign_leaf cluster "${NEO4J_CR_NAME}-cluster" "serverAuth,clientAuth" "${cluster_sans[@]}"
sign_leaf bolt "${NEO4J_CR_NAME}-bolt" "serverAuth" "${bolt_sans[@]}"

log "cluster SANs: ${cluster_sans[*]}"
log "bolt SANs: ${bolt_sans[*]}"

# --- Secrets ----------------------------------------------------------------------------------
create_secret() {  # create_secret <name> <leaf>
  local secret=$1 leaf=$2
  kubectl delete secret "${secret}" -n "${ns}" --ignore-not-found >/dev/null 2>&1 || true
  kubectl create secret generic "${secret}" -n "${ns}" \
    --from-file=private.key="${tmp}/${leaf}.key" \
    --from-file=public.crt="${tmp}/${leaf}.crt" \
    --from-file=ca.crt="${tmp}/ca.crt"
  kubectl label secret "${secret}" -n "${ns}" --overwrite \
    "neo4j.com/mountable-by-operator=true" \
    "app.kubernetes.io/instance=${NEO4J_CR_NAME}" >/dev/null
}
create_secret "${CLUSTER_SECRET}" cluster
create_secret "${BOLT_SECRET}" bolt

log "BYO cluster TLS Secrets ready: ${CLUSTER_SECRET}, ${BOLT_SECRET} (private.key/public.crt/ca.crt)"
