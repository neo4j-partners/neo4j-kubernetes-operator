#!/usr/bin/env bash
# Lab helper: generate BYO cluster/HTTPS TLS material and push Secrets for AKS testing.
#
# Usage:
#   ./hack/gen-cluster-tls.sh [namespace] [release-name] [primary-count]
# Standalone (bolt/https only — omit cluster in the CR):
#   ./hack/gen-cluster-tls.sh default dev 1
#
# Optional env (HTTPS / LoadBalancer — Jetty SNI needs a DNS name, not a floating IP):
#   EXTRA_DNS=neo4j.example.com   # stable hostname in the cert SAN (required for browser HTTPS)
#   EXTRA_IP=...                  # discouraged: Azure LB IPs change; use DNS → IP instead
#
# Do NOT bake Azure's ephemeral LB IP into the cert. Either:
#   A) DNS A/AAAA record for EXTRA_DNS → current LB IP (update DNS when IP changes), or
#   B) Allocate a static Azure Public IP and pin the Service to it (IP never changes).
#
# Example:
#   EXTRA_DNS=neo4j.lab.local ./hack/gen-cluster-tls.sh default prod 3
#   # /etc/hosts or real DNS: <current-lb-ip> neo4j.lab.local
#   # open https://neo4j.lab.local:7473/  — never https://<ip>:7473/
set -euo pipefail

NS="${1:-default}"
NAME="${2:-analytics}"
PRIMARIES="${3:-3}"
OUT="${TMPDIR:-/tmp}/neo4j-tls-${NAME}"
mkdir -p "$OUT"

openssl genrsa -out "$OUT/ca.key" 4096
openssl req -x509 -new -nodes -key "$OUT/ca.key" -sha256 -days 3650 \
  -subj "/CN=neo4j-lab-ca" -out "$OUT/ca.crt"

openssl genrsa -out "$OUT/cluster.key" 4096
openssl req -new -key "$OUT/cluster.key" -subj "/CN=${NAME}-cluster" -out "$OUT/cluster.csr"

{
  echo "subjectAltName = @alt_names"
  echo "extendedKeyUsage = serverAuth, clientAuth"
  echo "keyUsage = digitalSignature, keyEncipherment"
  echo "[alt_names]"
  i=1
  for ((ord = 0; ord < PRIMARIES; ord++)); do
    echo "DNS.${i} = ${NAME}-primary-${ord}.${NS}.svc.cluster.local"
    i=$((i + 1))
  done
  # Client Service (LB target) — needed when browsing via in-cluster DNS.
  echo "DNS.${i} = ${NAME}.${NS}.svc.cluster.local"
  i=$((i + 1))
  echo "DNS.${i} = ${NAME}.${NS}"
  i=$((i + 1))
  echo "DNS.${i} = *.${NS}.svc.cluster.local"
  i=$((i + 1))
  # Port-forward: Jetty often rejects bare "localhost" as SNI; neo4j.localhost
  # has a dot and resolves to 127.0.0.1 on modern OSes (RFC 6761).
  echo "DNS.${i} = neo4j.localhost"
  i=$((i + 1))
  echo "DNS.${i} = localhost"
  i=$((i + 1))
  if [[ -n "${EXTRA_DNS:-}" ]]; then
    echo "DNS.${i} = ${EXTRA_DNS}"
    i=$((i + 1))
  fi
  if [[ -n "${EXTRA_IP:-}" ]]; then
    echo "IP.1 = ${EXTRA_IP}"
  fi
} >"$OUT/cluster.ext"

openssl x509 -req -in "$OUT/cluster.csr" -CA "$OUT/ca.crt" -CAkey "$OUT/ca.key" \
  -CAcreateserial -out "$OUT/cluster.crt" -days 825 -sha256 -extfile "$OUT/cluster.ext"

kubectl -n "$NS" create secret generic "${NAME}-cluster-key" \
  --from-file=private.key="$OUT/cluster.key" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$NS" create secret generic "${NAME}-cluster-cert" \
  --from-file=public.crt="$OUT/cluster.crt" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$NS" create secret generic "${NAME}-cluster-ca" \
  --from-file=ca.crt="$OUT/ca.crt" --dry-run=client -o yaml | kubectl apply -f -

echo
echo "Certificate SANs:"
openssl x509 -in "$OUT/cluster.crt" -noout -ext subjectAltName
echo

cat <<EOF
Secrets created in ${NS}:
  ${NAME}-cluster-key
  ${NAME}-cluster-cert
  ${NAME}-cluster-ca

Port-forward (lab — Browser over HTTPS needs Bolt TLS):
  kubectl -n ${NS} port-forward svc/${NAME} 7473:7473 7687:7687
  open https://neo4j.localhost:7473/
  # Connect URI (direct, not neo4j+s routing):
  #   bolt+s://127.0.0.1:7687
  # Trust the lab CA (or allow insecure) for the self-signed cert.

CR trust block (cluster + https + bolt — Helm ssl.* parity):

  trust:
    enabled: true
    certificates:
      cluster:
        privateKey:
          secretName: ${NAME}-cluster-key
          subPath: private.key
        publicCertificate:
          secretName: ${NAME}-cluster-cert
          subPath: public.crt
        trustedCerts:
          sources:
            - secret:
                name: ${NAME}-cluster-ca
                items:
                  - key: ca.crt
                    path: ca.crt
      https:
        privateKey:
          secretName: ${NAME}-cluster-key
          subPath: private.key
        publicCertificate:
          secretName: ${NAME}-cluster-cert
          subPath: public.crt
        clientAuth: None
      bolt:
        privateKey:
          secretName: ${NAME}-cluster-key
          subPath: private.key
        publicCertificate:
          secretName: ${NAME}-cluster-cert
          subPath: public.crt
        clientAuth: None

HTTPS / external LB (optional EXTRA_DNS — do not bake floating Azure IPs):
  EXTRA_DNS=neo4j.example.com ./hack/gen-cluster-tls.sh ${NS} ${NAME} ${PRIMARIES}

  Then rebuild/redeploy the operator and roll Neo4j pods.

Material kept at: ${OUT}
EOF
