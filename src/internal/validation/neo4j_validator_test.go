package validation

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

func validStandalone() *neo4jv1beta1.Neo4j {
	return &neo4jv1beta1.Neo4j{
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
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "10Gi"},
					},
				},
			},
		},
	}
}

func TestValidateNeo4jOK(t *testing.T) {
	if err := ValidateNeo4j(validStandalone()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateNeo4jRejectsPrivileged(t *testing.T) {
	n := validStandalone()
	priv := true
	n.Spec.Security = &neo4jv1beta1.SecuritySpec{
		ContainerSecurityContext: &corev1.SecurityContext{Privileged: &priv},
	}
	err := ValidateNeo4j(n)
	if err == nil || !strings.Contains(err.Error(), "privileged") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateMinimumMembersImmutable(t *testing.T) {
	oldN := validStandalone()
	oldN.Spec.Topology = neo4jv1beta1.TopologySpec{
		Mode:           neo4jv1beta1.TopologyModeCluster,
		Primaries:      &neo4jv1beta1.PrimariesSpec{Members: 3},
		MinimumMembers: ptr(int32(3)),
	}
	newN := oldN.DeepCopy()
	*newN.Spec.Topology.MinimumMembers = 1
	err := validateMinimumMembersImmutable(oldN, newN)
	if err == nil || !strings.Contains(err.Error(), "minimumMembers cannot change") {
		t.Fatalf("got %v", err)
	}
	newSame := oldN.DeepCopy()
	if err := validateMinimumMembersImmutable(oldN, newSame); err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestValidateNeo4jRejectsHostPath(t *testing.T) {
	n := validStandalone()
	n.Spec.Storage.AdditionalMounts = []neo4jv1beta1.AdditionalMount{{
		Name:      "host",
		MountPath: "/host",
		Volume: corev1.Volume{VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: "/"},
		}},
	}}
	err := ValidateNeo4j(n)
	if err == nil || !strings.Contains(err.Error(), "hostPath") {
		t.Fatalf("got %v", err)
	}
}
