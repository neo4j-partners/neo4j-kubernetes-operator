#!/usr/bin/env bash
# Recompute derived Neo4j resource names after case reconciliation.

neo4j_derive_names() {
  # The operator names the workload StatefulSet <cr>-<pool>. Standalone renders a
  # single "server" pool; Cluster renders one StatefulSet per pool (primary, and
  # optionally analytics / read) per BDR-009. Cases that exercise Cluster mode set
  # NEO4J_POOL (e.g. "primary"); everything else keeps the Standalone default.
  export NEO4J_POOL="${NEO4J_POOL:-server}"
  export NEO4J_STS_NAME="${NEO4J_CR_NAME}-${NEO4J_POOL}"
  export NEO4J_AUTH_SECRET="${NEO4J_CR_NAME}-auth"
  # ConfigMap name mirrors the operator's render.Context.ConfigMapName: the "server" pool
  # (Standalone) renders <cr>-config; every other pool renders <cr>-<pool>-config.
  if [[ "${NEO4J_POOL}" == "server" ]]; then
    export NEO4J_CONFIGMAP="${NEO4J_CR_NAME}-config"
  else
    export NEO4J_CONFIGMAP="${NEO4J_CR_NAME}-${NEO4J_POOL}-config"
  fi
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
