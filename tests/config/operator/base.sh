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

# Helm install path (OP-2-001-PKG-02), used by the multi-namespace scope suite.
# The release gets its own namespace: Helm refuses to adopt objects it does not own, so a
# release in OPERATOR_NAMESPACE would collide with the kustomize install other suites apply
# there on a shared cluster.
export OPERATOR_HELM_RELEASE="${OPERATOR_HELM_RELEASE:-neo4j-operator}"
export OPERATOR_HELM_NAMESPACE="${OPERATOR_HELM_NAMESPACE:-neo4j-operator-scope}"
export OPERATOR_HELM_CHART="${OPERATOR_HELM_CHART:-charts/neo4j-operator}"

# Watched namespaces for OP-2-001-SCOPE-02. Dedicated names, never "default": the Role there
# belongs to the kustomize install and Helm would refuse to take it over.
export E2E_SCOPE_NAMESPACES="${E2E_SCOPE_NAMESPACES:-e2e-scope-a,e2e-scope-b}"
