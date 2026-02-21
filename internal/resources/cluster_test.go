package resources_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/resources"
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
			// Plugin management is now handled via separate Neo4jPlugin CRD
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

	// Plugins are now managed by the Neo4jPlugin CRD - no init containers expected
	assert.Len(t, podSpec.InitContainers, 0, "should have no init containers as plugins are managed via Neo4jPlugin CRD")
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

	// Query monitoring should expose metrics on the main container.
	require.Len(t, podSpec.Containers, 1, "should have only main container")
	assert.Equal(t, "neo4j", podSpec.Containers[0].Name)

	var metricsPort *corev1.ContainerPort
	for i := range podSpec.Containers[0].Ports {
		if podSpec.Containers[0].Ports[i].Name == "metrics" {
			metricsPort = &podSpec.Containers[0].Ports[i]
			break
		}
	}
	require.NotNil(t, metricsPort)
	assert.Equal(t, int32(resources.MetricsPort), metricsPort.ContainerPort)
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

	// Test that only main container is present when query monitoring is disabled (no backup sidecar due to centralized backup architecture)
	assert.Len(t, podSpec.Containers, 1, "should have only main container when query monitoring is disabled")
	assert.Equal(t, "neo4j", podSpec.Containers[0].Name)
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
			// Plugin management is now handled via separate Neo4jPlugin CRD
			QueryMonitoring: &neo4jv1alpha1.QueryMonitoringSpec{
				Enabled: true,
			},
		},
	}

	statefulSets := resources.BuildServerStatefulSetsForEnterprise(cluster)
	require.Len(t, statefulSets, 3, "should create 3 StatefulSets for 3 servers")

	// Test the first StatefulSet as a representative
	sts := statefulSets[0]

	// Test StatefulSet metadata (first StatefulSet should be server-0)
	assert.Equal(t, cluster.Name+"-server-0", sts.Name)
	assert.Equal(t, cluster.Namespace, sts.Namespace)

	// Test that pod template has the features
	podSpec := sts.Spec.Template.Spec
	assert.Len(t, podSpec.InitContainers, 0, "should have no init containers as plugins are managed via Neo4jPlugin CRD")
	assert.Len(t, podSpec.Containers, 1, "should have only main container (no backup sidecar)")

	// Test pod management policy
	assert.Equal(t, appsv1.ParallelPodManagement, sts.Spec.PodManagementPolicy, "should use parallel pod management")

	// Test Prometheus annotations
	annotations := sts.Spec.Template.Annotations
	assert.Equal(t, "true", annotations["prometheus.io/scrape"])
	assert.Equal(t, fmt.Sprintf("%d", resources.MetricsPort), annotations["prometheus.io/port"])
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

	// Test server StatefulSets
	serverStatefulSets := resources.BuildServerStatefulSetsForEnterprise(cluster)
	require.Len(t, serverStatefulSets, 3, "should create 3 StatefulSets for 3 servers")

	// Test the first StatefulSet as representative
	serverSts := serverStatefulSets[0]
	assert.Equal(t, appsv1.ParallelPodManagement, serverSts.Spec.PodManagementPolicy, "server StatefulSet should use parallel pod management")

	// Test that server configuration is set correctly in the ConfigMap startup script
	configMap := resources.BuildConfigMapForEnterprise(cluster)
	startupScript := configMap.Data["startup.sh"]
	assert.Contains(t, startupScript, "TOTAL_SERVERS=3", "startup script should set TOTAL_SERVERS")
	assert.Contains(t, startupScript, "dbms.cluster.minimum_initial_system_primaries_count=${TOTAL_SERVERS}", "should use TOTAL_SERVERS as minimum to prevent split-brain")
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

