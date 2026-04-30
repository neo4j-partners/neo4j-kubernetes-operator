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

package neo4j

import (
	"context"
	"fmt"
	"sort"
	"strings"

	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// AUTH RULE syntax is only parsed under Cypher 25. Neo4j 2026.x defaults the
// system database to Cypher 5 unless explicitly opted in, so every AUTH RULE
// statement we issue must be prefixed. Prepending the language directive is
// safe even when the database default is already 25.
const cypher25Prefix = "CYPHER 25 "

// AuthRuleInfo is the projection of one row of `SHOW AUTH RULES`. Used by the
// Neo4jAuthRule controller to diff desired vs. observed state.
type AuthRuleInfo struct {
	Name      string
	Condition string
	Enabled   bool
	// Roles is the set of role names currently granted by this rule.
	// Sorted ascending for stable diff output.
	Roles []string
}

// ShowAuthRule returns the auth rule with the given name, or (nil, nil) when
// no such rule exists. Errors are returned for transport issues only.
//
// Requires Neo4j 2026.03 or later. Older clusters do not expose `SHOW AUTH
// RULES` and the call will return an error which the caller should treat as
// "ABAC unavailable".
func (c *Client) ShowAuthRule(ctx context.Context, ruleName string) (*AuthRuleInfo, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	// `SHOW AUTH RULES` returns one row per rule with columns name, condition,
	// enabled, and roles (a list of strings). Filtering by name keeps the
	// driver round-trip small even on large clusters.
	result, err := session.Run(ctx,
		cypher25Prefix+"SHOW AUTH RULES YIELD name, condition, enabled, roles WHERE name = $name "+
			"RETURN name, condition, enabled, roles",
		map[string]any{"name": ruleName},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query SHOW AUTH RULES for %q: %w", ruleName, err)
	}

	if !result.Next(ctx) {
		if err := result.Err(); err != nil {
			return nil, fmt.Errorf("failed to read SHOW AUTH RULES result: %w", err)
		}
		return nil, nil
	}

	rec := result.Record()
	info := &AuthRuleInfo{
		Name:      stringValue(rec, "name"),
		Condition: stringValue(rec, "condition"),
		Enabled:   boolValue(rec, "enabled"),
		Roles:     stringListValue(rec, "roles"),
	}
	sort.Strings(info.Roles)
	return info, nil
}

// ListAuthRules returns every row of `SHOW AUTH RULES`, used for diagnostics.
// Requires Neo4j 2026.03 or later.
func (c *Client) ListAuthRules(ctx context.Context) ([]AuthRuleInfo, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		cypher25Prefix+"SHOW AUTH RULES YIELD name, condition, enabled, roles "+
			"RETURN name, condition, enabled, roles ORDER BY name",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query SHOW AUTH RULES: %w", err)
	}

	var rules []AuthRuleInfo
	for result.Next(ctx) {
		rec := result.Record()
		rule := AuthRuleInfo{
			Name:      stringValue(rec, "name"),
			Condition: stringValue(rec, "condition"),
			Enabled:   boolValue(rec, "enabled"),
			Roles:     stringListValue(rec, "roles"),
		}
		sort.Strings(rule.Roles)
		rules = append(rules, rule)
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("failed to read SHOW AUTH RULES result: %w", err)
	}
	return rules, nil
}

// CreateOrReplaceAuthRule creates an AUTH RULE with the given condition and
// enabled flag, replacing any existing rule of the same name. Roles are NOT
// granted by this call — use GrantRolesToAuthRule afterwards.
//
// The condition string is interpolated directly into the Cypher; the caller
// MUST validate that it does not contain malicious DDL (the validator package
// performs this check before reconcile).
func (c *Client) CreateOrReplaceAuthRule(ctx context.Context, ruleName, condition string, enabled bool) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	enabledClause := "SET ENABLED true"
	if !enabled {
		enabledClause = "SET ENABLED false"
	}
	query := fmt.Sprintf(
		cypher25Prefix+"CREATE OR REPLACE AUTH RULE `%s` SET CONDITION %s %s",
		escapeBackticks(ruleName),
		condition,
		enabledClause,
	)
	if _, err := session.Run(ctx, query, nil); err != nil {
		return fmt.Errorf("failed to create or replace auth rule %s: %w", ruleName, err)
	}
	return nil
}

