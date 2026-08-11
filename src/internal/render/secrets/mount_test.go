package secrets

import (
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/neo4j/neo4j-kubernetes-operator/src/api/v1beta1"
)

func TestValidateSpecRejectsServiceAccountToken(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Trust: &neo4jv1beta1.TrustSpec{
				Enabled: true,
				Certificates: &neo4jv1beta1.TrustCertificatesSpec{
					Bolt: &neo4jv1beta1.TLSPolicySpec{
						TrustedCerts: &neo4jv1beta1.TLSTrustedCertsSpec{
							Sources: []corev1.VolumeProjection{{
								ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Path: "token"},
							}},
						},
					},
				},
			},
		},
	}
	err := ValidateSpec(neo4j)
	if err == nil || !strings.Contains(err.Error(), "serviceAccountToken") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateSpecRequiresSecretMountItems(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Storage: &neo4jv1beta1.StorageSpec{
				SecretMounts: map[string]neo4jv1beta1.SecretMountSpec{
					"creds": {SecretName: "my-creds", MountPath: "/var/secrets/creds"},
				},
			},
		},
	}
	err := ValidateSpec(neo4j)
	if err == nil || !strings.Contains(err.Error(), "items is required") {
		t.Fatalf("got %v", err)
	}
}

func TestRequireMountable(t *testing.T) {
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	err := RequireMountable(s)
	if err == nil {
		t.Fatal("expected missing label error")
	}
	// status.PipelineErrorReason maps this sentinel to a stable condition/Event reason.
	if !errors.Is(err, ErrNotMountable) {
		t.Fatalf("error must wrap ErrNotMountable, got %v", err)
	}
	s.Labels = map[string]string{MountableLabel: MountableLabelValue}
	if err := RequireMountable(s); err != nil {
		t.Fatal(err)
	}
}

func TestRequireAuthSecretDelegated(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{ObjectMeta: metav1.ObjectMeta{Name: "orders"}}
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-auth"}}
	if err := RequireAuthSecretDelegated(s, neo4j); err == nil {
		t.Fatal("expected rejection without labels")
	}
	s.Labels = map[string]string{MountableLabel: MountableLabelValue}
	err := RequireAuthSecretDelegated(s, neo4j)
	if err == nil {
		t.Fatal("mountable alone must not authorize auth use")
	}
	if !errors.Is(err, ErrAuthNotDelegated) {
		t.Fatalf("error must wrap ErrAuthNotDelegated, got %v", err)
	}
	s.Labels[AllowedForLabel] = "orders"
	if err := RequireAuthSecretDelegated(s, neo4j); err != nil {
		t.Fatal(err)
	}
	s.Labels = map[string]string{
		"app.kubernetes.io/managed-by": "neo4j-operator",
		"app.kubernetes.io/instance":   "orders",
	}
	if err := RequireAuthSecretDelegated(s, neo4j); err != nil {
		t.Fatal(err)
	}
	s.Labels["app.kubernetes.io/instance"] = "other"
	if err := RequireAuthSecretDelegated(s, neo4j); err == nil {
		t.Fatal("managed-by for another instance must fail")
	}
}

func TestValidateSpecAllowsSecretAndConfigMapWithItems(t *testing.T) {
	neo4j := &neo4jv1beta1.Neo4j{
		Spec: neo4jv1beta1.Neo4jSpec{
			Trust: &neo4jv1beta1.TrustSpec{
				Enabled: true,
				Certificates: &neo4jv1beta1.TrustCertificatesSpec{
					Bolt: &neo4jv1beta1.TLSPolicySpec{
						TrustedCerts: &neo4jv1beta1.TLSTrustedCertsSpec{
							Sources: []corev1.VolumeProjection{
								{Secret: &corev1.SecretProjection{
									LocalObjectReference: corev1.LocalObjectReference{Name: "ca"},
									Items:                []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
								}},
								{ConfigMap: &corev1.ConfigMapProjection{
									LocalObjectReference: corev1.LocalObjectReference{Name: "ca-cm"},
									Items:                []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
								}},
							},
						},
					},
				},
			},
			Storage: &neo4jv1beta1.StorageSpec{
				SecretMounts: map[string]neo4jv1beta1.SecretMountSpec{
					"creds": {
						SecretName: "my-creds",
						MountPath:  "/var/secrets/creds",
						Items:      []neo4jv1beta1.SecretKeyToPath{{Key: "token", Path: "token"}},
					},
				},
			},
		},
	}
	if err := ValidateSpec(neo4j); err != nil {
		t.Fatal(err)
	}
}