// TestConfigMapDeterminism tests that ConfigMap generation is deterministic
// This addresses the ConfigMap oscillation issue where non-deterministic map iteration
// caused hash changes leading to unnecessary pod restarts
func TestConfigMapDeterminism(t *testing.T) {
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
			// Add configuration with multiple keys that could be iterated in different orders
			Config: map[string]string{
				"zebra.setting":                 "last",
				"alpha.setting":                 "first",
				"beta.setting":                  "second",
				"gamma.setting":                 "third",
				"dbms.memory.heap.initial_size": "1G",
				"dbms.memory.heap.max_size":     "2G",
			},
		},
	}

	// Generate ConfigMap multiple times to ensure consistency
	configMaps := make([]*corev1.ConfigMap, 10)
	for i := 0; i < 10; i++ {
		configMaps[i] = resources.BuildConfigMapForEnterprise(cluster)
	}

	// All ConfigMaps should be identical
	baseConfigMap := configMaps[0]
	for i := 1; i < len(configMaps); i++ {
		assert.Equal(t, baseConfigMap.Data, configMaps[i].Data,
			"ConfigMap data should be identical across multiple generations (iteration %d)", i)

		// Specifically check that startup script is identical
		assert.Equal(t, baseConfigMap.Data["startup.sh"], configMaps[i].Data["startup.sh"],
			"Startup script should be identical across multiple generations (iteration %d)", i)
	}

	// Verify that configuration keys are in sorted order in the neo4j.conf
	neo4jConf := baseConfigMap.Data["neo4j.conf"]

	// Check that alpha appears before beta, beta before gamma, etc.
	alphaIndex := assertContainsAndGetIndex(t, neo4jConf, "alpha.setting=first")
	betaIndex := assertContainsAndGetIndex(t, neo4jConf, "beta.setting=second")
	gammaIndex := assertContainsAndGetIndex(t, neo4jConf, "gamma.setting=third")
	zebraIndex := assertContainsAndGetIndex(t, neo4jConf, "zebra.setting=last")

	assert.Less(t, alphaIndex, betaIndex, "alpha.setting should appear before beta.setting")
	assert.Less(t, betaIndex, gammaIndex, "beta.setting should appear before gamma.setting")
	assert.Less(t, gammaIndex, zebraIndex, "gamma.setting should appear before zebra.setting")
}

// Helper function to assert a string contains a substring and return its index
func assertContainsAndGetIndex(t *testing.T, haystack, needle string) int {
	assert.Contains(t, haystack, needle)

	// Find the index of the needle in haystack
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}

	t.Fatalf("String '%s' should contain '%s' but index not found", haystack, needle)
	return -1
}

