package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestBuildPodSpecForEnterprise_WithPlugins(t *testing.T) {
	cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
		Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
			Image: neo4jv1alpha1.ImageSpec{
				Repo: "neo4j/neo4j",
				Tag:  "5.15.0-enterprise",
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
					Version: "5.15.0",
					Enabled: true,
					Source: &neo4jv1alpha1.PluginSource{
						URL: "https://github.com/neo4j/apoc/releases/download/5.15.0/apoc-5.15.0-core.jar",
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

	podSpec := buildPodSpecForEnterprise(cluster, "primary", "neo4j-admin-secret")

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
	assert.Contains(t, apocInitContainer.Args[0], "apoc-5.15.0-core.jar")
	assert.Contains(t, apocInitContainer.Args[0], "https://github.com/neo4j/apoc/releases/download/5.15.0/apoc-5.15.0-core.jar")

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
				Tag:  "5.15.0-enterprise",
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

	podSpec := buildPodSpecForEnterprise(cluster, "primary", "neo4j-admin-secret")

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
				Tag:  "5.15.0-enterprise",
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

	podSpec := buildPodSpecForEnterprise(cluster, "primary", "neo4j-admin-secret")

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
				Tag:  "5.15.0-enterprise",
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
					Version: "5.15.0",
					Enabled: true,
					Source: &neo4jv1alpha1.PluginSource{
						URL: "https://github.com/neo4j/apoc/releases/download/5.15.0/apoc-5.15.0-core.jar",
					},
				},
			},
			QueryMonitoring: &neo4jv1alpha1.QueryMonitoringSpec{
				Enabled: true,
			},
		},
	}

	sts := BuildPrimaryStatefulSetForEnterprise(cluster)

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
