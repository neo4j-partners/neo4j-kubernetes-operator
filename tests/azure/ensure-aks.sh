#!/usr/bin/env bash
# Ensure Azure resource group, ACR, and AKS exist; configure kubectl.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../lib/common.sh
source "${REPO_ROOT}/tests/lib/common.sh"
# shellcheck source=../config/reconcile.sh
source "${REPO_ROOT}/tests/config/reconcile.sh"
load_cloud_config azure-aks

require_cmd az kubectl

if [[ -z "${AZURE_SUBSCRIPTION_ID:-}" ]]; then
  AZURE_SUBSCRIPTION_ID="$(az account show --query id -o tsv)"
fi

log "Azure subscription: ${AZURE_SUBSCRIPTION_ID}"
az account set --subscription "${AZURE_SUBSCRIPTION_ID}"

if ! az group show --name "${AZURE_RESOURCE_GROUP}" >/dev/null 2>&1; then
  log "Creating resource group ${AZURE_RESOURCE_GROUP}"
  az group create --name "${AZURE_RESOURCE_GROUP}" --location "${AZURE_LOCATION}" >/dev/null
else
  log "Resource group ${AZURE_RESOURCE_GROUP} exists"
fi

if ! az acr show --name "${AZURE_ACR_NAME}" --resource-group "${AZURE_RESOURCE_GROUP}" >/dev/null 2>&1; then
  log "Creating ACR ${AZURE_ACR_NAME}"
  az acr create --resource-group "${AZURE_RESOURCE_GROUP}" --name "${AZURE_ACR_NAME}" --sku Basic >/dev/null
else
  log "ACR ${AZURE_ACR_NAME} exists"
fi

ACR_LOGIN_SERVER="${AZURE_ACR_NAME}.azurecr.io"

if ! az aks show --resource-group "${AZURE_RESOURCE_GROUP}" --name "${AZURE_AKS_NAME}" >/dev/null 2>&1; then
  log "Creating AKS cluster ${AZURE_AKS_NAME}"
  az aks create \
    --resource-group "${AZURE_RESOURCE_GROUP}" \
    --name "${AZURE_AKS_NAME}" \
    --node-count "${AZURE_AKS_NODE_COUNT}" \
    --node-vm-size "${AZURE_AKS_NODE_VM_SIZE}" \
    --attach-acr "${AZURE_ACR_NAME}" \
    --generate-ssh-keys \
    --output none
else
  log "AKS cluster ${AZURE_AKS_NAME} exists"
  az aks update \
    --resource-group "${AZURE_RESOURCE_GROUP}" \
    --name "${AZURE_AKS_NAME}" \
    --attach-acr "${AZURE_ACR_NAME}" \
    --output none 2>/dev/null || true
fi

az aks get-credentials \
  --resource-group "${AZURE_RESOURCE_GROUP}" \
  --name "${AZURE_AKS_NAME}" \
  --overwrite-existing

export ACR_LOGIN_SERVER
export OPERATOR_IMAGE="${ACR_LOGIN_SERVER}/neo4j-operator:ci-${GITHUB_SHA:-local}"

log "kubectl context configured for ${AZURE_AKS_NAME}"
kubectl get nodes
