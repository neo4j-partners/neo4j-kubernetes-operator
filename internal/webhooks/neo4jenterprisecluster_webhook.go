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

package webhooks

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	neo4jv1alpha1 "github.com/neo4j-labs/neo4j-kubernetes-operator/api/v1alpha1"
)

// Neo4jEnterpriseClusterWebhook implements admission webhook for Neo4jEnterpriseCluster
type Neo4jEnterpriseClusterWebhook struct {
	Client client.Client
}

// +kubebuilder:webhook:path=/mutate-neo4j-neo4j-com-v1alpha1-neo4jenterprisecluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters,verbs=create;update,versions=v1alpha1,name=mneo4jenterprisecluster.kb.io,admissionReviewVersions=v1

// Default implements the defaulting webhook for Neo4jEnterpriseCluster
func (w *Neo4jEnterpriseClusterWebhook) Default(ctx context.Context, obj runtime.Object) error {
	cluster, ok := obj.(*neo4jv1alpha1.Neo4jEnterpriseCluster)
	if !ok {
		return fmt.Errorf("expected Neo4jEnterpriseCluster, got %T", obj)
	}

	log := ctrl.LoggerFrom(ctx).WithName("neo4jenterprisecluster-webhook").WithValues("name", cluster.Name)
	log.Info("Applying defaults to Neo4jEnterpriseCluster")

	// Default edition to enterprise
	if cluster.Spec.Edition == "" {
		cluster.Spec.Edition = "enterprise"
		log.Info("Defaulted edition to enterprise")
	}

	// Default image pull policy
	if cluster.Spec.Image.PullPolicy == "" {
		cluster.Spec.Image.PullPolicy = "IfNotPresent"
		log.Info("Defaulted image pull policy to IfNotPresent")
	}

	// Default TLS configuration
	if cluster.Spec.TLS == nil {
		cluster.Spec.TLS = &neo4jv1alpha1.TLSSpec{
			Mode: "cert-manager",
		}
		log.Info("Defaulted TLS mode to cert-manager")
	} else if cluster.Spec.TLS.Mode == "" {
		cluster.Spec.TLS.Mode = "cert-manager"
		log.Info("Defaulted TLS mode to cert-manager")
	}

	// Default TLS issuer reference
	if cluster.Spec.TLS != nil && cluster.Spec.TLS.Mode == "cert-manager" && cluster.Spec.TLS.IssuerRef == nil {
		cluster.Spec.TLS.IssuerRef = &neo4jv1alpha1.IssuerRef{
			Kind: "ClusterIssuer",
		}
		log.Info("Defaulted TLS issuer kind to ClusterIssuer")
	}

	// Default auth configuration
	if cluster.Spec.Auth == nil {
		cluster.Spec.Auth = &neo4jv1alpha1.AuthSpec{
			Provider: "native",
		}
		log.Info("Defaulted auth provider to native")
	} else if cluster.Spec.Auth.Provider == "" {
		cluster.Spec.Auth.Provider = "native"
		log.Info("Defaulted auth provider to native")
	}

	// Default service configuration
	if cluster.Spec.Service == nil {
		cluster.Spec.Service = &neo4jv1alpha1.ServiceSpec{
			Type: "ClusterIP",
		}
		log.Info("Defaulted service type to ClusterIP")
	}

	// Ensure primaries is odd and >= 3
	if cluster.Spec.Topology.Primaries < 3 {
		cluster.Spec.Topology.Primaries = 3
		log.Info("Defaulted primaries to minimum value of 3")
	} else if cluster.Spec.Topology.Primaries%2 == 0 {
		cluster.Spec.Topology.Primaries++
		log.Info("Adjusted primaries to be odd", "value", cluster.Spec.Topology.Primaries)
	}

	return nil
}

// +kubebuilder:webhook:path=/validate-neo4j-neo4j-com-v1alpha1-neo4jenterprisecluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=neo4j.neo4j.com,resources=neo4jenterpriseclusters,verbs=create;update,versions=v1alpha1,name=vneo4jenterprisecluster.kb.io,admissionReviewVersions=v1

