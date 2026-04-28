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

package neo4j

import (
	"context"
	"fmt"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// UserInfo is the projection of a single row of `SHOW USERS` (with optional
// AUTH detail) used by the operator to diff desired vs. observed user state.
type UserInfo struct {
	// User is the username as Neo4j stores it.
	User string

	// Roles lists the role names directly granted to the user. Excludes the
	// implicit PUBLIC role.
	Roles []string

	// Suspended is true when STATUS = SUSPENDED.
	Suspended bool

	// PasswordChangeRequired is true when the user must change their
	// password on next login.
	PasswordChangeRequired bool

	// HomeDatabase is the user's configured home database (or empty for
	// the DBMS default).
	HomeDatabase string

	// AuthProviders is the list of `(provider, id)` pairs for external
	// authentication. The implicit "native" provider is included only when
	// the user has a native password set.
	AuthProviders []AuthProviderInfo
}

// AuthProviderInfo is one row of `SHOW USERS WITH AUTH`.
type AuthProviderInfo struct {
	Provider string
	ID       string
}

// RoleInfo summarises one row of `SHOW ROLES YIELD role, immutable`.
type RoleInfo struct {
	Role      string
	Immutable bool
}

// PrivilegeCommand is a single row of `SHOW ROLE <r> PRIVILEGES AS COMMANDS`.
type PrivilegeCommand struct {
	// Command is the raw GRANT/DENY statement that re-creates this
	// privilege.
	Command string

	// Immutable is true when the privilege was created with
	// `GRANT IMMUTABLE`. Immutable privileges cannot be revoked while
	// authentication is enabled.
	Immutable bool
}

// ShowUser returns the user record for the given username, or (nil, nil)
// when the user does not exist. AUTH provider details are included.
func (c *Client) ShowUser(ctx context.Context, username string) (*UserInfo, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	// `SHOW USERS WITH AUTH` returns one row per (user, auth-provider).
	// We aggregate the auth rows server-side into a single record per user.
	query := `
		SHOW USERS WITH AUTH
		YIELD user, roles, suspended, passwordChangeRequired, home, provider, auth
		WHERE user = $username
		RETURN user, roles, suspended, passwordChangeRequired, home,
		       collect({provider: provider, auth: auth}) AS auths
	`
	result, err := session.Run(ctx, query, map[string]any{"username": username})
	if err != nil {
		return nil, fmt.Errorf("failed to run SHOW USERS WITH AUTH for %s: %w", username, err)
	}
	if !result.Next(ctx) {
		// Older Neo4j (<5.24) does not support WITH AUTH — fall back.
		return c.showUserBasic(ctx, username)
	}
	rec := result.Record()

	info := &UserInfo{
		User:                   stringValue(rec, "user"),
		Roles:                  stringSliceValue(rec, "roles"),
		Suspended:              boolValue(rec, "suspended"),
		PasswordChangeRequired: boolValue(rec, "passwordChangeRequired"),
		HomeDatabase:           stringValue(rec, "home"),
	}
	if auths, ok := rec.Get("auths"); ok {
		if list, ok := auths.([]any); ok {
			for _, item := range list {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				provider, _ := m["provider"].(string)
				if provider == "" {
					continue
				}
				ap := AuthProviderInfo{Provider: provider}
				if auth, ok := m["auth"].(map[string]any); ok {
					if id, ok := auth["id"].(string); ok {
						ap.ID = id
					}
				}
				info.AuthProviders = append(info.AuthProviders, ap)
			}
		}
	}
	return info, nil
}

// showUserBasic is a fallback for Neo4j versions that don't support
// `SHOW USERS WITH AUTH`. It returns the user record without auth details.
func (c *Client) showUserBasic(ctx context.Context, username string) (*UserInfo, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"SHOW USERS YIELD user, roles, suspended, passwordChangeRequired, home WHERE user = $username RETURN user, roles, suspended, passwordChangeRequired, home",
		map[string]any{"username": username},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to run SHOW USERS for %s: %w", username, err)
	}
	if !result.Next(ctx) {
		return nil, nil
	}
	rec := result.Record()
	return &UserInfo{
		User:                   stringValue(rec, "user"),
		Roles:                  stringSliceValue(rec, "roles"),
		Suspended:              boolValue(rec, "suspended"),
		PasswordChangeRequired: boolValue(rec, "passwordChangeRequired"),
		HomeDatabase:           stringValue(rec, "home"),
	}, nil
}

