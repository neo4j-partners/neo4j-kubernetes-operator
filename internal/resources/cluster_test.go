package resources_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-labs/neo4j-kubernetes-operator/internal/resources"
)

func TestBuildPodSpecForEnterprise_WithPlugins(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j/neo4j",
				Tag:  "5.26-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 3,
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "fast-ssd",
				Size:      "10Gi",
			},
			Plugins: []neo4jv1alpha1.PluginSpec{
				{
					Name:    "apoc",
					Version: "5.26.0",
					Enabled: true,
					Source: &neo4jv1alpha1.PluginSource{
						URL: "https://github.com/neo4j/apoc/releases/download/5.26.0/apoc-5.26.0-core.jar",
					},
				},
				{
					Name:    "graph-data-science",
					Version: "2.4.0",
					Enabled: true,
					Source: &neo4jv1alpha1.PluginSource{
						URL: "https://graphdatascience.ninja/neo4j-graph-data-science-2.4.0.zip",
					},
				},
				{
					Name:    "disabled-plugin",
					Version: "1.0.0",
					Enabled: false,
					Source: &neo4jv1alpha1.PluginSource{
						URL: "https://example.com/disabled.jar",
					},
				},
			},
		},
	}

	podSpec := resources.BuildPodSpecForEnterprise(cluster, "server", "neo4j-admin-secret")

	// Test that plugins volume is added
	var pluginsVolume *corev1.Volume
	for _, volume := range podSpec.Volumes {
		if volume.Name == "plugins" {
			pluginsVolume = &volume
			break
		}
	}
	require.NotNil(t, pluginsVolume, "plugins volume should be added")
	assert.Equal(t, "plugins", pluginsVolume.Name)

	// Test that plugins volume mount is added to main container
	mainContainer := podSpec.Containers[0]
	var pluginsMount *corev1.VolumeMount
	for _, mount := range mainContainer.VolumeMounts {
		if mount.Name == "plugins" {
			pluginsMount = &mount
			break
		}
	}
	require.NotNil(t, pluginsMount, "plugins volume mount should be added to main container")
	assert.Equal(t, "/plugins", pluginsMount.MountPath)

	// Test that init containers are added for enabled plugins
	require.Len(t, podSpec.InitContainers, 2, "should have 2 init containers for enabled plugins")

	// Test first plugin init container
	apocInitContainer := podSpec.InitContainers[0]
	assert.Equal(t, "install-plugin-apoc", apocInitContainer.Name)
	assert.Equal(t, "alpine:3.18", apocInitContainer.Image)
	assert.Contains(t, apocInitContainer.Args[0], "apoc-5.26.0-core.jar")
	assert.Contains(t, apocInitContainer.Args[0], "https://github.com/neo4j/apoc/releases/download/5.26.0/apoc-5.26.0-core.jar")

	// Test second plugin init container
	gdsInitContainer := podSpec.InitContainers[1]
	assert.Equal(t, "install-plugin-graph-data-science", gdsInitContainer.Name)
	assert.Contains(t, gdsInitContainer.Args[0], "neo4j-graph-data-science-2.4.0.zip")
}

func TestBuildPodSpecForEnterprise_WithQueryMonitoring(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j/neo4j",
				Tag:  "5.26-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 3,
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "fast-ssd",
				Size:      "10Gi",
			},
			QueryMonitoring: &neo4jv1alpha1.QueryMonitoringSpec{
				Enabled: true,
			},
		},
	}

	podSpec := resources.BuildPodSpecForEnterprise(cluster, "server", "neo4j-admin-secret")

	// Test that Prometheus exporter sidecar is added
	require.Len(t, podSpec.Containers, 3, "should have 3 containers (main + backup + exporter)")

	// Find the exporter container (it should be the last one)
	var exporterContainer corev1.Container
	for _, c := range podSpec.Containers {
		if c.Name == "prometheus-exporter" {
			exporterContainer = c
			break
		}
	}
	assert.Equal(t, "prometheus-exporter", exporterContainer.Name)
	assert.Equal(t, "neo4j/prometheus-exporter:4.0.0", exporterContainer.Image)
	assert.Contains(t, exporterContainer.Args[0], "bolt://localhost:7687")

	// Test exporter port
	require.Len(t, exporterContainer.Ports, 1)
	assert.Equal(t, int32(2004), exporterContainer.Ports[0].ContainerPort)
	assert.Equal(t, "metrics", exporterContainer.Ports[0].Name)

	// Test that exporter has access to Neo4j auth
	require.Len(t, exporterContainer.Env, 1)
	assert.Equal(t, "NEO4J_AUTH", exporterContainer.Env[0].Name)
	assert.Equal(t, "neo4j-admin-secret", exporterContainer.Env[0].ValueFrom.SecretKeyRef.Name)
}