// ValidateCreate implements the validation webhook for Neo4jEnterpriseCluster creation
func (w *Neo4jEnterpriseClusterWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	cluster, ok := obj.(*neo4jv1alpha1.Neo4jEnterpriseCluster)
	if !ok {
		return nil, fmt.Errorf("expected Neo4jEnterpriseCluster, got %T", obj)
	}

	log := ctrl.LoggerFrom(ctx).WithName("neo4jenterprisecluster-webhook").WithValues("name", cluster.Name)
	log.Info("Validating Neo4jEnterpriseCluster creation")

	allErrs := w.validateCluster(cluster)
	if len(allErrs) > 0 {
		return nil, allErrs.ToAggregate()
	}

	return nil, nil
}

// ValidateUpdate implements the validation webhook for Neo4jEnterpriseCluster updates
func (w *Neo4jEnterpriseClusterWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	newCluster, ok := newObj.(*neo4jv1alpha1.Neo4jEnterpriseCluster)
	if !ok {
		return nil, fmt.Errorf("expected Neo4jEnterpriseCluster, got %T", newObj)
	}

	oldCluster, ok := oldObj.(*neo4jv1alpha1.Neo4jEnterpriseCluster)
	if !ok {
		return nil, fmt.Errorf("expected Neo4jEnterpriseCluster, got %T", oldObj)
	}

	log := ctrl.LoggerFrom(ctx).WithName("neo4jenterprisecluster-webhook").WithValues("name", newCluster.Name)
	log.Info("Validating Neo4jEnterpriseCluster update")

	allErrs := w.validateCluster(newCluster)
	allErrs = append(allErrs, w.validateClusterUpdate(oldCluster, newCluster)...)

	if len(allErrs) > 0 {
		return nil, allErrs.ToAggregate()
	}

	return nil, nil
}

// ValidateDelete implements the validation webhook for Neo4jEnterpriseCluster deletion
func (w *Neo4jEnterpriseClusterWebhook) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	// Allow all deletions
	return nil, nil
}

func (w *Neo4jEnterpriseClusterWebhook) validateCluster(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList

	// Preallocate slice with estimated capacity to reduce allocations
	allErrs = make(field.ErrorList, 0, 10)

	// Validate edition (fail fast - most critical validation)
	if editionErrs := w.validateEdition(cluster); len(editionErrs) > 0 {
		allErrs = append(allErrs, editionErrs...)
		// For edition errors, fail fast to avoid unnecessary processing
		return allErrs
	}

	// Validate topology (second most critical)
	allErrs = append(allErrs, w.validateTopology(cluster)...)

	// Validate image (fail fast if image validation fails)
	if imageErrs := w.validateImage(cluster); len(imageErrs) > 0 {
		allErrs = append(allErrs, imageErrs...)
		// If image is invalid, other validations are less meaningful
		return allErrs
	}

	// Continue with remaining validations only if critical ones pass
	allErrs = append(allErrs, w.validateStorage(cluster)...)
	allErrs = append(allErrs, w.validateTLS(cluster)...)
	allErrs = append(allErrs, w.validateAuth(cluster)...)

	// Cloud identity validation (least critical, do last)
	allErrs = append(allErrs, w.validateCloudIdentity(cluster)...)

	return allErrs
}