func TestValidateServerRoleHints(t *testing.T) {
	tests := []struct {
		name           string
		cluster        *neo4jv1alpha1.Neo4jEnterpriseCluster
		expectedErrors []string
	}{
		{
			name: "No role hints - should pass",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
				},
			},
			expectedErrors: nil,
		},
		{
			name: "Valid role hints - should pass",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
						ServerRoles: []neo4jv1alpha1.ServerRoleHint{
							{ServerIndex: 0, ModeConstraint: "PRIMARY"},
							{ServerIndex: 1, ModeConstraint: "SECONDARY"},
							{ServerIndex: 2, ModeConstraint: "NONE"},
						},
					},
				},
			},
			expectedErrors: nil,
		},
		{
			name: "Server index out of range - should fail",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
						ServerRoles: []neo4jv1alpha1.ServerRoleHint{
							{ServerIndex: 3, ModeConstraint: "PRIMARY"}, // Index 3 is out of range for 3 servers (0-2)
						},
					},
				},
			},
			expectedErrors: []string{"server role hint index 3 is out of range (0-2)"},
		},
		{
			name: "Duplicate server indices - should fail",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
						ServerRoles: []neo4jv1alpha1.ServerRoleHint{
							{ServerIndex: 0, ModeConstraint: "PRIMARY"},
							{ServerIndex: 0, ModeConstraint: "SECONDARY"}, // Duplicate index
						},
					},
				},
			},
			expectedErrors: []string{"duplicate server role hint for server index 0"},
		},
		{
			name: "All servers constrained to SECONDARY - should fail",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
						ServerRoles: []neo4jv1alpha1.ServerRoleHint{
							{ServerIndex: 0, ModeConstraint: "SECONDARY"},
							{ServerIndex: 1, ModeConstraint: "SECONDARY"},
							{ServerIndex: 2, ModeConstraint: "SECONDARY"},
						},
					},
				},
			},
			expectedErrors: []string{"all servers are constrained to SECONDARY mode - cluster would have no primary servers available"},
		},
		{
			name: "Invalid mode constraint - should fail",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
						ServerRoles: []neo4jv1alpha1.ServerRoleHint{
							{ServerIndex: 0, ModeConstraint: "INVALID"}, // Invalid mode
						},
					},
				},
			},
			expectedErrors: []string{"invalid mode constraint 'INVALID' for server 0 (valid values: NONE, PRIMARY, SECONDARY)"},
		},
		{
			name: "Multiple validation errors - should return all",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 2,
						ServerRoles: []neo4jv1alpha1.ServerRoleHint{
							{ServerIndex: 0, ModeConstraint: "PRIMARY"},
							{ServerIndex: 0, ModeConstraint: "SECONDARY"}, // Duplicate
							{ServerIndex: 3, ModeConstraint: "PRIMARY"},   // Out of range
						},
					},
				},
			},
			expectedErrors: []string{
				"server role hint index 3 is out of range (0-1)",
				"duplicate server role hint for server index 0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := resources.ValidateServerRoleHints(tt.cluster)

			if len(tt.expectedErrors) == 0 {
				assert.Empty(t, errors, "Expected no validation errors")
			} else {
				assert.Len(t, errors, len(tt.expectedErrors), "Expected number of errors to match")
				for _, expectedError := range tt.expectedErrors {
					assert.Contains(t, errors, expectedError, "Expected error message should be present")
				}
			}
		})
	}
}

func TestBuildServerModeConstraintConfig(t *testing.T) {
	tests := []struct {
		name             string
		cluster          *neo4jv1alpha1.Neo4jEnterpriseCluster
		expectedInConfig []string
		notInConfig      []string
	}{
		{
			name: "No server mode constraint or role hints",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
					},
				},
			},
			expectedInConfig: nil,
			notInConfig:      []string{"initial.server.mode_constraint"},
		},
		{
			name: "Global server mode constraint - PRIMARY",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers:              3,
						ServerModeConstraint: "PRIMARY",
					},
				},
			},
			expectedInConfig: []string{
				"initial.server.mode_constraint=PRIMARY",
				"Constrain all servers to PRIMARY mode",
			},
		},
		{
			name: "Per-server role hints",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers: 3,
						ServerRoles: []neo4jv1alpha1.ServerRoleHint{
							{ServerIndex: 0, ModeConstraint: "PRIMARY"},
							{ServerIndex: 1, ModeConstraint: "SECONDARY"},
						},
					},
				},
			},
			expectedInConfig: []string{
				"Per-server mode constraint configuration based on role hints",
				"SERVER_MODE_CONSTRAINT=\"NONE\"",
				"if [ \"$SERVER_INDEX\" = \"0\" ]; then",
				"SERVER_MODE_CONSTRAINT=\"PRIMARY\"",
				"if [ \"$SERVER_INDEX\" = \"1\" ]; then",
				"SERVER_MODE_CONSTRAINT=\"SECONDARY\"",
				"initial.server.mode_constraint=$SERVER_MODE_CONSTRAINT",
			},
		},
		{
			name: "Role hints take precedence over global constraint",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Topology: neo4jv1alpha1.TopologyConfiguration{
						Servers:              3,
						ServerModeConstraint: "SECONDARY", // This should be ignored
						ServerRoles: []neo4jv1alpha1.ServerRoleHint{
							{ServerIndex: 0, ModeConstraint: "PRIMARY"},
						},
					},
				},
			},
			expectedInConfig: []string{
				"Per-server mode constraint configuration based on role hints",
				"SERVER_MODE_CONSTRAINT=\"PRIMARY\"",
			},
			notInConfig: []string{
				"Constrain all servers to SECONDARY mode",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the startup script which includes the server mode constraint config
			startupScript := resources.BuildConfigMapForEnterprise(tt.cluster).Data["startup.sh"]

			for _, expected := range tt.expectedInConfig {
				assert.Contains(t, startupScript, expected, "Expected content should be in startup script")
			}

			for _, notExpected := range tt.notInConfig {
				assert.NotContains(t, startupScript, notExpected, "Content should not be in startup script")
			}
		})
	}
}