func TestBuildPodSpecForEnterprise_WithoutFeatures(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j/neo4j",
				Tag:  "5.26-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 3,
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "fast-ssd",
				Size:      "10Gi",
			},
		},
	}

	podSpec := resources.BuildPodSpecForEnterprise(cluster, "server", "neo4j-admin-secret")

	// Test that no init containers are added when no plugins
	assert.Len(t, podSpec.InitContainers, 0, "should have no init containers when no plugins")

	// Test that main container and backup sidecar are present when query monitoring is disabled
	assert.Len(t, podSpec.Containers, 2, "should have main container and backup sidecar when query monitoring is disabled")
	assert.Equal(t, "neo4j", podSpec.Containers[0].Name)
	assert.Equal(t, "backup-sidecar", podSpec.Containers[1].Name)
}

func TestBuildStatefulSetForEnterprise_WithFeatures(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j/neo4j",
				Tag:  "5.26-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 3,
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "fast-ssd",
				Size:      "10Gi",
			},
			Plugins: []neo4jv1alpha1.PluginSpec{
				{
					Name:    "apoc",
					Version: "5.26.0",
					Enabled: true,
					Source: &neo4jv1alpha1.PluginSource{
						URL: "https://github.com/neo4j/apoc/releases/download/5.26.0/apoc-5.26.0-core.jar",
					},
				},
			},
			QueryMonitoring: &neo4jv1alpha1.QueryMonitoringSpec{
				Enabled: true,
			},
		},
	}

	sts := resources.BuildServerStatefulSetForEnterprise(cluster)

	// Test StatefulSet metadata
	assert.Equal(t, cluster.Name+"-server", sts.Name)
	assert.Equal(t, cluster.Namespace, sts.Namespace)

	// Test that pod template has the features
	podSpec := sts.Spec.Template.Spec
	assert.Len(t, podSpec.InitContainers, 1, "should have init container for plugin")
	assert.Len(t, podSpec.Containers, 3, "should have main container + backup + exporter")

	// Test pod management policy
	assert.Equal(t, appsv1.ParallelPodManagement, sts.Spec.PodManagementPolicy, "should use parallel pod management")

	// Test Prometheus annotations
	annotations := sts.Spec.Template.Annotations
	assert.Equal(t, "true", annotations["prometheus.io/scrape"])
	assert.Equal(t, "2004", annotations["prometheus.io/port"])
	assert.Equal(t, "/metrics", annotations["prometheus.io/path"])
}

func TestBuildDiscoveryServiceAccountForEnterprise(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 3,
			},
		},
	}

	serviceAccount := resources.BuildDiscoveryServiceAccountForEnterprise(cluster)

	// Test ServiceAccount metadata
	assert.Equal(t, "test-cluster-discovery", serviceAccount.Name)
	assert.Equal(t, "default", serviceAccount.Namespace)

	// Test labels
	expectedLabels := map[string]string{
		"app.kubernetes.io/name":       "neo4j",
		"app.kubernetes.io/instance":   "test-cluster",
		"app.kubernetes.io/component":  "database",
		"app.kubernetes.io/part-of":    "neo4j-cluster",
		"app.kubernetes.io/managed-by": "neo4j-operator",
		"neo4j.com/cluster":            "test-cluster",
		"neo4j.com/role":               "discovery-service-account",
	}
	for key, expectedValue := range expectedLabels {
		assert.Equal(t, expectedValue, serviceAccount.Labels[key])
	}
}

