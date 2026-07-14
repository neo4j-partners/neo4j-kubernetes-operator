package connectivity

import (
	"fmt"

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
			Ports:                    connectorServicePorts(ctx, ctx.ClientExpose(), false),
		},
	}
}

// ClientService builds the north-south client Service (BDR-007).
func ClientService(ctx render.Context) *corev1.Service {
	svcType := corev1.ServiceTypeClusterIP
	if ctx.Neo4j.Spec.Connectivity != nil && ctx.Neo4j.Spec.Connectivity.Service != nil &&
		ctx.Neo4j.Spec.Connectivity.Service.Type != "" {
		svcType = corev1.ServiceType(ctx.Neo4j.Spec.Connectivity.Service.Type)
	}

	spec := corev1.ServiceSpec{
		Type:                     svcType,
		Selector:                 ctx.SelectorLabels(),
		Ports:                    connectorServicePorts(ctx, ctx.ClientExpose(), true),
		LoadBalancerSourceRanges: ctx.ClientLoadBalancerSourceRanges(),
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ctx.ClientServiceName(),
			Namespace:   ctx.Namespace(),
			Labels:      ctx.CommonLabels("connectivity"),
			Annotations: ctx.ClientServiceAnnotations(),
		},
		Spec: spec,
	}
}

// AdminService builds the derived admin Service (publishNotReadyAddresses) — BDR-007.
func AdminService(ctx render.Context) *corev1.Service {
	labels := ctx.CommonLabels("connectivity")
	labels[render.LabelServiceRole] = render.ServiceRoleAdmin
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.AdminServiceName(),
			Namespace: ctx.Namespace(),
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeClusterIP,
			PublishNotReadyAddresses: true,
			Selector:                 ctx.SelectorLabels(),
			Ports:                    connectorServicePorts(ctx, ctx.AdminExpose(), false),
		},
	}
}

// MemberClientService builds a per-pod client Service for advertised bolt addresses.
// ponytail: Helm emits one client Service per single-replica release; multi-replica STS needs
// one Service per ordinal so server.bolt.advertised_address matches K8s DNS. Not a BDR-007
// public API field — operator-derived only.
func MemberClientService(ctx render.Context, podName string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ctx.Namespace(),
			Labels:    ctx.CommonLabels("connectivity"),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: memberPodSelectorLabels(ctx, podName),
			Ports:    connectorServicePorts(ctx, ctx.ClientExpose(), false),
		},
	}
}

// MemberInternalsService builds a per-pod internals Service for K8S cluster discovery (BDR-007).
func MemberInternalsService(ctx render.Context, podName string) *corev1.Service {
	labels := ctx.CommonLabels("connectivity")
	labels[render.LabelServiceRole] = render.ServiceRoleInternals
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctx.MemberInternalsServiceName(podName),
			Namespace: ctx.Namespace(),
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeClusterIP,
			PublishNotReadyAddresses: true,
			Selector:                 memberPodSelectorLabels(ctx, podName),
			Ports:                    internalsServicePorts(ctx),
		},
	}
}

// ClusterMemberServices returns per-pod client and internals Services for all replicas in a pool.
func ClusterMemberServices(ctx render.Context) []*corev1.Service {
	replicas := ctx.PoolReplicas()
	if replicas <= 0 {
		return nil
	}
	services := make([]*corev1.Service, 0, replicas*2)
	for ord := int32(0); ord < replicas; ord++ {
		podName := ctx.PodName(ord)
		services = append(services,
			MemberClientService(ctx, podName),
			MemberInternalsService(ctx, podName),
		)
	}
	return services
}

func memberPodSelectorLabels(ctx render.Context, podName string) map[string]string {
	return map[string]string{
		render.LabelInstance:                 ctx.Name(),
		render.LabelPool:                     string(ctx.Pool),
		"statefulset.kubernetes.io/pod-name": podName,
	}
}

func connectorServicePorts(ctx render.Context, connectors []string, useFacade bool) []corev1.ServicePort {
	ports := make([]corev1.ServicePort, 0, len(connectors))
	for _, name := range connectors {
		target := ctx.ListenerPort(name)
		if target == 0 {
			continue
		}
		port := target
		if useFacade {
			port = ctx.ServiceFacadePort(name)
		}
		ports = append(ports, corev1.ServicePort{
			Name:       render.ServicePortName(name),
			Port:       port,
			TargetPort: intstr.FromInt32(target),
			Protocol:   corev1.ProtocolTCP,
		})
	}
	return ports
}

func internalsServicePorts(ctx render.Context) []corev1.ServicePort {
	// Enabled client/ops connectors plus fixed cluster ports (BDR-007).
	ports := connectorServicePorts(ctx, ctx.AdminExpose(), false)
	ports = append(ports,
		corev1.ServicePort{Name: "tcp-boltrouting", Port: 7688, TargetPort: intstr.FromInt32(7688), Protocol: corev1.ProtocolTCP},
		corev1.ServicePort{Name: "tcp-discovery", Port: 5000, TargetPort: intstr.FromInt32(5000), Protocol: corev1.ProtocolTCP},
		corev1.ServicePort{Name: "tcp-raft", Port: 7000, TargetPort: intstr.FromInt32(7000), Protocol: corev1.ProtocolTCP},
		corev1.ServicePort{Name: "tcp-tx", Port: 6000, TargetPort: intstr.FromInt32(6000), Protocol: corev1.ProtocolTCP},
	)
	return ports
}

// MemberServiceFQDN returns the cluster DNS name for a member client Service.
func MemberServiceFQDN(ctx render.Context, podName string) string {
	return fmt.Sprintf("%s.%s.svc.%s", podName, ctx.Namespace(), ctx.ClusterDomain())
}
