package resources_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
				Primaries: 3,
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

	podSpec := resources.BuildPodSpecForEnterprise(cluster, "primary", "neo4j-admin-secret")

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
				Primaries: 3,
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

	podSpec := resources.BuildPodSpecForEnterprise(cluster, "primary", "neo4j-admin-secret")

	// Test that Prometheus exporter sidecar is added
	require.Len(t, podSpec.Containers, 2, "should have 2 containers (main + exporter)")

	exporterContainer := podSpec.Containers[1]
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
				Primaries: 3,
			},
			Storage: neo4jv1alpha1.StorageSpec{
				ClassName: "fast-ssd",
				Size:      "10Gi",
			},
		},
	}

	podSpec := resources.BuildPodSpecForEnterprise(cluster, "primary", "neo4j-admin-secret")

	// Test that no init containers are added when no plugins
	assert.Len(t, podSpec.InitContainers, 0, "should have no init containers when no plugins")

	// Test that only main container is present when query monitoring is disabled
	assert.Len(t, podSpec.Containers, 1, "should have only main container when query monitoring is disabled")
}

func TestBuildStatefulSetForEnterprise_WithFeatures(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j/neo4j",
				Tag:  "5.26-enterprise",
			},
			Topology: neo4jv1alpha1.TopologyConfiguration{
				Primaries: 3,
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

	sts := resources.BuildPrimaryStatefulSetForEnterprise(cluster)

	// Test StatefulSet metadata
	assert.Equal(t, cluster.Name+"-primary", sts.Name)
	assert.Equal(t, cluster.Namespace, sts.Namespace)

	// Test that pod template has the features
	podSpec := sts.Spec.Template.Spec
	assert.Len(t, podSpec.InitContainers, 1, "should have init container for plugin")
	assert.Len(t, podSpec.Containers, 2, "should have main container + exporter")

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
				Primaries: 3,
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
				Primaries: 3,
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
				Primaries: 3,
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
						Primaries:   2,
						Secondaries: 1,
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
				// Primary pods via headless service
				"test-cluster-primary-0.test-cluster-headless",
				"test-cluster-primary-0.test-cluster-headless.default.svc.cluster.local",
				"test-cluster-primary-1.test-cluster-headless",
				"test-cluster-primary-1.test-cluster-headless.default.svc.cluster.local",
				// Secondary pod via headless service
				"test-cluster-secondary-0.test-cluster-headless",
				"test-cluster-secondary-0.test-cluster-headless.default.svc.cluster.local",
				// Primary pods via internals service
				"test-cluster-primary-0.test-cluster-internals",
				"test-cluster-primary-0.test-cluster-internals.default.svc.cluster.local",
				// Secondary pod via internals service
				"test-cluster-secondary-0.test-cluster-internals",
				"test-cluster-secondary-0.test-cluster-internals.default.svc.cluster.local",
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
						Primaries: 1,
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
