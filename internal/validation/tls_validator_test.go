package validation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

func TestNewTLSValidator(t *testing.T) {
	validator := NewTLSValidator()
	assert.NotNil(t, validator)
}

func TestTLSValidator_Validate(t *testing.T) {
	tests := []struct {
		name       string
		cluster    *neo4jv1beta1.Neo4jEnterpriseCluster
		wantErrors bool
	}{
		{
			name: "no TLS configuration",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{},
			},
			wantErrors: false,
		},
		{
			name: "valid cert-manager TLS configuration",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1beta1.IssuerRef{
							Name: "ca-cluster-issuer",
							Kind: "ClusterIssuer",
						},
						Duration:    stringPtr("2160h"), // 90 days
						RenewBefore: stringPtr("360h"),  // 15 days
						Usages:      []string{"digital signature", "key encipherment", "server auth", "client auth"},
					},
				},
			},
			wantErrors: false,
		},
		{
			name: "valid disabled TLS configuration",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "disabled",
					},
				},
			},
			wantErrors: false,
		},
		{
			name: "invalid TLS mode",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "invalid-mode",
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "cert-manager mode missing issuer name",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1beta1.IssuerRef{
							Kind: "ClusterIssuer",
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "cert-manager mode third-party issuer kind is accepted",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1beta1.IssuerRef{
							Name:  "aws-pca-issuer",
							Kind:  "AWSPCAClusterIssuer",
							Group: "awspca.cert-manager.io",
						},
					},
				},
			},
			wantErrors: false,
		},
		{
			name: "cert-manager mode invalid duration",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1beta1.IssuerRef{
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
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1beta1.IssuerRef{
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
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1beta1.IssuerRef{
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
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1beta1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1beta1.SecretStoreRef{
								Name: "aws-secret-store",
								Kind: "SecretStore",
							},
							RefreshInterval: "15m",
							Data: []neo4jv1beta1.ExternalSecretData{
								{
									SecretKey: "tls.crt",
									RemoteRef: &neo4jv1beta1.ExternalSecretRemoteRef{
										Key: "neo4j-tls-cert",
									},
								},
								{
									SecretKey: "tls.key",
									RemoteRef: &neo4jv1beta1.ExternalSecretRemoteRef{
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
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1beta1.ExternalSecretsConfig{
							Enabled: true,
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "external secrets missing secret store name",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1beta1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1beta1.SecretStoreRef{
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
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1beta1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1beta1.SecretStoreRef{
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
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1beta1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1beta1.SecretStoreRef{
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
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1beta1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1beta1.SecretStoreRef{
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
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1beta1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1beta1.SecretStoreRef{
								Name: "aws-secret-store",
								Kind: "SecretStore",
							},
							Data: []neo4jv1beta1.ExternalSecretData{
								{
									RemoteRef: &neo4jv1beta1.ExternalSecretRemoteRef{
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
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1beta1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1beta1.SecretStoreRef{
								Name: "aws-secret-store",
								Kind: "SecretStore",
							},
							Data: []neo4jv1beta1.ExternalSecretData{
								{
									SecretKey: "tls.crt",
									RemoteRef: &neo4jv1beta1.ExternalSecretRemoteRef{},
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
			// Always include server auth + client auth (required by Neo4j
			// for incoming TLS + the operator's strict cluster mutual TLS).
			// This test exercises usage-name validity, not the EKU
			// completeness check covered in
			// TestTLSValidator_RequiresServerAndClientAuthEKUs below.
			usages := []string{"server auth", "client auth"}
			if usage != "server auth" && usage != "client auth" {
				usages = append(usages, usage)
			}
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1beta1.IssuerRef{
							Name: "ca-cluster-issuer",
							Kind: "ClusterIssuer",
						},
						Usages: usages,
					},
				},
			}

			errors := validator.Validate(cluster)
			assert.Empty(t, errors, "Expected no validation errors for valid usage %q but got: %v", usage, errors)
		})
	}
}

// TestTLSValidator_RequiresServerAndClientAuthEKUs covers the requirement
// that a user-supplied spec.tls.usages must include both server-auth and
// client-auth. Neo4j needs server-auth for incoming Bolt/HTTPS/cluster
// TLS and client-auth for the mutual TLS the operator emits on cluster
// links under strict peer validation (client_auth=REQUIRE). Without
// either EKU the issued cert would fail the runtime handshake.
func TestTLSValidator_RequiresServerAndClientAuthEKUs(t *testing.T) {
	validator := NewTLSValidator()

	mk := func(usages []string) *neo4jv1beta1.Neo4jEnterpriseCluster {
		return &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				TLS: &neo4jv1beta1.TLSSpec{
					Mode:      "cert-manager",
					IssuerRef: &neo4jv1beta1.IssuerRef{Name: "ca-cluster-issuer", Kind: "ClusterIssuer"},
					Usages:    usages,
				},
			},
		}
	}

	hasRequiredError := func(errs []*field.Error, missingUsage string) bool {
		for _, e := range errs {
			if e.Type == field.ErrorTypeRequired && strings.Contains(e.Detail, "\""+missingUsage+"\"") {
				return true
			}
		}
		return false
	}

	t.Run("missing client auth → rejected", func(t *testing.T) {
		errs := validator.Validate(mk([]string{"digital signature", "key encipherment", "server auth"}))
		require.NotEmpty(t, errs)
		assert.True(t, hasRequiredError(errs, "client auth"),
			"expected Required error naming client auth; got %v", errs)
	})

	t.Run("missing server auth → rejected", func(t *testing.T) {
		errs := validator.Validate(mk([]string{"digital signature", "client auth"}))
		require.NotEmpty(t, errs)
		assert.True(t, hasRequiredError(errs, "server auth"),
			"expected Required error naming server auth; got %v", errs)
	})

	t.Run("missing both → both errors", func(t *testing.T) {
		errs := validator.Validate(mk([]string{"digital signature", "key encipherment"}))
		require.NotEmpty(t, errs)
		assert.True(t, hasRequiredError(errs, "server auth"))
		assert.True(t, hasRequiredError(errs, "client auth"))
	})

	t.Run("both present → accepted", func(t *testing.T) {
		errs := validator.Validate(mk([]string{"digital signature", "key encipherment", "server auth", "client auth"}))
		assert.Empty(t, errs)
	})

	t.Run("empty usages → no requirement (operator defaults apply)", func(t *testing.T) {
		// Empty list means the operator's defaults take effect (which
		// include both EKUs). Validator should not complain.
		cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
			Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
				TLS: &neo4jv1beta1.TLSSpec{
					Mode:      "cert-manager",
					IssuerRef: &neo4jv1beta1.IssuerRef{Name: "ca-cluster-issuer", Kind: "ClusterIssuer"},
				},
			},
		}
		assert.Empty(t, validator.Validate(cluster))
	})
}

func TestTLSValidator_ValidIssuerKinds(t *testing.T) {
	validator := NewTLSValidator()

	// Any kind is accepted — cert-manager's external issuer interface is open.
	cases := []struct {
		kind  string
		group string
	}{
		// Standard cert-manager issuers
		{kind: "Issuer"},
		{kind: "ClusterIssuer"},
		// Third-party external issuers (GitHub issue #26)
		{kind: "AWSPCAClusterIssuer", group: "awspca.cert-manager.io"},
		{kind: "AWSPCAIssuer", group: "awspca.cert-manager.io"},
		{kind: "VaultIssuer", group: "cert.cert-manager.io"},
		{kind: "GoogleCASIssuer", group: "cas-issuer.jetstack.io"},
	}

	for _, tc := range cases {
		t.Run("valid issuer kind: "+tc.kind, func(t *testing.T) {
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						IssuerRef: &neo4jv1beta1.IssuerRef{
							Name:  "my-issuer",
							Kind:  tc.kind,
							Group: tc.group,
						},
					},
				},
			}

			errors := validator.Validate(cluster)
			assert.Empty(t, errors, "Expected no validation errors for issuer kind %q but got: %v", tc.kind, errors)
		})
	}
}

func TestTLSValidator_ValidSecretStoreKinds(t *testing.T) {
	validator := NewTLSValidator()

	validKinds := []string{"SecretStore", "ClusterSecretStore"}

	for _, kind := range validKinds {
		t.Run("valid secret store kind: "+kind, func(t *testing.T) {
			cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					TLS: &neo4jv1beta1.TLSSpec{
						Mode: "cert-manager",
						ExternalSecrets: &neo4jv1beta1.ExternalSecretsConfig{
							Enabled: true,
							SecretStoreRef: &neo4jv1beta1.SecretStoreRef{
								Name: "test-secret-store",
								Kind: kind,
							},
							Data: []neo4jv1beta1.ExternalSecretData{
								{
									SecretKey: "tls.crt",
									RemoteRef: &neo4jv1beta1.ExternalSecretRemoteRef{
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