func (w *Neo4jEnterpriseClusterWebhook) validateEdition(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
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

func (w *Neo4jEnterpriseClusterWebhook) validateTopology(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	topologyPath := field.NewPath("spec", "topology")

	// Validate primaries
	if cluster.Spec.Topology.Primaries < 3 {
		allErrs = append(allErrs, field.Invalid(
			topologyPath.Child("primaries"),
			cluster.Spec.Topology.Primaries,
			"primaries must be at least 3 for quorum",
		))
	}

	if cluster.Spec.Topology.Primaries%2 == 0 {
		allErrs = append(allErrs, field.Invalid(
			topologyPath.Child("primaries"),
			cluster.Spec.Topology.Primaries,
			"primaries must be odd to maintain quorum",
		))
	}

	// Validate secondaries
	if cluster.Spec.Topology.Secondaries < 0 {
		allErrs = append(allErrs, field.Invalid(
			topologyPath.Child("secondaries"),
			cluster.Spec.Topology.Secondaries,
			"secondaries cannot be negative",
		))
	}

	return allErrs
}

func (w *Neo4jEnterpriseClusterWebhook) validateImage(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
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
		if !w.isVersionSupported(cluster.Spec.Image.Tag) {
			allErrs = append(allErrs, field.Invalid(
				imagePath.Child("tag"),
				cluster.Spec.Image.Tag,
				"Neo4j version must be 5.26 or higher for enterprise operator",
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

func (w *Neo4jEnterpriseClusterWebhook) validateStorage(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	storagePath := field.NewPath("spec", "storage")

	if cluster.Spec.Storage.ClassName == "" {
		allErrs = append(allErrs, field.Required(
			storagePath.Child("className"),
			"storage class name must be specified",
		))
	}

	if cluster.Spec.Storage.Size == "" {
		allErrs = append(allErrs, field.Required(
			storagePath.Child("size"),
			"storage size must be specified",
		))
	}

	// Validate size format
	if cluster.Spec.Storage.Size != "" {
		if !w.isValidStorageSize(cluster.Spec.Storage.Size) {
			allErrs = append(allErrs, field.Invalid(
				storagePath.Child("size"),
				cluster.Spec.Storage.Size,
				"storage size must be in format like '100Gi', '1Ti'",
			))
		}
	}

	return allErrs
}

func (w *Neo4jEnterpriseClusterWebhook) validateTLS(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList

	if cluster.Spec.TLS == nil {
		return allErrs
	}

	tlsPath := field.NewPath("spec", "tls")
	validModes := []string{"cert-manager", "disabled"}

	if cluster.Spec.TLS.Mode != "" {
		valid := false
		for _, mode := range validModes {
			if cluster.Spec.TLS.Mode == mode {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				tlsPath.Child("mode"),
				cluster.Spec.TLS.Mode,
				validModes,
			))
		}
	}

	// Validate cert-manager specific fields
	if cluster.Spec.TLS.Mode == "cert-manager" {
		if cluster.Spec.TLS.IssuerRef != nil {
			if cluster.Spec.TLS.IssuerRef.Name == "" {
				allErrs = append(allErrs, field.Required(
					tlsPath.Child("issuerRef", "name"),
					"issuer name must be specified when using cert-manager",
				))
			}

			validKinds := []string{"Issuer", "ClusterIssuer"}
			if cluster.Spec.TLS.IssuerRef.Kind != "" {
				valid := false
				for _, kind := range validKinds {
					if cluster.Spec.TLS.IssuerRef.Kind == kind {
						valid = true
						break
					}
				}
				if !valid {
					allErrs = append(allErrs, field.NotSupported(
						tlsPath.Child("issuerRef", "kind"),
						cluster.Spec.TLS.IssuerRef.Kind,
						validKinds,
					))
				}
			}
		}

		// Validate certificate duration and renewal settings
		if cluster.Spec.TLS.Duration != nil {
			if _, err := time.ParseDuration(*cluster.Spec.TLS.Duration); err != nil {
				allErrs = append(allErrs, field.Invalid(
					tlsPath.Child("duration"),
					*cluster.Spec.TLS.Duration,
					"invalid duration format",
				))
			}
		}

		if cluster.Spec.TLS.RenewBefore != nil {
			if _, err := time.ParseDuration(*cluster.Spec.TLS.RenewBefore); err != nil {
				allErrs = append(allErrs, field.Invalid(
					tlsPath.Child("renewBefore"),
					*cluster.Spec.TLS.RenewBefore,
					"invalid duration format",
				))
			}
		}

		// Validate certificate usages
		if len(cluster.Spec.TLS.Usages) > 0 {
			validUsages := []string{
				"digital signature", "key encipherment", "key agreement",
				"server auth", "client auth", "code signing", "email protection",
				"s/mime", "ipsec end system", "ipsec tunnel", "ipsec user",
				"timestamping", "ocsp signing", "microsoft sgc", "netscape sgc",
			}
			for i, usage := range cluster.Spec.TLS.Usages {
				valid := false
				for _, validUsage := range validUsages {
					if usage == validUsage {
						valid = true
						break
					}
				}
				if !valid {
					allErrs = append(allErrs, field.NotSupported(
						tlsPath.Child("usages").Index(i),
						usage,
						validUsages,
					))
				}
			}
		}
	}

	// Validate External Secrets configuration
	if cluster.Spec.TLS.ExternalSecrets != nil && cluster.Spec.TLS.ExternalSecrets.Enabled {
		esPath := tlsPath.Child("externalSecrets")

		if cluster.Spec.TLS.ExternalSecrets.SecretStoreRef == nil {
			allErrs = append(allErrs, field.Required(
				esPath.Child("secretStoreRef"),
				"secretStoreRef is required when external secrets is enabled",
			))
		} else {
			if cluster.Spec.TLS.ExternalSecrets.SecretStoreRef.Name == "" {
				allErrs = append(allErrs, field.Required(
					esPath.Child("secretStoreRef", "name"),
					"secretStore name is required",
				))
			}

			validKinds := []string{"SecretStore", "ClusterSecretStore"}
			if cluster.Spec.TLS.ExternalSecrets.SecretStoreRef.Kind != "" {
				valid := false
				for _, kind := range validKinds {
					if cluster.Spec.TLS.ExternalSecrets.SecretStoreRef.Kind == kind {
						valid = true
						break
					}
				}
				if !valid {
					allErrs = append(allErrs, field.NotSupported(
						esPath.Child("secretStoreRef", "kind"),
						cluster.Spec.TLS.ExternalSecrets.SecretStoreRef.Kind,
						validKinds,
					))
				}
			}
		}

		// Validate refresh interval
		if cluster.Spec.TLS.ExternalSecrets.RefreshInterval != "" {
			if _, err := time.ParseDuration(cluster.Spec.TLS.ExternalSecrets.RefreshInterval); err != nil {
				allErrs = append(allErrs, field.Invalid(
					esPath.Child("refreshInterval"),
					cluster.Spec.TLS.ExternalSecrets.RefreshInterval,
					"invalid duration format",
				))
			}
		}

		// Validate data mappings
		if len(cluster.Spec.TLS.ExternalSecrets.Data) == 0 {
			allErrs = append(allErrs, field.Required(
				esPath.Child("data"),
				"at least one data mapping is required when external secrets is enabled",
			))
		}

		for i, data := range cluster.Spec.TLS.ExternalSecrets.Data {
			dataPath := esPath.Child("data").Index(i)
			if data.SecretKey == "" {
				allErrs = append(allErrs, field.Required(
					dataPath.Child("secretKey"),
					"secretKey is required",
				))
			}
			if data.RemoteRef != nil && data.RemoteRef.Key == "" {
				allErrs = append(allErrs, field.Required(
					dataPath.Child("remoteRef", "key"),
					"remoteRef key is required",
				))
			}
		}
	}

	return allErrs
}

func (w *Neo4jEnterpriseClusterWebhook) validateAuth(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
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

func (w *Neo4jEnterpriseClusterWebhook) validateCloudIdentity(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
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
					allErrs = append(allErrs, w.validateProviderAnnotations(cloud.Provider, cloud.Identity.AutoCreate.Annotations, cloudPath.Child("identity", "autoCreate", "annotations"))...)
				}
			}
		}
	}

	return allErrs
}

func (w *Neo4jEnterpriseClusterWebhook) validateProviderAnnotations(provider string, annotations map[string]string, path *field.Path) field.ErrorList {
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

func (w *Neo4jEnterpriseClusterWebhook) validateClusterUpdate(oldCluster, newCluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList

	// Prevent downgrading primary count below quorum
	if newCluster.Spec.Topology.Primaries < oldCluster.Spec.Topology.Primaries {
		if newCluster.Spec.Topology.Primaries < 3 {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "topology", "primaries"),
				newCluster.Spec.Topology.Primaries,
				"cannot reduce primaries below 3",
			))
		}
	}

	// Validate image upgrades
	if oldCluster.Spec.Image.Tag != newCluster.Spec.Image.Tag {
		// Validate version upgrade path
		if err := w.validateVersionUpgrade(oldCluster.Spec.Image.Tag, newCluster.Spec.Image.Tag); err != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "image", "tag"),
				newCluster.Spec.Image.Tag,
				err.Error(),
			))
		}
	}

	// Validate upgrade strategy changes
	if newCluster.Spec.UpgradeStrategy != nil {
		allErrs = append(allErrs, w.validateUpgradeStrategy(newCluster)...)
	}

	// Warn about storage class changes
	if oldCluster.Spec.Storage.ClassName != newCluster.Spec.Storage.ClassName {
		// This would typically be a warning, but field.ErrorList doesn't support warnings
		// In a real implementation, this could be handled by returning admission.Warnings
	}

	return allErrs
}

