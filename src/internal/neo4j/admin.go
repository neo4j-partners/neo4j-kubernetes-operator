package neo4j

import (
	"context"
	"fmt"
	"strings"
)

// Server is one row from SHOW SERVERS (system DB).
type Server struct {
	Name    string
	Address string
	State   string
}

// DatabaseTopology is the requested/current hosting topology for one database.
type DatabaseTopology struct {
	Name                 string
	Type                 string
	RequestedPrimaries   int64
	RequestedSecondaries int64
	CurrentPrimaries     int64
	CurrentSecondaries   int64
	HasTopology          bool // false for composite / null counts
}

// Admin is the allowlisted Neo4j cluster-admin surface (ADR-007).
type Admin interface {
	ShowServers(ctx context.Context) ([]Server, error)
	ShowDatabaseTopologies(ctx context.Context) ([]DatabaseTopology, error)
	SetDatabaseTopology(ctx context.Context, name string, primaries, secondaries int64) error
	EnableServer(ctx context.Context, name, modeConstraint string) error
	DeallocateDatabases(ctx context.Context, name string) error
	DropServer(ctx context.Context, name string) error
	Close(ctx context.Context) error
}

// FindByAddress returns the server whose address matches (with or without :7687).
func FindByAddress(servers []Server, address string) (Server, bool) {
	want := normalizeAddress(address)
	for _, s := range servers {
		if normalizeAddress(s.Address) == want {
			return s, true
		}
	}
	return Server{}, false
}

// FindActiveByAddress prefers Enabled/Free; ignores Dropped/Deallocated identities
// left over from a prior scale-in (those cannot be ENABLE'd again).
func FindActiveByAddress(servers []Server, address string) (Server, bool) {
	want := normalizeAddress(address)
	for _, s := range servers {
		if normalizeAddress(s.Address) != want {
			continue
		}
		if IsEnabled(s.State) || IsFree(s.State) {
			return s, true
		}
	}
	return Server{}, false
}

// IsTerminalRemoval is true when the server can never be ENABLE'd again.
func IsTerminalRemoval(state string) bool {
	return IsDropped(state) || IsDeallocated(state)
}

func normalizeAddress(a string) string {
	a = strings.TrimSpace(strings.ToLower(a))
	a = strings.TrimSuffix(a, ":7687")
	return a
}

// State helpers (Neo4j SHOW SERVERS.state).
func IsEnabled(state string) bool {
	return strings.EqualFold(strings.TrimSpace(state), "Enabled")
}

func IsFree(state string) bool {
	return strings.EqualFold(strings.TrimSpace(state), "Free")
}

func IsDeallocated(state string) bool {
	return strings.EqualFold(strings.TrimSpace(state), "Deallocated")
}

func IsDropped(state string) bool {
	return strings.EqualFold(strings.TrimSpace(state), "Dropped")
}

func IsDeallocating(state string) bool {
	return strings.EqualFold(strings.TrimSpace(state), "Deallocating")
}

// ParseAuthSecret reads NEO4J_AUTH value "neo4j/<password>".
func ParseAuthSecret(neo4jAuth string) (user, password string, err error) {
	user, password, ok := strings.Cut(strings.TrimSpace(neo4jAuth), "/")
	if !ok || user == "" || password == "" {
		return "", "", fmt.Errorf("NEO4J_AUTH must be user/password")
	}
	return user, password, nil
}