func TestBuildDiscoveryRoleForEnterprise(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 3,
			},
		},
	}

	role := resources.BuildDiscoveryRoleForEnterprise(cluster)

	// Test Role metadata
	assert.Equal(t, "test-cluster-discovery", role.Name)
	assert.Equal(t, "default", role.Namespace)

	// Test permissions
	require.Len(t, role.Rules, 1, "should have one policy rule")
	rule := role.Rules[0]
	assert.Equal(t, []string{""}, rule.APIGroups)
	assert.Equal(t, []string{"services", "endpoints"}, rule.Resources)
	assert.Equal(t, []string{"get", "list", "watch"}, rule.Verbs)
}

func TestBuildDiscoveryRoleBindingForEnterprise(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 3,
			},
		},
	}

	roleBinding := resources.BuildDiscoveryRoleBindingForEnterprise(cluster)

	// Test RoleBinding metadata
	assert.Equal(t, "test-cluster-discovery", roleBinding.Name)
	assert.Equal(t, "default", roleBinding.Namespace)

	// Test subject
	require.Len(t, roleBinding.Subjects, 1, "should have one subject")
	subject := roleBinding.Subjects[0]
	assert.Equal(t, "ServiceAccount", subject.Kind)
	assert.Equal(t, "test-cluster-discovery", subject.Name)
	assert.Equal(t, "default", subject.Namespace)

	// Test role reference
	assert.Equal(t, "rbac.authorization.k8s.io", roleBinding.RoleRef.APIGroup)
	assert.Equal(t, "Role", roleBinding.RoleRef.Kind)
	assert.Equal(t, "test-cluster-discovery", roleBinding.RoleRef.Name)
}

func TestBuildStatefulSetForEnterprise_ParallelManagement(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
				Tag:  "5.26.0-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Servers: 3,
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "standard",
				Size:      "10Gi",
			},
		},
	}

	// Test server StatefulSet
	serverSts := resources.BuildServerStatefulSetForEnterprise(cluster)
	assert.Equal(t, appsv1.ParallelPodManagement, serverSts.Spec.PodManagementPolicy, "server StatefulSet should use parallel pod management")

	// Test that server configuration is set correctly in the ConfigMap startup script
	configMap := resources.BuildConfigMapForEnterprise(cluster)
	startupScript := configMap.Data["startup.sh"]
	assert.Contains(t, startupScript, "TOTAL_SERVERS=3", "startup script should set TOTAL_SERVERS")
	assert.Contains(t, startupScript, "dbms.cluster.minimum_initial_system_primaries_count=1", "should use fixed minimum for server bootstrap")
}

func TestBuildCertificateForEnterprise_DNSNames(t *testing.T) {
	tests := []struct {
		name       string
		cluster    *neo4jv1alpha1.Neo4jEnterpriseCluster
		wantDNS    []string
		notWantDNS []string
	}{
		{
			name: "Certificate includes headless service DNS names",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1alpha1.IssuerRef{
							Name: "test-issuer",
							Kind: "ClusterIssuer",
						},
					},
				},
			},
			wantDNS: []string{
				// Client service
				"test-cluster-client",
				"test-cluster-client.default.svc.cluster.local",
				// Internals service
				"test-cluster-internals",
				"test-cluster-internals.default.svc.cluster.local",
				// Headless service
				"test-cluster-headless",
				"test-cluster-headless.default.svc.cluster.local",
				// Server pods via headless service
				"test-cluster-server-0.test-cluster-headless",
				"test-cluster-server-0.test-cluster-headless.default.svc.cluster.local",
				"test-cluster-server-1.test-cluster-headless",
				"test-cluster-server-1.test-cluster-headless.default.svc.cluster.local",
				"test-cluster-server-2.test-cluster-headless",
				"test-cluster-server-2.test-cluster-headless.default.svc.cluster.local",
				// Server pods via internals service
				"test-cluster-server-0.test-cluster-internals",
				"test-cluster-server-0.test-cluster-internals.default.svc.cluster.local",
				"test-cluster-server-1.test-cluster-internals",
				"test-cluster-server-1.test-cluster-internals.default.svc.cluster.local",
				"test-cluster-server-2.test-cluster-internals",
				"test-cluster-server-2.test-cluster-internals.default.svc.cluster.local",
			},
		},
		{
			name: "No certificate when TLS disabled",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 2,
					},
				},
			},
			wantDNS: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cert := resources.BuildCertificateForEnterprise(tt.cluster)
			if tt.wantDNS == nil {
				assert.Nil(t, cert)
				return
			}

			assert.NotNil(t, cert)
			for _, dns := range tt.wantDNS {
				assert.Contains(t, cert.Spec.DNSNames, dns, "Certificate should include DNS name: %s", dns)
			}
		})
	}
}

