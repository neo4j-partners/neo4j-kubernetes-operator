#!/usr/bin/env bash
# Create a BYO Neo4j auth Secret for examples/standalone/02-auth-existing-secret.yaml.
# Does not commit a password into the repo (NEO-021).
#
# Usage:
#   ./examples/secrets/create-auth-secret.sh [namespace] [neo4j-cr-name] [secret-name]
# Defaults: default / dev-auth-secret / neo4j-auth
set -euo pipefail

NS="${1:-default}"
CR_NAME="${2:-dev-auth-secret}"
SECRET_NAME="${3:-neo4j-auth}"
PASS="$(openssl rand -base64 24 | tr -d '/+=' | head -c 24)"

kubectl -n "$NS" create secret generic "$SECRET_NAME" \
  --from-literal="NEO4J_AUTH=neo4j/${PASS}" \
  --dry-run=client -o yaml |
  kubectl label --local -f - \
    neo4j.com/mountable-by-operator=true \
    "neo4j.com/allowed-for=${CR_NAME}" \
    -o yaml |
  kubectl apply -f -

echo "Created Secret ${NS}/${SECRET_NAME} for Neo4j ${CR_NAME} (password not printed)."
echo "Retrieve later only if needed: kubectl -n ${NS} get secret ${SECRET_NAME} -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d; echo"
