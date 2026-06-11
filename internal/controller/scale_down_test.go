/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controller

// Unit tests for the #173 scale-down drain decision layer. These are pure over
// a SHOW SERVERS result + desired server count, so they're verified without a
// live cluster. (The live procedure calls + replica gating are exercised on Kind
// — that's where the "address goes NULL during Deallocating" id-tracking
// requirement was found.)

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

func sv(addr, state string) neo4jclient.ServerInfo {
	return neo4jclient.ServerInfo{ID: "id-" + addr, Name: addr, Address: addr, State: state}
}

func TestInitialRemovedServerIDs(t *testing.T) {
	a := func(n int) string { return "c-server-" + strconv.Itoa(n) }
	servers := []neo4jclient.ServerInfo{
		sv(a(0), "Enabled"), sv(a(1), "Enabled"), sv(a(2), "Enabled"),
		sv(a(3), "Enabled"), sv(a(4), "Dropped"),
	}
	// ordinals 3,4 are >= desired 3; ordinal 4 is Dropped (excluded).
	got := initialRemovedServerIDs(servers, "c", 3)
	assert.Equal(t, []string{"id-" + a(3)}, got)

	// No scale-down: nothing removable.
	assert.Empty(t, initialRemovedServerIDs(servers[:3], "c", 3))
}

func TestServerIdentifierPrefersID(t *testing.T) {
	assert.Equal(t, "uuid-x", serverIdentifier(neo4jclient.ServerInfo{ID: "uuid-x", Name: "", Address: "a:7687"}))
	assert.Equal(t, "name-x", serverIdentifier(neo4jclient.ServerInfo{Name: "name-x", Address: "a:7687"}))
	assert.Equal(t, "a:7687", serverIdentifier(neo4jclient.ServerInfo{Address: "a:7687"}))
}

func TestProvisionalDrainHoldNames(t *testing.T) {
	// 4 -> 2: removes ordinals 2,3. Seeds the replica hold before Neo4j is
	// reachable so pods aren't deleted ahead of the drain (#210 Bugbot high).
	assert.Equal(t, []string{"c-server-2", "c-server-3"}, provisionalDrainHoldNames("c", 4, 2))
	// No scale-down (current == desired) → nothing to hold.
	assert.Empty(t, provisionalDrainHoldNames("c", 3, 3))
	// Single server removed.
	assert.Equal(t, []string{"c-server-2"}, provisionalDrainHoldNames("c", 3, 2))
}

func TestPlanScaleDownStep(t *testing.T) {
	a := func(n int) string { return "c-server-" + strconv.Itoa(n) }

	t.Run("none when empty", func(t *testing.T) {
		assert.Equal(t, scaleDownNone, planScaleDownStep(nil).phase)
	})

	t.Run("cordon first when any enabled", func(t *testing.T) {
		step := planScaleDownStep([]neo4jclient.ServerInfo{sv(a(3), "Enabled"), sv(a(4), "Cordoned")})
		assert.Equal(t, scaleDownCordon, step.phase)
		assert.Equal(t, []string{"id-" + a(3)}, step.serverIDs)
	})

	t.Run("deallocate cordoned once all cordoned", func(t *testing.T) {
		step := planScaleDownStep([]neo4jclient.ServerInfo{sv(a(3), "Cordoned"), sv(a(4), "Cordoned")})
		assert.Equal(t, scaleDownDeallocate, step.phase)
		assert.ElementsMatch(t, []string{"id-" + a(3), "id-" + a(4)}, step.serverIDs)
	})

	t.Run("wait while deallocating and still hosting user dbs", func(t *testing.T) {
		// Still hosting a user db (neo4j) → not yet droppable.
		step := planScaleDownStep([]neo4jclient.ServerInfo{{ID: "id-x", State: "Deallocating", Hosting: []string{"neo4j", "system"}}})
		assert.Equal(t, scaleDownWaitDeallocating, step.phase)
	})

	t.Run("drop when deallocating hosts only system (never reaches Deallocated)", func(t *testing.T) {
		// The key fix: a system-primary server stays "Deallocating" forever, so
		// gate DROP on hosting only `system`, not on the Deallocated state.
		step := planScaleDownStep([]neo4jclient.ServerInfo{{ID: "id-x", State: "Deallocating", Hosting: []string{"system"}}})
		assert.Equal(t, scaleDownDrop, step.phase)
		assert.Equal(t, []string{"id-x"}, step.serverIDs)
	})

	t.Run("drop when deallocated (hosting empty)", func(t *testing.T) {
		step := planScaleDownStep([]neo4jclient.ServerInfo{{ID: "id-x", State: "Deallocated"}})
		assert.Equal(t, scaleDownDrop, step.phase)
		assert.Equal(t, []string{"id-x"}, step.serverIDs)
	})

	t.Run("cordon takes priority over later phases", func(t *testing.T) {
		step := planScaleDownStep([]neo4jclient.ServerInfo{sv(a(3), "Enabled"), {ID: "id-y", State: "Deallocated"}})
		assert.Equal(t, scaleDownCordon, step.phase)
	})
}