// validateVersionUpgrade validates that the version upgrade is supported
func (w *Neo4jEnterpriseClusterWebhook) validateVersionUpgrade(currentVersion, targetVersion string) error {
	// Parse current and target versions
	current := w.parseVersion(currentVersion)
	target := w.parseVersion(targetVersion)

	if current == nil || target == nil {
		return fmt.Errorf("invalid version format")
	}

	// Prevent downgrades
	if w.isDowngrade(current, target) {
		return fmt.Errorf("downgrades are not supported (current: %s, target: %s)", currentVersion, targetVersion)
	}

	// Validate upgrade path based on versioning scheme
	if w.isCalVer(current) && w.isCalVer(target) {
		// CalVer to CalVer upgrade (2025.x.x -> 2025.y.y or 2026.x.x)
		return w.validateCalVerUpgrade(current, target, currentVersion, targetVersion)
	} else if w.isSemVer(current) && w.isSemVer(target) {
		// SemVer to SemVer upgrade (5.x.x -> 5.y.y)
		return w.validateSemVerUpgrade(current, target, currentVersion, targetVersion)
	} else if w.isSemVer(current) && w.isCalVer(target) {
		// SemVer to CalVer upgrade (5.x.x -> 2025.x.x)
		return w.validateSemVerToCalVerUpgrade(current, target, currentVersion, targetVersion)
	} else {
		// CalVer to SemVer (not supported)
		return fmt.Errorf("downgrade from CalVer to SemVer is not supported")
	}
}

