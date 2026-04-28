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
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/neo4j-partners/neo4j-kubernetes-operator/api/v1beta1"
)

// UserValidator validates Neo4jUser resources.
type UserValidator struct {
	client client.Client
}

// NewUserValidator constructs a UserValidator backed by a controller-runtime
// client (used to resolve clusterRef and Secret references).
func NewUserValidator(c client.Client) *UserValidator {
	return &UserValidator{client: c}
}

// UserValidationResult collects errors and non-fatal warnings.
type UserValidationResult struct {
	Errors   field.ErrorList
	Warnings []string
}

// neo4jUsernamePattern enforces Neo4j's username rules: ASCII letter
// followed by letters, digits, underscore, dot or hyphen.
var neo4jUsernamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.\-]*$`)

const maxNeo4jUsernameLength = 65

// reservedUsernames are usernames the operator refuses to manage to prevent
// accidentally locking the operator out of the cluster.
var reservedUsernames = map[string]struct{}{
	"system": {}, // not a real user but a reserved keyword
}

// Validate runs all checks on a Neo4jUser spec.
func (v *UserValidator) Validate(ctx context.Context, user *neo4jv1beta1.Neo4jUser) *UserValidationResult {
	result := &UserValidationResult{}

	username := user.Spec.Username
	if username == "" {
		username = user.Name
	}

	// Username shape
	if err := validateUsername(username, field.NewPath("spec", "username")); len(err) > 0 {
		result.Errors = append(result.Errors, err...)
	}

	if _, reserved := reservedUsernames[strings.ToLower(username)]; reserved {
		result.Errors = append(result.Errors, field.Forbidden(
			field.NewPath("spec", "username"),
			fmt.Sprintf("%q is reserved", username)))
	}

	// At least one auth provider (password Secret OR ExternalAuth) must be set.
	hasPassword := user.Spec.PasswordSecretRef != nil
	hasExternal := len(user.Spec.ExternalAuth) > 0
	if !hasPassword && !hasExternal {
		result.Errors = append(result.Errors, field.Required(
			field.NewPath("spec"),
			"either passwordSecretRef or at least one externalAuth provider must be set (Neo4j requires at least one auth provider per user)",
		))
	}

	// Account status enum (kubebuilder enum already enforces this; we keep
	// a defensive runtime check).
	if user.Spec.AccountStatus != "" && user.Spec.AccountStatus != "active" && user.Spec.AccountStatus != "suspended" {
		result.Errors = append(result.Errors, field.NotSupported(
			field.NewPath("spec", "accountStatus"),
			user.Spec.AccountStatus,
			[]string{"active", "suspended"}))
	}

	// Home database name
	if user.Spec.HomeDatabase != "" {
		if errs, _ := validateDatabaseName(user.Spec.HomeDatabase, field.NewPath("spec", "homeDatabase")); len(errs) > 0 {
			result.Errors = append(result.Errors, errs...)
		}
	}

	// Roles list
	for i, role := range user.Spec.Roles {
		path := field.NewPath("spec", "roles").Index(i)
		if role == "" {
			result.Errors = append(result.Errors, field.Invalid(path, role, "role name must not be empty"))
			continue
		}
		if strings.EqualFold(role, "PUBLIC") {
			result.Warnings = append(result.Warnings,
				"PUBLIC is granted to every user automatically; listing it here has no effect")
		}
	}

	// External auth providers
	for i, ap := range user.Spec.ExternalAuth {
		path := field.NewPath("spec", "externalAuth").Index(i)
		if strings.TrimSpace(ap.Provider) == "" {
			result.Errors = append(result.Errors, field.Required(path.Child("provider"), "provider must not be empty"))
		}
		if strings.TrimSpace(ap.ID) == "" {
			result.Errors = append(result.Errors, field.Required(path.Child("id"), "id must not be empty"))
		}
		if strings.EqualFold(ap.Provider, "native") {
			result.Errors = append(result.Errors, field.Invalid(path.Child("provider"), ap.Provider,
				"use spec.passwordSecretRef to configure native authentication; the 'native' provider is not valid in externalAuth"))
		}
	}

	// PasswordSecretRef structure and existence
	if hasPassword {
		v.validatePasswordSecret(ctx, user, result)
	}

	// clusterRef must resolve.
	v.validateClusterRef(ctx, user, result)

	return result
}

func (v *UserValidator) validatePasswordSecret(ctx context.Context, user *neo4jv1beta1.Neo4jUser, result *UserValidationResult) {
	if v.client == nil {
		return
	}
	path := field.NewPath("spec", "passwordSecretRef")
	ref := user.Spec.PasswordSecretRef
	if ref.Name == "" {
		result.Errors = append(result.Errors, field.Required(path.Child("name"), "secret name is required"))
		return
	}
	key := ref.Key
	if key == "" {
		key = "password"
	}

	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{Name: ref.Name, Namespace: user.Namespace}
	if err := v.client.Get(ctx, secretKey, secret); err != nil {
		if errors.IsNotFound(err) {
			result.Errors = append(result.Errors, field.NotFound(path.Child("name"),
				fmt.Sprintf("Secret %q not found in namespace %q", ref.Name, user.Namespace)))
			return
		}
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("could not verify password secret %q: %v", ref.Name, err))
		return
	}
	if _, ok := secret.Data[key]; !ok {
		result.Errors = append(result.Errors, field.NotFound(path.Child("key"),
			fmt.Sprintf("Secret %q has no key %q", ref.Name, key)))
		return
	}
	if len(secret.Data[key]) == 0 {
		result.Errors = append(result.Errors, field.Invalid(path.Child("key"), key,
			"password secret value must not be empty"))
	}
	if len(secret.Data[key]) < 8 {
		result.Warnings = append(result.Warnings,
			"password is shorter than Neo4j's default minimum (8 characters); CREATE/ALTER USER will be rejected unless dbms.security.auth_minimum_password_length is lowered")
	}
}

func (v *UserValidator) validateClusterRef(ctx context.Context, user *neo4jv1beta1.Neo4jUser, result *UserValidationResult) {
	if v.client == nil {
		return
	}
	clusterRefPath := field.NewPath("spec", "clusterRef")
	if user.Spec.ClusterRef == "" {
		result.Errors = append(result.Errors, field.Required(clusterRefPath, "clusterRef is required"))
		return
	}
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	clusterKey := types.NamespacedName{Name: user.Spec.ClusterRef, Namespace: user.Namespace}
	if err := v.client.Get(ctx, clusterKey, cluster); err == nil {
		return
	} else if !errors.IsNotFound(err) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("could not verify clusterRef %q: %v", user.Spec.ClusterRef, err))
		return
	}

	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	if err := v.client.Get(ctx, clusterKey, standalone); err == nil {
		return
	} else if !errors.IsNotFound(err) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("could not verify clusterRef %q: %v", user.Spec.ClusterRef, err))
		return
	}
	result.Errors = append(result.Errors, field.NotFound(clusterRefPath,
		fmt.Sprintf("no Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone named %q in namespace %q", user.Spec.ClusterRef, user.Namespace)))
}

func validateUsername(name string, path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if name == "" {
		errs = append(errs, field.Required(path, "username is required"))
		return errs
	}
	if len(name) > maxNeo4jUsernameLength {
		errs = append(errs, field.Invalid(path, name,
			fmt.Sprintf("must be no more than %d characters", maxNeo4jUsernameLength)))
	}
	if !neo4jUsernamePattern.MatchString(name) {
		errs = append(errs, field.Invalid(path, name,
			"must start with an ASCII letter and contain only letters, digits, underscore, dot or hyphen"))
	}
	return errs
}
