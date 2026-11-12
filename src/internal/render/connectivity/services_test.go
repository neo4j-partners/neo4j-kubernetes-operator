package connectivity

import (
	"testing"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMemberInternalsService(t *testing.T) {
	neo4j := &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1.Neo4jSpec{
			Topology: neo4jv1.TopologySpec{
				Mode:      neo4jv1.TopologyModeCluster,
				Primaries: &neo4jv1.PrimariesSpec{Members: 3},
			},
		},
	}
	ctx := render.ContextForPool(neo4j, render.PoolPrimary)
	svc := MemberInternalsService(ctx, "prod-primary-0")
	if svc.Name != "prod-primary-0-internals" {
		t.Fatalf("internals service name = %q", svc.Name)
	}
	if !svc.Spec.PublishNotReadyAddresses {
		t.Fatal("internals service must publish not-ready addresses")
	}
	if svc.Labels[render.LabelServiceRole] != render.ServiceRoleInternals {
		t.Fatalf("internals service role label = %q", svc.Labels[render.LabelServiceRole])
	}
	if svc.Labels[render.LabelClustering] != "true" {
		t.Fatalf("primary internals clustering label = %q, want true", svc.Labels[render.LabelClustering])
	}
	ports := map[string]int32{}
	for _, p := range svc.Spec.Ports {
		ports[p.Name] = p.Port
	}
	for name, port := range map[string]int32{
		"tcp-bolt": 7687, "tcp-http": 7474, "tcp-boltrouting": 7688,
		"tcp-discovery": 5000, "tcp-raft": 7000, "tcp-tx": 6000,
	} {
		if ports[name] != port {
			t.Fatalf("port %s = %d, want %d", name, ports[name], port)
		}
	}

	analytics := MemberInternalsService(render.ContextForPool(neo4j, render.PoolAnalytics), "prod-analytics-0")
	if analytics.Labels[render.LabelClustering] != "false" {
		t.Fatalf("analytics internals clustering label = %q, want false", analytics.Labels[render.LabelClustering])
	}
}

func TestClusterMemberServicesCount(t *testing.T) {
	neo4j := &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1.Neo4jSpec{
			Topology: neo4jv1.TopologySpec{
				Mode:      neo4jv1.TopologyModeCluster,
				Primaries: &neo4jv1.PrimariesSpec{Members: 3},
			},
		},
	}
	services := ClusterMemberServices(render.ContextForPool(neo4j, render.PoolPrimary))
	if len(services) != 6 {
		t.Fatalf("expected 6 member services (3 client + 3 internals), got %d", len(services))
	}
}

func TestClientServiceHonorsExposeAndFacadePorts(t *testing.T) {
	httpFacade := int32(80)
	neo4j := &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1.Neo4jSpec{
			Topology: neo4jv1.TopologySpec{Mode: neo4jv1.TopologyModeStandalone},
			Connectivity: &neo4jv1.ConnectivitySpec{
				Service: &neo4jv1.ConnectivityServiceSpec{
					Type:        neo4jv1.ServiceType("LoadBalancer"),
					Expose:      []string{"bolt", "http"},
					Annotations: map[string]string{"service.beta.kubernetes.io/aws-load-balancer-type": "nlb"},
					Ports: &neo4jv1.ServicePortsSpec{
						HTTP: &httpFacade,
					},
					LoadBalancerSourceRanges: []string{"10.0.0.0/8"},
				},
			},
		},
	}
	svc := ClientService(render.StandaloneContext(neo4j))
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		t.Fatalf("type = %s", svc.Spec.Type)
	}
	if svc.Annotations["service.beta.kubernetes.io/aws-load-balancer-type"] != "nlb" {
		t.Fatalf("annotations = %#v", svc.Annotations)
	}
	if len(svc.Spec.LoadBalancerSourceRanges) != 1 || svc.Spec.LoadBalancerSourceRanges[0] != "10.0.0.0/8" {
		t.Fatalf("source ranges = %#v", svc.Spec.LoadBalancerSourceRanges)
	}
	byName := map[string]corev1.ServicePort{}
	for _, p := range svc.Spec.Ports {
		byName[p.Name] = p
	}
	if byName["tcp-http"].Port != 80 || byName["tcp-http"].TargetPort.IntVal != 7474 {
		t.Fatalf("http façade port = %#v", byName["tcp-http"])
	}
	if byName["tcp-bolt"].Port != 7687 {
		t.Fatalf("bolt port = %#v", byName["tcp-bolt"])
	}
}

func TestClientServiceExposeBoltOnly(t *testing.T) {
	neo4j := &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1.Neo4jSpec{
			Topology: neo4jv1.TopologySpec{Mode: neo4jv1.TopologyModeStandalone},
			Connectivity: &neo4jv1.ConnectivitySpec{
				Service: &neo4jv1.ConnectivityServiceSpec{
					Expose: []string{"bolt"},
				},
			},
		},
	}
	svc := ClientService(render.StandaloneContext(neo4j))
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Name != "tcp-bolt" {
		t.Fatalf("ports = %#v", svc.Spec.Ports)
	}
}