// isDowngrade checks if target version is lower than current version
func (w *Neo4jEnterpriseClusterWebhook) isDowngrade(current, target *VersionInfo) bool {
	// For CalVer (year-based)
	if w.isCalVer(current) && w.isCalVer(target) {
		if target.Major < current.Major {
			return true
		}
		if target.Major == current.Major && target.Minor < current.Minor {
			return true
		}
		if target.Major == current.Major && target.Minor == current.Minor && target.Patch < current.Patch {
			return true
		}
		return false
	}

	// For SemVer or mixed comparison
	if target.Major < current.Major {
		return true
	}
	if target.Major == current.Major && target.Minor < current.Minor {
		return true
	}
	if target.Major == current.Major && target.Minor == current.Minor && target.Patch < current.Patch {
		return true
	}

	// Special case: CalVer to SemVer is always a downgrade
	if w.isCalVer(current) && w.isSemVer(target) {
		return true
	}

	return false
}

// isCalVer checks if version follows CalVer format (2025+)
func (w *Neo4jEnterpriseClusterWebhook) isCalVer(version *VersionInfo) bool {
	return version.Major >= 2025
}

// isSemVer checks if version follows SemVer format (5.x)
func (w *Neo4jEnterpriseClusterWebhook) isSemVer(version *VersionInfo) bool {
	return version.Major >= 4 && version.Major <= 10 // Neo4j 4.x, 5.x
}

// validateCalVerUpgrade validates CalVer to CalVer upgrades
func (w *Neo4jEnterpriseClusterWebhook) validateCalVerUpgrade(current, target *VersionInfo, currentStr, targetStr string) error {
	// Allow upgrades within same year (patch/minor)
	if current.Major == target.Major {
		return nil // 2025.1.0 -> 2025.1.1 or 2025.1.0 -> 2025.2.0
	}

	// Allow upgrades to newer years
	if target.Major > current.Major {
		return nil // 2025.x.x -> 2026.x.x
	}

	return fmt.Errorf("unsupported CalVer upgrade path from %s to %s", currentStr, targetStr)
}

// validateSemVerUpgrade validates SemVer to SemVer upgrades
func (w *Neo4jEnterpriseClusterWebhook) validateSemVerUpgrade(current, target *VersionInfo, currentStr, targetStr string) error {
	// Only allow upgrades within same major version
	if target.Major != current.Major {
		return fmt.Errorf("major version upgrades are not supported")
	}

	// Allow minor and patch upgrades within supported range
	if current.Major == 5 && target.Major == 5 {
		if current.Minor >= 26 && target.Minor >= 26 {
			return nil // Allow upgrades within 5.26+
		}
		return fmt.Errorf("only Neo4j 5.26+ versions are supported")
	}

	if current.Major == 4 && target.Major == 4 {
		if current.Minor >= 4 {
			return nil // Allow upgrades within 4.4+
		}
		return fmt.Errorf("only Neo4j 4.4+ versions are supported")
	}

	return fmt.Errorf("unsupported SemVer upgrade path from %s to %s", currentStr, targetStr)
}

// validateSemVerToCalVerUpgrade validates upgrades from SemVer to CalVer
func (w *Neo4jEnterpriseClusterWebhook) validateSemVerToCalVerUpgrade(current, _ *VersionInfo, currentStr, targetStr string) error {
	// Only allow upgrades from Neo4j 5.26+ to CalVer
	if current.Major == 5 && current.Minor >= 26 {
		return nil // 5.26+ -> 2025.x.x is allowed
	}

	return fmt.Errorf("upgrade from %s to CalVer %s requires Neo4j 5.26 or higher", currentStr, targetStr)
}

