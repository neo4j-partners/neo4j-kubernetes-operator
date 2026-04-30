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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	neo4jv1beta1 "github.com/priyolahiri/neo4j-kubernetes-operator/api/v1beta1"
	"github.com/priyolahiri/neo4j-kubernetes-operator/internal/neo4j"
)

// RoleValidator validates Neo4jRole resources.
type RoleValidator struct {
	client client.Client
}

// NewRoleValidator constructs a RoleValidator backed by a controller-runtime
// client (used to resolve clusterRef references).
func NewRoleValidator(c client.Client) *RoleValidator {
	return &RoleValidator{client: c}
}

// RoleValidationResult collects errors and non-fatal warnings.
type RoleValidationResult struct {
	Errors   field.ErrorList
	Warnings []string
}

// neo4jRoleNamePattern enforces Neo4j's role-name rules: ASCII letter
// followed by letters, digits or underscore.
var neo4jRoleNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// builtInRoles is the closed set of pre-defined Neo4j roles.
var builtInRoles = map[string]struct{}{
	"PUBLIC":    {},
	"reader":    {},
	"editor":    {},
	"publisher": {},
	"architect": {},
	"admin":     {},
}

// IsBuiltInRole reports whether name is a Neo4j built-in role (case-sensitive
// match for the lowercase forms; PUBLIC is case-sensitive uppercase).
func IsBuiltInRole(name string) bool {
	if _, ok := builtInRoles[name]; ok {
		return true
	}
	return false
}

// Validate runs all checks on a Neo4jRole spec.
func (v *RoleValidator) Validate(ctx context.Context, role *neo4jv1beta1.Neo4jRole) *RoleValidationResult {
	result := &RoleValidationResult{}

	roleName := role.Spec.Name
	if roleName == "" {
		roleName = role.Name
	}

	// Name shape
	if err := validateRoleName(roleName, field.NewPath("spec", "name")); err != nil {
		result.Errors = append(result.Errors, err...)
	}

	// Built-in adoption guard
	if IsBuiltInRole(roleName) && !role.Spec.AdoptBuiltin {
		result.Errors = append(result.Errors, field.Forbidden(
			field.NewPath("spec", "name"),
			fmt.Sprintf("%q is a Neo4j built-in role. Set spec.adoptBuiltin=true to manage its privileges. Built-in roles will never be dropped on CR delete.", roleName),
		))
	}

	// CopyOf only meaningful at create-time and not for built-ins.
	if role.Spec.CopyOf != "" {
		if role.Spec.AdoptBuiltin {
			result.Warnings = append(result.Warnings,
				"spec.copyOf is ignored when adoptBuiltin=true (built-in roles already exist)")
		}
		if errs := validateRoleName(role.Spec.CopyOf, field.NewPath("spec", "copyOf")); len(errs) > 0 {
			result.Errors = append(result.Errors, errs...)
		}
	}

	// Privilege statements
	for i, stmt := range role.Spec.Privileges {
		path := field.NewPath("spec", "privileges").Index(i)
		trimmed := strings.TrimSpace(stmt)
		if trimmed == "" {
			result.Errors = append(result.Errors, field.Invalid(path, stmt, "privilege statement must not be empty"))
			continue
		}

		verb := neo4j.PrivilegeStatementVerb(trimmed)
		if verb == "" {
			result.Errors = append(result.Errors, field.Invalid(path, stmt,
				"privilege statement must begin with GRANT or DENY"))
			continue
		}

		if !neo4j.PrivilegeStatementMatchesRole(trimmed, roleName) {
			result.Errors = append(result.Errors, field.Invalid(path, stmt,
				fmt.Sprintf("privilege statement must end with `TO %s` (the role being defined)", roleName)))
		}

		if strings.Contains(trimmed, ";") {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("spec.privileges[%d] contains a semicolon; only single statements are supported and the semicolon will be stripped", i))
		}
	}

	// clusterRef must resolve to a cluster or standalone in the same namespace.
	v.validateClusterRef(ctx, role, result)

	// PBAC sharded-database guard. Property-based access control (FOR pattern
	// WHERE …) is unsupported on sharded property databases per
	// https://neo4j.com/docs/operations-manual/current/authentication-authorization/property-based-access-control/.
	// Reject privileges naming a Neo4jShardedDatabase by name; warn for `ON GRAPH *`
	// which would silently no-op against any sharded DBs in scope.
	v.validatePBACSharded(ctx, role, result)

	return result
}

