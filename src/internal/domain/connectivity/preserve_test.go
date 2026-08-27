package connectivity

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// Regression for the AWS LBC loadBalancerClass loop: fields the operator doesn't
// model must survive the wholesale Spec overwrite, but only when desired leaves
// them zero-valued.
func TestPreserveServiceServerFields(t *testing.T) {
	nlb := "service.k8s.aws/nlb"
	live := &corev1.ServiceSpec{
		ClusterIP:           "10.0.0.5",
		ClusterIPs:          []string{"10.0.0.5"},
		LoadBalancerClass:   &nlb,
		HealthCheckNodePort: 31234,
		IPFamilies:          []corev1.IPFamily{corev1.IPv4Protocol},
		Ports: []corev1.ServicePort{
			{Name: "tcp-bolt", NodePort: 30001},
		},
	}
	desired := &corev1.ServiceSpec{
		Type:  corev1.ServiceTypeLoadBalancer,
		Ports: []corev1.ServicePort{{Name: "tcp-bolt"}},
	}

	preserveServiceServerFields(live, desired)

	if desired.LoadBalancerClass == nil || *desired.LoadBalancerClass != nlb {
		t.Fatalf("loadBalancerClass not preserved: %v", desired.LoadBalancerClass)
	}
	if desired.ClusterIP != "10.0.0.5" {
		t.Fatalf("clusterIP not preserved: %q", desired.ClusterIP)
	}
	if desired.HealthCheckNodePort != 31234 {
		t.Fatalf("healthCheckNodePort not preserved: %d", desired.HealthCheckNodePort)
	}
	if len(desired.Ports) != 1 || desired.Ports[0].NodePort != 30001 {
		t.Fatalf("nodePort not preserved: %+v", desired.Ports)
	}

	// Operator-managed values must win over live ones.
	other := "other"
	desired2 := &corev1.ServiceSpec{
		Type:              corev1.ServiceTypeLoadBalancer,
		ClusterIP:         "None",
		LoadBalancerClass: &other,
		Ports:             []corev1.ServicePort{{Name: "tcp-bolt", NodePort: 32222}},
	}
	preserveServiceServerFields(live, desired2)
	if desired2.ClusterIP != "None" {
		t.Fatalf("desired clusterIP overwritten: %q", desired2.ClusterIP)
	}
	if *desired2.LoadBalancerClass != other {
		t.Fatalf("desired loadBalancerClass overwritten: %q", *desired2.LoadBalancerClass)
	}
	if desired2.Ports[0].NodePort != 32222 {
		t.Fatalf("desired nodePort overwritten: %d", desired2.Ports[0].NodePort)
	}
}

// A LoadBalancer -> ClusterIP downgrade must NOT carry LoadBalancer/NodePort-scoped
// fields forward, or the API server rejects them ("may only be set when type is
// 'LoadBalancer'") — swapping one reconcile loop for another.
func TestPreserveServiceServerFields_TypeDowngrade(t *testing.T) {
	nlb := "service.k8s.aws/nlb"
	local := corev1.ServiceExternalTrafficPolicyLocal
	live := &corev1.ServiceSpec{
		Type:                  corev1.ServiceTypeLoadBalancer,
		ClusterIP:             "10.0.0.5",
		LoadBalancerClass:     &nlb,
		HealthCheckNodePort:   31234,
		ExternalTrafficPolicy: local,
		Ports:                 []corev1.ServicePort{{Name: "tcp-bolt", NodePort: 30001}},
	}
	desired := &corev1.ServiceSpec{
		Type:  corev1.ServiceTypeClusterIP,
		Ports: []corev1.ServicePort{{Name: "tcp-bolt"}},
	}

	preserveServiceServerFields(live, desired)

	if desired.LoadBalancerClass != nil {
		t.Fatalf("loadBalancerClass leaked onto ClusterIP service: %v", *desired.LoadBalancerClass)
	}
	if desired.HealthCheckNodePort != 0 {
		t.Fatalf("healthCheckNodePort leaked onto ClusterIP service: %d", desired.HealthCheckNodePort)
	}
	if desired.ExternalTrafficPolicy != "" {
		t.Fatalf("externalTrafficPolicy leaked onto ClusterIP service: %q", desired.ExternalTrafficPolicy)
	}
	if desired.Ports[0].NodePort != 0 {
		t.Fatalf("nodePort leaked onto ClusterIP service: %d", desired.Ports[0].NodePort)
	}
	// clusterIP is valid for ClusterIP too, so it is still preserved.
	if desired.ClusterIP != "10.0.0.5" {
		t.Fatalf("clusterIP not preserved on downgrade: %q", desired.ClusterIP)
	}
}

// externalIPs is a security-sensitive field (CVE-2020-8554) the operator does not
// model; it must be left for the wholesale overwrite to clear, not preserved.
func TestPreserveServiceServerFields_ExternalIPsNotPreserved(t *testing.T) {
	live := &corev1.ServiceSpec{
		Type:        corev1.ServiceTypeLoadBalancer,
		ExternalIPs: []string{"203.0.113.5"},
	}
	desired := &corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer}

	preserveServiceServerFields(live, desired)

	if len(desired.ExternalIPs) != 0 {
		t.Fatalf("externalIPs should not be preserved: %v", desired.ExternalIPs)
	}
}
