#!/usr/bin/env bash
# Recompute derived Neo4j resource names after case reconciliation.

neo4j_derive_names() {
  export NEO4J_STS_NAME="${NEO4J_CR_NAME}-server"
  export NEO4J_AUTH_SECRET="${NEO4J_CR_NAME}-auth"
  export NEO4J_CONFIGMAP="${NEO4J_CR_NAME}-config"
  export NEO4J_CLIENT_SVC="${NEO4J_CR_NAME}"
}

neo4j_apply_storage_class_flag() {
  if [[ "${NEO4J_USE_STORAGE_CLASS:-false}" == "true" ]]; then
    if [[ -z "${STORAGE_CLASS_NAME:-}" ]]; then
      echo "neo4j case ${NEO4J_CASE_NAME} requires STORAGE_CLASS_NAME from cloud profile" >&2
      return 1
    fi
  fi
}