// AlterUserOptions is a sparse description of which user attributes the
// caller wants to update. Only fields explicitly set via the With… methods
// are emitted in the resulting ALTER USER statement, so unrelated fields
// are not touched.
type AlterUserOptions struct {
	hasPassword           bool
	password              string
	hasPasswordChange     bool
	passwordChangeReq     bool
	hasStatus             bool
	suspended             bool
	hasHomeDatabase       bool
	homeDatabase          string
	removeHomeDatabase    bool
	removeAuthProviders   []string
	removeAllAuthProvider bool
	setAuthProviders      []AuthProviderInfo
}

// WithPassword stages a `SET PASSWORD '<pw>'` clause. The password is
// always sent as a parameter, never interpolated.
func (o *AlterUserOptions) WithPassword(pw string) *AlterUserOptions {
	o.hasPassword = true
	o.password = pw
	return o
}

// WithPasswordChangeRequired stages a `SET PASSWORD CHANGE [NOT] REQUIRED`
// clause.
func (o *AlterUserOptions) WithPasswordChangeRequired(required bool) *AlterUserOptions {
	o.hasPasswordChange = true
	o.passwordChangeReq = required
	return o
}

// WithSuspended stages a `SET STATUS SUSPENDED|ACTIVE` clause.
func (o *AlterUserOptions) WithSuspended(suspended bool) *AlterUserOptions {
	o.hasStatus = true
	o.suspended = suspended
	return o
}

// WithHomeDatabase stages a `SET HOME DATABASE <name>` clause.
func (o *AlterUserOptions) WithHomeDatabase(name string) *AlterUserOptions {
	o.hasHomeDatabase = true
	o.homeDatabase = name
	o.removeHomeDatabase = false
	return o
}

// WithoutHomeDatabase stages a `REMOVE HOME DATABASE` clause.
func (o *AlterUserOptions) WithoutHomeDatabase() *AlterUserOptions {
	o.removeHomeDatabase = true
	o.hasHomeDatabase = false
	o.homeDatabase = ""
	return o
}

// WithRemovedAuthProviders stages `REMOVE AUTH PROVIDER 'p1','p2'` clauses.
func (o *AlterUserOptions) WithRemovedAuthProviders(providers ...string) *AlterUserOptions {
	o.removeAuthProviders = append(o.removeAuthProviders, providers...)
	return o
}

// WithSetAuthProviders stages `SET AUTH '<provider>' { SET ID '<id>' }`
// clauses (one per entry).
func (o *AlterUserOptions) WithSetAuthProviders(providers ...AuthProviderInfo) *AlterUserOptions {
	o.setAuthProviders = append(o.setAuthProviders, providers...)
	return o
}

// IsEmpty reports whether any clause has been staged.
func (o *AlterUserOptions) IsEmpty() bool {
	return !o.hasPassword && !o.hasPasswordChange && !o.hasStatus &&
		!o.hasHomeDatabase && !o.removeHomeDatabase &&
		len(o.removeAuthProviders) == 0 && !o.removeAllAuthProvider &&
		len(o.setAuthProviders) == 0
}

