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

// CloudValidator validates Neo4j cloud identity configuration
type CloudValidator struct{}

// NewCloudValidator creates a new cloud validator
func NewCloudValidator() *CloudValidator {
	return &CloudValidator{}
}

// Validate validates the cloud identity configuration
func (v *CloudValidator) Validate(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList

	if cluster.Spec.Backups == nil || cluster.Spec.Backups.Cloud == nil {
		return allErrs
	}

	cloudPath := field.NewPath("spec", "backups", "cloud")
	cloud := cluster.Spec.Backups.Cloud

	// If cloud provider is specified, validate identity configuration
	if cloud.Provider != "" {
		validProviders := []string{"aws", "gcp", "azure"}
		valid := false
		for _, provider := range validProviders {
			if cloud.Provider == provider {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				cloudPath.Child("provider"),
				cloud.Provider,
				validProviders,
			))
		}

		// Validate identity configuration
		if cloud.Identity != nil {
			if cloud.Identity.Provider != cloud.Provider {
				allErrs = append(allErrs, field.Invalid(
					cloudPath.Child("identity", "provider"),
					cloud.Identity.Provider,
					"identity provider must match cloud provider",
				))
			}

			// Validate service account creation
			if cloud.Identity.AutoCreate != nil && cloud.Identity.AutoCreate.Enabled {
				if cloud.Identity.AutoCreate.Annotations == nil {
					allErrs = append(allErrs, field.Required(
						cloudPath.Child("identity", "autoCreate", "annotations"),
						"annotations are required for auto-created service accounts",
					))
				} else {
					// Validate provider-specific annotations
					allErrs = append(allErrs, v.validateProviderAnnotations(cloud.Provider, cloud.Identity.AutoCreate.Annotations, cloudPath.Child("identity", "autoCreate", "annotations"))...)
				}
			}
		}
	}

	return allErrs
}

// validateProviderAnnotations validates provider-specific annotations
func (v *CloudValidator) validateProviderAnnotations(provider string, annotations map[string]string, path *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	switch provider {
	case "aws":
		if _, exists := annotations["eks.amazonaws.com/role-arn"]; !exists {
			allErrs = append(allErrs, field.Required(
				path.Key("eks.amazonaws.com/role-arn"),
				"AWS IRSA requires role-arn annotation",
			))
		}
	case "gcp":
		if _, exists := annotations["iam.gke.io/gcp-service-account"]; !exists {
			allErrs = append(allErrs, field.Required(
				path.Key("iam.gke.io/gcp-service-account"),
				"GCP Workload Identity requires gcp-service-account annotation",
			))
		}
	case "azure":
		if _, exists := annotations["azure.workload.identity/client-id"]; !exists {
			allErrs = append(allErrs, field.Required(
				path.Key("azure.workload.identity/client-id"),
				"Azure Workload Identity requires client-id annotation",
			))
		}
	}

	return allErrs
}