func TestBuildPodSpecForEnterprise_DefaultSecurityContext(t *testing.T) {
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

	require.NotNil(t, podSpec.SecurityContext)
	require.NotNil(t, podSpec.SecurityContext.RunAsUser)
	assert.Equal(t, int64(7474), *podSpec.SecurityContext.RunAsUser)
	require.NotNil(t, podSpec.SecurityContext.FSGroup)
	assert.Equal(t, int64(7474), *podSpec.SecurityContext.FSGroup)

	require.NotEmpty(t, podSpec.Containers)
	for _, container := range podSpec.Containers {
		require.NotNil(t, container.SecurityContext)
		require.NotNil(t, container.SecurityContext.RunAsUser)
		assert.Equal(t, int64(7474), *container.SecurityContext.RunAsUser)
	}
}

func TestBuildPodSpecForEnterprise_CustomSecurityContext(t *testing.T) {
	customPodSC := &corev1.PodSecurityContext{
		RunAsNonRoot: ptr.To(true),
	}
	customContainerSC := &corev1.SecurityContext{
		RunAsNonRoot:             ptr.To(true),
		ReadOnlyRootFilesystem:   ptr.To(true),
		AllowPrivilegeEscalation: ptr.To(false),
	}

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
			SecurityContext: &neo4jv1alpha1.SecurityContextSpec{
				PodSecurityContext:       customPodSC,
				ContainerSecurityContext: customContainerSC,
			},
		},
	}

	podSpec := resources.BuildPodSpecForEnterprise(cluster, "server", "neo4j-admin-secret")

	assert.Equal(t, customPodSC, podSpec.SecurityContext)
	require.NotEmpty(t, podSpec.Containers)
	for _, container := range podSpec.Containers {
		assert.Equal(t, customContainerSC, container.SecurityContext)
	}
}

// TestBuildServerStatefulSet_CustomAdminSecret verifies that spec.auth.adminSecret is
// respected when building the server StatefulSet (regression test for GitHub issue #27).
func TestBuildServerStatefulSet_CustomAdminSecret(t *testing.T) {
	clusterWithCustomSecret := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image:    neo4jv1alpha1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
			Topology: neo4jv1alpha1.TopologyConfiguration{Servers: 3},
			Storage:  neo4jv1alpha1.StorageSpec{ClassName: "standard", Size: "10Gi"},
			Auth: &neo4jv1alpha1.AuthSpec{
				AdminSecret: "my-custom-secret",
			},
		},
	}

	clusterWithDefaultSecret := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image:    neo4jv1alpha1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
			Topology: neo4jv1alpha1.TopologyConfiguration{Servers: 3},
			Storage:  neo4jv1alpha1.StorageSpec{ClassName: "standard", Size: "10Gi"},
		},
	}

	assertSecretName := func(t *testing.T, sts *appsv1.StatefulSet, expectedSecret string) {
		t.Helper()
		containers := sts.Spec.Template.Spec.Containers
		require.NotEmpty(t, containers)
		var dbUser, dbPass *corev1.EnvVar
		for i := range containers[0].Env {
			switch containers[0].Env[i].Name {
			case "DB_USERNAME":
				dbUser = &containers[0].Env[i]
			case "DB_PASSWORD":
				dbPass = &containers[0].Env[i]
			}
		}
		require.NotNil(t, dbUser, "DB_USERNAME env var must be present")
		require.NotNil(t, dbPass, "DB_PASSWORD env var must be present")
		require.NotNil(t, dbUser.ValueFrom)
		require.NotNil(t, dbPass.ValueFrom)
		assert.Equal(t, expectedSecret, dbUser.ValueFrom.SecretKeyRef.Name,
			"DB_USERNAME should reference secret %q", expectedSecret)
		assert.Equal(t, expectedSecret, dbPass.ValueFrom.SecretKeyRef.Name,
			"DB_PASSWORD should reference secret %q", expectedSecret)
	}

	t.Run("custom adminSecret is used", func(t *testing.T) {
		sts := resources.BuildServerStatefulSetForEnterprise(clusterWithCustomSecret)
		require.NotNil(t, sts)
		assertSecretName(t, sts, "my-custom-secret")
	})

	t.Run("default adminSecret is used when auth is nil", func(t *testing.T) {
		sts := resources.BuildServerStatefulSetForEnterprise(clusterWithDefaultSecret)
		require.NotNil(t, sts)
		assertSecretName(t, sts, resources.DefaultAdminSecret)
	})
}