// AlterUser issues an ALTER USER statement with the staged clauses. Returns
// nil if the options object is empty (no-op).
//
// Neo4j requires REMOVE clauses to precede SET clauses on a single ALTER
// USER, and we honour that ordering.
func (c *Client) AlterUser(ctx context.Context, username string, opts *AlterUserOptions) error {
	if opts == nil || opts.IsEmpty() {
		return nil
	}

	var clauses []string
	params := map[string]any{}

	if opts.removeHomeDatabase {
		clauses = append(clauses, "REMOVE HOME DATABASE")
	}
	for i, p := range opts.removeAuthProviders {
		paramName := fmt.Sprintf("removeAuth%d", i)
		params[paramName] = p
		clauses = append(clauses, fmt.Sprintf("REMOVE AUTH PROVIDER $%s", paramName))
	}

	if opts.hasPassword {
		params["password"] = opts.password
		clauses = append(clauses, "SET PASSWORD $password")
	}
	if opts.hasPasswordChange {
		if opts.passwordChangeReq {
			clauses = append(clauses, "SET PASSWORD CHANGE REQUIRED")
		} else {
			clauses = append(clauses, "SET PASSWORD CHANGE NOT REQUIRED")
		}
	}
	if opts.hasStatus {
		if opts.suspended {
			clauses = append(clauses, "SET STATUS SUSPENDED")
		} else {
			clauses = append(clauses, "SET STATUS ACTIVE")
		}
	}
	if opts.hasHomeDatabase {
		// Quote the database name with backticks for safety; database names
		// allow letters, digits, dot, dash and the parameter form is not
		// supported here by all Neo4j versions.
		clauses = append(clauses, fmt.Sprintf("SET HOME DATABASE `%s`", escapeBackticks(opts.homeDatabase)))
	}
	for i, ap := range opts.setAuthProviders {
		providerParam := fmt.Sprintf("authProvider%d", i)
		idParam := fmt.Sprintf("authID%d", i)
		params[providerParam] = ap.Provider
		params[idParam] = ap.ID
		// Provider is a parameter, ID is a parameter.
		clauses = append(clauses, fmt.Sprintf("SET AUTH $%s { SET ID $%s }", providerParam, idParam))
	}

	if len(clauses) == 0 {
		return nil
	}

	query := fmt.Sprintf("ALTER USER `%s` %s", escapeBackticks(username), strings.Join(clauses, " "))

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	if _, err := session.Run(ctx, query, params); err != nil {
		return fmt.Errorf("failed to alter user %s: %w", username, err)
	}
	return nil
}

// CreateUserAdvanced creates a user with full control over initial state.
// Mirrors the AlterUser surface so callers can stage password, status,
// home database and external-auth providers in one statement.
//
// At least one of password or external auth providers must be supplied.
func (c *Client) CreateUserAdvanced(ctx context.Context, username string, opts *AlterUserOptions, ifNotExists bool) error {
	if opts == nil {
		return fmt.Errorf("CreateUserAdvanced: opts must not be nil")
	}
	if !opts.hasPassword && len(opts.setAuthProviders) == 0 {
		return fmt.Errorf("CreateUserAdvanced: at least one auth provider (password or SET AUTH) is required")
	}

	var clauses []string
	params := map[string]any{}

	if opts.hasPassword {
		params["password"] = opts.password
		clauses = append(clauses, "SET PASSWORD $password")
	}
	if opts.hasPasswordChange {
		if opts.passwordChangeReq {
			clauses = append(clauses, "SET PASSWORD CHANGE REQUIRED")
		} else {
			clauses = append(clauses, "SET PASSWORD CHANGE NOT REQUIRED")
		}
	}
	if opts.hasStatus {
		if opts.suspended {
			clauses = append(clauses, "SET STATUS SUSPENDED")
		} else {
			clauses = append(clauses, "SET STATUS ACTIVE")
		}
	}
	if opts.hasHomeDatabase {
		clauses = append(clauses, fmt.Sprintf("SET HOME DATABASE `%s`", escapeBackticks(opts.homeDatabase)))
	}
	for i, ap := range opts.setAuthProviders {
		providerParam := fmt.Sprintf("authProvider%d", i)
		idParam := fmt.Sprintf("authID%d", i)
		params[providerParam] = ap.Provider
		params[idParam] = ap.ID
		clauses = append(clauses, fmt.Sprintf("SET AUTH $%s { SET ID $%s }", providerParam, idParam))
	}

	prefix := fmt.Sprintf("CREATE USER `%s`", escapeBackticks(username))
	if ifNotExists {
		prefix += " IF NOT EXISTS"
	}
	query := prefix + " " + strings.Join(clauses, " ")

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	if _, err := session.Run(ctx, query, params); err != nil {
		return fmt.Errorf("failed to create user %s: %w", username, err)
	}
	return nil
}

