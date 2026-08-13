package storage

import (
	"strings"
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

func baseStorage() *neo4jv1beta1.StorageSpec {
	return &neo4jv1beta1.StorageSpec{
		Volumes: &neo4jv1beta1.VolumesSpec{
			Data: neo4jv1beta1.DataVolumeSpec{
				Mode:    neo4jv1beta1.VolumeModeDynamic,
				Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "1Gi"},
			},
		},
	}
}

func TestValidateRejectsReservedDynamicLabels(t *testing.T) {
	s := baseStorage()
	s.Volumes.Data.Dynamic.Labels = map[string]string{
		"app.kubernetes.io/instance": "victim",
	}
	err := Validate(&neo4jv1beta1.Neo4j{Spec: neo4jv1beta1.Neo4jSpec{Storage: s}})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("got %v", err)
	}
}

func TestDynamicPVCKeepsOperatorIdentityLabels(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode: neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{
							Size: "1Gi",
							Labels: map[string]string{
								"team":                       "platform",
								"app.kubernetes.io/instance": "attacker",
							},
						},
					},
				},
			},
		},
	}
	// Validation would reject reserved keys; merge still keeps operator identity if they slipped through.
	pvc := dynamicPVC(render.StandaloneContext(neo4j), "data", neo4j.Spec.Storage.Volumes.Data.Dynamic)
	if pvc.Labels["app.kubernetes.io/instance"] != "dev" {
		t.Fatalf("instance = %q", pvc.Labels["app.kubernetes.io/instance"])
	}
	if pvc.Labels["team"] != "platform" {
		t.Fatalf("user label lost: %#v", pvc.Labels)
	}
}

func TestValidateRejectsWorldReadableSecretMode(t *testing.T) {
	mode := int32(0o777)
	s := baseStorage()
	s.SecretMounts = map[string]neo4jv1beta1.SecretMountSpec{
		"creds": {
			SecretName:  "my-creds",
			MountPath:   "/var/secrets/creds",
			Items:       []neo4jv1beta1.SecretKeyToPath{{Key: "token", Path: "token"}},
			DefaultMode: &mode,
		},
	}
	err := Validate(&neo4jv1beta1.Neo4j{Spec: neo4jv1beta1.Neo4jSpec{Storage: s}})
	if err == nil || !strings.Contains(err.Error(), "0440") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRejectsHostPathAdditionalMount(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{Spec: neo4jv1beta1.Neo4jSpec{Storage: baseStorage()}}
	neo4j.Spec.Storage.AdditionalMounts = []neo4jv1beta1.AdditionalMount{{
		Name:      "host",
		MountPath: "/host",
		Volume: corev1.Volume{VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: "/"},
		}},
	}}
	err := Validate(neo4j)
	if err == nil || !strings.Contains(err.Error(), "hostPath") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRejectsReservedAdditionalMount(t *testing.T) {
	cases := []struct {
		name, mount, want string
	}{
		{name: "data", mount: "/extra", want: "reserved"},
		{name: "wedge", mount: "/data", want: "reserved path"},
		{name: "wedge", mount: "/data/sub", want: "reserved path"},
		{name: "wedge", mount: "/var/lib/neo4j/certificates", want: "reserved path"},
		{name: "wedge", mount: "/config/neo4j.conf", want: "reserved path"},
	}
	for _, tc := range cases {
		s := baseStorage()
		s.AdditionalMounts = []neo4jv1beta1.AdditionalMount{{
			Name: tc.name, MountPath: tc.mount,
			Volume: corev1.Volume{VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		}}
		err := Validate(&neo4jv1beta1.Neo4j{Spec: neo4jv1beta1.Neo4jSpec{Storage: s}})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s@%s: got %v want substring %q", tc.name, tc.mount, err, tc.want)
		}
	}
	s := baseStorage()
	s.AdditionalMounts = []neo4jv1beta1.AdditionalMount{
		{Name: "a", MountPath: "/ok1", Volume: corev1.Volume{VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
		{Name: "a", MountPath: "/ok2", Volume: corev1.Volume{VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
	}
	err := Validate(&neo4jv1beta1.Neo4j{Spec: neo4jv1beta1.Neo4jSpec{Storage: s}})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("dup: got %v", err)
	}
}

func TestValidateAllowsNonReservedAdditionalMount(t *testing.T) {
	s := baseStorage()
	s.AdditionalMounts = []neo4jv1beta1.AdditionalMount{{
		Name: "extra-data", MountPath: "/extra-data",
		Volume: corev1.Volume{VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}}
	if err := Validate(&neo4jv1beta1.Neo4j{Spec: neo4jv1beta1.Neo4jSpec{Storage: s}}); err != nil {
		t.Fatal(err)
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
					"creds": {
						SecretName: "my-creds",
						MountPath:  "/var/secrets/creds",
						Items:      []neo4jv1beta1.SecretKeyToPath{{Key: "token", Path: "token"}},
					},
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
