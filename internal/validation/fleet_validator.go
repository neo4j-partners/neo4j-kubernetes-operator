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
	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
)

// validateAuraFleetManagement validates the auraFleetManagement spec for a cluster or standalone.
func validateAuraFleetManagement(spec *neo4jv1alpha1.AuraFleetManagementSpec, path *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if spec == nil || !spec.Enabled {
		return allErrs
	}

	// tokenSecretRef is optional: the plugin is installed even without it, but registration
	// is skipped until a token is provided. Validate the ref only when present.
	if spec.TokenSecretRef != nil {
		if spec.TokenSecretRef.Name == "" {
			allErrs = append(allErrs, field.Required(
				path.Child("tokenSecretRef", "name"),
				"tokenSecretRef.name must not be empty when tokenSecretRef is set",
			))
		}
	}

	return allErrs
}