func TestBuildClientServiceForEnterprise_WithEnhancedFeatures(t *testing.T) {
	tests := []struct {
		name      string
		cluster   *neo4jv1alpha1.Neo4jEnterpriseCluster
		checkFunc func(t *testing.T, service *corev1.Service)
	}{
		{
			name: "LoadBalancer with IP and external traffic policy",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Service: &neo4jv1alpha1.ServiceSpec{
						Type:                  "LoadBalancer",
						LoadBalancerIP:        "10.0.0.100",
						ExternalTrafficPolicy: "Local",
						LoadBalancerSourceRanges: []string{
							"10.0.0.0/8",
							"192.168.0.0/16",
						},
					},
				},
			},
			checkFunc: func(t *testing.T, service *corev1.Service) {
				assert.Equal(t, corev1.ServiceTypeLoadBalancer, service.Spec.Type)
				assert.Equal(t, "10.0.0.100", service.Spec.LoadBalancerIP)
				assert.Equal(t, corev1.ServiceExternalTrafficPolicyTypeLocal, service.Spec.ExternalTrafficPolicy)
				assert.Equal(t, []string{"10.0.0.0/8", "192.168.0.0/16"}, service.Spec.LoadBalancerSourceRanges)
			},
		},
		{
			name: "NodePort service",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Service: &neo4jv1alpha1.ServiceSpec{
						Type: "NodePort",
					},
				},
			},
			checkFunc: func(t *testing.T, service *corev1.Service) {
				assert.Equal(t, corev1.ServiceTypeNodePort, service.Spec.Type)
			},
		},
		{
			name: "Default ClusterIP when service spec is nil",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Service: nil,
				},
			},
			checkFunc: func(t *testing.T, service *corev1.Service) {
				assert.Equal(t, corev1.ServiceTypeClusterIP, service.Spec.Type)
			},
		},
		{
			name: "Service with annotations",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Service: &neo4jv1alpha1.ServiceSpec{
						Type: "LoadBalancer",
						Annotations: map[string]string{
							"service.beta.kubernetes.io/aws-load-balancer-type": "nlb",
							"custom-annotation": "custom-value",
						},
					},
				},
			},
			checkFunc: func(t *testing.T, service *corev1.Service) {
				assert.Equal(t, corev1.ServiceTypeLoadBalancer, service.Spec.Type)
				assert.Equal(t, "nlb", service.Annotations["service.beta.kubernetes.io/aws-load-balancer-type"])
				assert.Equal(t, "custom-value", service.Annotations["custom-annotation"])
			},
		},
		{
			name: "Service with external traffic policy Cluster",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Service: &neo4jv1alpha1.ServiceSpec{
						Type:                  "LoadBalancer",
						ExternalTrafficPolicy: "Cluster",
					},
				},
			},
			checkFunc: func(t *testing.T, service *corev1.Service) {
				assert.Equal(t, corev1.ServiceTypeLoadBalancer, service.Spec.Type)
				assert.Equal(t, corev1.ServiceExternalTrafficPolicyTypeCluster, service.Spec.ExternalTrafficPolicy)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := resources.BuildClientServiceForEnterprise(tt.cluster)
			assert.NotNil(t, service)
			assert.Equal(t, "test-cluster-client", service.Name)
			assert.Equal(t, "default", service.Namespace)

			// Check that basic ports are present (2 without TLS, 3 with TLS)
			assert.GreaterOrEqual(t, len(service.Spec.Ports), 2)

			// Run custom checks
			tt.checkFunc(t, service)
		})
	}
}
