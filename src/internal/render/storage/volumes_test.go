package storage

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
)

func TestApplyDynamicDataAndShareLogs(t *testing.T) {
	shareFrom := neo4jv1beta1.ShareFromData
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "10Gi"},
					},
					Logs: &neo4jv1beta1.AuxiliaryVolumeSpec{
						Mode:      neo4jv1beta1.VolumeModeShare,
						ShareFrom: &shareFrom,
					},
				},
			},
		},
	}
	ctx := render.StandaloneContext(neo4j)
	c := &corev1.Container{}
	pod := &corev1.PodSpec{}
	vcts := Apply(ctx, c, pod)
	if len(vcts) != 1 || vcts[0].Name != "data" {
		t.Fatalf("vcts = %#v", vcts)
	}
	foundData, foundLogs := false, false
	for _, m := range c.VolumeMounts {
		if m.Name == "data" && m.MountPath == "/data" {
			foundData = true
		}
		if m.Name == "data" && m.MountPath == "/logs" && m.SubPathExpr == "logs/$(POD_NAME)" {
			foundLogs = true
		}
	}
	if !foundData || !foundLogs {
		t.Fatalf("mounts = %#v", c.VolumeMounts)
	}
}

func TestApplySharePlugins(t *testing.T) {
	shareFrom := neo4jv1beta1.ShareFromData
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "10Gi"},
					},
					Plugins: &neo4jv1beta1.AuxiliaryVolumeSpec{
						Mode:      neo4jv1beta1.VolumeModeShare,
						ShareFrom: &shareFrom,
					},
				},
			},
		},
	}
	ctx := render.StandaloneContext(neo4j)
	c := &corev1.Container{}
	pod := &corev1.PodSpec{}
	_ = Apply(ctx, c, pod)
	for _, m := range c.VolumeMounts {
		if m.Name == "data" && m.MountPath == "/plugins" && m.SubPathExpr == "plugins" {
			return
		}
	}
	t.Fatalf("expected /plugins Share mount, got %#v", c.VolumeMounts)
}

func TestApplyExistingClaimName(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode: neo4jv1beta1.VolumeModeExisting,
						Existing: &neo4jv1beta1.ExistingVolumeSpec{ClaimName: "my-data-pvc"},
					},
				},
			},
		},
	}
	ctx := render.StandaloneContext(neo4j)
	c := &corev1.Container{}
	pod := &corev1.PodSpec{}
	vcts := Apply(ctx, c, pod)
	if len(vcts) != 0 {
		t.Fatalf("expected no VCT, got %#v", vcts)
	}
	found := false
	for _, v := range pod.Volumes {
		if v.Name == "data" && v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == "my-data-pvc" {
			found = true
		}
	}
	if !found {
		t.Fatalf("volumes = %#v", pod.Volumes)
	}
	name, ok := DataPVCLookup(ctx)
	if !ok || name != "my-data-pvc" {
		t.Fatalf("lookup = %q %v", name, ok)
	}
}

func TestValidateExistingOneOf(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode: neo4jv1beta1.VolumeModeExisting,
						Existing: &neo4jv1beta1.ExistingVolumeSpec{
							ClaimName: "a",
							Volume:    &corev1.Volume{Name: "x"},
						},
					},
				},
			},
		},
	}
	if err := Validate(neo4j); err == nil {
		t.Fatal("expected oneOf error")
	}
}

func TestApplySecretAndAdditionalMounts(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "10Gi"},
					},
				},
				AdditionalMounts: []neo4jv1beta1.AdditionalMount{{
					Name:      "extra",
					MountPath: "/extra",
					Volume:    corev1.Volume{VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				}},
				SecretMounts: map[string]neo4jv1beta1.SecretMountSpec{
					"creds": {SecretName: "my-creds", MountPath: "/var/secrets/creds"},
				},
			},
		},
	}
	c := &corev1.Container{}
	pod := &corev1.PodSpec{}
	_ = Apply(render.StandaloneContext(neo4j), c, pod)
	foundExtra, foundSecret := false, false
	for _, m := range c.VolumeMounts {
		if m.MountPath == "/extra" {
			foundExtra = true
		}
		if m.MountPath == "/var/secrets/creds" {
			foundSecret = true
		}
	}
	if !foundExtra || !foundSecret {
		t.Fatalf("mounts = %#v", c.VolumeMounts)
	}
}
