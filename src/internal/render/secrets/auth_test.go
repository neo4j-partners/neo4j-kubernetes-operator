package secrets

import (
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func authSecret(value string) *corev1.Secret {
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-auth"}}
	if value != "" {
		s.Data = map[string][]byte{AuthKey: []byte(value)}
	}
	return s
}

func TestRequireUsableAuthValueAccepts(t *testing.T) {
	for _, value := range []string{
		"neo4j/s3cret-with-dashes-inside",
		"neo4j/24CharAlphanumericValue00",
		"neo4j/forced-change/true",
		"none",
	} {
		if err := RequireUsableAuthValue(authSecret(value)); err != nil {
			t.Errorf("value %q must be accepted, got %v", value, err)
		}
	}
}

func TestRequireUsableAuthValueRejects(t *testing.T) {
	// Every case makes the image entrypoint exit before Neo4j starts, so the operator
	// refuses up front instead of leaving the pod in CrashLoopBackOff.
	cases := map[string]string{
		"leading dash is parsed as an option by neo4j-admin": "neo4j/-XX:whatever",
		"slash breaks the entrypoint's NEO4J_AUTH parse":     "neo4j/pass/word",
		"only the neo4j user is accepted":                    "admin/s3cret",
		"the default password is refused by the image":       "neo4j/neo4j",
		"missing key":                                        "",
	}
	for name, value := range cases {
		err := RequireUsableAuthValue(authSecret(value))
		if err == nil {
			t.Errorf("%s: value %q must be rejected", name, value)
			continue
		}
		// status.PipelineErrorReason maps this sentinel to AuthSecretInvalid.
		if !errors.Is(err, ErrAuthValueRejected) {
			t.Errorf("%s: error must wrap ErrAuthValueRejected, got %v", name, err)
		}
	}
}

// A refusal lands in a status condition and a Kubernetes Event, both world-readable to
// anyone with get on the CR, so the message must not carry the password.
func TestRequireUsableAuthValueNeverQuotesTheSecret(t *testing.T) {
	const password = "-topsecretvalue"
	err := RequireUsableAuthValue(authSecret("neo4j/" + password))
	if err == nil {
		t.Fatal("expected rejection")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("message leaks the password: %v", err)
	}
}
