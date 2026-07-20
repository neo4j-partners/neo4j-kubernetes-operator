package serverconfig

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/domain/workload"
	rendercfg "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/serverconfig"
)

func TestConfigReconcileUpdatesConfigMapAndRollsWorkload(t *testing.T) {
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("core scheme: %v", err)
	}
	if err := neo4jv1beta1.AddToScheme(s); err != nil {
		t.Fatalf("neo4j scheme: %v", err)
	}

	neo4j := standaloneNeo4j(t)
	neo4j.Spec.Config = &neo4jv1beta1.ConfigSpec{
		Neo4j: map[string]string{"db.transaction.timeout": "42s"},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(neo4j).WithStatusSubresource(neo4j).Build()
	cfg := New(c, s)
	wl := workload.New(c, s)

	if out := cfg.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatalf("initial config reconcile: %v", out.Err)
	}
	if out := wl.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatalf("initial workload reconcile: %v", out.Err)
	}

	before := mustGetConfigMap(t, c, neo4j)
	beforeSTS := mustGetStatefulSet(t, c, neo4j)
	beforeChecksum := beforeSTS.Spec.Template.Annotations[rendercfg.ConfigChecksumAnnotation]

	neo4j.Spec.Config.Neo4j["dbms.security.auth_minimum_password_length"] = "7"
	if err := c.Update(t.Context(), neo4j); err != nil {
		t.Fatalf("update neo4j: %v", err)
	}

	if out := cfg.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatalf("config reconcile after patch: %v", out.Err)
	}
	if out := wl.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatalf("workload reconcile after patch: %v", out.Err)
	}

	after := mustGetConfigMap(t, c, neo4j)
	afterSTS := mustGetStatefulSet(t, c, neo4j)
	afterChecksum := afterSTS.Spec.Template.Annotations[rendercfg.ConfigChecksumAnnotation]

	for key, want := range map[string]string{
		"db.transaction.timeout":                    "42s",
		"dbms.security.auth_minimum_password_length": "7",
	} {
		if after.Data[key] != want {
			t.Fatalf("configmap key %q = %q, want %q", key, after.Data[key], want)
		}
	}
	if after.Data["db.transaction.timeout"] == before.Data["db.transaction.timeout"] &&
		after.Data["dbms.security.auth_minimum_password_length"] == before.Data["dbms.security.auth_minimum_password_length"] {
		t.Fatalf("configmap data was not updated")
	}
	if afterChecksum == "" {
		t.Fatal("statefulset missing config checksum annotation")
	}
	if beforeChecksum == afterChecksum {
		t.Fatalf("statefulset checksum did not change: %s", afterChecksum)
	}

	env := envValue(afterSTS, rendercfg.ConfigChecksumEnv)
	if env != afterChecksum {
		t.Fatalf("checksum env %q != annotation %q", env, afterChecksum)
	}
}

func standaloneNeo4j(t *testing.T) *neo4jv1beta1.Neo4j {
	t.Helper()
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition: neo4jv1beta1.EditionEnterprise,
			Version: "2026.05.0",
			License: neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{
				Mode: neo4jv1beta1.TopologyModeStandalone,
			},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode: neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{
							Size: "10Gi",
						},
					},
				},
			},
		},
	}
}

func mustGetConfigMap(t *testing.T, c client.Client, neo4j *neo4jv1beta1.Neo4j) *corev1.ConfigMap {
	t.Helper()
	cm := &corev1.ConfigMap{}
	if err := c.Get(t.Context(), client.ObjectKey{Name: neo4j.Name + "-config", Namespace: neo4j.Namespace}, cm); err != nil {
		t.Fatalf("get configmap: %v", err)
	}
	return cm
}

func mustGetStatefulSet(t *testing.T, c client.Client, neo4j *neo4jv1beta1.Neo4j) *appsv1.StatefulSet {
	t.Helper()
	sts := &appsv1.StatefulSet{}
	if err := c.Get(t.Context(), client.ObjectKey{Name: neo4j.Name + "-server", Namespace: neo4j.Namespace}, sts); err != nil {
		t.Fatalf("get statefulset: %v", err)
	}
	return sts
}

func envValue(sts *appsv1.StatefulSet, name string) string {
	for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}
