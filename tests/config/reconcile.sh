#!/usr/bin/env bash
# Reconcile final e2e config: profile + operator case + neo4j case + cloud.

set -euo pipefail

CONFIG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=operator/base.sh
source "${CONFIG_DIR}/operator/base.sh"
# shellcheck source=neo4j/base.sh
source "${CONFIG_DIR}/neo4j/base.sh"
# shellcheck source=derive.sh
source "${CONFIG_DIR}/derive.sh"

_config_source_case() {
  local domain=$1
  local case_name=$2
  local path="${CONFIG_DIR}/${domain}/cases/${case_name}.sh"
  [[ -f "${path}" ]] || {
    echo "unknown ${domain} case: ${case_name} (expected ${path})" >&2
    return 1
  }
  # shellcheck source=/dev/null
  source "${path}"
}

# Clouds whose nodes pull the operator image from a registry (ACR, Artifact Registry, ECR) rather
# than having it loaded into them. They share an operator case, an OPERATOR_IMAGE requirement, and
# a provisioning script that sets it — so the distinction is worth naming once.
_config_cloud_uses_registry() {
  case "${CLOUD_ID:-}" in
    azure-aks | gcp-gke | aws-eks) return 0 ;;
    *) return 1 ;;
  esac
}

_config_happy_path_operator_case() {
  if _config_cloud_uses_registry; then
    export OPERATOR_CASE=registry-image
    return 0
  fi
  case "${CLOUD_ID:-}" in
    local-kind) export OPERATOR_CASE=local-image ;;
    *) export OPERATOR_CASE=default ;;
  esac
}

_config_align_operator_case_to_cloud() {
  # `if` rather than `[[ ]] &&`: a false test would make the function return non-zero and
  # abort the caller under `set -e`, with no message to explain the exit.
  if _config_cloud_uses_registry; then
    if [[ "${OPERATOR_CASE}" == "local-image" ]]; then
      export OPERATOR_CASE=registry-image
    fi
    return 0
  fi
  case "${CLOUD_ID:-}" in
    local-kind)
      if [[ "${OPERATOR_CASE}" == "registry-image" ]]; then
        export OPERATOR_CASE=local-image
      fi
      ;;
  esac
}

# Operator cases that make sense on each cloud (skip registry-image on kind, etc.).
_config_operator_cases_for_cloud() {
  if _config_cloud_uses_registry; then
    printf '%s\n' default registry-image
    return 0
  fi
  case "${CLOUD_ID:-}" in
    local-kind) printf '%s\n' default local-image ;;
    *) printf '%s\n' default local-image registry-image ;;
  esac
}

# Neo4j cases compatible with a scenario (extend when cluster scenarios land).
_config_neo4j_cases_for_scenario() {
  local scenario=${1:-workload-standalone}
  case "${scenario}" in
    workload-standalone)
      printf '%s\n' standalone-minimal standalone-storage-class standalone-named-cr
      ;;
    *)
      echo "unknown scenario for neo4j case matrix: ${scenario}" >&2
      return 1
      ;;
  esac
}

_config_reset_case_vars() {
  unset NEO4J_CR_NAME NEO4J_DATA_SIZE NEO4J_USE_STORAGE_CLASS NEO4J_CASE_NAME
  unset OPERATOR_CASE_NAME OPERATOR_IMAGE_PULL_POLICY
  # Cluster-only knobs — cleared so a Cluster case never leaks its pool/member
  # count into a later Standalone case (derive.sh then falls back to "server").
  # The database expectations matter just as much: a case inheriting the previous one's
  # primary count or resize targets asserts the wrong shape, or resizes a cluster that
  # was never meant to be resized.
  unset NEO4J_POOL CLUSTER_EXPECTED_MEMBERS CLUSTER_EXPECTED_DB_PRIMARIES CLUSTER_DEFAULT_DB
  unset CLUSTER_SCALE_OUT_MEMBERS CLUSTER_SCALE_IN_MEMBERS CLUSTER_SCALE_DB CLUSTER_SCALE_TIMEOUT
  unset CLUSTER_SCALE_STABLE_SECONDS
  # Credentials knobs — cases run in one shell, so without this a case reading
  # ${AUTH_KNOWN_PASSWORD:-default} silently inherits the previous case's password and
  # asserts the wrong thing (the same holds for the expected Secret name and reason).
  unset AUTH_SECRET_CREATE AUTH_SECRET_NAME AUTH_KNOWN_PASSWORD AUTH_SECRET_LABELS
  unset CRED_EXPECT_GENERATED CRED_EXPECT_SECRET EXPECT_REASON
}

