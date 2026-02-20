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

	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

func TestValidateAuraFleetManagement(t *testing.T) {
	path := field.NewPath("spec", "auraFleetManagement")

	tests := []struct {
		name      string
		spec      *neo4jv1alpha1.AuraFleetManagementSpec
		wantErrs  int
		errDetail string
	}{
		{
			name:     "nil spec — no errors",
			spec:     nil,
			wantErrs: 0,
		},
		{
			name:     "disabled — no errors even without tokenSecretRef",
			spec:     &neo4jv1alpha1.AuraFleetManagementSpec{Enabled: false},
			wantErrs: 0,
		},
		{
			name:     "enabled without tokenSecretRef — valid (deferred registration)",
			spec:     &neo4jv1alpha1.AuraFleetManagementSpec{Enabled: true},
			wantErrs: 0,
		},
		{
			name: "enabled with valid tokenSecretRef — valid",
			spec: &neo4jv1alpha1.AuraFleetManagementSpec{
				Enabled: true,
				TokenSecretRef: &neo4jv1alpha1.AuraTokenSecretRef{
					Name: "aura-fleet-token",
					Key:  "token",
				},
			},
			wantErrs: 0,
		},
		{
			name: "enabled with tokenSecretRef but empty name — error",
			spec: &neo4jv1alpha1.AuraFleetManagementSpec{
				Enabled: true,
				TokenSecretRef: &neo4jv1alpha1.AuraTokenSecretRef{
					Name: "",
					Key:  "token",
				},
			},
			wantErrs:  1,
			errDetail: "tokenSecretRef.name must not be empty when tokenSecretRef is set",
		},
		{
			name: "enabled with tokenSecretRef and default key — valid",
			spec: &neo4jv1alpha1.AuraFleetManagementSpec{
				Enabled: true,
				TokenSecretRef: &neo4jv1alpha1.AuraTokenSecretRef{
					Name: "aura-fleet-token",
					// Key omitted — defaults to "token"
				},
			},
			wantErrs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateAuraFleetManagement(tt.spec, path)
			if len(errs) != tt.wantErrs {
				t.Errorf("got %d errors, want %d: %v", len(errs), tt.wantErrs, errs)
				return
			}
			if tt.errDetail != "" && len(errs) > 0 {
				found := false
				for _, e := range errs {
					if e.Detail == tt.errDetail {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error detail %q, got: %v", tt.errDetail, errs)
				}
			}
		})
	}
}
