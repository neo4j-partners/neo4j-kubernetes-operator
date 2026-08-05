#!/usr/bin/env bash
# Operator base pins — see operator/base.yaml

export OPERATOR_CRD="${OPERATOR_CRD:-neo4js.neo4j.com}"
export OPERATOR_NAMESPACE="${OPERATOR_NAMESPACE:-neo4j-operator-system}"
export OPERATOR_DEPLOYMENT="${OPERATOR_DEPLOYMENT:-neo4j-operator-controller-manager}"
export OPERATOR_LABEL_SELECTOR="${OPERATOR_LABEL_SELECTOR:-app.kubernetes.io/name=neo4j-operator}"
export OPERATOR_MANAGER_IMAGE="${OPERATOR_MANAGER_IMAGE:-controller:latest}"

export OPERATOR_CRD_MANIFEST="${OPERATOR_CRD_MANIFEST:-config/crd/bases/neo4j.com_neo4js.yaml}"
export OPERATOR_NAMESPACE_MANIFEST="${OPERATOR_NAMESPACE_MANIFEST:-config/default/namespace.yaml}"
export OPERATOR_RBAC_KUSTOMIZE="${OPERATOR_RBAC_KUSTOMIZE:-config/rbac}"
export OPERATOR_MANAGER_KUSTOMIZE="${OPERATOR_MANAGER_KUSTOMIZE:-config/manager}"

export E2E_OPERATOR_TIMEOUT="${E2E_OPERATOR_TIMEOUT:-180s}"