load_cloud_config() {
  local cloud=${1:-}
  [[ -n "${cloud}" ]] || return 0

  local profile="${CONFIG_DIR}/cloud/${cloud}.sh"
  [[ -f "${profile}" ]] || {
    echo "unknown cloud profile: ${cloud} (expected ${profile})" >&2
    return 1
  }
  # shellcheck source=/dev/null
  source "${profile}"
  export CLOUD="${cloud}"
  export CLOUD_ID="${CLOUD_ID:-${cloud}}"
}

_reconcile_apply() {
  local profile=${1:-happy-path}

  if [[ "${profile}" == "happy-path" ]]; then
    _config_happy_path_operator_case
  else
    _config_align_operator_case_to_cloud
  fi

  : "${OPERATOR_CASE:?OPERATOR_CASE not set}"
  : "${NEO4J_CASE:?NEO4J_CASE not set}"

  if _config_cloud_uses_registry && [[ -z "${OPERATOR_IMAGE:-}" ]]; then
    echo "OPERATOR_IMAGE must be set for ${CLOUD_ID} (run the cloud's ensure script first)" >&2
    return 1
  fi

  _config_reset_case_vars
  _config_source_case operator "${OPERATOR_CASE}"
  _config_source_case neo4j "${NEO4J_CASE}"

  neo4j_apply_storage_class_flag
  neo4j_derive_names

  export E2E_CONFIG_SUMMARY="profile=${E2E_PROFILE} cloud=${CLOUD_ID} operator=${OPERATOR_CASE_NAME} neo4j=${NEO4J_CASE_NAME} cr=${NEO4J_CR_NAME}"
}

# Print one line per combination: "<operator-case> <neo4j-case>".
reconcile_list_combinations() {
  local cloud=${1:-}
  local scenario=${2:-workload-standalone}

  load_cloud_config "${cloud}"

  local op neo4j
  while IFS= read -r op; do
    [[ -n "${op}" ]] || continue
    while IFS= read -r neo4j; do
      [[ -n "${neo4j}" ]] || continue
      printf '%s %s\n' "${op}" "${neo4j}"
    done < <(_config_neo4j_cases_for_scenario "${scenario}")
  done < <(_config_operator_cases_for_cloud)
}

reconcile_config_for_cases() {
  local cloud=${1:-}
  local operator_case=${2:-}
  local neo4j_case=${3:-}
  local profile=${4:-matrix}

  export E2E_PROFILE="${profile}"
  export OPERATOR_CASE="${operator_case}"
  export NEO4J_CASE="${neo4j_case}"

  load_cloud_config "${cloud}"
  _reconcile_apply "${profile}"
}

reconcile_config() {
  local profile=${1:-happy-path}
  local cloud=${2:-}

  export E2E_PROFILE="${profile}"

  case "${profile}" in
    happy-path)
      # shellcheck source=profiles/happy-path.sh
      source "${CONFIG_DIR}/profiles/happy-path.sh"
      ;;
    explicit)
      : "${OPERATOR_CASE:?OPERATOR_CASE required for E2E_PROFILE=explicit}"
      : "${NEO4J_CASE:?NEO4J_CASE required for E2E_PROFILE=explicit}"
      ;;
    matrix)
      echo "E2E_PROFILE=matrix runs all combinations in run-e2e.sh (do not call reconcile_config directly)" >&2
      return 1
      ;;
    *)
      echo "unknown e2e profile: ${profile} (use happy-path, matrix, or explicit)" >&2
      return 1
      ;;
  esac

  load_cloud_config "${cloud}"
  _reconcile_apply "${profile}"
}
