package workload

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

// NEO-3-004-IMG-01: existing STS without pull secrets must pick them up on reconcile.
func TestReconcilePropagatesImagePullSecrets(t *testing.T) {
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("core scheme: %v", err)
	}
	if err := neo4jv1beta1.AddToScheme(s); err != nil {
		t.Fatalf("neo4j scheme: %v", err)
	}

	neo4j := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev-pullsecrets", Namespace: "default"},
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

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(neo4j).WithStatusSubresource(neo4j).Build()
	r := New(c, s)

	if out := r.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatalf("initial reconcile: %v", out.Err)
	}
	sts := mustGetSTS(t, c, "dev-pullsecrets-server")
	if len(sts.Spec.Template.Spec.ImagePullSecrets) != 0 {
		t.Fatalf("expected no pull secrets initially, got %#v", sts.Spec.Template.Spec.ImagePullSecrets)
	}

	neo4j.Spec.Image = &neo4jv1beta1.ImageSpec{
		PullSecrets: []string{"my-registry-secret"},
	}
	if err := c.Update(t.Context(), neo4j); err != nil {
		t.Fatalf("update neo4j: %v", err)
	}
	if out := r.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatalf("reconcile with pullSecrets: %v", out.Err)
	}

	sts = mustGetSTS(t, c, "dev-pullsecrets-server")
	got := sts.Spec.Template.Spec.ImagePullSecrets
	if len(got) != 1 || got[0].Name != "my-registry-secret" {
		t.Fatalf("ImagePullSecrets = %#v", got)
	}
}

func mustGetSTS(t *testing.T, c client.Client, name string) *appsv1.StatefulSet {
	t.Helper()
	sts := &appsv1.StatefulSet{}
	if err := c.Get(t.Context(), client.ObjectKey{Name: name, Namespace: "default"}, sts); err != nil {
		t.Fatalf("get sts: %v", err)
	}
	return sts
}
