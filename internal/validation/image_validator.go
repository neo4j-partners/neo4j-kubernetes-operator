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
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
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
		if !v.isVersionSupported(cluster.Spec.Image.Tag) {
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

// isVersionSupported checks if the Neo4j version is supported
func (v *ImageValidator) isVersionSupported(version string) bool {
	// Remove any prefix like "v" and suffixes like "-enterprise"
	cleanVersion := strings.TrimPrefix(version, "v")
	if idx := strings.Index(cleanVersion, "-"); idx != -1 {
		cleanVersion = cleanVersion[:idx]
	}

	parts := strings.Split(cleanVersion, ".")
	if len(parts) < 2 {
		return false
	}

	// Check for CalVer format (2025.x.x and up)
	if strings.HasPrefix(cleanVersion, "2025.") {
		return true // All 2025.x.x versions are supported
	}

	// Check for SemVer format (5.26.x and up)
	if strings.HasPrefix(cleanVersion, "5.") {
		if len(parts) >= 2 {
			if minorStr := parts[1]; minorStr != "" {
				// Parse minor version
				if len(minorStr) == 2 {
					// Handle versions like 5.26, 5.27, etc.
					switch minorStr {
					case "26", "27", "28", "29", "30", "31", "32", "33", "34", "35", "36", "37", "38", "39":
						return true
					}
				} else if len(minorStr) >= 3 {
					// Handle versions like 5.100+ (future versions)
					return true
				}
			}
		}
	}

	return false
}
