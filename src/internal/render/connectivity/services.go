package connectivity

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
)

// HeadlessService builds the StatefulSet headless Service.
func HeadlessService(ctx render.Context) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.HeadlessServiceName(),
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("connectivity"),
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                "None",
			PublishNotReadyAddresses: true,
			Selector:                 ctx.SelectorLabels(),
			Ports: []corev1.ServicePort{
				{Name: "bolt", Port: ctx.BoltPort(), TargetPort: intstrFromInt32(ctx.BoltPort())},
				{Name: "http", Port: ctx.HTTPPort(), TargetPort: intstrFromInt32(ctx.HTTPPort())},
			},
		},
	}
}

// ClientService builds the north-south ClusterIP Service (BDR-007).
func ClientService(ctx render.Context) *corev1.Service {
	svcType := corev1.ServiceTypeClusterIP
	if ctx.Neo4j.Spec.Connectivity != nil && ctx.Neo4j.Spec.Connectivity.Service != nil &&
		ctx.Neo4j.Spec.Connectivity.Service.Type != "" {
		svcType = corev1.ServiceType(ctx.Neo4j.Spec.Connectivity.Service.Type)
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.ClientServiceName(),
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("connectivity"),
		},
		Spec: corev1.ServiceSpec{
			Type:     svcType,
			Selector: ctx.SelectorLabels(),
			Ports: []corev1.ServicePort{
				{Name: "bolt", Port: ctx.BoltPort(), TargetPort: intstrFromInt32(ctx.BoltPort())},
				{Name: "http", Port: ctx.HTTPPort(), TargetPort: intstrFromInt32(ctx.HTTPPort())},
			},
		},
	}
}

func intstrFromInt32(port int32) intstr.IntOrString {
	return intstr.FromInt32(port)
}
