package workload

import (
	"testing"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
	rendercfg "github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render/serverconfig"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStandaloneStatefulSet(t *testing.T) {
	size := "10Gi"
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition:  neo4jv1beta1.EditionEnterprise,
			Version:  "2026.05.0",
			License:  neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: size},
					},
				},
			},
		},
	}
	sts := StandaloneStatefulSet(render.StandaloneContext(neo4j))
	if sts.Name != "dev-server" {
		t.Fatalf("sts name = %q", sts.Name)
	}
	if *sts.Spec.Replicas != 1 {
		t.Fatalf("replicas = %d", *sts.Spec.Replicas)
	}
	if len(sts.Spec.VolumeClaimTemplates) != 1 {
		t.Fatalf("expected one VCT")
	}
	env := sts.Spec.Template.Spec.Containers[0].Env
	envByName := map[string]string{}
	for _, e := range env {
		if e.Value != "" {
			envByName[e.Name] = e.Value
		}
	}
	if envByName["NEO4J_ACCEPT_LICENSE_AGREEMENT"] != "yes" {
		t.Fatalf("license env = %q", envByName["NEO4J_ACCEPT_LICENSE_AGREEMENT"])
	}
	if envByName["NEO4J_EDITION"] != "ENTERPRISE_K8S" {
		t.Fatalf("NEO4J_EDITION = %q", envByName["NEO4J_EDITION"])
	}
	if envByName["EXTENDED_CONF"] != "yes" {
		t.Fatalf("EXTENDED_CONF = %q", envByName["EXTENDED_CONF"])
	}
	if envByName["NEO4J_CONF"] != "/config/" {
		t.Fatalf("NEO4J_CONF = %q", envByName["NEO4J_CONF"])
	}
	var podNameFromField bool
	for _, e := range env {
		if e.Name == "POD_NAME" && e.ValueFrom != nil && e.ValueFrom.FieldRef != nil &&
			e.ValueFrom.FieldRef.FieldPath == "metadata.name" {
			podNameFromField = true
		}
	}
	if !podNameFromField {
		t.Fatal("expected POD_NAME env from metadata.name fieldRef")
	}

	mounts := sts.Spec.Template.Spec.Containers[0].VolumeMounts
	var configMount *corev1.VolumeMount
	for i := range mounts {
		if mounts[i].Name == neo4jConfVolumeName {
			configMount = &mounts[i]
			break
		}
	}
	if configMount == nil {
		t.Fatal("expected neo4j-conf volume mount")
	}
	if configMount.MountPath != "/config/neo4j.conf" {
		t.Fatalf("config mount path = %q", configMount.MountPath)
	}
	if configMount.SubPath != "" {
		t.Fatalf("config mount must not use subPath, got %q", configMount.SubPath)
	}

	var configMode *int32
	for i := range sts.Spec.Template.Spec.Volumes {
		if sts.Spec.Template.Spec.Volumes[i].Name == neo4jConfVolumeName &&
			sts.Spec.Template.Spec.Volumes[i].Projected != nil {
			configMode = sts.Spec.Template.Spec.Volumes[i].Projected.DefaultMode
			break
		}
	}
	if configMode == nil || *configMode != configVolumeDefaultMode {
		t.Fatalf("config volume defaultMode = %v, want %o", configMode, configVolumeDefaultMode)
	}

	podSC := sts.Spec.Template.Spec.SecurityContext
	if podSC == nil || podSC.FSGroup == nil || *podSC.FSGroup != 7474 {
		t.Fatalf("expected pod fsGroup 7474, got %#v", podSC)
	}

	if sts.Spec.Template.Annotations[rendercfg.ConfigChecksumAnnotation] == "" {
		t.Fatal("expected config checksum annotation on pod template")
	}
	if envByName[rendercfg.ConfigChecksumEnv] != sts.Spec.Template.Annotations[rendercfg.ConfigChecksumAnnotation] {
		t.Fatalf("checksum env must match annotation")
	}
	if _, ok := envByName["NEO4J_PLUGINS"]; ok {
		t.Fatal("NEO4J_PLUGINS should be omitted when spec.plugins is empty")
	}
}

