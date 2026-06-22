package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

func TestNewStorageValidator(t *testing.T) {
	validator := NewStorageValidator()
	assert.NotNil(t, validator)
}

func TestStorageValidator_Validate(t *testing.T) {
	tests := []struct {
		name       string
		cluster    *neo4jv1beta1.Neo4jEnterpriseCluster
		wantErrors bool
		errorCount int
	}{
		{
			name: "valid storage configuration",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Storage: neo4jv1beta1.StorageSpec{
						ClassName: "fast-ssd",
						Size:      "100Gi",
					},
				},
			},
			wantErrors: false,
		},
		{
			name: "valid storage with different size formats",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Storage: neo4jv1beta1.StorageSpec{
						ClassName: "standard",
						Size:      "1Ti",
					},
				},
			},
			wantErrors: false,
		},
		{
			// An empty className is valid: the PVC inherits the cluster's default
			// StorageClass. Existence of a named class is checked at apply time by
			// the reconciler, not here.
			name: "empty storage class name uses cluster default",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Storage: neo4jv1beta1.StorageSpec{
						Size: "100Gi",
					},
				},
			},
			wantErrors: false,
		},
		{
			name: "missing storage size",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Storage: neo4jv1beta1.StorageSpec{
						ClassName: "standard",
					},
				},
			},
			wantErrors: true,
			errorCount: 1,
		},
		{
			name: "missing size (empty class name is allowed)",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Storage:                neo4jv1beta1.StorageSpec{},
				},
			},
			wantErrors: true,
			errorCount: 1,
		},
		{
			name: "invalid storage size format",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Storage: neo4jv1beta1.StorageSpec{
						ClassName: "standard",
						Size:      "invalid-size",
					},
				},
			},
			wantErrors: true,
			errorCount: 1,
		},
		{
			name: "empty storage size",
			cluster: &neo4jv1beta1.Neo4jEnterpriseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-namespace",
				},
				Spec: neo4jv1beta1.Neo4jEnterpriseClusterSpec{
					AcceptLicenseAgreement: "eval",
					Storage: neo4jv1beta1.StorageSpec{
						ClassName: "standard",
						Size:      "",
					},
				},
			},
			wantErrors: true,
			errorCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewStorageValidator()
			errors := validator.Validate(tt.cluster)

			if tt.wantErrors {
				assert.NotEmpty(t, errors, "Expected validation errors but got none")
				if tt.errorCount > 0 {
					assert.Len(t, errors, tt.errorCount, "Expected %d errors but got %d", tt.errorCount, len(errors))
				}
			} else {
				assert.Empty(t, errors, "Expected no validation errors but got: %v", errors)
			}
		})
	}
}

func TestStorageValidator_isValidStorageSize(t *testing.T) {
	validator := NewStorageValidator()

	tests := []struct {
		name  string
		size  string
		valid bool
	}{
		{
			name:  "valid size in Gi",
			size:  "100Gi",
			valid: true,
		},
		{
			name:  "valid size in Ti",
			size:  "1Ti",
			valid: true,
		},
		{
			name:  "valid size in Mi",
			size:  "2048Mi",
			valid: true,
		},
		{
			name:  "valid size in Ki",
			size:  "1048576Ki",
			valid: true,
		},
		{
			name:  "valid size in G",
			size:  "100G",
			valid: true,
		},
		{
			name:  "valid size in T",
			size:  "1T",
			valid: true,
		},
		{
			name:  "valid size in M",
			size:  "2048M",
			valid: true,
		},
		{
			name:  "valid size in K",
			size:  "1048576K",
			valid: true,
		},
		{
			name:  "valid size without unit",
			size:  "1073741824",
			valid: true,
		},
		{
			name:  "invalid size with invalid unit",
			size:  "100Zi",
			valid: false,
		},
		{
			name:  "invalid size with text",
			size:  "one-hundred-gigabytes",
			valid: false,
		},
		{
			name:  "invalid size with mixed text and numbers",
			size:  "100GB-storage",
			valid: false,
		},
		{
			name:  "empty size",
			size:  "",
			valid: false,
		},
		{
			name:  "size with only unit",
			size:  "Gi",
			valid: false,
		},
		{
			name:  "negative size",
			size:  "-100Gi",
			valid: false,
		},
		{
			name:  "size with decimal",
			size:  "100.5Gi",
			valid: false,
		},
		{
			name:  "size with spaces",
			size:  "100 Gi",
			valid: false,
		},
		{
			name:  "valid large size",
			size:  "999999Gi",
			valid: true,
		},
		{
			name:  "valid small size",
			size:  "1Ki",
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.isValidStorageSize(tt.size)
			assert.Equal(t, tt.valid, result, "Expected isValidStorageSize(%q) to be %v", tt.size, tt.valid)
		})
	}
}
