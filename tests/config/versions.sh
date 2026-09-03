#!/usr/bin/env bash
# The Kubernetes and Neo4j versions the e2e harness targets.
#
# Sourced by the harness and, through tests/bin/resolve-versions.sh, by the GitHub workflows, so a
# laptop run and CI cannot disagree about what is under test.
#
# Every value here is a concrete version, moved by hand. Nothing resolves itself at run time: a
# suite that asked a registry for "the newest" could not be re-run on the same version a month
# later, which is exactly what someone reproducing a nightly failure needs. Holding the list still
# is what makes a run reproducible — so the default is simply the newest entry, and there is no
# second "pinned" value to keep in step with it.
#
# The workflows offer the same numbers as dropdowns. GitHub requires `type: choice` options to be
# literal YAML, so those lists — and the fallbacks in the two `run-name:` lines, which GitHub
# evaluates before any job can read this file — are hand-maintained copies. Move a version here
# and in .github/workflows/ci.yml and .github/workflows/e2e-all-platforms.yml in one change.

# ---------------------------------------------------------------- Kubernetes
#
# One value per platform, because the four cannot be given the same number. Each provider offers
# its own set and accepts a different shape, so a single shared version would be wrong somewhere:
#
#   kind  exact published patch. Each kind release builds a handful of kindest/node tags and a
#         plausible-looking value simply 404s — 1.36.0 was never published. Take these from the
#         release notes of the kind version .github/actions/e2e pins, since the binary and the
#         node image are a pair: a kind older than the image fails to load images into it with
#         "unknown containerd config version".
#   AKS   minor accepted, latest patch selected by Azure. Offered 1.31 to 1.36 today.
#   GKE   minor accepted, patch and -gke suffix selected by Google. Rapid channel carries 1.34 to
#         1.36; 1.37 exists only as an alpha preview, on alpha clusters.
#   EKS   minor only — a patch is rejected outright. Standard support covers 1.36, 1.35 and 1.34;
#         below that is extended support, billed per cluster hour.
#
# The ceiling common to all four is therefore 1.36: only kind can run 1.37 today.
export KUBERNETES_VERSION_KIND="${KUBERNETES_VERSION_KIND:-1.37.0}"
export KUBERNETES_VERSION_AKS="${KUBERNETES_VERSION_AKS:-1.36}"
export KUBERNETES_VERSION_GKE="${KUBERNETES_VERSION_GKE:-1.36}"
export KUBERNETES_VERSION_EKS="${KUBERNETES_VERSION_EKS:-1.36}"

# The floor the operator documents — README, the chart's kubeVersion, the installation
# prerequisites. envtest runs the unit tests here rather than on the newest version, so the CRD's
# CEL rules are proved against the oldest Kubernetes the operator claims to support: that is the
# version where a rule using a newer CEL library would fail, and the one no other job exercises.
export KUBERNETES_VERSION_FLOOR="${KUBERNETES_VERSION_FLOOR:-1.35.0}"

# --------------------------------------------------------------------- Neo4j
#
# Both editions are published under the same tag with an `-enterprise` suffix, which is why only
# the version is configurable here and NEO4J_EDITION stays a separate knob.
export NEO4J_VERSION_DEFAULT="${NEO4J_VERSION_DEFAULT:-2026.07.1}"
