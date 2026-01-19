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

	neo4jv1alpha1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/neo4j"
)

// ImageValidator validates Neo4j image configuration
type ImageValidator struct{}

// NewImageValidator creates a new image validator
func NewImageValidator() *ImageValidator {
	return &ImageValidator{}
}

// Validate validates the image configuration
func (v *ImageValidator) Validate(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	imagePath := field.NewPath("spec", "image")

	if cluster.Spec.Image.Repo == "" {
		allErrs = append(allErrs, field.Required(
			imagePath.Child("repo"),
			"image repository must be specified",
		))
	}

	if cluster.Spec.Image.Tag == "" {
		allErrs = append(allErrs, field.Required(
			imagePath.Child("tag"),
			"image tag must be specified",
		))
	}

	// Validate Neo4j version (must be 5.26+)
	if cluster.Spec.Image.Tag != "" {
		version, err := neo4j.ParseVersion(cluster.Spec.Image.Tag)
		if err != nil || !version.IsSupported() {
			allErrs = append(allErrs, field.Invalid(
				imagePath.Child("tag"),
				cluster.Spec.Image.Tag,
				"Neo4j version must be 5.26+ (Semver) or 2025.01.0+ (Calver) for enterprise operator",
			))
		}
	}

	// Validate pull policy
	validPullPolicies := []string{"Always", "Never", "IfNotPresent"}
	if cluster.Spec.Image.PullPolicy != "" {
		valid := false
		for _, policy := range validPullPolicies {
			if cluster.Spec.Image.PullPolicy == policy {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				imagePath.Child("pullPolicy"),
				cluster.Spec.Image.PullPolicy,
				validPullPolicies,
			))
		}
	}

	return allErrs
}

func (v *ImageValidator) isVersionSupported(version string) bool {
	parsed, err := neo4j.ParseVersion(version)
	if err != nil {
		return false
	}
	return parsed.IsSupported()
}
