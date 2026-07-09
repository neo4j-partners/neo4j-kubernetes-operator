#!/usr/bin/env bash
# Cloud profile: Azure AKS (CI and manual runs).

CLOUD_ID=azure-aks
STORAGE_CLASS_NAME="${STORAGE_CLASS_NAME:-managed-csi}"
# Set by tests/azure/ensure-aks.sh before run-e2e when not provided.
OPERATOR_IMAGE="${OPERATOR_IMAGE:-}"

AZURE_RESOURCE_GROUP="${AZURE_RESOURCE_GROUP:-neo4j-operator-ci-rg}"
AZURE_LOCATION="${AZURE_LOCATION:-westeurope}"
AZURE_AKS_NAME="${AZURE_AKS_NAME:-neo4j-operator-ci-aks}"
AZURE_ACR_NAME="${AZURE_ACR_NAME:-neo4joperatorci}"
AZURE_AKS_NODE_COUNT="${AZURE_AKS_NODE_COUNT:-2}"
AZURE_AKS_NODE_VM_SIZE="${AZURE_AKS_NODE_VM_SIZE:-Standard_D4s_v3}"
