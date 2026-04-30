/*
Copyright 2026.

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
)

// AuthRuleValidator validates Neo4jAuthRule resources.
type AuthRuleValidator struct {
	client client.Client
}

// NewAuthRuleValidator constructs an AuthRuleValidator backed by a
// controller-runtime client (used to resolve clusterRef references).
func NewAuthRuleValidator(c client.Client) *AuthRuleValidator {
	return &AuthRuleValidator{client: c}
}

// AuthRuleValidationResult collects errors and non-fatal warnings.
type AuthRuleValidationResult struct {
	Errors   field.ErrorList
	Warnings []string
}

// authRuleNamePattern enforces the same identifier shape as the CRD: ASCII
// letter followed by letters, digits, underscore, or hyphen.
var authRuleNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

// disallowedConditionKeywords are Cypher DDL keywords that must never appear
// in an AUTH RULE condition. The Neo4j parser also rejects them, but a
// controller-side check produces a clearer error and prevents a single
// malformed CR from emitting noisy CREATE OR REPLACE failures on every
// reconcile.
//
// Matching is whitespace-bounded and case-insensitive.
var disallowedConditionKeywords = []string{
	"CREATE",
	"DROP",
	"ALTER",
	"GRANT",
	"DENY",
	"REVOKE",
	"SHOW",
	"RENAME",
	// Multi-statement attempts: a semicolon would let a malicious CR sneak in
	// extra statements after the SET CONDITION clause.
	";",
}

// Validate runs all checks on a Neo4jAuthRule spec.
func (v *AuthRuleValidator) Validate(ctx context.Context, rule *neo4jv1beta1.Neo4jAuthRule) *AuthRuleValidationResult {
	result := &AuthRuleValidationResult{}

	ruleName := rule.Spec.Name
	if ruleName == "" {
		ruleName = rule.Name
	}

	// Name shape — duplicates the CRD-level Pattern check so we can also
	// emit an explicit error when controllers run validation programmatically
	// (e.g. via webhook simulators or unit tests).
	if ruleName == "" {
		result.Errors = append(result.Errors, field.Required(
			field.NewPath("spec", "name"),
			"auth rule name is required",
		))
	} else if !authRuleNamePattern.MatchString(ruleName) {
		result.Errors = append(result.Errors, field.Invalid(
			field.NewPath("spec", "name"),
			ruleName,
			"must start with an ASCII letter and contain only letters, digits, underscores, or hyphens",
		))
	} else if len(ruleName) > 65 {
		result.Errors = append(result.Errors, field.TooLong(
			field.NewPath("spec", "name"),
			ruleName,
			65,
		))
	}

	// Condition presence + DDL-injection guard.
	cond := strings.TrimSpace(rule.Spec.Condition)
	if cond == "" {
		result.Errors = append(result.Errors, field.Required(
			field.NewPath("spec", "condition"),
			"condition is required",
		))
	} else {
		condUpper := strings.ToUpper(cond)
		for _, kw := range disallowedConditionKeywords {
			if !containsBoundedKeyword(condUpper, kw) {
				continue
			}
			result.Errors = append(result.Errors, field.Invalid(
				field.NewPath("spec", "condition"),
				rule.Spec.Condition,
				fmt.Sprintf("condition must be a pure expression; %q is not allowed (auth rule conditions cannot contain DDL or multiple statements)", kw),
			))
		}
	}

	// GrantedRoles min items + role-name shape (defensive — the CRD already
	// enforces minItems=1 but we include the empty-slice case in the error
	// path so unit tests don't have to round-trip through admission).
	if len(rule.Spec.GrantedRoles) == 0 {
		result.Errors = append(result.Errors, field.Required(
			field.NewPath("spec", "grantedRoles"),
			"at least one role must be granted",
		))
	}
	seen := make(map[string]struct{}, len(rule.Spec.GrantedRoles))
	for i, r := range rule.Spec.GrantedRoles {
		path := field.NewPath("spec", "grantedRoles").Index(i)
		trimmed := strings.TrimSpace(r)
		if trimmed == "" {
			result.Errors = append(result.Errors, field.Invalid(path, r, "role name must not be empty"))
			continue
		}
		if _, dup := seen[trimmed]; dup {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("spec.grantedRoles[%d] (%q) is listed more than once; duplicates have no effect", i, trimmed))
			continue
		}
		seen[trimmed] = struct{}{}
	}

	// Cluster resolution. Same pattern as Neo4jRole.
	v.validateClusterRef(ctx, rule, result)

	return result
}

func (v *AuthRuleValidator) validateClusterRef(ctx context.Context, rule *neo4jv1beta1.Neo4jAuthRule, result *AuthRuleValidationResult) {
	if v.client == nil {
		return
	}
	path := field.NewPath("spec", "clusterRef")
	if rule.Spec.ClusterRef == "" {
		result.Errors = append(result.Errors, field.Required(path, "clusterRef is required"))
		return
	}
	cluster := &neo4jv1beta1.Neo4jEnterpriseCluster{}
	err := v.client.Get(ctx, types.NamespacedName{Name: rule.Spec.ClusterRef, Namespace: rule.Namespace}, cluster)
	if err == nil {
		return
	}
	if !errors.IsNotFound(err) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("could not verify clusterRef %q: %v", rule.Spec.ClusterRef, err))
		return
	}
	standalone := &neo4jv1beta1.Neo4jEnterpriseStandalone{}
	if err := v.client.Get(ctx, types.NamespacedName{Name: rule.Spec.ClusterRef, Namespace: rule.Namespace}, standalone); err == nil {
		// Standalone is fine — auth rules work on any cluster type that
		// supports Neo4j 2026.03+. The reconciler enforces the version gate.
		return
	} else if !errors.IsNotFound(err) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("could not verify clusterRef %q: %v", rule.Spec.ClusterRef, err))
		return
	}
	result.Errors = append(result.Errors, field.NotFound(path,
		fmt.Sprintf("no Neo4jEnterpriseCluster or Neo4jEnterpriseStandalone named %q in namespace %q", rule.Spec.ClusterRef, rule.Namespace)))
}

// containsBoundedKeyword reports whether s contains kw bounded by a non-word
// character on each side (or by the start/end of the string). For multi-char
// keywords this is a token-level check; for single-char keywords like ";"
// it's an exact substring check.
func containsBoundedKeyword(s, kw string) bool {
	if len(kw) == 1 {
		return strings.Contains(s, kw)
	}
	idx := 0
	for {
		off := strings.Index(s[idx:], kw)
		if off < 0 {
			return false
		}
		start := idx + off
		end := start + len(kw)
		// Boundary check on each side.
		if (start == 0 || !isWordChar(rune(s[start-1]))) && (end == len(s) || !isWordChar(rune(s[end]))) {
			return true
		}
		idx = end
	}
}

func isWordChar(r rune) bool {
	switch {
	case r >= 'A' && r <= 'Z':
		return true
	case r >= 'a' && r <= 'z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '_':
		return true
	}
	return false
}
