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
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	neo4jv1alpha1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1alpha1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
)

// StandaloneValidator validates Neo4j standalone configuration
type StandaloneValidator struct {
	editionValidator *EditionValidator
	imageValidator   *ImageValidator
	storageValidator *StorageValidator
	tlsValidator     *TLSValidator
	authValidator    *AuthValidator
}

// NewStandaloneValidator creates a new standalone validator
func NewStandaloneValidator() *StandaloneValidator {
	return &StandaloneValidator{
		editionValidator: NewEditionValidator(),
		imageValidator:   NewImageValidator(),
		storageValidator: NewStorageValidator(),
		tlsValidator:     NewTLSValidator(),
		authValidator:    NewAuthValidator(),
	}
}

// ValidateCreate validates a new standalone deployment
func (v *StandaloneValidator) ValidateCreate(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) field.ErrorList {
	var allErrs field.ErrorList

	// Edition validation removed - operator only supports enterprise edition

	// Validate image
	if errs := v.validateImage(standalone); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	// Validate storage
	if errs := v.validateStorage(standalone); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	// Validate TLS configuration
	if errs := v.validateTLS(standalone); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	// Validate auth configuration
	if errs := v.validateAuth(standalone); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	// Validate custom configuration for single mode
	if errs := v.validateConfig(standalone); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	// Validate MCP configuration
	allErrs = append(allErrs, validateMCPConfig(standalone.Spec.MCP, field.NewPath("spec", "mcp"))...)

	// Aura Fleet Management validation
	allErrs = append(allErrs, validateAuraFleetManagement(standalone.Spec.AuraFleetManagement, field.NewPath("spec", "auraFleetManagement"))...)

	return allErrs
}

// ValidateUpdate validates an update to a standalone deployment
func (v *StandaloneValidator) ValidateUpdate(oldStandalone, newStandalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) field.ErrorList {
	var allErrs field.ErrorList

	// Validate the new standalone configuration
	if errs := v.ValidateCreate(newStandalone); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	// Validate image upgrade path
	if errs := v.validateImageUpgrade(oldStandalone, newStandalone); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	// Validate storage changes (should be immutable)
	if errs := v.validateStorageChanges(oldStandalone, newStandalone); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	return allErrs
}

// validateEdition validates the edition field
// Edition field has been removed - operator only supports enterprise edition
func (v *StandaloneValidator) validateEdition(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) field.ErrorList {
	return field.ErrorList{}
}

// validateImage validates the image configuration
func (v *StandaloneValidator) validateImage(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) field.ErrorList {
	var allErrs field.ErrorList
	imagePath := field.NewPath("spec", "image")

	// Use the existing image validator
	if standalone.Spec.Image.Repo == "" {
		allErrs = append(allErrs, field.Required(
			imagePath.Child("repo"),
			"image repository is required",
		))
	}

	if standalone.Spec.Image.Tag == "" {
		allErrs = append(allErrs, field.Required(
			imagePath.Child("tag"),
			"image tag is required",
		))
	}

	// Validate Neo4j version (5.26.x last semver LTS or 2025.x.x CalVer)
	if errs := v.validateNeo4jVersion(standalone.Spec.Image.Tag); len(errs) > 0 {
		for _, err := range errs {
			allErrs = append(allErrs, field.Invalid(
				imagePath.Child("tag"),
				standalone.Spec.Image.Tag,
				err.Error(),
			))
		}
	}

	return allErrs
}

// validateStorage validates the storage configuration
func (v *StandaloneValidator) validateStorage(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) field.ErrorList {
	var allErrs field.ErrorList
	storagePath := field.NewPath("spec", "storage")

	if standalone.Spec.Storage.ClassName == "" {
		allErrs = append(allErrs, field.Required(
			storagePath.Child("className"),
			"storage class name is required",
		))
	}

	if standalone.Spec.Storage.Size == "" {
		allErrs = append(allErrs, field.Required(
			storagePath.Child("size"),
			"storage size is required",
		))
	}

	return allErrs
}

// validateTLS validates the TLS configuration
func (v *StandaloneValidator) validateTLS(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) field.ErrorList {
	var allErrs field.ErrorList

	if standalone.Spec.TLS == nil {
		return allErrs
	}

	tlsPath := field.NewPath("spec", "tls")

	// Validate TLS mode
	if standalone.Spec.TLS.Mode != "" && standalone.Spec.TLS.Mode != "cert-manager" && standalone.Spec.TLS.Mode != "disabled" {
		allErrs = append(allErrs, field.Invalid(
			tlsPath.Child("mode"),
			standalone.Spec.TLS.Mode,
			"TLS mode must be 'cert-manager' or 'disabled'",
		))
	}

	// If cert-manager mode, validate issuer reference
	if standalone.Spec.TLS.Mode == "cert-manager" {
		if standalone.Spec.TLS.IssuerRef == nil {
			allErrs = append(allErrs, field.Required(
				tlsPath.Child("issuerRef"),
				"issuer reference is required when TLS mode is 'cert-manager'",
			))
		} else if standalone.Spec.TLS.IssuerRef.Name == "" {
			allErrs = append(allErrs, field.Required(
				tlsPath.Child("issuerRef", "name"),
				"issuer name is required",
			))
		}
	}

	return allErrs
}

