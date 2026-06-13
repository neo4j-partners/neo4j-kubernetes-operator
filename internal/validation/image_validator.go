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

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
)

// ImageValidator validates Neo4j image configuration
type ImageValidator struct{}

// NewImageValidator creates a new image validator
func NewImageValidator() *ImageValidator {
	return &ImageValidator{}
}

// Validate validates the image configuration
func (v *ImageValidator) Validate(cluster *neo4jv1beta1.Neo4jEnterpriseCluster) field.ErrorList {
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

	// Invariant 3 (ENTERPRISE-IMAGES, docs/knowledge/invariants.md): reject an
	// image whose tag explicitly marks it community. We reject only the
	// unambiguous `-community` signal — we do NOT require a literal
	// `-enterprise` suffix — so a legitimately retagged Enterprise image in a
	// private registry (e.g. myco/neo4j:5.26.0) still passes. The running
	// operator's `CALL dbms.components()` edition check remains the backstop
	// for a bare or mislabeled tag that turns out to be community.
	if isCommunityTag(cluster.Spec.Image.Tag) {
		allErrs = append(allErrs, field.Invalid(
			imagePath.Child("tag"),
			cluster.Spec.Image.Tag,
			"community images are not supported — use a Neo4j Enterprise image (e.g. neo4j:5.26-enterprise). "+
				"The operator emits Enterprise-only config and Cypher (clustering, SHOW SERVERS, role/privilege management, online backup) that fails on community.",
		))
	}

	// Validate Neo4j version (must be 5.26.x last semver LTS, or 2025.x.x+ CalVer)
	if cluster.Spec.Image.Tag != "" {
		version, err := neo4j.ParseVersion(cluster.Spec.Image.Tag)
		if err != nil || !version.IsSupported() {
			allErrs = append(allErrs, field.Invalid(
				imagePath.Child("tag"),
				cluster.Spec.Image.Tag,
				"Neo4j version must be 5.26.x (last semver LTS) or 2025.01.0+ (CalVer) for enterprise operator",
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

// isCommunityTag reports whether an image tag explicitly marks a Neo4j
// community build. Matches `community` anywhere in the tag (case-insensitive)
// so `5.26.0-community`, `5.26-community`, and `2025.01.0-community` are all
// caught. A bare tag with no edition marker (e.g. `5.26.0`) is NOT treated as
// community here — see the Validate comment for why and for the runtime
// backstop.
func isCommunityTag(tag string) bool {
	return strings.Contains(strings.ToLower(tag), "community")
}

func (v *ImageValidator) isVersionSupported(version string) bool {
	parsed, err := neo4j.ParseVersion(version)
	if err != nil {
		return false
	}
	return parsed.IsSupported()
}
