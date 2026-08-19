package workload

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
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
	rendertrust "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/trust"
)

func TestTLSSecretRotationRollsStatefulSet(t *testing.T) {
	s := workloadScheme(t)
	neo4j := standaloneWithBolt(t)
	key := tlsSecret("dev-bolt-key", "private.key", []byte("old-key"))
	cert := tlsSecret("dev-bolt-cert", "public.crt", []byte("old-cert"))
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(neo4j, key, cert).WithStatusSubresource(neo4j).Build()
	r := New(c, s)

	if out := r.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatalf("initial reconcile: %v", out.Err)
	}
	before := mustSTS(t, c, neo4j)
	beforeSum := before.Spec.Template.Annotations[rendertrust.ChecksumAnnotation]
	if beforeSum == "" {
		t.Fatal("expected tls-checksum on pod template")
	}

	rotated := &corev1.Secret{}
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(cert), rotated); err != nil {
		t.Fatalf("get cert: %v", err)
	}
	rotated.Data["public.crt"] = []byte("new-cert")
	if err := c.Update(t.Context(), rotated); err != nil {
		t.Fatalf("rotate cert: %v", err)
	}
	if out := r.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatalf("reconcile after rotate: %v", out.Err)
	}
	after := mustSTS(t, c, neo4j)
	afterSum := after.Spec.Template.Annotations[rendertrust.ChecksumAnnotation]
	if afterSum == "" || afterSum == beforeSum {
		t.Fatalf("tls-checksum did not change: before=%s after=%s", beforeSum, afterSum)
	}
}

func TestRolloutRestartAnnotationIsKept(t *testing.T) {
	s := workloadScheme(t)
	neo4j := standaloneWithBolt(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		neo4j,
		tlsSecret("dev-bolt-key", "private.key", []byte("key")),
		tlsSecret("dev-bolt-cert", "public.crt", []byte("cert")),
	).WithStatusSubresource(neo4j).Build()
	r := New(c, s)
	if out := r.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatalf("initial reconcile: %v", out.Err)
	}

	sts := mustSTS(t, c, neo4j)
	if sts.Spec.Template.Annotations == nil {
		sts.Spec.Template.Annotations = map[string]string{}
	}
	sts.Spec.Template.Annotations[kubectlRestartedAt] = "2026-08-19T12:00:00Z"
	if err := c.Update(t.Context(), sts); err != nil {
		t.Fatalf("stamp restartedAt: %v", err)
	}
	if out := r.Reconcile(t.Context(), neo4j); out.Err != nil {
		t.Fatalf("reconcile after restart: %v", out.Err)
	}
	got := mustSTS(t, c, neo4j)
	if got.Spec.Template.Annotations[kubectlRestartedAt] != "2026-08-19T12:00:00Z" {
		t.Fatalf("restartedAt was clobbered: %#v", got.Spec.Template.Annotations)
	}
}

func workloadScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("core scheme: %v", err)
	}
	if err := neo4jv1beta1.AddToScheme(s); err != nil {
		t.Fatalf("neo4j scheme: %v", err)
	}
	return s
}

func standaloneWithBolt(t *testing.T) *neo4jv1beta1.Neo4j {
	t.Helper()
	return &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Edition: neo4jv1beta1.EditionEnterprise,
			Version: "2026.05.0",
			License: &neo4jv1beta1.LicenseSpec{Accept: neo4jv1beta1.LicenseAcceptYes},
			Topology: neo4jv1beta1.TopologySpec{
				Mode: neo4jv1beta1.TopologyModeStandalone,
			},
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:    neo4jv1beta1.VolumeModeDynamic,
						Dynamic: &neo4jv1beta1.DynamicVolumeSpec{Size: "10Gi"},
					},
				},
			},
			Trust: &neo4jv1beta1.TrustSpec{
				Enabled: true,
				Certificates: &neo4jv1beta1.TrustCertificatesSpec{
					Bolt: &neo4jv1beta1.TLSPolicySpec{
						PrivateKey:        &neo4jv1beta1.TLSSecretKeyRef{SecretName: "dev-bolt-key", SubPath: "private.key"},
						PublicCertificate: &neo4jv1beta1.TLSSecretKeyRef{SecretName: "dev-bolt-cert", SubPath: "public.crt"},
					},
				},
			},
		},
	}
}

func tlsSecret(name, key string, value []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    rendersecrets.WithMountableLabel(nil),
		},
		Data: map[string][]byte{key: value},
	}
}

func mustSTS(t *testing.T, c client.Client, neo4j *neo4jv1beta1.Neo4j) *appsv1.StatefulSet {
	t.Helper()
	sts := &appsv1.StatefulSet{}
	if err := c.Get(t.Context(), client.ObjectKey{Name: neo4j.Name + "-server", Namespace: neo4j.Namespace}, sts); err != nil {
		t.Fatalf("get statefulset: %v", err)
	}
	return sts
}