// validateAuth validates the auth configuration
func (v *StandaloneValidator) validateAuth(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) field.ErrorList {
	var allErrs field.ErrorList

	if standalone.Spec.Auth == nil {
		return allErrs
	}

	authPath := field.NewPath("spec", "auth")

	// Validate auth provider
	validProviders := []string{"native", "ldap", "kerberos", "jwt"}
	if standalone.Spec.Auth.Provider != "" {
		found := false
		for _, provider := range validProviders {
			if standalone.Spec.Auth.Provider == provider {
				found = true
				break
			}
		}
		if !found {
			allErrs = append(allErrs, field.Invalid(
				authPath.Child("provider"),
				standalone.Spec.Auth.Provider,
				fmt.Sprintf("auth provider must be one of: %s", strings.Join(validProviders, ", ")),
			))
		}
	}

	return allErrs
}

// validateConfig validates the custom configuration for single mode
func (v *StandaloneValidator) validateConfig(standalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) field.ErrorList {
	var allErrs field.ErrorList

	if standalone.Spec.Config == nil {
		return allErrs
	}

	configPath := field.NewPath("spec", "config")

	// Validate that clustering-related configurations are not set
	clusteringConfigs := []string{
		"dbms.cluster.discovery.version",
		"dbms.cluster.discovery.type",
		"dbms.kubernetes.service_port_name",
		"dbms.kubernetes.discovery.v2.service_port_name",
		"dbms.kubernetes.discovery.service_port_name",
		"dbms.cluster.minimum_initial_system_primaries_count",
		"internal.dbms.single_raft_enabled",
		"dbms.cluster.discovery.endpoints",
		"dbms.cluster.network.advertised_address",
		"dbms.cluster.network.listen_address",
		"dbms.cluster.bind_address",
		"dbms.cluster.raft.advertised_address",
		"dbms.cluster.raft.listen_address",
		"dbms.cluster.raft.bind_address",
	}

	for _, config := range clusteringConfigs {
		if _, exists := standalone.Spec.Config[config]; exists {
			allErrs = append(allErrs, field.Invalid(
				configPath.Key(config),
				standalone.Spec.Config[config],
				fmt.Sprintf("clustering configuration '%s' is not allowed in standalone deployments", config),
			))
		}
	}

	// Validate that dbms.mode is not set (deprecated in Neo4j 5.x+)
	if mode, exists := standalone.Spec.Config["dbms.mode"]; exists {
		allErrs = append(allErrs, field.Invalid(
			configPath.Key("dbms.mode"),
			mode,
			"dbms.mode is deprecated in Neo4j 5.x+ and should not be used. Remove this setting for Neo4j 5.26+ deployments",
		))
	}

	return allErrs
}

// validateImageUpgrade validates image upgrade path
func (v *StandaloneValidator) validateImageUpgrade(oldStandalone, newStandalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) field.ErrorList {
	var allErrs field.ErrorList

	// For now, allow all upgrades - more sophisticated validation can be added later
	return allErrs
}

// validateStorageChanges validates storage changes (should be immutable)
func (v *StandaloneValidator) validateStorageChanges(oldStandalone, newStandalone *neo4jv1alpha1.Neo4jEnterpriseStandalone) field.ErrorList {
	var allErrs field.ErrorList
	storagePath := field.NewPath("spec", "storage")

	// Storage class should not change
	if oldStandalone.Spec.Storage.ClassName != newStandalone.Spec.Storage.ClassName {
		allErrs = append(allErrs, field.Invalid(
			storagePath.Child("className"),
			newStandalone.Spec.Storage.ClassName,
			"storage class name cannot be changed after creation",
		))
	}

	// Storage size can only increase
	if oldStandalone.Spec.Storage.Size != newStandalone.Spec.Storage.Size {
		// Note: More sophisticated size comparison logic would be needed here
		// For now, we'll allow any size change
	}

	return allErrs
}

// validateNeo4jVersion validates Neo4j version requirements
func (v *StandaloneValidator) validateNeo4jVersion(tag string) []error {
	var errs []error

	version, err := neo4j.ParseVersion(tag)
	if err != nil || !version.IsSupported() {
		errs = append(errs, fmt.Errorf("Neo4j version must be 5.26.x (last semver LTS) or 2025.01+ (CalVer), got: %s", tag))
	}

	return errs
}
