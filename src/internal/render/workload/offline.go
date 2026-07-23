package workload

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

const offlineMaintenanceCommand = "while true; do echo 'Neo4j is not running. Pod is in offline maintenance mode. Set spec.maintenance.offlineMode: false and re-apply the Neo4j CR to resume.'; sleep 60; done"

// applyOfflineMaintenance replaces the Neo4j process with a sleep loop (Helm
// neo4j.offlineMaintenanceModeEnabled parity). Liveness/startup are cleared so
// kubelet does not restart the sleep container; readiness stays on Bolt so the
// pod stays NotReady and is removed from Service endpoints.
func applyOfflineMaintenance(ctx render.Context, container *corev1.Container, podSpec *corev1.PodSpec) {
	if !ctx.OfflineModeEnabled() {
		return
	}
	container.Command = []string{"bash", "-c", offlineMaintenanceCommand}
	container.StartupProbe = nil
	container.LivenessProbe = nil
	zero := int64(0)
	podSpec.TerminationGracePeriodSeconds = &zero
}
