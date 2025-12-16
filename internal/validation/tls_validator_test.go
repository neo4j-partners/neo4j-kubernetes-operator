package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1alpha1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestNewTLSValidator(t *testing.T) {
	validator := NewTLSValidator()
	assert.NotNil(t, validator)
}

func TestTLSValidator_Validate(t *testing.T) {
	tests := []struct {
		name       string
		cluster    *neo4jv1alpha1.Neo4jEnterpriseCluster
		wantErrors bool
	}{
		{
			name: "no TLS configuration",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{},
			},
			wantErrors: false,
		},
		{
			name: "valid cert-manager TLS configuration",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1alpha1.IssuerRef{
							Name: "ca-cluster-issuer",
							Kind: "ClusterIssuer",
						},
						Duration:    stringPtr("2160h"), // 90 days
						RenewBefore: stringPtr("360h"),  // 15 days
						Usages:      []string{"digital signature", "key encipherment", "server auth"},
					},
				},
			},
			wantErrors: false,
		},
		{
			name: "valid disabled TLS configuration",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "disabled",
					},
				},
			},
			wantErrors: false,
		},
		{
			name: "invalid TLS mode",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "invalid-mode",
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "cert-manager mode missing issuer name",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1alpha1.IssuerRef{
							Kind: "ClusterIssuer",
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "cert-manager mode invalid issuer kind",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1alpha1.IssuerRef{
							Name: "ca-cluster-issuer",
							Kind: "InvalidKind",
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "cert-manager mode invalid duration",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1alpha1.IssuerRef{
							Name: "ca-cluster-issuer",
							Kind: "ClusterIssuer",
						},
						Duration: stringPtr("invalid-duration"),
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "cert-manager mode invalid renewBefore",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1alpha1.IssuerRef{
							Name: "ca-cluster-issuer",
							Kind: "ClusterIssuer",
						},
						RenewBefore: stringPtr("invalid-duration"),
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "cert-manager mode invalid usage",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1alpha1.IssuerRef{
							Name: "ca-cluster-issuer",
							Kind: "ClusterIssuer",
						},
						Usages: []string{"digital signature", "invalid-usage"},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "valid external secrets TLS configuration",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1alpha1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1alpha1.SecretStoreRef{
								Name: "aws-secret-store",
								Kind: "SecretStore",
							},
							RefreshInterval: "15m",
							Data: []neo4jv1alpha1.ExternalSecretData{
								{
									SecretKey: "tls.crt",
									RemoteRef: &neo4jv1alpha1.ExternalSecretRemoteRef{
										Key: "neo4j-tls-cert",
									},
								},
								{
									SecretKey: "tls.key",
									RemoteRef: &neo4jv1alpha1.ExternalSecretRemoteRef{
										Key: "neo4j-tls-key",
									},
								},
							},
						},
					},
				},
			},
			wantErrors: false,
		},
		{
			name: "external secrets enabled but missing secret store ref",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1alpha1.ExternalSecretsConfig{
							Enabled: true,
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "external secrets missing secret store name",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1alpha1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1alpha1.SecretStoreRef{
								Kind: "SecretStore",
							},
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "external secrets invalid secret store kind",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1alpha1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1alpha1.SecretStoreRef{
								Name: "aws-secret-store",
								Kind: "InvalidKind",
							},
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "external secrets invalid refresh interval",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1alpha1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1alpha1.SecretStoreRef{
								Name: "aws-secret-store",
								Kind: "SecretStore",
							},
							RefreshInterval: "invalid-interval",
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "external secrets missing data mappings",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1alpha1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1alpha1.SecretStoreRef{
								Name: "aws-secret-store",
								Kind: "SecretStore",
							},
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "external secrets missing secret key in data mapping",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1alpha1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1alpha1.SecretStoreRef{
								Name: "aws-secret-store",
								Kind: "SecretStore",
							},
							Data: []neo4jv1alpha1.ExternalSecretData{
								{
									RemoteRef: &neo4jv1alpha1.ExternalSecretRemoteRef{
										Key: "neo4j-tls-cert",
									},
								},
							},
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "external secrets missing remote ref key",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1alpha1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1alpha1.SecretStoreRef{
								Name: "aws-secret-store",
								Kind: "SecretStore",
							},
							Data: []neo4jv1alpha1.ExternalSecretData{
								{
									SecretKey: "tls.crt",
									RemoteRef: &neo4jv1alpha1.ExternalSecretRemoteRef{},
								},
							},
						},
					},
				},
			},
			wantErrors: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewTLSValidator()
			errors := validator.Validate(tt.cluster)

			if tt.wantErrors {
				assert.NotEmpty(t, errors, "Expected validation errors but got none")
			} else {
				assert.Empty(t, errors, "Expected no validation errors but got: %v", errors)
			}
		})
	}
}

func TestTLSValidator_ValidUsages(t *testing.T) {
	validator := NewTLSValidator()

	validUsages := []string{
		"digital signature",
		"key encipherment",
		"key agreement",
		"server auth",
		"client auth",
		"code signing",
		"email protection",
		"s/mime",
		"ipsec end system",
		"ipsec tunnel",
		"ipsec user",
		"timestamping",
		"ocsp signing",
		"microsoft sgc",
		"netscape sgc",
	}

	for _, usage := range validUsages {
		t.Run("valid usage: "+usage, func(t *testing.T) {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1alpha1.IssuerRef{
							Name: "ca-cluster-issuer",
							Kind: "ClusterIssuer",
						},
						Usages: []string{usage},
					},
				},
			}

			errors := validator.Validate(cluster)
			assert.Empty(t, errors, "Expected no validation errors for valid usage %q but got: %v", usage, errors)
		})
	}
}

func TestTLSValidator_ValidIssuerKinds(t *testing.T) {
	validator := NewTLSValidator()

	validKinds := []string{"Issuer", "ClusterIssuer"}

	for _, kind := range validKinds {
		t.Run("valid issuer kind: "+kind, func(t *testing.T) {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1alpha1.IssuerRef{
							Name: "ca-cluster-issuer",
							Kind: kind,
						},
					},
				},
			}

			errors := validator.Validate(cluster)
			assert.Empty(t, errors, "Expected no validation errors for valid issuer kind %q but got: %v", kind, errors)
		})
	}
}

func TestTLSValidator_ValidSecretStoreKinds(t *testing.T) {
	validator := NewTLSValidator()

	validKinds := []string{"SecretStore", "ClusterSecretStore"}

	for _, kind := range validKinds {
		t.Run("valid secret store kind: "+kind, func(t *testing.T) {
			cluster := &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1alpha1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1alpha1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1alpha1.SecretStoreRef{
								Name: "test-secret-store",
								Kind: kind,
							},
							Data: []neo4jv1alpha1.ExternalSecretData{
								{
									SecretKey: "tls.crt",
									RemoteRef: &neo4jv1alpha1.ExternalSecretRemoteRef{
										Key: "neo4j-tls-cert",
									},
								},
							},
						},
					},
				},
			}

			errors := validator.Validate(cluster)
			assert.Empty(t, errors, "Expected no validation errors for valid secret store kind %q but got: %v", kind, errors)
		})
	}
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}
