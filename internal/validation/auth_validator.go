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
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// AuthValidator validates Neo4j authentication configuration
type AuthValidator struct{}

// NewAuthValidator creates a new auth validator
func NewAuthValidator() *AuthValidator {
	return &AuthValidator{}
}

// Validate validates the authentication configuration
func (v *AuthValidator) Validate(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList

	if cluster.Spec.Auth == nil {
		return allErrs
	}

	authPath := field.NewPath("spec", "auth")
	validProviders := []string{"native", "ldap", "kerberos", "jwt"}

	if cluster.Spec.Auth.Provider != "" {
		valid := false
		for _, provider := range validProviders {
			if cluster.Spec.Auth.Provider == provider {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				authPath.Child("provider"),
				cluster.Spec.Auth.Provider,
				validProviders,
			))
		}
	}

	// Validate that external auth providers have secretRef
	if cluster.Spec.Auth.Provider != "" && cluster.Spec.Auth.Provider != "native" {
		if cluster.Spec.Auth.SecretRef == "" {
			allErrs = append(allErrs, field.Required(
				authPath.Child("secretRef"),
				fmt.Sprintf("secretRef is required for %s auth provider", cluster.Spec.Auth.Provider),
			))
		}
	}

	return allErrs
}