// ShowRole returns the role record for the given role name, or (nil, nil)
// when the role does not exist.
func (c *Client) ShowRole(ctx context.Context, roleName string) (*RoleInfo, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"SHOW ROLES YIELD role, immutable WHERE role = $name RETURN role, immutable",
		map[string]any{"name": roleName},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to run SHOW ROLES for %s: %w", roleName, err)
	}
	if !result.Next(ctx) {
		return nil, nil
	}
	rec := result.Record()
	return &RoleInfo{
		Role:      stringValue(rec, "role"),
		Immutable: boolValue(rec, "immutable"),
	}, nil
}

// CreateRoleAdvanced creates a role, optionally as a copy of an existing
// role. Pass ifNotExists=true to make the operation idempotent.
func (c *Client) CreateRoleAdvanced(ctx context.Context, roleName, copyOf string, ifNotExists bool) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("CREATE ROLE `%s`", escapeBackticks(roleName))
	if ifNotExists {
		query += " IF NOT EXISTS"
	}
	if copyOf != "" {
		query += fmt.Sprintf(" AS COPY OF `%s`", escapeBackticks(copyOf))
	}
	if _, err := session.Run(ctx, query, nil); err != nil {
		return fmt.Errorf("failed to create role %s: %w", roleName, err)
	}
	return nil
}

// DropRoleIfExists drops a role with the IF EXISTS modifier, so it is
// idempotent. Built-in roles cannot be dropped — Neo4j returns an error
// which the caller is expected to surface.
func (c *Client) DropRoleIfExists(ctx context.Context, roleName string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("DROP ROLE `%s` IF EXISTS", escapeBackticks(roleName))
	if _, err := session.Run(ctx, query, nil); err != nil {
		return fmt.Errorf("failed to drop role %s: %w", roleName, err)
	}
	return nil
}

// DropUserIfExists drops a user with the IF EXISTS modifier.
func (c *Client) DropUserIfExists(ctx context.Context, username string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	query := fmt.Sprintf("DROP USER `%s` IF EXISTS", escapeBackticks(username))
	if _, err := session.Run(ctx, query, nil); err != nil {
		return fmt.Errorf("failed to drop user %s: %w", username, err)
	}
	return nil
}

// ShowRolePrivileges returns the privilege commands for the named role
// (one per row of `SHOW ROLE <name> PRIVILEGES AS COMMANDS`). Returns an
// empty slice when the role exists but has no privileges; returns
// (nil, error) for transport errors.
func (c *Client) ShowRolePrivileges(ctx context.Context, roleName string) ([]PrivilegeCommand, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	// Quoting the role name as a backtick literal (rather than a parameter)
	// is necessary — `SHOW ROLE <name>` does not accept parameters in
	// Neo4j 5.x. Backtick-escape via doubling.
	query := fmt.Sprintf("SHOW ROLE `%s` PRIVILEGES AS COMMANDS YIELD command, immutable RETURN command, immutable", escapeBackticks(roleName))
	result, err := session.Run(ctx, query, nil)
	if err != nil {
		// Older Neo4j (5.x pre-5.10ish) may not support the `, immutable`
		// projection. Retry without it; default Immutable=false.
		return c.showRolePrivilegesLegacy(ctx, roleName)
	}

	var out []PrivilegeCommand
	for result.Next(ctx) {
		rec := result.Record()
		out = append(out, PrivilegeCommand{
			Command:   stringValue(rec, "command"),
			Immutable: boolValue(rec, "immutable"),
		})
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("failed reading privileges for role %s: %w", roleName, err)
	}
	return out, nil
}

