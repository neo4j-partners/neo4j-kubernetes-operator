#!/usr/bin/env bash
# assert/plugins-import — the manual JAR import channel: storage.volumes.plugins mode
# Existing with no spec.plugins. It is the only route the operator supports (no init-container
# hook since 8837095 removed spec.podTemplate; additionalMounts/secretMounts reject /plugins).
# Proves the channel, not a loaded JAR — the PVC is empty, seeding it needs a Job we lack.
#
# The subPathExpr check matters: render/storage/volumes.go applies the Share subPath to
# Existing volumes too, so an imported JAR must sit at <pvc-root>/plugins/, not the PVC root.
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
container='.spec.template.spec.containers[?(@.name=="neo4j")]'

# No spec.plugins means the entrypoint must not be told to install anything, or it would
# write its own JAR over the imported directory.
plugins_env="$(kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
  -o "jsonpath={${container}.env[?(@.name=='NEO4J_PLUGINS')].value}" 2>/dev/null || true)"
[[ -z "${plugins_env}" ]] \
  || die "NEO4J_PLUGINS is '${plugins_env}' with no spec.plugins — the entrypoint would install over the imported volume"

subpath="$(kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
  -o "jsonpath={${container}.volumeMounts[?(@.name=='plugins')].subPathExpr}" 2>/dev/null || true)"
[[ "${subpath}" == "plugins" ]] \
  || die "plugins volumeMount subPathExpr is '${subpath:-<unset>}', expected 'plugins' — an imported JAR must live at <pvc-root>/plugins/"

storage_assert_mountpoint /plugins plugins-import

POD="$(storage_pod)"
conn_exec_serverpod() { kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"; }
CONN_EXEC_FN=conn_exec_serverpod
password="$(neo4j_password)"

# The volume alone points Neo4j at /plugins, with no catalog plugin assigned.
conn_assert_setting localhost "${password}" server.directories.plugins /plugins plugins-import

log "Manual import channel: /plugins from an existing PVC under subPath 'plugins', NEO4J_PLUGINS unset"
