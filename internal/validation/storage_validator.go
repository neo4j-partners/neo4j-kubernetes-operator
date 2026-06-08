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
	"regexp"

	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
)

// StorageValidator validates Neo4j storage configuration
type StorageValidator struct{}

// NewStorageValidator creates a new storage validator
func NewStorageValidator() *StorageValidator {
	return &StorageValidator{}
}

// Validate validates the storage configuration
func (v *StorageValidator) Validate(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	storagePath := field.NewPath("spec", "storage")

	// An empty className is intentionally allowed: the PVC then inherits the
	// cluster's default StorageClass (see resources.StorageClassNamePtr). When a
	// className IS given, the reconciler verifies it exists at apply time and
	// surfaces an explicit error rather than leaving pods Pending indefinitely.

	if cluster.Spec.Storage.Size == "" {
		allErrs = append(allErrs, field.Required(
			storagePath.Child("size"),
			"storage size must be specified",
		))
	}

	// Validate size format
	if cluster.Spec.Storage.Size != "" {
		if !v.isValidStorageSize(cluster.Spec.Storage.Size) {
			allErrs = append(allErrs, field.Invalid(
				storagePath.Child("size"),
				cluster.Spec.Storage.Size,
				"storage size must be in format like '100Gi', '1Ti'",
			))
		}
	}

	return allErrs
}

// isValidStorageSize validates storage size format
func (v *StorageValidator) isValidStorageSize(size string) bool {
	// Simple storage size validation
	matched, err := regexp.MatchString(`^\d+([KMGT]i?)?$`, size)
	if err != nil {
		return false // Invalid regex should not happen, but handle gracefully
	}
	return matched
}
