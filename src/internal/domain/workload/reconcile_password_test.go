package workload

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rendersecrets "github.com/neo4j/neo4j-kubernetes-operator/src/internal/render/secrets"
)

// The Neo4j image entrypoint hands the password to `neo4j-admin dbms set-initial-password`
// as a positional argument with no "--" separator, and parses NEO4J_AUTH on "/". A password
// drawn outside this alphabet crash-loops the pod, so the draw is checked over enough
// iterations to catch a single stray symbol (a leading "-" was 1 in 64 with base64).
func TestRandomPasswordStaysInTheSafeAlphabet(t *testing.T) {
	for i := 0; i < 5000; i++ {
		password, err := randomPassword(generatedPasswordLength)
		if err != nil {
			t.Fatal(err)
		}
		if len(password) != generatedPasswordLength {
			t.Fatalf("length %d, want %d", len(password), generatedPasswordLength)
		}
		if strings.ContainsFunc(password, func(r rune) bool {
			return !strings.ContainsRune(generatedPasswordAlphabet, r)
		}) {
			t.Fatalf("password %q leaves the alphanumeric alphabet", password)
		}
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "generated-auth"},
			Data:       map[string][]byte{rendersecrets.AuthKey: []byte("neo4j/" + password)},
		}
		if err := rendersecrets.RequireUsableAuthValue(secret); err != nil {
			t.Fatalf("generated value must satisfy the entrypoint contract: %v", err)
		}
	}
}

func TestRandomPasswordDoesNotRepeat(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 100; i++ {
		password, err := randomPassword(generatedPasswordLength)
		if err != nil {
			t.Fatal(err)
		}
		if _, dup := seen[password]; dup {
			t.Fatalf("password %q drawn twice", password)
		}
		seen[password] = struct{}{}
	}
}
