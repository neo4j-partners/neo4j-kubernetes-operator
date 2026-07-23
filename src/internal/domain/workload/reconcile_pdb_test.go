package workload

import (
	"testing"

	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

func TestReconcilePDBCreateAndDelete(t *testing.T) {
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("core scheme: %v", err)
	}
	if err := neo4jv1beta1.AddToScheme(s); err != nil {
		t.Fatalf("neo4j scheme: %v", err)
	}
	if err := policyv1.AddToScheme(s); err != nil {
		t.Fatalf("policy scheme: %v", err)
	}

	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition: neo4jv1beta1.EditionEnterprise,
			Version: "2026.05.0",
			License: neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{
				Mode:      neo4jv1beta1.TopologyModeCluster,
				Primaries: &neo4jv1beta1.PrimariesSpec{Members: 3},
			},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "10Gi"},
					},
				},
			},
			PodDisruptionBudget: &neo4jv1beta1.PodDisruptionBudgetSpec{Enabled: true},
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(neo4j).WithStatusSubresource(neo4j).Build()
	r := New(c, s)
	if out := r.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatalf("reconcile with PDB: %v", out.Err)
	}

	pdb := &policyv1.PodDisruptionBudget{}
	if err := c.Get(t.Context(), client.ObjectKey{Name: "prod-pdb", Namespace: "default"}, pdb); err != nil {
		t.Fatalf("get pdb: %v", err)
	}
	if pdb.Spec.MinAvailable == nil || pdb.Spec.MinAvailable.IntValue() != 2 {
		t.Fatalf("minAvailable = %#v", pdb.Spec.MinAvailable)
	}

	neo4j.Spec.PodDisruptionBudget.Enabled = false
	if err := c.Update(t.Context(), neo4j); err != nil {
		t.Fatalf("update neo4j: %v", err)
	}
	if out := r.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatalf("reconcile disable PDB: %v", out.Err)
	}
	if err := c.Get(t.Context(), client.ObjectKey{Name: "prod-pdb", Namespace: "default"}, pdb); err == nil {
		t.Fatal("expected PDB deleted when disabled")
	}
}
