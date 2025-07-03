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

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// EditionValidator validates Neo4j edition configuration
type EditionValidator struct{}

// NewEditionValidator creates a new edition validator
func NewEditionValidator() *EditionValidator {
	return &EditionValidator{}
}

// Validate validates the edition configuration
func (v *EditionValidator) Validate(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	if cluster.Spec.Edition != "enterprise" {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("edition"),
			cluster.Spec.Edition,
			"only 'enterprise' edition is supported",
		))
	}

	return allErrs
}