func TestClientServiceSessionAffinity(t *testing.T) {
	cluster := func(svc *neo4jv1.ConnectivityServiceSpec) *neo4jv1.Neo4j {
		return &neo4jv1.Neo4j{
			ObjectMeta: metav1.ObjectMeta{Name: "cl", Namespace: "default"},
			Spec: neo4jv1.Neo4jSpec{
				Topology:     neo4jv1.TopologySpec{Mode: neo4jv1.TopologyModeCluster, Primaries: &neo4jv1.PrimariesSpec{Members: 3}},
				Connectivity: &neo4jv1.ConnectivitySpec{Service: svc},
			},
		}
	}
	standalone := func(svc *neo4jv1.ConnectivityServiceSpec) *neo4jv1.Neo4j {
		return &neo4jv1.Neo4j{
			ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
			Spec: neo4jv1.Neo4jSpec{
				Topology:     neo4jv1.TopologySpec{Mode: neo4jv1.TopologyModeStandalone},
				Connectivity: &neo4jv1.ConnectivitySpec{Service: svc},
			},
		}
	}
	custom := int32(60)

	// Cluster + unset → ClientIP with the 3h default (BDR-016).
	svc := ClientService(render.ClientServiceContext(cluster(nil)))
	if svc.Spec.SessionAffinity != corev1.ServiceAffinityClientIP {
		t.Fatalf("cluster default affinity = %q, want ClientIP", svc.Spec.SessionAffinity)
	}
	if svc.Spec.SessionAffinityConfig == nil || svc.Spec.SessionAffinityConfig.ClientIP == nil ||
		*svc.Spec.SessionAffinityConfig.ClientIP.TimeoutSeconds != 10800 {
		t.Fatalf("cluster default timeout = %#v, want 10800", svc.Spec.SessionAffinityConfig)
	}

	// Standalone + unset → None, no config.
	svc = ClientService(render.ClientServiceContext(standalone(nil)))
	if svc.Spec.SessionAffinity != corev1.ServiceAffinityNone || svc.Spec.SessionAffinityConfig != nil {
		t.Fatalf("standalone default = %q/%#v, want None/nil", svc.Spec.SessionAffinity, svc.Spec.SessionAffinityConfig)
	}

	// Explicit None on a cluster overrides the topology default.
	svc = ClientService(render.ClientServiceContext(cluster(&neo4jv1.ConnectivityServiceSpec{SessionAffinity: "None"})))
	if svc.Spec.SessionAffinity != corev1.ServiceAffinityNone || svc.Spec.SessionAffinityConfig != nil {
		t.Fatalf("cluster explicit None = %q/%#v, want None/nil", svc.Spec.SessionAffinity, svc.Spec.SessionAffinityConfig)
	}

	// Explicit ClientIP + custom timeout is honored (e.g. on a standalone).
	svc = ClientService(render.ClientServiceContext(standalone(&neo4jv1.ConnectivityServiceSpec{SessionAffinity: "ClientIP", SessionAffinityTimeoutSeconds: &custom})))
	if svc.Spec.SessionAffinity != corev1.ServiceAffinityClientIP ||
		svc.Spec.SessionAffinityConfig == nil || *svc.Spec.SessionAffinityConfig.ClientIP.TimeoutSeconds != 60 {
		t.Fatalf("explicit ClientIP+timeout = %q/%#v, want ClientIP/60", svc.Spec.SessionAffinity, svc.Spec.SessionAffinityConfig)
	}
}

func TestAdminServiceCreatedWithBackupAndMetrics(t *testing.T) {
	backup := int32(6362)
	metrics := int32(2004)
	neo4j := &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1.Neo4jSpec{
			Topology: neo4jv1.TopologySpec{Mode: neo4jv1.TopologyModeStandalone},
			Features: &neo4jv1.FeaturesSpec{
				Backup: &neo4jv1.BackupFeatureSpec{Enabled: true},
				Monitoring: &neo4jv1.MonitoringFeaturesSpec{
					Prometheus: &neo4jv1.PrometheusMonitoringSpec{Enabled: true},
				},
			},
			Connectivity: &neo4jv1.ConnectivitySpec{
				Listeners: &neo4jv1.ConnectivityListenersSpec{
					Backup:  &backup,
					Metrics: &metrics,
				},
			},
		},
	}
	ctx := render.StandaloneContext(neo4j)
	if !ctx.ShouldCreateAdminService() {
		t.Fatal("expected admin service when backup/prometheus enabled")
	}
	svc := AdminService(ctx)
	if svc.Name != "prod-admin" {
		t.Fatalf("admin name = %q", svc.Name)
	}
	if !svc.Spec.PublishNotReadyAddresses {
		t.Fatal("admin must publish not-ready addresses")
	}
	ports := map[string]int32{}
	for _, p := range svc.Spec.Ports {
		ports[p.Name] = p.Port
	}
	for _, name := range []string{"tcp-bolt", "tcp-http", "tcp-backup", "tcp-prometheus"} {
		if _, ok := ports[name]; !ok {
			t.Fatalf("admin missing port %s: %#v", name, ports)
		}
	}
}

func TestAdminServiceForClusterMode(t *testing.T) {
	neo4j := &neo4jv1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1.Neo4jSpec{
			Topology: neo4jv1.TopologySpec{
				Mode:      neo4jv1.TopologyModeCluster,
				Primaries: &neo4jv1.PrimariesSpec{Members: 3},
			},
		},
	}
	ctx := render.ContextForPool(neo4j, render.PoolPrimary)
	if !ctx.ShouldCreateAdminService() {
		t.Fatal("expected admin service in Cluster mode")
	}
}
