package v1beta1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestNeo4jGroupVersionKind(t *testing.T) {
	gvk := GroupVersion.WithKind("Neo4j")
	obj := &Neo4j{}
	obj.APIVersion = gvk.GroupVersion().String()
	obj.Kind = gvk.Kind
	if obj.GetObjectKind().GroupVersionKind() != gvk {
		t.Fatalf("expected GVK %v, got %v", gvk, obj.GetObjectKind().GroupVersionKind())
	}
}

func TestNeo4jSchemeRegistration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	gvk := GroupVersion.WithKind("Neo4j")
	if _, err := scheme.New(gvk); err != nil {
		t.Fatalf("scheme.New: %v", err)
	}
}

func TestNeo4jDeepCopy(t *testing.T) {
	trueVal := true
	orig := &Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "graph-dev"},
		Spec: Neo4jSpec{
			Edition: EditionEnterprise,
			Version: "2026.05.0",
			License: LicenseSpec{Accept: LicenseAcceptYes},
			Topology: TopologySpec{Mode: TopologyModeStandalone},
			Auth:     &AuthSpec{GeneratePassword: &trueVal},
		},
	}
	copy := orig.DeepCopy()
	if copy == orig {
		t.Fatal("DeepCopy returned same pointer")
	}
	if copy.Spec.Auth.GeneratePassword == orig.Spec.Auth.GeneratePassword {
		t.Fatal("expected distinct GeneratePassword pointer after DeepCopy")
	}
	if *copy.Spec.Auth.GeneratePassword != *orig.Spec.Auth.GeneratePassword {
		t.Fatal("GeneratePassword value mismatch after DeepCopy")
	}
}

func TestStandaloneTopologyFields(t *testing.T) {
	spec := Neo4jSpec{
		Edition: EditionEnterprise,
		Version: "2026.05.0",
		License: LicenseSpec{Accept: LicenseAcceptYes},
		Topology: TopologySpec{
			Mode: TopologyModeStandalone,
		},
	}
	if spec.Topology.Primaries != nil || spec.Topology.Secondaries != nil {
		t.Fatal("standalone spec should not set primaries or secondaries in minimal example")
	}
}

func TestClusterReadPoolForScale(t *testing.T) {
	members := int32(3)
	spec := Neo4jSpec{
		Topology: TopologySpec{
			Mode: TopologyModeCluster,
			Primaries: &PrimariesSpec{
				Members: 3,
			},
			Secondaries: &SecondariesSpec{
				Read: &SecondaryPoolSpec{Members: members},
			},
		},
	}
	if spec.Topology.Secondaries.Read.Members != members {
		t.Fatalf("read pool members = %d, want %d", spec.Topology.Secondaries.Read.Members, members)
	}
}
