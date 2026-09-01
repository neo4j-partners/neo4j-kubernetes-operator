#!/usr/bin/env bash
# The Kubernetes and Neo4j versions the e2e harness targets.
#
# Sourced by the harness and, through tests/bin/resolve-versions.sh, by the GitHub workflows, so a
# laptop run and CI cannot disagree about what is under test.
#
# PINNED is what every pull request and push runs. Holding it still is the point: a red check then
# means the change under review broke something, rather than an upstream release having landed
# overnight on a branch nobody touched.
#
# LATEST is what the nightly all-platforms run exercises, so drift against a new Kubernetes or
# Neo4j shows up within a day, on a run no one is waiting on. Promote by copying LATEST over
# PINNED once the nightly has been green on it.
#
# Both workflows also offer these as dropdowns. GitHub requires `type: choice` options to be
# literal YAML, so those option lists are hand-maintained copies of the versions below — move a
# pin here and add the value there in the same change.

# PINNED stays on 1.35, the floor the operator documents (README, chart kubeVersion, envtest,
# prerequisites), so the promise and the tested version are the same number — and on its first
# patch, which is the strongest test of that floor and the one every kind release still boots.
#
# Patch levels are not free-form. kind publishes only a few kindest/node tags per minor, so a
# plausible-looking value can simply 404 when the cluster is created: v1.36.0 was never published
# at all. Take newer entries from the release notes of the kind version .github/actions/e2e pins,
# because the binary and the node image are a pair — a kind older than the image fails to load
# images into it with "unknown containerd config version", which is why the floor stays on a patch
# that predates the split.
export KUBERNETES_VERSION_PINNED="${KUBERNETES_VERSION_PINNED:-1.35.0}"
export KUBERNETES_VERSION_LATEST="${KUBERNETES_VERSION_LATEST:-1.37.0}"

# Both editions are published under the same tag with an `-enterprise` suffix, which is why only
# the version is configurable here and NEO4J_EDITION stays a separate knob.
export NEO4J_VERSION_PINNED="${NEO4J_VERSION_PINNED:-2026.05.0}"
export NEO4J_VERSION_LATEST="${NEO4J_VERSION_LATEST:-2026.07.1}"