// AlterAuthRule updates the condition and/or enabled flag of an existing rule.
// At least one of setCondition or setEnabled must be non-nil; otherwise the
// call is a no-op and returns nil.
func (c *Client) AlterAuthRule(ctx context.Context, ruleName string, setCondition *string, setEnabled *bool) error {
	if setCondition == nil && setEnabled == nil {
		return nil
	}
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	var clauses []string
	if setCondition != nil {
		clauses = append(clauses, fmt.Sprintf("SET CONDITION %s", *setCondition))
	}
	if setEnabled != nil {
		if *setEnabled {
			clauses = append(clauses, "SET ENABLED true")
		} else {
			clauses = append(clauses, "SET ENABLED false")
		}
	}
	query := fmt.Sprintf(
		cypher25Prefix+"ALTER AUTH RULE `%s` %s",
		escapeBackticks(ruleName),
		strings.Join(clauses, " "),
	)
	if _, err := session.Run(ctx, query, nil); err != nil {
		return fmt.Errorf("failed to alter auth rule %s: %w", ruleName, err)
	}
	return nil
}

// DropAuthRuleIfExists drops the named auth rule with IF EXISTS, so the
// operation is idempotent.
func (c *Client) DropAuthRuleIfExists(ctx context.Context, ruleName string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf(cypher25Prefix+"DROP AUTH RULE `%s` IF EXISTS", escapeBackticks(ruleName))
	if _, err := session.Run(ctx, query, nil); err != nil {
		return fmt.Errorf("failed to drop auth rule %s: %w", ruleName, err)
	}
	return nil
}

// GrantRolesToAuthRule executes `GRANT ROLE r1, r2, ... TO AUTH RULE name` for
// every role in the input. Returns nil when the input is empty.
func (c *Client) GrantRolesToAuthRule(ctx context.Context, ruleName string, roles []string) error {
	if len(roles) == 0 {
		return nil
	}
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	roleList := joinBacktickedIdentifiers(roles)
	query := fmt.Sprintf(
		cypher25Prefix+"GRANT ROLES %s TO AUTH RULE `%s`",
		roleList,
		escapeBackticks(ruleName),
	)
	if _, err := session.Run(ctx, query, nil); err != nil {
		return fmt.Errorf("failed to grant roles to auth rule %s: %w", ruleName, err)
	}
	return nil
}

// RevokeRolesFromAuthRule executes `REVOKE ROLE r1, r2, ... FROM AUTH RULE
// name`. Returns nil when the input is empty.
func (c *Client) RevokeRolesFromAuthRule(ctx context.Context, ruleName string, roles []string) error {
	if len(roles) == 0 {
		return nil
	}
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	roleList := joinBacktickedIdentifiers(roles)
	query := fmt.Sprintf(
		cypher25Prefix+"REVOKE ROLES %s FROM AUTH RULE `%s`",
		roleList,
		escapeBackticks(ruleName),
	)
	if _, err := session.Run(ctx, query, nil); err != nil {
		return fmt.Errorf("failed to revoke roles from auth rule %s: %w", ruleName, err)
	}
	return nil
}

// joinBacktickedIdentifiers returns "`r1`, `r2`, …" with each role-name
// backtick-escaped. The input must be non-empty.
func joinBacktickedIdentifiers(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("`%s`", escapeBackticks(n))
	}
	return strings.Join(parts, ", ")
}

// stringListValue safely extracts a list-of-string column from a record. Nil
// inputs and non-list types both yield a nil slice.
func stringListValue(rec *neo4j.Record, key string) []string {
	v, ok := rec.Get(key)
	if !ok || v == nil {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
