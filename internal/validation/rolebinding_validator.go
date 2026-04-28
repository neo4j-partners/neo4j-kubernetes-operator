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
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// RoleBindingValidator validates Neo4jRoleBinding resources.
type RoleBindingValidator struct {
	client client.Client
}

// NewRoleBindingValidator constructs a RoleBindingValidator backed by a
// controller-runtime client (used to resolve clusterRef and detect overlap
// with Neo4jUser CRs in the same namespace).
func NewRoleBindingValidator(c client.Client) *RoleBindingValidator {
	return &RoleBindingValidator{client: c}
}

// RoleBindingValidationResult collects errors and non-fatal warnings.
type RoleBindingValidationResult struct {
	Errors   field.ErrorList
	Warnings []string
}

// Validate runs all checks on a Neo4jRoleBinding spec.
func (v *RoleBindingValidator) Validate(ctx context.Context, rb *neo4jv1beta1.Neo4jRoleBinding) *RoleBindingValidationResult {
	result := &RoleBindingValidationResult{}

	// Username shape (kubebuilder enforces pattern + maxLength but we keep a
	// runtime check for parity with the Neo4jUser validator).
	if errs := validateUsername(rb.Spec.Username, field.NewPath("spec", "username")); len(errs) > 0 {
		result.Errors = append(result.Errors, errs...)
	}

	// Roles list — must be non-empty (kubebuilder MinItems=1 enforces this
	// at apply-time). Also reject empty entries and warn on PUBLIC.
	if len(rb.Spec.Roles) == 0 {
		result.Errors = append(result.Errors, field.Required(
			field.NewPath("spec", "roles"),
			"at least one role must be granted by a Neo4jRoleBinding",
		))
	}
	for i, role := range rb.Spec.Roles {
		path := field.NewPath("spec", "roles").Index(i)
		if strings.TrimSpace(role) == "" {
			result.Errors = append(result.Errors, field.Invalid(path, role, "role name must not be empty"))
			continue
		}
		if strings.EqualFold(role, "PUBLIC") {
			result.Warnings = append(result.Warnings,
				"PUBLIC is granted to every user automatically; listing it here has no effect")
		}
	}

	// Deletion policy enum (kubebuilder enforces; defensive runtime check).
	switch rb.Spec.DeletionPolicy {
	case "", "Revoke", "Retain":
	default:
		result.Errors = append(result.Errors, field.NotSupported(
			field.NewPath("spec", "deletionPolicy"),
			rb.Spec.DeletionPolicy,
			[]string{"Revoke", "Retain"},
		))
	}

	// clusterRef must resolve, and must not overlap with a Neo4jUser CR
	// targeting the same username — that would create two CRs racing on the
	// same role grants.
	v.validateClusterRefAndUserOverlap(ctx, rb, result)

	return result
}

func (v *RoleBindingValidator) validateClusterRefAndUserOverlap(ctx context.Context, rb *neo4jv1beta1.Neo4jRoleBinding, result *RoleBindingValidationResult) {
	if v.client == nil {
		return
	}
	clusterRefPath := field.NewPath("spec", "clusterRef")
	if rb.Spec.ClusterRef == "" {
		result.Errors = append(result.Errors, field.Required(clusterRefPath, "clusterRef is required"))
		return
	}

	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	clusterKey := types.NamespacedName{Name: rb.Spec.ClusterRef, Namespace: rb.Namespace}
	clusterFound := false
	if err := v.client.Get(ctx, clusterKey, cluster); err == nil {
		clusterFound = true
	} else if !errors.IsNotFound(err) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("could not verify clusterRef %q: %v", rb.Spec.ClusterRef, err))
	}
	if !clusterFound {
		standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
		if err := v.client.Get(ctx, clusterKey, standalone); err == nil {
			clusterFound = true
		} else if !errors.IsNotFound(err) {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("could not verify clusterRef %q: %v", rb.Spec.ClusterRef, err))
		}
	}
	if !clusterFound {
		result.Errors = append(result.Errors, field.NotFound(clusterRefPath,
			fmt.Sprintf("no Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone named %q in namespace %q", rb.Spec.ClusterRef, rb.Namespace)))
	}

	// Refuse to overlap with a Neo4jUser CR managing the same username.
	users := &neo4jv1beta1.Neo4jUserList{}
	if err := v.client.List(ctx, users, client.InNamespace(rb.Namespace)); err == nil {
		for i := range users.Items {
			u := &users.Items[i]
			if u.Spec.ClusterRef != rb.Spec.ClusterRef {
				continue
			}
			username := u.Spec.Username
			if username == "" {
				username = u.Name
			}
			if username == rb.Spec.Username {
				result.Errors = append(result.Errors, field.Forbidden(
					field.NewPath("spec", "username"),
					fmt.Sprintf("Neo4jUser %q in this namespace already manages user %q on cluster %q; manage role grants there instead of via Neo4jRoleBinding", u.Name, username, rb.Spec.ClusterRef),
				))
				break
			}
		}
	}
}
