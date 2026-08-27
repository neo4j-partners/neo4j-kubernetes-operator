#!/usr/bin/env bash
# assert/plugins-import — the manual JAR import channel: storage.volumes.plugins mode
# Existing with no spec.plugins. It is the only route the operator supports (no init-container
# hook since 8837095 removed spec.podTemplate; additionalMounts/secretMounts reject /plugins).
#
# Two halves. The wiring — /plugins mounted from the claim, NEO4J_PLUGINS unset,
# server.directories.plugins pointed at it — and then the part that actually matters to a user:
# a JAR put on that claim is loaded and its procedures answer after a restart.
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

# The import itself. A user's JAR is any file they put on the claim; the one JAR available
# without a download is the APOC core the image already ships in /var/lib/neo4j/labs, and the
# entrypoint will not touch it here because NEO4J_PLUGINS is unset — asserted above.
log "[plugins-import] seeding the imported volume with the bundled APOC jar"
kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- \
  bash -c 'cp /var/lib/neo4j/labs/apoc-*-core.jar /plugins/apoc.jar' \
  || die "[plugins-import] could not write /plugins/apoc.jar — an Existing plugins volume must be writable by the neo4j user"

# Plugins load at startup only, so the JAR counts for nothing until the server comes back.
# Deleting the pod is also what proves the file is on the claim and not in the container layer.
log "[plugins-import] restarting ${POD} to pick the imported jar up"
kubectl delete pod "${POD}" -n "${NEO4J_NAMESPACE}" >/dev/null \
  || die "[plugins-import] could not delete ${POD}"

# kubectl wait races the StatefulSet controller recreating pod-0, so retry until it exists.
ready=1
for _ in 1 2 3 4 5 6; do
  if kubectl wait --for=condition=Ready "pod/${POD}" -n "${NEO4J_NAMESPACE}" \
    --timeout="${E2E_ASSERT_TIMEOUT:-300s}" >/dev/null 2>&1; then
    ready=0
    break
  fi
  sleep 5
done
[[ "${ready}" -eq 0 ]] || die "[plugins-import] ${POD} did not become Ready again after the seeding restart"

conn_assert_cypher localhost "${password}" \
  "SHOW PROCEDURES YIELD name WHERE name STARTS WITH 'apoc.' RETURN count(*) > 0 AS ok;" \
  TRUE plugins-import

log "Manual import channel: a JAR placed on the Existing claim at /plugins is loaded and callable"
