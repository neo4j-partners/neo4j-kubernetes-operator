package workload

import (
	"testing"

	neo4jv1beta1 "github.com/neo-technology-field/ps-kubernetes-operator/src/api/v1beta1"
	"github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render"
	rendercfg "github.com/neo-technology-field/ps-kubernetes-operator/src/internal/render/serverconfig"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStandaloneStatefulSet(t *testing.T) {
	size := "10Gi"
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition: neo4jv1beta1.EditionEnterprise,
			Version: "2026.05.0",
			License: neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode: neo4jv1beta1.VolumeModeDynamic,
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
	if envByName["NEO4J_CONF"] != "/config" {
		t.Fatalf("NEO4J_CONF = %q", envByName["NEO4J_CONF"])
	}

	mounts := sts.Spec.Template.Spec.Containers[0].VolumeMounts
	var configMount *corev1.VolumeMount
	for i := range mounts {
		if mounts[i].Name == "config" {
			configMount = &mounts[i]
			break
		}
	}
	if configMount == nil {
		t.Fatal("expected config volume mount")
	}
	if configMount.MountPath != "/config" {
		t.Fatalf("config mount path = %q", configMount.MountPath)
	}
	if configMount.SubPath != "" {
		t.Fatalf("config mount must not use subPath, got %q", configMount.SubPath)
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

func TestStandaloneStatefulSetNEO4JPlugins(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition: neo4jv1beta1.EditionEnterprise,
			Version: "2026.05.0",
			License: neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Plugins: []string{"apoc", "gds"},
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
