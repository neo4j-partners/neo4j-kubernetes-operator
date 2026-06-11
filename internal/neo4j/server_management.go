/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package neo4j

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// serverIDPattern matches the Neo4j serverId UUID returned by SHOW SERVERS
// (e.g. "25a7efc7-d063-44b8-bdee-f23357f89f01"). DEALLOCATE / DROP SERVER are
// DDL commands that do NOT accept query parameters for the server identifier,
// so the id is interpolated into the statement. Restricting it to the UUID
// grammar keeps that interpolation injection-safe (defense-in-depth — the id
// always originates from SHOW SERVERS, never user input).
var serverIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validateServerIDs(serverIDs []string) error {
	if len(serverIDs) == 0 {
		return fmt.Errorf("no server ids provided")
	}
	for _, id := range serverIDs {
		if !serverIDPattern.MatchString(id) {
			return fmt.Errorf("refusing to use server id %q in a server-management command: not a Neo4j serverId UUID", id)
		}
	}
	return nil
}

// buildDeallocateStatement builds a `[DRYRUN] DEALLOCATE DATABASES FROM SERVER
// 'id'[, ...]` command. Server ids are validated as UUIDs before interpolation.
func buildDeallocateStatement(serverIDs []string, dryRun bool) (string, error) {
	if err := validateServerIDs(serverIDs); err != nil {
		return "", err
	}
	quoted := make([]string, len(serverIDs))
	for i, id := range serverIDs {
		quoted[i] = "'" + id + "'"
	}
	stmt := "DEALLOCATE DATABASES FROM SERVER " + strings.Join(quoted, ", ")
	if dryRun {
		stmt = "DRYRUN " + stmt
	}
	return stmt, nil
}

// buildDropServerStatement builds a `DROP SERVER 'id'` command.
func buildDropServerStatement(serverID string) (string, error) {
	if err := validateServerIDs([]string{serverID}); err != nil {
		return "", err
	}
	return "DROP SERVER '" + serverID + "'", nil
}

// CordonServer marks a server as cordoned so the allocator places no new
// databases on it and prefers it first when deallocating. Reversible. Run
// against the system database. Procedure form is stable across 5.26 LTS and
// CalVer (`dbms.cluster.cordonServer`); the id goes through a query parameter,
// so no escaping is needed.
func (c *Client) CordonServer(ctx context.Context, serverID string) error {
	if err := validateServerIDs([]string{serverID}); err != nil {
		return err
	}
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	_, err := session.Run(ctx, "CALL dbms.cluster.cordonServer($id)", map[string]any{"id": serverID})
	if err != nil {
		return fmt.Errorf("cordonServer(%s): %w", serverID, err)
	}
	return nil
}

// DeallocateServers reallocates every user database off the given servers onto
// the remaining cluster members (servers transition Enabled -> Deallocating ->
// Deallocated). With dryRun=true it only previews feasibility: the command
// fails (returning an error) if the remaining servers can't satisfy a database
// topology, a database is offline, or a sole primary would be stranded — so a
// nil error from a dry run means the real deallocation is safe to perform.
func (c *Client) DeallocateServers(ctx context.Context, serverIDs []string, dryRun bool) error {
	stmt, err := buildDeallocateStatement(serverIDs, dryRun)
	if err != nil {
		return err
	}
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, stmt, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", stmt, err)
	}
	// Drain the result so a server-side failure (e.g. infeasible topology on a
	// DRYRUN) surfaces as an error rather than being silently ignored.
	if _, err := result.Consume(ctx); err != nil {
		return fmt.Errorf("%s: %w", stmt, err)
	}
	return nil
}

// DropServer deregisters a server from the cluster. Only safe once SHOW SERVERS
// reports the server as Deallocated (hosting only the system database) —
// dropping a Deallocating server risks data loss. Run against system.
func (c *Client) DropServer(ctx context.Context, serverID string) error {
	stmt, err := buildDropServerStatement(serverID)
	if err != nil {
		return err
	}
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx, stmt, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", stmt, err)
	}
	if _, err := result.Consume(ctx); err != nil {
		return fmt.Errorf("%s: %w", stmt, err)
	}
	return nil
}

// MinimumSystemPrimaries returns dbms.cluster.minimum_initial_system_primaries_count
// — the floor on servers hosting the `system` database in primary mode. A
// cluster cannot be scaled (servers dropped) below this, so the operator uses it
// to refuse an infeasible scale-down BEFORE the irreversible deallocate (which
// would strand the server). Defaults to 3 (the Neo4j default) if the setting
// can't be read.
func (c *Client) MinimumSystemPrimaries(ctx context.Context) int {
	const def = 3
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeRead,
		DatabaseName: "system",
	})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"SHOW SETTINGS YIELD name, value WHERE name = 'dbms.cluster.minimum_initial_system_primaries_count' RETURN value",
		nil)
	if err != nil {
		return def
	}
	rec, err := result.Single(ctx)
	if err != nil {
		return def
	}
	v, ok := rec.Get("value")
	if !ok {
		return def
	}
	if s, ok := v.(string); ok {
		if n, perr := strconv.Atoi(strings.TrimSpace(s)); perr == nil && n > 0 {
			return n
		}
	}
	return def
}
