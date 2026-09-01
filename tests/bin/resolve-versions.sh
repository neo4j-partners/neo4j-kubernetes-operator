#!/usr/bin/env bash
# Turn the version selectors the workflows pass into the concrete Kubernetes and Neo4j versions a
# run tests, and hand them to the steps that follow.
#
# A selector is `pinned`, `latest`, an explicit version, or empty — empty meaning `pinned`, so a
# push or pull request that sets nothing gets the reproducible pair. The mapping lives here rather
# than in a workflow expression for two reasons: one copy serves both workflows, and GitHub's
# `a && b || c` idiom silently returns `c` whenever `b` is an empty string, which is exactly the
# case this has to get right.
#
# Outside Actions it prints what it would pick and changes nothing, which is how to check a
# selector before dispatching a run.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../config/versions.sh
source "${REPO_ROOT}/tests/config/versions.sh"

resolve() {
  local selector=$1 pinned=$2 latest=$3
  case "${selector}" in
    "" | pinned) printf '%s' "${pinned}" ;;
    latest) printf '%s' "${latest}" ;;
    *) printf '%s' "${selector}" ;;
  esac
}

kubernetes="$(resolve "${KUBERNETES_VERSION_SELECTOR:-}" "${KUBERNETES_VERSION_PINNED}" "${KUBERNETES_VERSION_LATEST}")"
neo4j="$(resolve "${NEO4J_VERSION_SELECTOR:-}" "${NEO4J_VERSION_PINNED}" "${NEO4J_VERSION_LATEST}")"

# kindest/node tags carry a leading v and Kubernetes versions are written both ways, so accept
# either and store the bare number the cloud profile expects.
kubernetes="${kubernetes#v}"

# Only kind takes its Kubernetes version from here. AKS, GKE and EKS clusters are created — and
# then reused across runs — by their ensure script, so a selector could not move their version
# without recreating the cluster; printing one anyway would put a number in the log that nothing
# in the run honours.
if [[ "${E2E_CLOUD:-local-kind}" == "local-kind" ]]; then
  echo "Kubernetes ${kubernetes} (selector: ${KUBERNETES_VERSION_SELECTOR:-pinned})"
else
  echo "Kubernetes owned by ${E2E_CLOUD} — its ensure script fixes the cluster version"
  kubernetes=""
fi
echo "Neo4j      ${neo4j} (selector: ${NEO4J_VERSION_SELECTOR:-pinned})"

# GITHUB_ENV is how one step hands values to the next. Everything downstream — the cloud profile's
# node image, the fixtures' spec.version, the image CI pre-pulls — reads these through a `:-`
# default, so setting them here overrides the pins without touching a file.
# `if` rather than `[[ ]] && echo`: a false test makes the list exit non-zero, which under
# `set -e` would end the script here instead of falling through to the Neo4j line.
if [[ -n "${GITHUB_ENV:-}" ]]; then
  if [[ -n "${kubernetes}" ]]; then
    echo "KUBERNETES_VERSION=${kubernetes}" >>"${GITHUB_ENV}"
  fi
  echo "NEO4J_VERSION=${neo4j}" >>"${GITHUB_ENV}"
fi