func (v *RoleValidator) validateClusterRef(ctx context.Context, role *neo4jv1beta1.Neo4jRole, result *RoleValidationResult) {
	if v.client == nil {
		return
	}
	clusterRefPath := field.NewPath("spec", "clusterRef")
	if role.Spec.ClusterRef == "" {
		result.Errors = append(result.Errors, field.Required(clusterRefPath, "clusterRef is required"))
		return
	}

	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	clusterKey := types.NamespacedName{Name: role.Spec.ClusterRef, Namespace: role.Namespace}
	if err := v.client.Get(ctx, clusterKey, cluster); err == nil {
		return
	} else if !errors.IsNotFound(err) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("could not verify clusterRef %q: %v", role.Spec.ClusterRef, err))
		return
	}

	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	if err := v.client.Get(ctx, clusterKey, standalone); err == nil {
		return
	} else if !errors.IsNotFound(err) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("could not verify clusterRef %q: %v", role.Spec.ClusterRef, err))
		return
	}
	result.Errors = append(result.Errors, field.NotFound(clusterRefPath,
		fmt.Sprintf("no Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone named %q in namespace %q", role.Spec.ClusterRef, role.Namespace)))
}

// validatePBACSharded rejects privileges that combine a `FOR pattern WHERE …`
// clause (PBAC) with a database-name token (after `ON GRAPH`) that resolves to
// a Neo4jShardedDatabase in the same namespace. Such a privilege would be
// silently ineffective at runtime.
//
// `ON GRAPH *` (any-graph) cannot be statically resolved to a single sharded
// DB, so we emit a warning rather than an error: the user may legitimately
// want this privilege to apply to non-sharded databases on the same cluster.
func (v *RoleValidator) validatePBACSharded(ctx context.Context, role *neo4jv1beta1.Neo4jRole, result *RoleValidationResult) {
	if v.client == nil {
		return
	}
	for i, stmt := range role.Spec.Privileges {
		canon := neo4j.CanonicalisePrivilegeStatement(stmt)
		if canon == "" {
			continue
		}
		// PBAC always uses a `FOR ` token between the body and `TO <role>`.
		// (FOR is upper-cased by the canonicaliser.)
		if !strings.Contains(canon, " FOR ") {
			continue
		}
		dbName := pbacDatabaseName(canon)
		if dbName == "" {
			continue
		}
		path := field.NewPath("spec", "privileges").Index(i)
		if dbName == "*" {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"spec.privileges[%d] uses property-based access control on `ON GRAPH *`; PBAC is silently ineffective against any Neo4jShardedDatabase in scope (https://neo4j.com/docs/operations-manual/current/authentication-authorization/property-based-access-control/).",
				i,
			))
			continue
		}
		// Look up a Neo4jShardedDatabase named dbName in the role's namespace.
		// If found AND it points at the same clusterRef, reject the privilege.
		shard := &neo4jv1beta1.Neo4jShardedDatabase{}
		err := v.client.Get(ctx, types.NamespacedName{Name: dbName, Namespace: role.Namespace}, shard)
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("could not check whether %q is a Neo4jShardedDatabase: %v", dbName, err))
			continue
		}
		if shard.Spec.ClusterRef != role.Spec.ClusterRef {
			// The sharded DB exists in this namespace but belongs to a
			// different cluster — not our concern.
			continue
		}
		result.Errors = append(result.Errors, field.Invalid(path, stmt,
			fmt.Sprintf("property-based access control is unsupported on sharded property databases; %q is a Neo4jShardedDatabase. See https://neo4j.com/docs/operations-manual/current/authentication-authorization/property-based-access-control/.", dbName),
		))
	}
}

// pbacDatabaseName extracts the graph name from `ON GRAPH <name> ...` in a
// canonicalised privilege. Returns "" if `ON GRAPH` is absent or the token
// after it is not a bare identifier (e.g. backtick-escaped names with embedded
// spaces are not supported here — they would be rare in practice and the
// PBAC sharded-DB check is best-effort).
func pbacDatabaseName(canon string) string {
	const marker = "ON GRAPH "
	idx := strings.Index(canon, marker)
	if idx < 0 {
		return ""
	}
	rest := canon[idx+len(marker):]
	// First whitespace-delimited token after `ON GRAPH `.
	end := strings.IndexAny(rest, " \t")
	if end < 0 {
		return strings.Trim(rest, "`")
	}
	return strings.Trim(rest[:end], "`")
}

func validateRoleName(name string, path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if name == "" {
		errs = append(errs, field.Required(path, "role name is required"))
		return errs
	}
	if !neo4jRoleNamePattern.MatchString(name) {
		errs = append(errs, field.Invalid(path, name,
			"must start with an ASCII letter and contain only letters, digits, or underscore"))
	}
	return errs
}
