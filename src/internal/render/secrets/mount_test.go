package secrets

import (
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
	if err := RequireMountable(s); err == nil {
		t.Fatal("expected missing label error")
	}
	s.Labels = map[string]string{MountableLabel: MountableLabelValue}
	if err := RequireMountable(s); err != nil {
		t.Fatal(err)
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