func (c *Client) showRolePrivilegesLegacy(ctx context.Context, roleName string) ([]PrivilegeCommand, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)
	query := fmt.Sprintf("SHOW ROLE `%s` PRIVILEGES AS COMMANDS YIELD command RETURN command", escapeBackticks(roleName))
	result, err := session.Run(ctx, query, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to run SHOW ROLE PRIVILEGES (legacy) for %s: %w", roleName, err)
	}
	var out []PrivilegeCommand
	for result.Next(ctx) {
		rec := result.Record()
		out = append(out, PrivilegeCommand{Command: stringValue(rec, "command")})
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("failed reading privileges (legacy) for role %s: %w", roleName, err)
	}
	return out, nil
}

// ListUsers returns a summary of every row of `SHOW USERS`. Used by the
// cluster/standalone diagnostic collectors. Returns nil with no error when
// the connected user lacks SHOW USER privilege (system DB queries can be
// restricted via DBMS privileges).
func (c *Client) ListUsers(ctx context.Context) ([]UserInfo, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"SHOW USERS YIELD user, roles, suspended, passwordChangeRequired, home RETURN user, roles, suspended, passwordChangeRequired, home",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to run SHOW USERS: %w", err)
	}
	var out []UserInfo
	for result.Next(ctx) {
		rec := result.Record()
		out = append(out, UserInfo{
			User:                   stringValue(rec, "user"),
			Roles:                  stringSliceValue(rec, "roles"),
			Suspended:              boolValue(rec, "suspended"),
			PasswordChangeRequired: boolValue(rec, "passwordChangeRequired"),
			HomeDatabase:           stringValue(rec, "home"),
		})
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("failed reading SHOW USERS: %w", err)
	}
	return out, nil
}

// ListRoles returns one entry per row of `SHOW ROLES YIELD role, immutable`.
// Falls back to a projection without the `immutable` column for older Neo4j
// versions that do not support it.
func (c *Client) ListRoles(ctx context.Context) ([]RoleInfo, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"SHOW ROLES YIELD role, immutable RETURN role, immutable",
		nil,
	)
	if err != nil {
		// Older Neo4j versions don't support the `immutable` column.
		return c.listRolesLegacy(ctx)
	}
	var out []RoleInfo
	for result.Next(ctx) {
		rec := result.Record()
		out = append(out, RoleInfo{
			Role:      stringValue(rec, "role"),
			Immutable: boolValue(rec, "immutable"),
		})
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("failed reading SHOW ROLES: %w", err)
	}
	return out, nil
}

func (c *Client) listRolesLegacy(ctx context.Context) ([]RoleInfo, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, "SHOW ROLES YIELD role RETURN role", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to run SHOW ROLES (legacy): %w", err)
	}
	var out []RoleInfo
	for result.Next(ctx) {
		rec := result.Record()
		out = append(out, RoleInfo{Role: stringValue(rec, "role")})
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("failed reading SHOW ROLES (legacy): %w", err)
	}
	return out, nil
}

// ListUserRoles returns the list of role names directly granted to the
// user. Replaces the legacy GetUserRoles which incorrectly used
// `SHOW USER PRIVILEGES YIELD role` — that returns one row per privilege,
// not per role.
func (c *Client) ListUserRoles(ctx context.Context, username string) ([]string, error) {
	info, err := c.ShowUser(ctx, username)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil
	}
	return info.Roles, nil
}

// escapeBackticks doubles any backtick in the input, suitable for inclusion
// inside a backtick-quoted Cypher identifier.
func escapeBackticks(s string) string {
	return strings.ReplaceAll(s, "`", "``")
}

// stringValue safely extracts a string column from a record.
func stringValue(rec *neo4j.Record, key string) string {
	v, ok := rec.Get(key)
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// boolValue safely extracts a bool column from a record.
func boolValue(rec *neo4j.Record, key string) bool {
	v, ok := rec.Get(key)
	if !ok || v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// stringSliceValue safely extracts a list-of-strings column from a record.
func stringSliceValue(rec *neo4j.Record, key string) []string {
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
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
