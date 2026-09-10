package neo4j

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/render"
	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
)

func TestMapSecretToNeo4jEnqueuesMountingCR(t *testing.T) {
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := neo4jv1beta1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	cr := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-cm", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Topology: neo4jv1beta1.TopologySpec{Mode: neo4jv1beta1.TopologyModeStandalone},
			Trust: &neo4jv1beta1.TrustSpec{
				Enabled: true,
				CertManager: &neo4jv1beta1.CertManagerSpec{
					Enabled:   true,
					IssuerRef: &neo4jv1beta1.IssuerRef{Name: "corp-ca"},
				},
				Certificates: &neo4jv1beta1.TrustCertificatesSpec{
					Bolt: &neo4jv1beta1.TLSPolicySpec{SecretName: "prod-cm-bolt-tls"},
				},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prod-cm-bolt-tls",
			Namespace: "default",
			Labels:    rendersecrets.WithMountableLabel(nil),
		},
	}
	other := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unrelated",
			Namespace: "default",
			Labels:    rendersecrets.WithMountableLabel(nil),
		},
	}
	unlabeled := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-cm-bolt-tls", Namespace: "default"},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, secret, other).Build()
	r := &Neo4jReconciler{Client: c}

	got := r.mapSecretToNeo4j(t.Context(), secret)
	if len(got) != 1 || got[0].Name != "prod-cm" {
		t.Fatalf("tls secret enqueue = %#v", got)
	}
	if reqs := r.mapSecretToNeo4j(t.Context(), other); len(reqs) != 0 {
		t.Fatalf("unrelated secret enqueue = %#v", reqs)
	}
	if reqs := r.mapSecretToNeo4j(t.Context(), unlabeled); len(reqs) != 0 {
		t.Fatalf("unlabeled secret enqueue = %#v", reqs)
	}
}

func watchScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := neo4jv1beta1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func storageClaim(name, instance string) *corev1.PersistentVolumeClaim {
	l := render.OperandInstanceLabels(instance)
	l[render.LabelComponent] = render.ComponentStorage
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: l},
	}
}

// A Dynamic claim is created by the StatefulSet controller, so the instance label is the only
// thing tying it back to a CR. It must not take a List to find that out.
func TestMapPVCToNeo4jFollowsOperandLabels(t *testing.T) {
	cr := &neo4jv1beta1.Neo4j{ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(watchScheme(t)).WithObjects(cr).Build()
	r := &Neo4jReconciler{Client: c}

	got := r.mapPVCToNeo4j(t.Context(), storageClaim("data-prod-server-0", "prod"))
	if len(got) != 1 || got[0].Name != "prod" || got[0].Namespace != "default" {
		t.Fatalf("dynamic claim enqueue = %#v", got)
	}

	// Provenance must be complete: a claim carrying the component label but not managed-by is
	// somebody else's object that happens to share a naming convention.
	forged := storageClaim("data-prod-server-0", "prod")
	delete(forged.Labels, render.LabelManagedBy)
	if reqs := r.mapPVCToNeo4j(t.Context(), forged); len(reqs) != 0 {
		t.Fatalf("claim without managed-by enqueue = %#v", reqs)
	}

	if reqs := r.mapPVCToNeo4j(t.Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "not-a-claim", Namespace: "default"},
	}); len(reqs) != 0 {
		t.Fatalf("non-claim enqueue = %#v", reqs)
	}
}

// An Existing.claimName PVC is the user's own object: it carries no operator label, and the CR
// that binds it can only be found by name. Pods cannot start on an unbound claim, so no
// StatefulSet event will arrive to cover for a missing bind notification here.
func TestMapPVCToNeo4jFindsClaimBoundByName(t *testing.T) {
	byo := &neo4jv1beta1.Neo4j{
		ObjectMeta: metav1.ObjectMeta{Name: "byo", Namespace: "default"},
		Spec: neo4jv1beta1.Neo4jSpec{
			Storage: &neo4jv1beta1.StorageSpec{
				Volumes: &neo4jv1beta1.VolumesSpec{
					Data: neo4jv1beta1.DataVolumeSpec{
						Mode:     neo4jv1beta1.VolumeModeExisting,
						Existing: &neo4jv1beta1.ExistingVolumeSpec{ClaimName: "warm-restore"},
					},
				},
			},
		},
	}
	dynamic := &neo4jv1beta1.Neo4j{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(watchScheme(t)).WithObjects(byo, dynamic).Build()
	r := &Neo4jReconciler{Client: c}

	claim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "warm-restore", Namespace: "default"},
	}
	got := r.mapPVCToNeo4j(t.Context(), claim)
	if len(got) != 1 || got[0].Name != "byo" {
		t.Fatalf("byo claim enqueue = %#v", got)
	}

	stranger := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "someone-elses", Namespace: "default"},
	}
	if reqs := r.mapPVCToNeo4j(t.Context(), stranger); len(reqs) != 0 {
		t.Fatalf("unreferenced claim enqueue = %#v", reqs)
	}
}

func TestPVCStatusChangedIgnoresNoise(t *testing.T) {
	bound := func() *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data-prod-server-0", Namespace: "default"},
			Status: corev1.PersistentVolumeClaimStatus{
				Phase:    corev1.ClaimBound,
				Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("5Gi")},
			},
		}
	}
	p := pvcStatusChanged()

	// Binding stamps annotations such as pv.kubernetes.io/bind-completed on the same object. Each
	// is an Update, and each would otherwise cost a full pipeline pass.
	noisy := bound()
	noisy.Annotations = map[string]string{"pv.kubernetes.io/bind-completed": "yes"}
	if p.Update(event.UpdateEvent{ObjectOld: bound(), ObjectNew: noisy}) {
		t.Error("annotation-only update must not enqueue")
	}

	pending := bound()
	pending.Status.Phase = corev1.ClaimPending
	if !p.Update(event.UpdateEvent{ObjectOld: pending, ObjectNew: bound()}) {
		t.Error("bind must enqueue")
	}

	grown := bound()
	grown.Status.Capacity = corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")}
	if !p.Update(event.UpdateEvent{ObjectOld: bound(), ObjectNew: grown}) {
		t.Error("capacity catching up must enqueue")
	}

	resizing := bound()
	resizing.Status.Conditions = []corev1.PersistentVolumeClaimCondition{
		{Type: corev1.PersistentVolumeClaimFileSystemResizePending, Status: corev1.ConditionTrue},
	}
	if !p.Update(event.UpdateEvent{ObjectOld: bound(), ObjectNew: resizing}) {
		t.Error("resize condition must enqueue")
	}

	// A scale-out claim is born at the template's older size and must be grown on sight.
	if !p.Create(event.CreateEvent{Object: bound()}) {
		t.Error("claim creation must enqueue")
	}
}
