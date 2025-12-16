package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestNewCloudValidator(t *testing.T) {
	validator := NewCloudValidator()
	assert.NotNil(t, validator)
}

func TestCloudValidator_Validate(t *testing.T) {
	tests := []struct {
		name       string
		cluster    *neo4jv1alpha1.Neo4jEnterpriseCluster
		wantErrors bool
	}{
		{
			name: "no cloud configuration",
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
			name: "valid AWS cloud configuration",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Backups: &neo4jv1alpha1.BackupsSpec{
						Cloud: &neo4jv1alpha1.CloudBlock{
							Provider: "aws",
							Identity: &neo4jv1alpha1.CloudIdentity{
								Provider: "aws",
								AutoCreate: &neo4jv1alpha1.AutoCreateSpec{
									Enabled: true,
									Annotations: map[string]string{
										"eks.amazonaws.com/role-arn": "arn:aws:iam::123456789012:role/my-role",
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
			name: "valid GCP cloud configuration",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Backups: &neo4jv1alpha1.BackupsSpec{
						Cloud: &neo4jv1alpha1.CloudBlock{
							Provider: "gcp",
							Identity: &neo4jv1alpha1.CloudIdentity{
								Provider: "gcp",
								AutoCreate: &neo4jv1alpha1.AutoCreateSpec{
									Enabled: true,
									Annotations: map[string]string{
										"iam.gke.io/gcp-service-account": "my-service-account@project.iam.gserviceaccount.com",
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
			name: "valid Azure cloud configuration",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Backups: &neo4jv1alpha1.BackupsSpec{
						Cloud: &neo4jv1alpha1.CloudBlock{
							Provider: "azure",
							Identity: &neo4jv1alpha1.CloudIdentity{
								Provider: "azure",
								AutoCreate: &neo4jv1alpha1.AutoCreateSpec{
									Enabled: true,
									Annotations: map[string]string{
										"azure.workload.identity/client-id": "12345678-1234-1234-1234-123456789012",
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
			name: "invalid cloud provider",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Backups: &neo4jv1alpha1.BackupsSpec{
						Cloud: &neo4jv1alpha1.CloudBlock{
							Provider: "invalid-provider",
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "mismatched identity provider",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Backups: &neo4jv1alpha1.BackupsSpec{
						Cloud: &neo4jv1alpha1.CloudBlock{
							Provider: "aws",
							Identity: &neo4jv1alpha1.CloudIdentity{
								Provider: "gcp", // Mismatch
							},
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "missing AWS role-arn annotation",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Backups: &neo4jv1alpha1.BackupsSpec{
						Cloud: &neo4jv1alpha1.CloudBlock{
							Provider: "aws",
							Identity: &neo4jv1alpha1.CloudIdentity{
								Provider: "aws",
								AutoCreate: &neo4jv1alpha1.AutoCreateSpec{
									Enabled: true,
									Annotations: map[string]string{
										"other-annotation": "value",
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
			name: "missing GCP service account annotation",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Backups: &neo4jv1alpha1.BackupsSpec{
						Cloud: &neo4jv1alpha1.CloudBlock{
							Provider: "gcp",
							Identity: &neo4jv1alpha1.CloudIdentity{
								Provider: "gcp",
								AutoCreate: &neo4jv1alpha1.AutoCreateSpec{
									Enabled: true,
									Annotations: map[string]string{
										"other-annotation": "value",
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
			name: "missing Azure client-id annotation",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Backups: &neo4jv1alpha1.BackupsSpec{
						Cloud: &neo4jv1alpha1.CloudBlock{
							Provider: "azure",
							Identity: &neo4jv1alpha1.CloudIdentity{
								Provider: "azure",
								AutoCreate: &neo4jv1alpha1.AutoCreateSpec{
									Enabled: true,
									Annotations: map[string]string{
										"other-annotation": "value",
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
			name: "missing annotations for auto-create",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Backups: &neo4jv1alpha1.BackupsSpec{
						Cloud: &neo4jv1alpha1.CloudBlock{
							Provider: "aws",
							Identity: &neo4jv1alpha1.CloudIdentity{
								Provider: "aws",
								AutoCreate: &neo4jv1alpha1.AutoCreateSpec{
									Enabled: true,
									// Missing annotations
								},
							},
						},
					},
				},
			},
			wantErrors: true,
		},
		{
			name: "auto-create disabled",
			cluster: &neo4jv1alpha1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1alpha1.Neo4jEnterpriseClusterSpec{
					Backups: &neo4jv1alpha1.BackupsSpec{
						Cloud: &neo4jv1alpha1.CloudBlock{
							Provider: "aws",
							Identity: &neo4jv1alpha1.CloudIdentity{
								Provider: "aws",
								AutoCreate: &neo4jv1alpha1.AutoCreateSpec{
									Enabled: false,
									// Annotations not required when disabled
								},
							},
						},
					},
				},
			},
			wantErrors: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewCloudValidator()
			errors := validator.Validate(tt.cluster)

			if tt.wantErrors {
				assert.NotEmpty(t, errors, "Expected validation errors but got none")
			} else {
				assert.Empty(t, errors, "Expected no validation errors but got: %v", errors)
			}
		})
	}
}

func TestCloudValidator_validateProviderAnnotations(t *testing.T) {
	validator := NewCloudValidator()

	tests := []struct {
		name        string
		provider    string
		annotations map[string]string
		wantErrors  bool
	}{
		{
			name:     "valid AWS annotations",
			provider: "aws",
			annotations: map[string]string{
				"eks.amazonaws.com/role-arn": "arn:aws:iam::123456789012:role/my-role",
			},
			wantErrors: false,
		},
		{
			name:     "missing AWS role-arn",
			provider: "aws",
			annotations: map[string]string{
				"other-annotation": "value",
			},
			wantErrors: true,
		},
		{
			name:     "valid GCP annotations",
			provider: "gcp",
			annotations: map[string]string{
				"iam.gke.io/gcp-service-account": "my-service-account@project.iam.gserviceaccount.com",
			},
			wantErrors: false,
		},
		{
			name:     "missing GCP service account",
			provider: "gcp",
			annotations: map[string]string{
				"other-annotation": "value",
			},
			wantErrors: true,
		},
		{
			name:     "valid Azure annotations",
			provider: "azure",
			annotations: map[string]string{
				"azure.workload.identity/client-id": "12345678-1234-1234-1234-123456789012",
			},
			wantErrors: false,
		},
		{
			name:     "missing Azure client-id",
			provider: "azure",
			annotations: map[string]string{
				"other-annotation": "value",
			},
			wantErrors: true,
		},
		{
			name:        "unknown provider",
			provider:    "unknown",
			annotations: map[string]string{},
			wantErrors:  false, // No validation for unknown providers
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use a dummy field path for testing
			path := &field.Path{}
			errors := validator.validateProviderAnnotations(tt.provider, tt.annotations, path)

			if tt.wantErrors {
				assert.NotEmpty(t, errors, "Expected validation errors but got none")
			} else {
				assert.Empty(t, errors, "Expected no validation errors but got: %v", errors)
			}
		})
	}
}