func TestClusterConfigContainsBackupListenAddress(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "backup-listen-test", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image:    neo4jv1alpha1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
			Topology: neo4jv1alpha1.TopologyConfiguration{Servers: 3},
			Storage:  neo4jv1alpha1.StorageSpec{ClassName: "standard", Size: "10Gi"},
		},
	}
	neo4jConf := resources.BuildConfigMapForEnterprise(cluster).Data["neo4j.conf"]
	assert.Contains(t, neo4jConf, "server.backup.listen_address=0.0.0.0:6362",
		"backup service must listen on all interfaces so backup Jobs can connect")
	assert.Contains(t, neo4jConf, "server.backup.enabled=true")
}

func TestBuildBackupFromAddresses(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image:    neo4jv1alpha1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
			Topology: neo4jv1alpha1.TopologyConfiguration{Servers: 3},
			Storage:  neo4jv1alpha1.StorageSpec{ClassName: "standard", Size: "10Gi"},
		},
	}

	addrs := resources.BuildBackupFromAddresses(cluster)
	expected := "my-cluster-server-0.my-cluster-headless.default.svc.cluster.local:6362," +
		"my-cluster-server-1.my-cluster-headless.default.svc.cluster.local:6362," +
		"my-cluster-server-2.my-cluster-headless.default.svc.cluster.local:6362"
	assert.Equal(t, expected, addrs)
}

func TestBuildBackupFromAddressesStandaloneEquivalent(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "ops"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image:    neo4jv1alpha1.ImageSpec{Repo: "neo4j", Tag: "5.26-enterprise"},
			Topology: neo4jv1alpha1.TopologyConfiguration{Servers: 1},
			Storage:  neo4jv1alpha1.StorageSpec{ClassName: "standard", Size: "10Gi"},
		},
	}

	addrs := resources.BuildBackupFromAddresses(cluster)
	assert.Equal(t, "my-cluster-server-0.my-cluster-headless.ops.svc.cluster.local:6362", addrs)
}

func TestBuildPodSpecForEnterprise_WithPullSecrets(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo:        "neo4j",
				Tag:         "5.26-enterprise",
				PullSecrets: []string{"my-registry-secret", "another-secret"},
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

	require.Len(t, podSpec.ImagePullSecrets, 2)
	assert.Equal(t, "my-registry-secret", podSpec.ImagePullSecrets[0].Name)
	assert.Equal(t, "another-secret", podSpec.ImagePullSecrets[1].Name)
}

func TestBuildPodSpecForEnterprise_WithNoPullSecrets(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j",
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

	assert.Empty(t, podSpec.ImagePullSecrets)
}
