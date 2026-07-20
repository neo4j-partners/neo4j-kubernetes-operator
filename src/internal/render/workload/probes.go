package workload

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

// applyProbes sets startup/readiness/liveness on the Neo4j container.
// Unset fields keep operator defaults (NEO-3-009-PROBE-01); set fields replace that probe (PROBE-02).
func applyProbes(ctx render.Context, container *corev1.Container) {
	container.StartupProbe = boltTCPProbe(ctx, 1000, 5, 10)
	container.ReadinessProbe = boltTCPProbe(ctx, 20, 5, 10)
	container.LivenessProbe = boltTCPProbe(ctx, 40, 5, 10)

	p := ctx.Neo4j.Spec.Probes
	if p == nil {
		return
	}
	if p.Startup != nil {
		container.StartupProbe = p.Startup.DeepCopy()
	}
	if p.Readiness != nil {
		container.ReadinessProbe = p.Readiness.DeepCopy()
	}
	if p.Liveness != nil {
		container.LivenessProbe = p.Liveness.DeepCopy()
	}
}

// boltTCPProbe matches Helm defaults (tcpSocket on Bolt); cluster formation can take many minutes.
func boltTCPProbe(ctx render.Context, failureThreshold, periodSeconds, timeoutSeconds int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(ctx.BoltPort())},
		},
		FailureThreshold: failureThreshold,
		PeriodSeconds:    periodSeconds,
		TimeoutSeconds:   timeoutSeconds,
	}
}
