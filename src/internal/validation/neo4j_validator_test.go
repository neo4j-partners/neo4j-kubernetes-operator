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
			License:  &neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
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

func TestValidateUpdateSkipsSpecOnDelete(t *testing.T) {
	n := validStandalone()
	n.Spec.Security = &neo4jv1beta1.SecuritySpec{
		NetworkPolicy: &neo4jv1beta1.NetworkPolicySpec{Enabled: true},
	}
	now := metav1.Now()
	n.DeletionTimestamp = &now
	v := &Neo4jValidator{}
	if _, err := v.ValidateUpdate(t.Context(), n, n); err != nil {
		t.Fatalf("deleting object must skip spec validation, got %v", err)
	}
}

// The gate is read once at bootstrap, so it cannot move afterwards.
func TestValidateMinimumMembersImmutable(t *testing.T) {
	v := &Neo4jValidator{}
	three, five := int32(3), int32(5)
	old := validStandalone()
	old.Spec.Topology = neo4jv1beta1.TopologySpec{
		Mode:           neo4jv1beta1.TopologyModeCluster,
		Primaries:      &neo4jv1beta1.PrimariesSpec{Members: 5},
		MinimumMembers: &five,
	}
	updated := old.DeepCopy()
	updated.Spec.Topology.MinimumMembers = &three
	if _, err := v.ValidateUpdate(t.Context(), old, updated); err == nil ||
		!strings.Contains(err.Error(), "minimumMembers cannot change") {
		t.Fatalf("got %v", err)
	}

	// Scaling the pool below the immutable gate is allowed: the value is inert after bootstrap.
	shrunk := old.DeepCopy()
	shrunk.Spec.Topology.Primaries.Members = 3
	if _, err := v.ValidateUpdate(t.Context(), old, shrunk); err != nil {
		t.Fatalf("scale-in below the gate must pass, got %v", err)
	}
}

// At create the gate must fit the pool it has to bootstrap, or the DBMS never comes online.
func TestValidateCreateRejectsGateAbovePool(t *testing.T) {
	v := &Neo4jValidator{}
	five := int32(5)
	n := validStandalone()
	n.Spec.Topology = neo4jv1beta1.TopologySpec{
		Mode:           neo4jv1beta1.TopologyModeCluster,
		Primaries:      &neo4jv1beta1.PrimariesSpec{Members: 3},
		MinimumMembers: &five,
	}
	if _, err := v.ValidateCreate(t.Context(), n); err == nil ||
		!strings.Contains(err.Error(), "cannot exceed topology.primaries.members") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRejectsOversizedCluster(t *testing.T) {
	n := validStandalone()
	n.Spec.Topology = neo4jv1beta1.TopologySpec{
		Mode:      neo4jv1beta1.TopologyModeCluster,
		Primaries: &neo4jv1beta1.PrimariesSpec{Members: 99},
	}
	err := ValidateNeo4j(n)
	if err == nil || !strings.Contains(err.Error(), "primaries.members") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRejectsInvalidListenPort(t *testing.T) {
	n := validStandalone()
	bad := int32(0)
	n.Spec.Connectivity = &neo4jv1beta1.ConnectivitySpec{
		Listeners: &neo4jv1beta1.ConnectivityListenersSpec{Bolt: &bad},
	}
	err := ValidateNeo4j(n)
	if err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("got %v", err)
	}
}

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
