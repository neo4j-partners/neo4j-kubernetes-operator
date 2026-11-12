package neo4jstudio

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	neo4jv1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1"
)

func studioScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := neo4jv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func studioForProvision(secretName string) *neo4jv1.Neo4jStudio {
	return &neo4jv1.Neo4jStudio{
		ObjectMeta: metav1.ObjectMeta{Name: "nes", Namespace: "ns"},
		Spec: neo4jv1.Neo4jStudioSpec{
			AssetStore: neo4jv1.StudioAssetStore{
				Neo4jRef:  &neo4jv1.Neo4jRef{Name: "cl"},
				Provision: true,
				Database:  "studio-assets",
				Auth:      neo4jv1.CredentialRef{SecretRef: neo4jv1.SecretRef{Name: secretName}},
			},
		},
	}
}

// When provisioning and the auth Secret is absent, the operator generates it (owned), returning a
// default user and a non-empty password.
func TestAssetStoreCredsGeneratesSecretWhenAbsent(t *testing.T) {
	s := studioScheme(t)
	studio := studioForProvision("gen-auth")
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(studio).Build()
	r := &StudioReconciler{Client: c, Scheme: s}

	user, pass, err := r.assetStoreCreds(context.Background(), studio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user != assetStoreDefaultUser || pass == "" {
		t.Fatalf("want generated creds (user %q, non-empty password), got user=%q passlen=%d", assetStoreDefaultUser, user, len(pass))
	}

	var sec corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Name: "gen-auth", Namespace: "ns"}, &sec); err != nil {
		t.Fatalf("secret was not created: %v", err)
	}
	if len(sec.OwnerReferences) != 1 || sec.OwnerReferences[0].Name != "nes" {
		t.Fatalf("generated secret must be owned by the Neo4jStudio, got %+v", sec.OwnerReferences)
	}
}

// An existing Secret is used verbatim and never overwritten — this is also the stability guarantee
// for later reconciles (once generated, the Secret exists and is read, not regenerated).
func TestAssetStoreCredsUsesExistingSecretVerbatim(t *testing.T) {
	s := studioScheme(t)
	studio := studioForProvision("byo")
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "byo", Namespace: "ns"},
		Data:       map[string][]byte{"username": []byte("alice"), "password": []byte("s3cret")},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(studio, existing).Build()
	r := &StudioReconciler{Client: c, Scheme: s}

	user, pass, err := r.assetStoreCreds(context.Background(), studio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user != "alice" || pass != "s3cret" {
		t.Fatalf("existing secret must be used verbatim, got %q/%q", user, pass)
	}
	// No owner reference stamped on a BYO secret the operator did not create.
	var sec corev1.Secret
	_ = c.Get(context.Background(), types.NamespacedName{Name: "byo", Namespace: "ns"}, &sec)
	if len(sec.OwnerReferences) != 0 {
		t.Fatalf("BYO secret must not be adopted, got owner refs %+v", sec.OwnerReferences)
	}
}

// An existing Secret missing a key is a clear error, not a silent generate.
func TestAssetStoreCredsRejectsIncompleteSecret(t *testing.T) {
	s := studioScheme(t)
	studio := studioForProvision("partial")
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "partial", Namespace: "ns"},
		Data:       map[string][]byte{"username": []byte("alice")}, // no password
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(studio, existing).Build()
	r := &StudioReconciler{Client: c, Scheme: s}

	if _, _, err := r.assetStoreCreds(context.Background(), studio); err == nil {
		t.Fatal("want an error for a secret missing the password key")
	}
}
