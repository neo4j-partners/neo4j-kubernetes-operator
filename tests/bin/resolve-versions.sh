#!/usr/bin/env bash
# Decide which Kubernetes and Neo4j versions a run tests, and hand them to the steps that follow.
#
# The workflows pass a version straight from a dropdown, or nothing at all — nothing being the
# scheduled nightly and every push and pull request, none of which fill in a form. Empty means
# "this platform's default", which tests/config/versions.sh holds one of per platform, because
# kind, AKS, GKE and EKS cannot be given the same number.
#
# The mapping lives here rather than in a workflow expression so one copy serves both workflows,
# and so a laptop run reaches the same answer as CI.
#
# Outside Actions it prints what it would pick and changes nothing, which is how to check a
# dropdown value before dispatching a run.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../config/versions.sh
source "${REPO_ROOT}/tests/config/versions.sh"

cloud="${E2E_CLOUD:-local-kind}"

case "${cloud}" in
  local-kind) default_kubernetes="${KUBERNETES_VERSION_KIND}" ;;
  azure-aks) default_kubernetes="${KUBERNETES_VERSION_AKS}" ;;
  gcp-gke) default_kubernetes="${KUBERNETES_VERSION_GKE}" ;;
  aws-eks) default_kubernetes="${KUBERNETES_VERSION_EKS}" ;;
  *)
    echo "unknown platform ${cloud} — expected local-kind, azure-aks, gcp-gke or aws-eks" >&2
    exit 1
    ;;
esac

kubernetes="${KUBERNETES_VERSION_INPUT:-${default_kubernetes}}"
neo4j="${NEO4J_VERSION_INPUT:-${NEO4J_VERSION_DEFAULT}}"

# kindest/node tags carry a leading v and Kubernetes versions are written both ways, so accept
# either and store the bare number every platform expects.
kubernetes="${kubernetes#v}"

# The dropdowns once offered the words `pinned` and `latest`. They no longer do, and a caller still
# passing one would otherwise reach the cluster as a version string and fail somewhere far from
# here — `kindest/node:vpinned` is a pull error, not an explanation.
if [[ ! "${kubernetes}" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
  echo "Kubernetes version ${kubernetes} is not a version number — the selectors were removed, pass 1.36 or 1.37.0" >&2
  exit 1
fi
if [[ ! "${neo4j}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Neo4j version ${neo4j} is not a version number — the selectors were removed, pass 2026.07.1 or 5.26.29" >&2
  exit 1
fi

# `if` rather than `[[ ]] && assignment`: under `set -e` a false test makes the list exit
# non-zero, ending the script two lines before it prints anything.
if [[ -n "${KUBERNETES_VERSION_INPUT:-}" ]]; then
  origin_k8s="requested"
else
  origin_k8s="default for ${cloud}"
fi
if [[ -n "${NEO4J_VERSION_INPUT:-}" ]]; then
  origin_neo4j="requested"
else
  origin_neo4j="default"
fi

echo "Kubernetes ${kubernetes} (${origin_k8s})"
echo "Neo4j      ${neo4j} (${origin_neo4j})"

# GITHUB_ENV is how one step hands values to the next. Everything downstream — the kind node image,
# the cloud ensure scripts, the fixtures' spec.version, the image CI pre-pulls — reads these
# through a `:-` default, so setting them here overrides the file without touching it.
# `if` rather than `[[ ]] && echo`: a false test makes the list exit non-zero, which under
# `set -e` would end the script here instead of falling through to the next line.
if [[ -n "${GITHUB_ENV:-}" ]]; then
  echo "KUBERNETES_VERSION=${kubernetes}" >>"${GITHUB_ENV}"
  echo "NEO4J_VERSION=${neo4j}" >>"${GITHUB_ENV}"
fi

# One line on the run page, for the workflows that opt in. A run's name is evaluated before any job
# starts, so it can only carry what the dropdowns said; with `all` selected it cannot name four
# different Kubernetes versions at once. This is where each platform records the number it ran.
if [[ "${VERSIONS_JOB_SUMMARY:-false}" == "true" && -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  echo "\`${cloud}\` — Kubernetes \`${kubernetes}\` · Neo4j \`${neo4j}\`" >>"${GITHUB_STEP_SUMMARY}"
fi