func TestClusterPoolStatefulSet(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition: neo4jv1beta1.EditionEnterprise,
			Version: "2026.05.0",
			License: neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{
				Mode: neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{
					Members: 3,
					Plugins: []string{"apoc"},
				},
				Secondaries: &neo4jv1beta1.SecondariesSpec{
					Analytics: &neo4jv1beta1.SecondaryPoolSpec{
						Members: 1,
						Plugins: []string{"gds"},
					},
				},
			},
			PluginDefinitions: map[string]neo4jv1beta1.PluginDefinitionSpec{
				"gds": {LicenseSecretRef: "gds-license"},
			},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "10Gi"},
					},
				},
			},
		},
	}

	primary := PoolStatefulSet(render.ContextForPool(neo4j, render.PoolPrimary))
	if primary.Name != "prod-primary" {
		t.Fatalf("primary sts = %q", primary.Name)
	}
	if *primary.Spec.Replicas != 3 {
		t.Fatalf("primary replicas = %d", *primary.Spec.Replicas)
	}
	if primary.Spec.PodManagementPolicy != appsv1.ParallelPodManagement {
		t.Fatalf("podManagementPolicy = %q, want Parallel (cluster quorum)", primary.Spec.PodManagementPolicy)
	}
	primaryEnv := map[string]string{}
	for _, e := range primary.Spec.Template.Spec.Containers[0].Env {
		if e.Value != "" {
			primaryEnv[e.Name] = e.Value
		}
	}
	if primaryEnv["SERVICE_NEO4J"] != "$(POD_NAME).$(NAMESPACE).svc.$(CLUSTER_DOMAIN)" {
		t.Fatalf("SERVICE_NEO4J = %q", primaryEnv["SERVICE_NEO4J"])
	}
	if primaryEnv["SERVICE_NEO4J_INTERNALS"] != "$(POD_NAME)-internals.$(NAMESPACE).svc.$(CLUSTER_DOMAIN)" {
		t.Fatalf("SERVICE_NEO4J_INTERNALS = %q", primaryEnv["SERVICE_NEO4J_INTERNALS"])
	}
	if primary.Spec.Template.Spec.Containers[0].StartupProbe == nil ||
		primary.Spec.Template.Spec.Containers[0].ReadinessProbe == nil {
		t.Fatal("expected startup and readiness probes on Neo4j container")
	}
	primaryPorts := map[int32]bool{}
	for _, p := range primary.Spec.Template.Spec.Containers[0].Ports {
		primaryPorts[p.ContainerPort] = true
	}
	for _, port := range []int32{7474, 7687, 7688, 5000, 6000, 7000} {
		if !primaryPorts[port] {
			t.Fatalf("primary container missing port %d", port)
		}
	}

	analytics := PoolStatefulSet(render.ContextForPool(neo4j, render.PoolAnalytics))
	if analytics.Name != "prod-analytics" {
		t.Fatalf("analytics sts = %q", analytics.Name)
	}
	envByName := map[string]string{}
	for _, e := range analytics.Spec.Template.Spec.Containers[0].Env {
		if e.Value != "" {
			envByName[e.Name] = e.Value
		}
	}
	if envByName["NEO4J_PLUGINS"] != `["graph-data-science"]` {
		t.Fatalf("analytics NEO4J_PLUGINS = %q", envByName["NEO4J_PLUGINS"])
	}
	var licenseMount *corev1.VolumeMount
	for i := range analytics.Spec.Template.Spec.Containers[0].VolumeMounts {
		if analytics.Spec.Template.Spec.Containers[0].VolumeMounts[i].Name == "license-gds" {
			licenseMount = &analytics.Spec.Template.Spec.Containers[0].VolumeMounts[i]
			break
		}
	}
	if licenseMount == nil || licenseMount.MountPath != "/licenses/gds" {
		t.Fatalf("expected gds license mount, got %#v", licenseMount)
	}
}

func TestStandaloneStatefulSetNEO4JPlugins(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition:  neo4jv1beta1.EditionEnterprise,
			Version:  "2026.05.0",
			License:  neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Plugins:  []string{"apoc", "gds"},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "10Gi"},
					},
				},
			},
		},
	}
	sts := StandaloneStatefulSet(render.StandaloneContext(neo4j))
	envByName := map[string]string{}
	for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
		if e.Value != "" {
			envByName[e.Name] = e.Value
		}
	}
	if envByName["NEO4J_PLUGINS"] != `["apoc","graph-data-science"]` {
		t.Fatalf("NEO4J_PLUGINS = %q", envByName["NEO4J_PLUGINS"])
	}
}

func TestStandaloneContainerPortsIncludeOptionalListeners(t *testing.T) {
	https := int32(7473)
	backup := int32(6362)
	metrics := int32(2004)
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition:  neo4jv1beta1.EditionEnterprise,
			Version:  "2026.05.0",
			License:  neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Connectivity: &neo4jv1beta1.ConnectivitySpec{
				Listeners: &neo4jv1beta1.ConnectivityListenersSpec{
					HTTPS:   &https,
					Backup:  &backup,
					Metrics: &metrics,
				},
			},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "10Gi"},
					},
				},
			},
		},
	}
	ports := map[string]int32{}
	for _, p := range StandaloneStatefulSet(render.StandaloneContext(neo4j)).Spec.Template.Spec.Containers[0].Ports {
		ports[p.Name] = p.ContainerPort
	}
	for name, want := range map[string]int32{
		"http": 7474, "bolt": 7687, "https": 7473, "backup": 6362, "tcp-prometheus": 2004,
	} {
		if ports[name] != want {
			t.Fatalf("port %s = %d, want %d (%#v)", name, ports[name], want, ports)
		}
	}
}
