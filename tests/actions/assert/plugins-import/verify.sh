#!/usr/bin/env bash
# assert/plugins-import — the manual JAR import channel: storage.volumes.plugins mode
# Existing, with no spec.plugins entry.
#
# This is the only manual-import route the operator supports. spec.podTemplate.initContainers
# is schema-only and never rendered, and additionalMounts/secretMounts reject /plugins as a
# reserved path. So this case pins the channel, not a loaded JAR — the PVC is empty because
# seeding a real JAR needs a Job the harness does not have.
#
# The subPathExpr check is the valuable one. render/storage/volumes.go applies
# shareSubPathExpr to Existing volumes too, so an imported JAR must live at
# <pvc-root>/plugins/<jar>, NOT at the PVC root. Nothing documents that, and a user who
# populates the root sees an empty /plugins. Pinning it here means a fix has to be deliberate.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_AUTH_SECRET
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

storage_wait_ready

STS="statefulset/${NEO4J_STS_NAME}"

# No spec.plugins means the image must not be told to install anything — otherwise the
# entrypoint would write its own JAR over the imported directory.
plugins_env="$(kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.template.spec.containers[?(@.name=="neo4j")].env[?(@.name=="NEO4J_PLUGINS")].value}' 2>/dev/null || true)"
[[ -z "${plugins_env}" ]] \
  || die "NEO4J_PLUGINS is set to '${plugins_env}' with no spec.plugins — the entrypoint would install over the imported volume"

# Imported content must sit under a 'plugins' subpath of the PVC, not at its root.
subpath="$(kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.template.spec.containers[?(@.name=="neo4j")].volumeMounts[?(@.name=="plugins")].subPathExpr}' 2>/dev/null || true)"
[[ "${subpath}" == "plugins" ]] \
  || die "plugins volumeMount subPathExpr is '${subpath:-<unset>}', expected 'plugins' — an imported JAR must live at <pvc-root>/plugins/"

storage_assert_mountpoint /plugins plugins-import

POD="$(storage_pod)"
conn_exec_serverpod() { kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"; }
CONN_EXEC_FN=conn_exec_serverpod
password="$(neo4j_password)"

# The plugins volume alone is enough to point Neo4j at /plugins, with no catalog plugin assigned.
conn_assert_setting localhost "${password}" server.directories.plugins /plugins plugins-import

log "Manual import channel: /plugins mounted from an existing PVC under subPath 'plugins', NEO4J_PLUGINS unset"
