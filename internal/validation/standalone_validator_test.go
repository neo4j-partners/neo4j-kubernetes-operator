/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package validation

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	neo4jv1alpha1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1alpha1"
)

// validStandalone returns a minimal standalone that should pass all validations.
func validStandalone() *neo4jv1alpha1.Neo4jEnterpriseStandalone {
	return &neo4jv1alpha1.Neo4jEnterpriseStandalone{
		ObjectMeta: metav1.ObjectMeta{Name: "test-standalone", Namespace: "default"},
		Spec: neo4jv1alpha1.Neo4jEnterpriseStandaloneSpec{
			Image:   neo4jv1alpha1.ImageSpec{Repo: "neo4j", Tag: "5.26.0-enterprise"},
			Storage: neo4jv1alpha1.StorageSpec{ClassName: "standard", Size: "1Gi"},
		},
	}
}

// ---------------------------------------------------------------------------
// TestStandaloneValidator_ValidateCreate
// ---------------------------------------------------------------------------

func TestStandaloneValidator_ValidateCreate(t *testing.T) {
	v := NewStandaloneValidator()

	cases := []struct {
		name     string
		mutate   func(*neo4jv1alpha1.Neo4jEnterpriseStandalone)
		wantErrs int
		errField string
	}{
		{
			name:     "valid standalone - no errors",
			mutate:   func(_ *neo4jv1alpha1.Neo4jEnterpriseStandalone) {},
			wantErrs: 0,
		},
		{
			name:     "missing image.repo",
			mutate:   func(s *neo4jv1alpha1.Neo4jEnterpriseStandalone) { s.Spec.Image.Repo = "" },
			wantErrs: 1,
			errField: "spec.image.repo",
		},
		{
			// When tag is empty, two errors are raised: Required + version-invalid
			name:     "missing image.tag",
			mutate:   func(s *neo4jv1alpha1.Neo4jEnterpriseStandalone) { s.Spec.Image.Tag = "" },
			wantErrs: 2,
			errField: "spec.image.tag",
		},
		{
			name:     "unsupported Neo4j 4.x version",
			mutate:   func(s *neo4jv1alpha1.Neo4jEnterpriseStandalone) { s.Spec.Image.Tag = "4.4.0-enterprise" },
			wantErrs: 1,
			errField: "spec.image.tag",
		},
		{
			name:     "missing storage.className",
			mutate:   func(s *neo4jv1alpha1.Neo4jEnterpriseStandalone) { s.Spec.Storage.ClassName = "" },
			wantErrs: 1,
			errField: "spec.storage.className",
		},
		{
			name:     "missing storage.size",
			mutate:   func(s *neo4jv1alpha1.Neo4jEnterpriseStandalone) { s.Spec.Storage.Size = "" },
			wantErrs: 1,
			errField: "spec.storage.size",
		},
		{
			name: "TLS mode mutual-tls is invalid",
			mutate: func(s *neo4jv1alpha1.Neo4jEnterpriseStandalone) {
				s.Spec.TLS = &neo4jv1alpha1.TLSSpec{Mode: "mutual-tls"}
			},
			wantErrs: 1,
			errField: "spec.tls.mode",
		},
		{
			name: "TLS mode cert-manager without issuerRef",
			mutate: func(s *neo4jv1alpha1.Neo4jEnterpriseStandalone) {
				s.Spec.TLS = &neo4jv1alpha1.TLSSpec{Mode: "cert-manager"}
			},
			wantErrs: 1,
			errField: "spec.tls.issuerRef",
		},
		{
			name: "TLS mode cert-manager without issuerRef.name",
			mutate: func(s *neo4jv1alpha1.Neo4jEnterpriseStandalone) {
				s.Spec.TLS = &neo4jv1alpha1.TLSSpec{
					Mode:      "cert-manager",
					IssuerRef: &neo4jv1alpha1.IssuerRef{Name: ""},
				}
			},
			wantErrs: 1,
			errField: "spec.tls.issuerRef.name",
		},
		{
			name: "clustering key in spec.config is rejected",
			mutate: func(s *neo4jv1alpha1.Neo4jEnterpriseStandalone) {
				s.Spec.Config = map[string]string{
					"dbms.cluster.discovery.version": "V2_ONLY",
				}
			},
			wantErrs: 1,
		},
		{
			name: "dbms.mode in spec.config is rejected",
			mutate: func(s *neo4jv1alpha1.Neo4jEnterpriseStandalone) {
				s.Spec.Config = map[string]string{"dbms.mode": "SINGLE"}
			},
			wantErrs: 1,
		},
		{
			name: "invalid auth provider",
			mutate: func(s *neo4jv1alpha1.Neo4jEnterpriseStandalone) {
				s.Spec.Auth = &neo4jv1alpha1.AuthSpec{AuthenticationProviders: []string{"bogus-auth"}}
			},
			wantErrs: 1, // invalid provider name
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := validStandalone()
			tc.mutate(s)

			errs := v.ValidateCreate(s)
			if len(errs) != tc.wantErrs {
				t.Errorf("expected %d errors, got %d: %v", tc.wantErrs, len(errs), errs)
				return
			}

			if tc.errField != "" && len(errs) > 0 {
				found := false
				for _, err := range errs {
					if err.Field == tc.errField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error on field %q, got: %v", tc.errField, errs)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestStandaloneValidator_ValidateUpdate
// ---------------------------------------------------------------------------

func TestStandaloneValidator_ValidateUpdate(t *testing.T) {
	v := NewStandaloneValidator()

	t.Run("storage class change is rejected", func(t *testing.T) {
		old := validStandalone()
		updated := validStandalone()
		updated.Spec.Storage.ClassName = "premium"

		errs := v.ValidateUpdate(old, updated)
		if len(errs) == 0 {
			t.Error("expected error when storage class changes, got none")
		}
		found := false
		for _, err := range errs {
			if err.Field == "spec.storage.className" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected error on spec.storage.className, got: %v", errs)
		}
	})

	t.Run("image tag change is accepted", func(t *testing.T) {
		old := validStandalone()
		updated := validStandalone()
		updated.Spec.Image.Tag = "2025.01.0-enterprise"

		errs := v.ValidateUpdate(old, updated)
		if len(errs) != 0 {
			t.Errorf("expected no errors for image tag change, got: %v", errs)
		}
	})
}