// parseVersion parses a version string into components, handling both SemVer and CalVer
func (w *Neo4jEnterpriseClusterWebhook) parseVersion(version string) *VersionInfo {
	// Remove any prefix like "v" and suffixes like "-enterprise"
	version = strings.TrimPrefix(version, "v")
	if idx := strings.Index(version, "-"); idx != -1 {
		version = version[:idx]
	}

	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return nil
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil
	}

	patch := 0
	if len(parts) > 2 {
		if p, err := strconv.Atoi(parts[2]); err == nil {
			patch = p
		}
	}

	return &VersionInfo{
		Major: major,
		Minor: minor,
		Patch: patch,
	}
}

// VersionInfo represents parsed version information
type VersionInfo struct {
	Major int
	Minor int
	Patch int
}

// Helper methods

func (w *Neo4jEnterpriseClusterWebhook) isVersionSupported(version string) bool {
	// Simple version validation - in production this would be more sophisticated
	if strings.HasPrefix(version, "5.") {
		// Extract minor version
		parts := strings.Split(version, ".")
		if len(parts) >= 2 {
			switch parts[1] {
			case "26", "27", "28", "29", "30", "31", "32", "33", "34", "35":
				return true
			}
			// For versions like 5.XX where XX > 25, assume supported
			if len(parts[1]) >= 2 {
				return true
			}
		}
	}
	return false
}

func (w *Neo4jEnterpriseClusterWebhook) isValidStorageSize(size string) bool {
	// Simple storage size validation
	matched, _ := regexp.MatchString(`^\d+([KMGT]i?)?$`, size)
	return matched
}

func (w *Neo4jEnterpriseClusterWebhook) validateUpgradeStrategy(cluster *neo4jv1alpha1.Neo4jEnterpriseCluster) field.ErrorList {
	var allErrs field.ErrorList
	strategyPath := field.NewPath("spec", "upgradeStrategy")

	if cluster.Spec.UpgradeStrategy == nil {
		return allErrs
	}

	strategy := cluster.Spec.UpgradeStrategy

	// Validate strategy type
	validStrategies := []string{"RollingUpgrade", "Recreate"}
	if strategy.Strategy != "" {
		valid := false
		for _, validStrategy := range validStrategies {
			if strategy.Strategy == validStrategy {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				strategyPath.Child("strategy"),
				strategy.Strategy,
				validStrategies,
			))
		}
	}

	// Validate timeout durations
	if strategy.UpgradeTimeout != "" {
		if _, err := time.ParseDuration(strategy.UpgradeTimeout); err != nil {
			allErrs = append(allErrs, field.Invalid(
				strategyPath.Child("upgradeTimeout"),
				strategy.UpgradeTimeout,
				"invalid duration format",
			))
		}
	}

	if strategy.HealthCheckTimeout != "" {
		if _, err := time.ParseDuration(strategy.HealthCheckTimeout); err != nil {
			allErrs = append(allErrs, field.Invalid(
				strategyPath.Child("healthCheckTimeout"),
				strategy.HealthCheckTimeout,
				"invalid duration format",
			))
		}
	}

	if strategy.StabilizationTimeout != "" {
		if _, err := time.ParseDuration(strategy.StabilizationTimeout); err != nil {
			allErrs = append(allErrs, field.Invalid(
				strategyPath.Child("stabilizationTimeout"),
				strategy.StabilizationTimeout,
				"invalid duration format",
			))
		}
	}

	// Validate maxUnavailableDuringUpgrade
	if strategy.MaxUnavailableDuringUpgrade != nil {
		if *strategy.MaxUnavailableDuringUpgrade < 0 {
			allErrs = append(allErrs, field.Invalid(
				strategyPath.Child("maxUnavailableDuringUpgrade"),
				*strategy.MaxUnavailableDuringUpgrade,
				"must be non-negative",
			))
		}
	}

	return allErrs
}

// SetupWebhookWithManager configures the webhook with the manager
func (w *Neo4jEnterpriseClusterWebhook) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&neo4jv1alpha1.Neo4jEnterpriseCluster{}).
		WithDefaulter(w).
		WithValidator(w).
		Complete()
}
