/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

// Unit tests for the #173 scale-down drain detection (PR1: honest status). The
// detection is pure over a SHOW SERVERS result + desired server count, so it's
// verified without a live cluster.

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	neo4jclient "github.com/neo4j-partners/neo4j-kubernetes-operator/internal/neo4j"
)

func TestParseServerOrdinal(t *testing.T) {
	cases := []struct {
		in      string
		cluster string
		want    int
		ok      bool
	}{
		{"my-cluster-server-2.my-cluster-headless.ns.svc.cluster.local:7687", "my-cluster", 2, true},
		{"my-cluster-server-0:6000", "my-cluster", 0, true},
		{"my-cluster-server-11", "my-cluster", 11, true},
		{"10.0.0.5:7687", "my-cluster", 0, false},       // bare IP — no match
		{"other-server-3", "my-cluster", 0, false},      // wrong cluster prefix
		{"my-cluster-server-", "my-cluster", 0, false},  // no digits
		{"my-cluster-server-x", "my-cluster", 0, false}, // non-numeric
	}
	for _, c := range cases {
		got, ok := parseServerOrdinal(c.in, c.cluster)
		assert.Equal(t, c.ok, ok, "ok mismatch for %q", c.in)
		if c.ok {
			assert.Equal(t, c.want, got, "ordinal mismatch for %q", c.in)
		}
	}
}

func srv(addr, state string) neo4jclient.ServerInfo {
	return neo4jclient.ServerInfo{Name: addr, Address: addr, State: state, Health: "Available"}
}

func TestServersPendingDrain(t *testing.T) {
	cluster := "c"
	addr := func(n int) string { return "c-server-" + strconv.Itoa(n) + ".c-headless.ns.svc.cluster.local:7687" }

	t.Run("no scale-down: all ordinals < desired", func(t *testing.T) {
		servers := []neo4jclient.ServerInfo{srv(addr(0), "Enabled"), srv(addr(1), "Enabled"), srv(addr(2), "Enabled")}
		assert.Empty(t, serversPendingDrain(servers, cluster, 3))
	})

	t.Run("5->3 scale-down: ordinals 3,4 still Enabled are pending", func(t *testing.T) {
		servers := []neo4jclient.ServerInfo{
			srv(addr(0), "Enabled"), srv(addr(1), "Enabled"), srv(addr(2), "Enabled"),
			srv(addr(3), "Enabled"), srv(addr(4), "Enabled"),
		}
		pending := serversPendingDrain(servers, cluster, 3)
		assert.ElementsMatch(t, []string{addr(3), addr(4)}, pending)
	})

	t.Run("servers already Dropped/Deallocated are excluded", func(t *testing.T) {
		servers := []neo4jclient.ServerInfo{
			srv(addr(0), "Enabled"), srv(addr(1), "Enabled"), srv(addr(2), "Enabled"),
			srv(addr(3), "Dropped"), srv(addr(4), "Deallocated"),
		}
		assert.Empty(t, serversPendingDrain(servers, cluster, 3), "terminal-state servers are being handled, not pending")
	})

	t.Run("unmatched addresses are ignored", func(t *testing.T) {
		servers := []neo4jclient.ServerInfo{srv("10.0.0.9:7687", "Enabled"), srv(addr(3), "Enabled")}
		assert.Equal(t, []string{addr(3)}, serversPendingDrain(servers, cluster, 3))
	})

	t.Run("reports serverId (not name/address) — name often empty", func(t *testing.T) {
		// Matches the real shape: ordinal lives in the address, the reportable
		// identifier is the serverId, and name is blank.
		servers := []neo4jclient.ServerInfo{
			{ID: "uuid-3", Name: "", Address: addr(3), State: "Enabled"},
		}
		assert.Equal(t, []string{"uuid-3"}, serversPendingDrain(servers, cluster, 3),
			"DEALLOCATE/DROP SERVER take the serverId, not the bolt address")
	})
}
